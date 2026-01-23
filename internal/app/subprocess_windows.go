//go:build windows

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"office_find_item/internal/search"
)

type daemonProcess struct {
	root  string
	cmd   *exec.Cmd
	stdin io.WriteCloser

	mu     sync.Mutex
	closed bool
}

func (p *daemonProcess) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	stdin := p.stdin
	cmd := p.cmd
	p.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (p *daemonProcess) SetQuery(query string, query2 string, query3 string, queryID uint64, contextLen int, maxSnippets int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("daemon 已关闭")
	}
	if p.stdin == nil {
		return errors.New("daemon stdin 不可用")
	}
	cmd := daemonCmd{Cmd: "setQuery", Query: query, Query2: query2, Query3: query3, QueryID: queryID, ContextLen: contextLen, MaxSnippets: maxSnippets}
	b, _ := json.Marshal(cmd)
	b = append(b, '\n')
	_, err := p.stdin.Write(b)
	return err
}

type subprocGroup struct {
	mu   sync.Mutex
	cmds []*exec.Cmd
}

func (g *subprocGroup) killAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.cmds {
		if c != nil && c.Process != nil {
			_ = c.Process.Kill()
		}
	}
	g.cmds = nil
}

func startWorkerProcess(ctx context.Context, exePath string, root string, query string, workers int, onResult func(search.Result)) error {
	args := []string{"-worker", "-roots", root, "-q", query}
	if workers > 0 {
		args = append(args, "-workers", strconv.Itoa(workers))
	}
	cmd := exec.CommandContext(ctx, exePath, args...)
	debugConsole := os.Getenv("OFIND_DEBUG_CONSOLE") == "1"
	hide := !debugConsole
	flags := uint32(0)
	if debugConsole {
		// CREATE_NEW_CONSOLE (0x00000010)
		flags = 0x00000010
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: hide, CreationFlags: flags}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	// debug 时保留 stderr 到控制台/父进程；非 debug 合并到 stdout 避免阻塞且便于采集。
	if debugConsole {
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stderr = cmd.Stdout
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	go func() {
		s := bufio.NewScanner(stdout)
		// 提高默认 token 上限，避免 snippet 较长时 scan 失败
		buf := make([]byte, 0, 64*1024)
		s.Buffer(buf, 2*1024*1024)
		for s.Scan() {
			line := s.Bytes()
			var r search.Result
			if err := json.Unmarshal(line, &r); err == nil {
				onResult(r)
			}
		}
		_ = cmd.Wait()
	}()

	return nil
}

func startDaemonProcess(exePath string, root string, workers int, onOut func(daemonOut)) (*daemonProcess, error) {
	args := []string{"-daemon", "-roots", root}
	if workers > 0 {
		args = append(args, "-workers", strconv.Itoa(workers))
	}
	cmd := exec.Command(exePath, args...)
	debugConsole := os.Getenv("OFIND_DEBUG_CONSOLE") == "1"
	hide := !debugConsole
	flags := uint32(0)
	if debugConsole {
		// CREATE_NEW_CONSOLE (0x00000010)
		flags = 0x00000010
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: hide, CreationFlags: flags}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	// debug 时保留 stderr 到控制台/父进程；非 debug 合并到 stdout 避免阻塞且便于采集。
	if debugConsole {
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stderr = cmd.Stdout
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}

	p := &daemonProcess{root: root, cmd: cmd, stdin: stdin}

	go func() {
		s := bufio.NewScanner(stdout)
		buf := make([]byte, 0, 64*1024)
		s.Buffer(buf, 2*1024*1024)
		for s.Scan() {
			line := s.Bytes()
			var out daemonOut
			if err := json.Unmarshal(line, &out); err == nil {
				onOut(out)
			} else {
				// 尝试捕获非 JSON 输出（如 panic 或 log），转发给 UI 以便于排查问题
				txt := strings.TrimSpace(string(line))
				if txt != "" {
					onOut(daemonOut{Type: "status", Message: "Daemon Log: " + txt})
				}
			}
		}
		_ = cmd.Wait()
		p.Close()
	}()

	return p, nil
}

package app

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"office_find_item/internal/winutil"
)

type CLIOptions struct {
	Roots   string
	Query   string
	Query2  string
	Query3  string
	Workers int
	OpenIdx int
}

func RunCLI(opts CLIOptions) error {
	q1 := strings.TrimSpace(opts.Query)
	q2 := strings.TrimSpace(opts.Query2)
	q3 := strings.TrimSpace(opts.Query3)
	if q1 == "" && q2 == "" && q3 == "" {
		return errors.New("缺少查询参数：-q/-q2/-q3 至少一个")
	}

	roots := parseRoots(opts.Roots)
	if len(roots) == 0 {
		// 对齐 GUI：默认全盘
		roots = winutil.ListSearchableDrives()
	}

	for i := range roots {
		abs, err := filepath.Abs(roots[i])
		if err == nil {
			roots[i] = abs
		}
	}

	exePath, _ := os.Executable()

	fmt.Printf("Roots: %s\n", strings.Join(roots, "; "))
	if q1 != "" {
		fmt.Printf("Query: %s\n", q1)
	}
	if q2 != "" {
		fmt.Printf("Query2: %s\n", q2)
	}
	if q3 != "" {
		fmt.Printf("Query3: %s\n", q3)
	}
	fmt.Println("Searching (daemon-backed, cached)...")

	outCh := make(chan daemonOut, 1024)
	procMu := sync.Mutex{}
	procs := make([]*daemonProcess, 0, len(roots))
	defer func() {
		procMu.Lock()
		for _, p := range procs {
			p.Close()
		}
		procMu.Unlock()
	}()

	doneNeed := 0
	for _, root := range roots {
		dproc, err := startDaemonProcess(exePath, root, opts.Workers, func(out daemonOut) {
			select {
			case outCh <- out:
			default:
				// drop if overwhelmed
			}
		})
		if err != nil {
			continue
		}
		procMu.Lock()
		procs = append(procs, dproc)
		procMu.Unlock()
		doneNeed++
	}
	if doneNeed == 0 {
		return errors.New("无法启动 daemon 子进程（roots 为空或启动失败）")
	}

	queryID := uint64(1)
	procMu.Lock()
	for _, p := range procs {
		_ = p.SetQuery(q1, q2, q3, queryID, 30, 3)
	}
	procMu.Unlock()

	// 收集 & 去重
	path2snips := map[string][]string{}
	ordered := make([]string, 0, 256)
	doneGot := 0

	for doneGot < doneNeed {
		out := <-outCh
		if out.QueryID != queryID {
			continue
		}
		switch out.Type {
		case "result":
			if _, ok := path2snips[out.Path]; !ok {
				ordered = append(ordered, out.Path)
			}
			path2snips[out.Path] = out.Snippets
		case "done":
			doneGot++
		}
	}

	if len(ordered) == 0 {
		fmt.Println("No matches.")
		return nil
	}

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	for i, p := range ordered {
		snips := strings.Join(path2snips[p], "  |  ")
		fmt.Fprintf(w, "%d\t%s\t%s\n", i+1, p, snips)
	}

	if opts.OpenIdx > 0 {
		idx := opts.OpenIdx - 1
		if idx < 0 || idx >= len(ordered) {
			return fmt.Errorf("-open 超出范围：1..%d", len(ordered))
		}
		return winutil.RevealInExplorer(ordered[idx])
	}

	return nil
}

func parseRoots(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

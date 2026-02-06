//go:build windows

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"office_find_item/internal/extract"
	"office_find_item/internal/winutil"
)

type daemonCmd struct {
	Cmd         string `json:"cmd"`
	Query       string `json:"query"`
	Query2      string `json:"query2,omitempty"`
	Query3      string `json:"query3,omitempty"`
	QueryID     uint64 `json:"queryId"`
	ContextLen  int    `json:"contextLen"`
	MaxSnippets int    `json:"maxSnippets"`
}

type daemonOut struct {
	Type      string   `json:"type"`
	QueryID   uint64   `json:"queryId"`
	Path      string   `json:"path,omitempty"`
	Snippets  []string `json:"snippets,omitempty"`
	Message   string   `json:"message,omitempty"`
	Extension string   `json:"extension,omitempty"`
	Size      int64    `json:"size,omitempty"`
	ModTime   int64    `json:"modTime,omitempty"`
}

var daemonSupportedExt = map[string]struct{}{
	".txt":  {},
	".md":   {},
	".log":  {},
	".csv":  {},
	".json": {},
	".xml":  {},
	".ini":  {},
	".yaml": {},
	".yml":  {},
	".doc":  {},
	".docx": {},
	".xls":  {},
	".xlsx": {},
	".ppt":  {},
	".pptx": {},
	".pdf":  {},
	".vsdx": {},
}

func RunDaemon(opts CLIOptions) error {
	roots := parseRoots(opts.Roots)
	if len(roots) == 0 {
		return errors.New("roots 为空")
	}
	root := strings.TrimSpace(roots[0])
	if root == "" {
		return errors.New("root 为空")
	}

	in := bufio.NewReader(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	outMu := sync.Mutex{}
	emit := func(out daemonOut) {
		outMu.Lock()
		_ = enc.Encode(out)
		outMu.Unlock()
	}

	var (
		searchMu sync.Mutex
		cancel   context.CancelFunc
	)

	debugEnabled := os.Getenv("OFIND_DEBUG_CONSOLE") == "1" || os.Getenv("OFIND_DEBUG") == "1"
	type currentWork struct {
		Path  string
		Start time.Time
	}
	var cur atomic.Value
	cur.Store(currentWork{})
	var processed uint64

	startQueryMonitor := func(ctx context.Context, cmd daemonCmd, cancel context.CancelFunc) {
		go func() {
			maxAlloc := maxAllocBytes()
			// 如果用户明确设置OFIND_MAX_ALLOC_MB=0，则不启动内存监控
			if maxAlloc == 0 {
				return
			}
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			var lastIO winutil.ProcessIOCounters
			var lastAt time.Time
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}

				var m runtime.MemStats
				runtime.ReadMemStats(&m)

				// 内存硬限制：始终生效，无论是否启用调试模式
				if m.Alloc > maxAlloc {
					log.Printf("[HARD-LIMIT] PID=%d | QueryID=%d | Alloc=%.2f MiB | Limit=%.2f MiB | Action=cancel",
						os.Getpid(), cmd.QueryID,
						float64(m.Alloc)/1024/1024, float64(maxAlloc)/1024/1024)
					debug.FreeOSMemory()
					if cancel != nil {
						cancel()
					}
					return
				}

				// 仅在调试模式下输出详细监控信息
				if debugEnabled {
					ioStat, _ := winutil.GetProcessIOCounters()
					now := time.Now()
					dt := now.Sub(lastAt).Seconds()
					if lastAt.IsZero() || dt <= 0 {
						dt = 0
					}
					dRead := uint64(0)
					dWrite := uint64(0)
					if ioStat.ReadBytes >= lastIO.ReadBytes {
						dRead = ioStat.ReadBytes - lastIO.ReadBytes
					}
					if ioStat.WriteBytes >= lastIO.WriteBytes {
						dWrite = ioStat.WriteBytes - lastIO.WriteBytes
					}
					readRate := 0.0
					writeRate := 0.0
					if dt > 0 {
						readRate = float64(dRead) / 1024 / 1024 / dt
						writeRate = float64(dWrite) / 1024 / 1024 / dt
					}
					lastIO = ioStat
					lastAt = now

					cw, _ := cur.Load().(currentWork)
					curFor := time.Duration(0)
					if cw.Path != "" && !cw.Start.IsZero() {
						curFor = time.Since(cw.Start)
					}

					// 注意：这里是 debug 日志，用于定位卡顿/内存暴涨。路径可能较长，但更利于定位具体文件。
					// processed 为近似计数（job 被取出即算一次）。
					// Use log.Printf (same output as ofind_debug.log in debug mode).
					// Example:
					// [QMON] QueryID=1 | Root=E:\Docs | Processed=123 | Alloc=... | IO(R/W)=... | IO(R/W)=.../s | Cur=... | CurFor=...
					// Keep format stable-ish for grep.
					log.Printf("[QMON] PID=%d | QueryID=%d | Root=%s | Processed=%d | Goroutines=%d | Alloc=%.2f MiB | Sys=%.2f MiB | NumGC=%d | IO(R/W)=%.2f/%.2f MiB | IO(R/W)=%.2f/%.2f MiB/s | CurFor=%s | Cur=%s",
						os.Getpid(), cmd.QueryID, root, atomic.LoadUint64(&processed), runtime.NumGoroutine(),
						float64(m.Alloc)/1024/1024, float64(m.Sys)/1024/1024, m.NumGC,
						float64(ioStat.ReadBytes)/1024/1024, float64(ioStat.WriteBytes)/1024/1024,
						readRate, writeRate,
						curFor.Truncate(10*time.Millisecond).String(),
						cw.Path)
				}
			}
		}()
	}

	startSearch := func(cmd daemonCmd) {
		termsRaw := []string{strings.TrimSpace(cmd.Query), strings.TrimSpace(cmd.Query2), strings.TrimSpace(cmd.Query3)}
		terms := make([]string, 0, 3)
		for _, t := range termsRaw {
			if t == "" {
				continue
			}
			terms = append(terms, t)
		}

		searchMu.Lock()
		if cancel != nil {
			cancel()
			cancel = nil
		}
		ctx, cxl := context.WithCancel(context.Background())
		cancel = cxl
		searchMu.Unlock()

		if len(terms) == 0 {
			emit(daemonOut{Type: "status", QueryID: cmd.QueryID, Message: "idle"})
			return
		}

		atomic.StoreUint64(&processed, 0)
		cur.Store(currentWork{})
		startQueryMonitor(ctx, cmd, cxl)

		workers := opts.Workers
		if workers <= 0 {
			workers = runtime.NumCPU()
			if workers <= 0 {
				workers = 4
			}
			if runtime.GOARCH == "386" && workers > 2 {
				workers = 2
			}
			if workers > 4 {
				workers = 4
			}
		}
		contextLen := cmd.ContextLen
		maxSnips := cmd.MaxSnippets
		if maxSnips <= 0 {
			maxSnips = 3
		}
		// 多关键词时，整体最多展示 maxSnips*len(terms)，并做上限保护。
		maxTotal := maxSnips
		if len(terms) > 1 {
			maxTotal = maxSnips * len(terms)
			if maxTotal > 12 {
				maxTotal = 12
			}
		}

		jobs := make(chan string, workers*4)
		wg := sync.WaitGroup{}
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func() {
				defer wg.Done()
				for p := range jobs {
					if ctx.Err() != nil {
						return
					}
					atomic.AddUint64(&processed, 1)
					startAt := time.Now()
					cur.Store(currentWork{Path: p, Start: startAt})

					fileName := filepath.Base(p)
					fileNameLower := strings.ToLower(fileName)
					ext := strings.ToLower(filepath.Ext(p))

					// 先用文件名做快速匹配：若某个词在文件名中命中，则该词无需再提取全文。
					matchedInName := make([]bool, len(terms))
					for i, t := range terms {
						if t == "" {
							continue
						}
						if strings.Contains(fileName, t) || strings.Contains(fileNameLower, strings.ToLower(t)) {
							matchedInName[i] = true
						}
					}

					// 统一流式处理（不再预提取全文）
					allMatch := true
					snipsOut := make([]string, 0, maxTotal)
					for i, t := range terms {
						if matchedInName[i] {
							nameSnips := extract.FindSnippets(fileName, t, contextLen, maxSnips)
							for _, s := range nameSnips {
								if len(snipsOut) >= maxTotal {
									break
								}
								snipsOut = append(snipsOut, "文件名: "+s)
							}
							continue
						}

						// 需要从内容搜索
						snips, err := extract.FileFindSnippets(ctx, p, t, contextLen, maxSnips)
						if err != nil {
							if debugEnabled {
								log.Printf("[ERROR] FileFindSnippets failed for %s: %v", p, err)
							}
							allMatch = false
							break
						}
						if len(snips) == 0 {
							allMatch = false
							break
						}
						// 只要命中，就加入 snippets（如果不超过配额）
						for _, s := range snips {
							if len(snipsOut) >= maxTotal {
								break
							}
							snipsOut = append(snipsOut, s)
						}
					}

					if !allMatch || len(snipsOut) == 0 {
						if debugEnabled {
							elapsed := time.Since(startAt)
							if elapsed >= 1200*time.Millisecond {
								log.Printf("[SLOW-MISS] PID=%d | QID=%d | Elapsed=%s | Ext=%s | Path=%s",
									os.Getpid(), cmd.QueryID, elapsed.Truncate(10*time.Millisecond), ext, p)
							}
						}
						continue
					}

					var (
						size    int64
						modTime int64
					)
					if st, err := os.Stat(p); err == nil {
						size = st.Size()
						modTime = st.ModTime().Unix()
					}
					emit(daemonOut{
						Type:      "result",
						QueryID:   cmd.QueryID,
						Path:      p,
						Snippets:  snipsOut,
						Extension: ext,
						Size:      size,
						ModTime:   modTime,
					})

					if debugEnabled {
						elapsed := time.Since(startAt)
						if elapsed >= 800*time.Millisecond {
							log.Printf("[SLOW-HIT] PID=%d | QID=%d | Elapsed=%s | Size=%d | Path=%s",
								os.Getpid(), cmd.QueryID, elapsed.Truncate(10*time.Millisecond), size, p)
						}
					}
				}
			}()
		}

		// 启动流式遍历：边遍历边搜索，解决卡顿和内存占用问题。
		go func() {
			defer close(jobs)
			_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if ctx.Err() != nil {
					return filepath.SkipAll
				}
				if err != nil {
					return nil
				}
				if d.IsDir() {
					return nil
				}
				ext := strings.ToLower(filepath.Ext(d.Name()))
				if _, ok := daemonSupportedExt[ext]; !ok {
					return nil
				}
				select {
				case jobs <- path:
				case <-ctx.Done():
					return filepath.SkipAll
				}
				return nil
			})
		}()

		go func() {
			wg.Wait()
			cur.Store(currentWork{})
			emit(daemonOut{Type: "done", QueryID: cmd.QueryID})
		}()
	}

	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var cmd daemonCmd
		if err := json.Unmarshal(bytesTrimSpace(line), &cmd); err != nil {
			continue
		}
		switch cmd.Cmd {
		case "setQuery":
			startSearch(cmd)
		}
	}
}

func bytesTrimSpace(b []byte) []byte {
	// avoid importing bytes everywhere
	i := 0
	for i < len(b) {
		c := b[i]
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			break
		}
		i++
	}
	j := len(b)
	for j > i {
		c := b[j-1]
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			break
		}
		j--
	}
	return b[i:j]
}

func maxAllocBytes() uint64 {
	if v := strings.TrimSpace(os.Getenv("OFIND_MAX_ALLOC_MB")); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			if n == 0 {
				return 0 // 用户明确禁用内存限制
			}
			// 添加合理上限（16384 MiB = 16GB），避免设置过大值导致问题
			const maxLimit = 16384
			if n > maxLimit {
				n = maxLimit
			}
			return n * 1024 * 1024
		}
		// 解析失败时使用默认值
	}
	if runtime.GOARCH == "386" {
		return 1200 * 1024 * 1024
	}
	return 4096 * 1024 * 1024
}

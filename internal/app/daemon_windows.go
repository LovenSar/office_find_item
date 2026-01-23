//go:build windows

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"office_find_item/internal/cache"
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
	Type     string   `json:"type"`
	QueryID  uint64   `json:"queryId"`
	Path     string   `json:"path,omitempty"`
	Snippets []string `json:"snippets,omitempty"`
	Message  string   `json:"message,omitempty"`
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

	cacheRoot, err := winutil.BestCacheDir()
	if err != nil {
		return err
	}
	c := &cache.Cache{Root: filepath.Join(cacheRoot, "text"), MaxTextBytes: 2 * 1024 * 1024}

	filesMu := sync.Mutex{}
	files := make([]string, 0, 1024)
	indexDone := make(chan struct{})

	go func() {
		defer close(indexDone)
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
			filesMu.Lock()
			files = append(files, path)
			filesMu.Unlock()
			return nil
		})
	}()

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
		cancel  context.CancelFunc
	)

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

		workers := opts.Workers
		if workers <= 0 {
			workers = runtime.NumCPU()
			if workers <= 0 {
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

					fileName := filepath.Base(p)
					fileNameLower := strings.ToLower(fileName)

					// 先用文件名做快速匹配：若某个词在文件名中命中，则该词无需再提取全文。
					matchedInName := make([]bool, len(terms))
					needText := false
					for i, t := range terms {
						if t == "" {
							continue
						}
						if strings.Contains(fileName, t) || strings.Contains(fileNameLower, strings.ToLower(t)) {
							matchedInName[i] = true
						} else {
							needText = true
						}
					}

					var text string
					if needText {
						t, err := c.GetOrExtract(ctx, p, func(ctx context.Context, path string) (string, error) {
							return extract.FileExtractText(ctx, path, c.MaxTextBytes)
						})
						if err != nil {
							continue
						}
						text = t
					}

					// 交集：每个词都必须命中（文件名命中或正文命中均可）。
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
						if text == "" {
							allMatch = false
							break
						}
						textSnips := extract.FindSnippets(text, t, contextLen, maxSnips)
						if len(textSnips) == 0 {
							allMatch = false
							break
						}
						for _, s := range textSnips {
							if len(snipsOut) >= maxTotal {
								break
							}
							snipsOut = append(snipsOut, s)
						}
					}
					if !allMatch || len(snipsOut) == 0 {
						continue
					}
					emit(daemonOut{Type: "result", QueryID: cmd.QueryID, Path: p, Snippets: snipsOut})
				}
			}()
		}

		go func() {
			defer close(jobs)
			i := 0
			for {
				if ctx.Err() != nil {
					return
				}
				var path string
				filesMu.Lock()
				if i < len(files) {
					path = files[i]
					i++
				}
				filesMu.Unlock()
				if path != "" {
					jobs <- path
					continue
				}
				select {
				case <-indexDone:
					return
				case <-time.After(60 * time.Millisecond):
				}
			}
		}()

		go func() {
			wg.Wait()
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

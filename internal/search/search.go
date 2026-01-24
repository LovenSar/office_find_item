package search

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"office_find_item/internal/extract"
)

type Config struct {
	Roots   []string
	Query   string
	Workers int
	// ContextLen 表示命中后输出的上下文字符数（左右各多少 rune）
	ContextLen int
}

func (c Config) WorkerCount() int {
	if c.Workers > 0 {
		return c.Workers
	}
	if n := runtime.NumCPU(); n > 0 {
		return n
	}
	return 4
}

type Result struct {
	Path      string
	Extension string
	Size      int64
	ModTime   int64
	// Snippet 为命中上下文（已包含对 query 的“标记高亮”）
	Snippet string
}

type Progress struct {
	FilesScanned uint64
	Matches      uint64
}

type ProgressFn func(Progress)

type ResultFn func(Result)

var supportedExt = map[string]struct{}{
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

func Find(cfg Config, onProgress ProgressFn) ([]Result, error) {
	ch, _, err := FindAsync(cfg, onProgress)
	if err != nil {
		return nil, err
	}
	return <-ch, nil
}

func FindAsync(cfg Config, onProgress ProgressFn) (<-chan []Result, func(), error) {
	q := strings.TrimSpace(cfg.Query)
	if q == "" {
		return nil, nil, errors.New("query 为空")
	}
	if len(cfg.Roots) == 0 {
		return nil, nil, errors.New("roots 为空")
	}

	ctx, cancel := context.WithCancel(context.Background())

	out := make(chan []Result, 1)

	go func() {
		defer close(out)
		results := findWithContext(ctx, cfg, onProgress)
		out <- results
	}()

	return out, cancel, nil
}

// Search 执行搜索并在找到命中时回调 onResult；适合 UI/worker 流式输出。
// 该函数在所有扫描结束后返回。
func Search(cfg Config, onProgress ProgressFn, onResult ResultFn) error {
	q := strings.TrimSpace(cfg.Query)
	if q == "" {
		return errors.New("query 为空")
	}
	if len(cfg.Roots) == 0 {
		return errors.New("roots 为空")
	}
	ctx := context.Background()
	searchWithContext(ctx, cfg, onProgress, onResult)
	return nil
}

func findWithContext(ctx context.Context, cfg Config, onProgress ProgressFn) []Result {
	results := make([]Result, 0, 256)
	mu := sync.Mutex{}
	searchWithContext(ctx, cfg, onProgress, func(r Result) {
		mu.Lock()
		results = append(results, r)
		mu.Unlock()
	})
	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	return results
}

func searchWithContext(ctx context.Context, cfg Config, onProgress ProgressFn, onResult ResultFn) {
	workers := cfg.WorkerCount()

	jobs := make(chan string, workers*4)

	var scanned uint64
	var matches uint64

	resCh := make(chan Result, workers*2)

	wg := sync.WaitGroup{}
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for path := range jobs {
				if ctx.Err() != nil {
					return
				}
				atomic.AddUint64(&scanned, 1)
				if onProgress != nil {
					onProgress(Progress{FilesScanned: atomic.LoadUint64(&scanned), Matches: atomic.LoadUint64(&matches)})
				}

				found, snippet, _ := extract.FileFindFirst(ctx, path, cfg.Query, cfg.ContextLen)
				if found {
					atomic.AddUint64(&matches, 1)
					var (
						size    int64
						modTime int64
					)
					if st, err := os.Stat(path); err == nil {
						size = st.Size()
						modTime = st.ModTime().Unix()
					}
					select {
					case resCh <- Result{
						Path:      path,
						Snippet:   snippet,
						Extension: strings.ToLower(filepath.Ext(path)),
						Size:      size,
						ModTime:   modTime,
					}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	walkDone := make(chan struct{})
	go func() {
		defer close(walkDone)
		for _, root := range cfg.Roots {
			root = strings.TrimSpace(root)
			if root == "" {
				continue
			}
			_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if ctx.Err() != nil {
					return context.Canceled
				}
				if d.IsDir() {
					return nil
				}
				ext := strings.ToLower(filepath.Ext(d.Name()))
				if _, ok := supportedExt[ext]; !ok {
					return nil
				}

				select {
				case jobs <- path:
				case <-ctx.Done():
					return context.Canceled
				}
				return nil
			})
			if ctx.Err() != nil {
				return
			}
		}
	}()

	go func() {
		<-walkDone
		close(jobs)
		wg.Wait()
		close(resCh)
	}()

	for r := range resCh {
		if onResult != nil {
			onResult(r)
		}
	}
}

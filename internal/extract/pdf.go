package extract

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ledongthuc/pdf"
)

var errTooManyPages = errors.New("PDF 页数超过上限")

var (
	pdfHasIFilter     bool
	pdfHasIFilterOnce sync.Once

	// PDF并发控制
	pdfConcurrentLimit     int
	pdfConcurrentLimitOnce sync.Once
	pdfSemaphore           chan struct{}
	pdfSemaphoreOnce       sync.Once
	activePDFTasks         int32
	pdfMemoryLimitBytes    int64
	pdfMemoryLimitOnce     sync.Once
)

func pdfPageWorkers() int {
	// 并行解析 PDF 页的 worker 数（仅影响纯 Go PDF fallback）。
	// 默认关闭（=1），避免在某些 PDF 上导致 CPU/内存暴涨；可通过环境变量显式开启。
	const def = 1
	v := strings.TrimSpace(os.Getenv("OFIND_PDF_PAGE_WORKERS"))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func pdfMaxFileBytes() int64 {
	// 纯 Go PDF 解析在大文件上可能产生巨量内存/CPU；这里给出更保守默认值。
	// Windows 下优先使用 IFilter（若可用），仅在 fallback 时才应用该限制。
	// 若需要放宽，可通过环境变量显式设置。
	const def = 20 * 1024 * 1024
	v := strings.TrimSpace(os.Getenv("OFIND_PDF_MAX_FILE_BYTES"))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func pdfMaxPages() int {
	// PDF 页数限制，避免处理超大 PDF 时内存暴涨。
	// 默认限制为 100 页，可通过环境变量调整。
	const def = 100
	v := strings.TrimSpace(os.Getenv("OFIND_PDF_MAX_PAGES"))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func checkPdfPages(r *pdf.Reader) error {
	// 检查PDF页数是否超过限制
	if r.NumPage() > pdfMaxPages() {
		return errTooManyPages
	}
	return nil
}

// pdfConcurrentLimitValue 返回同时处理的PDF文件数量上限
func pdfConcurrentLimitValue() int {
	pdfConcurrentLimitOnce.Do(func() {
		const def = 2 // 默认同时处理2个PDF文件
		v := strings.TrimSpace(os.Getenv("OFIND_PDF_CONCURRENT_LIMIT"))
		if v == "" {
			pdfConcurrentLimit = def
			return
		}
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 10 {
				n = 10
			}
			pdfConcurrentLimit = n
		} else {
			pdfConcurrentLimit = def
		}
	})
	return pdfConcurrentLimit
}

// pdfMemoryLimitValue 返回PDF处理的内存上限（字节）
func pdfMemoryLimitValue() int64 {
	pdfMemoryLimitOnce.Do(func() {
		const def = int64(2 * 1024 * 1024 * 1024) // 默认2GB
		v := strings.TrimSpace(os.Getenv("OFIND_PDF_MEMORY_LIMIT_MB"))
		if v == "" {
			pdfMemoryLimitBytes = def
			return
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			if n > 16384 { // 最大16GB
				n = 16384
			}
			pdfMemoryLimitBytes = n * 1024 * 1024
		} else {
			pdfMemoryLimitBytes = def
		}
	})
	return pdfMemoryLimitBytes
}

// isMemoryHigh 检查内存使用是否超过阈值
func isMemoryHigh() bool {
	limit := pdfMemoryLimitValue()
	if limit <= 0 {
		return false
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc > uint64(limit)
}

// getPDFSemaphore 返回全局PDF处理信号量
func getPDFSemaphore() chan struct{} {
	pdfSemaphoreOnce.Do(func() {
		limit := pdfConcurrentLimitValue()
		pdfSemaphore = make(chan struct{}, limit)
		// 预填充信号量
		for i := 0; i < limit; i++ {
			pdfSemaphore <- struct{}{}
		}
	})
	return pdfSemaphore
}

// acquirePDFSlot 获取PDF处理槽位
func acquirePDFSlot(ctx context.Context) error {
	// 检查内存使用
	if isMemoryHigh() {
		// 内存使用高，等待一段时间或返回错误
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// 短暂等待后重试
			if isMemoryHigh() {
				return errors.New("内存使用过高，暂停PDF处理")
			}
		}
	}

	sem := getPDFSemaphore()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-sem:
		atomic.AddInt32(&activePDFTasks, 1)
		return nil
	}
}

// releasePDFSlot 释放PDF处理槽位
func releasePDFSlot() {
	atomic.AddInt32(&activePDFTasks, -1)
	getPDFSemaphore() <- struct{}{}
}

// pdfOpenWithLimit 带并发限制的PDF打开函数
func pdfOpenWithLimit(ctx context.Context, path string) (*os.File, *pdf.Reader, error) {
	if err := acquirePDFSlot(ctx); err != nil {
		return nil, nil, err
	}

	f, r, err := pdfOpen(path)
	if err != nil {
		releasePDFSlot()
		return nil, nil, err
	}

	// 返回原始文件，但调用者需要在defer中调用releasePDFSlotOnClose
	// 注意：调用者必须确保在文件关闭后调用releasePDFSlot()
	// 我们在这里不自动处理，因为defer的顺序很重要
	return f, r, nil
}

// releasePDFSlotOnClose 返回一个函数，在调用时释放PDF槽位
// 应该在defer中使用：defer releasePDFSlotOnClose()()
func releasePDFSlotOnClose() func() {
	return func() {
		releasePDFSlot()
	}
}

func pdfPureGoFallbackEnabled() bool {
	// 默认策略：
	// - Windows：PDF 依赖系统 IFilter（README 说明）。纯 Go fallback 在部分 PDF 上可能导致严重内存/CPU 暴涨，
	//   因此默认关闭，可通过环境变量显式开启。
	// - 非 Windows：没有 IFilter，保持原有纯 Go 行为（默认开启）。
	if runtime.GOOS != "windows" {
		return true
	}
	v := strings.TrimSpace(os.Getenv("OFIND_PDF_PUREGO"))
	if v != "" {
		// 用户明确设置了环境变量，尊重用户选择
		switch strings.ToLower(v) {
		case "1", "true", "yes", "y", "on":
			return true
		default:
			return false
		}
	}

	// 用户未设置环境变量，自动检测系统是否有可用的PDF IFilter
	pdfHasIFilterOnce.Do(func() {
		pdfHasIFilter = HasPDFIFilter()
	})

	// 如果有PDF IFilter，禁用纯Go回退（优先使用更稳定的IFilter）
	// 如果没有PDF IFilter，启用纯Go回退（否则PDF文件无法处理）
	return !pdfHasIFilter
}

func pdfOpen(path string) (*os.File, *pdf.Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	r, err := pdf.NewReader(f, fi.Size())
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, r, nil
}

func pdfFindFirst(ctx context.Context, path string, query string, contextLen int) (bool, string, error) {
	q := stringsTrimSpace(query)
	if q == "" {
		return false, "", errors.New("query 为空")
	}

	// Windows 优先 IFilter（更节省内存，且支持真正的流式 chunk）。
	if runtime.GOOS == "windows" {
		found, snip, err := ifilterFindFirst(ctx, path, q, contextLen)
		if err == nil {
			return found, snip, nil
		}
		// 默认不做纯 Go fallback（见 README：PDF 依赖系统 IFilter）。
		if !pdfPureGoFallbackEnabled() {
			return false, "", err
		}
	}

	// 注意：pdf.Open 可能比较耗时，应关注 ctx 是否已取消
	if ctx.Err() != nil {
		return false, "", ctx.Err()
	}

	// 纯 Go fallback：对大文件做上限保护，避免极端内存暴涨。
	if st, err := os.Stat(path); err == nil {
		if st.Size() > pdfMaxFileBytes() {
			return false, "", errTooLarge
		}
	}

	f, r, err := pdfOpenWithLimit(ctx, path)
	if err != nil {
		return false, "", err
	}
	defer f.Close()
	defer releasePDFSlotOnClose()()

	if ctx.Err() != nil {
		return false, "", ctx.Err()
	}

	if err := checkPdfPages(r); err != nil {
		return false, "", err
	}
	pages := r.NumPage()
	fonts := make(map[string]*pdf.Font)
	nextPage := 1
	next := func(ctx context.Context) (string, error) {
		if nextPage > pages {
			return "", io.EOF
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		p := r.Page(nextPage)
		nextPage++
		for _, name := range p.Fonts() {
			if _, ok := fonts[name]; ok {
				continue
			}
			f := p.Font(name)
			fonts[name] = &f
		}
		return p.GetPlainText(fonts)
	}
	return streamFindFirst(ctx, next, q, contextLen)
}

// pdfFindSnippetsStream collects up to maxSnippets snippets without extracting the full text.
func pdfFindSnippetsStream(ctx context.Context, path string, query string, contextLen int, maxSnippets int) ([]string, error) {
	q := stringsTrimSpace(query)
	if q == "" {
		return nil, errors.New("query 为空")
	}
	if maxSnippets <= 0 {
		maxSnippets = 1
	}

	// Windows 优先 IFilter：更节省内存，且流式返回 chunk。
	if runtime.GOOS == "windows" {
		snips, err := ifilterFindSnippets(ctx, path, q, contextLen, maxSnippets)
		if err == nil {
			return snips, nil
		}
		if !pdfPureGoFallbackEnabled() {
			return nil, err
		}
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if st, err := os.Stat(path); err == nil {
		if st.Size() > pdfMaxFileBytes() {
			return nil, errTooLarge
		}
	}

	f, r, err := pdfOpenWithLimit(ctx, path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	defer releasePDFSlotOnClose()()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := checkPdfPages(r); err != nil {
		return nil, err
	}
	pages := r.NumPage()
	fonts := make(map[string]*pdf.Font)
	nextPage := 1
	next := func(ctx context.Context) (string, error) {
		if nextPage > pages {
			return "", io.EOF
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		p := r.Page(nextPage)
		nextPage++
		for _, name := range p.Fonts() {
			if _, ok := fonts[name]; ok {
				continue
			}
			f := p.Font(name)
			fonts[name] = &f
		}
		return p.GetPlainText(fonts)
	}
	return streamFindSnippets(ctx, next, q, contextLen, maxSnippets)
}

// PDFFindSnippetsStream is an exported wrapper for streaming PDF snippet search.
func PDFFindSnippetsStream(ctx context.Context, path string, query string, contextLen int, maxSnippets int) ([]string, error) {
	return pdfFindSnippetsStream(ctx, path, query, contextLen, maxSnippets)
}

func pdfExtractText(ctx context.Context, path string, maxBytes int64) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	maxBytes = maxBytesOrDefault(maxBytes)

	// Windows 优先 IFilter：避免纯 Go PDF 解析导致的内存暴涨。
	if runtime.GOOS == "windows" {
		if text, err := ifilterExtractText(ctx, path, maxBytes); err == nil {
			return text, nil
		}
		// 默认不做纯 Go fallback（见 README：PDF 依赖系统 IFilter）。
		if !pdfPureGoFallbackEnabled() {
			return "", errors.New("PDF 提取需要系统 IFilter（请安装对应组件或设置 OFIND_PDF_PUREGO=1 开启纯 Go fallback）")
		}
	}

	// 纯 Go fallback：对大文件做上限保护，避免极端内存暴涨。
	if st, err := os.Stat(path); err == nil {
		if st.Size() > pdfMaxFileBytes() {
			return "", errTooLarge
		}
	}

	f, r, err := pdfOpenWithLimit(ctx, path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	defer releasePDFSlotOnClose()()

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	workers := pdfPageWorkers()
	if workers <= 1 {
		return pdfExtractTextSequential(ctx, r, maxBytes)
	}
	return pdfExtractTextParallel(ctx, r, maxBytes, workers)
}

func pdfExtractTextSequential(ctx context.Context, r *pdf.Reader, maxBytes int64) (string, error) {
	var sb strings.Builder
	var approx int64

	if err := checkPdfPages(r); err != nil {
		return "", err
	}
	pages := r.NumPage()
	fonts := make(map[string]*pdf.Font)
	for i := 1; i <= pages; i++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		remaining := maxBytes - approx
		if remaining <= 0 {
			return sb.String(), nil
		}

		p := r.Page(i)
		for _, name := range p.Fonts() {
			if _, ok := fonts[name]; ok {
				continue
			}
			f := p.Font(name)
			fonts[name] = &f
		}
		text, err := p.GetPlainText(fonts)
		if err != nil {
			return "", err
		}
		if text == "" {
			continue
		}
		if int64(len(text)) > remaining {
			text = text[:remaining]
		}
		sb.WriteString(text)
		approx += int64(len(text))
		if approx >= maxBytes {
			return sb.String(), nil
		}
	}
	return sb.String(), nil
}

func pdfExtractTextParallel(ctx context.Context, r *pdf.Reader, maxBytes int64, workers int) (string, error) {
	type pageResult struct {
		page int
		text string
		err  error
	}

	if err := checkPdfPages(r); err != nil {
		return "", err
	}
	pages := r.NumPage()
	if pages <= 1 {
		return pdfExtractTextSequential(ctx, r, maxBytes)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int, workers*2)
	results := make(chan pageResult, workers*2)

	// feed jobs
	go func() {
		defer close(jobs)
		for i := 1; i <= pages; i++ {
			select {
			case jobs <- i:
			case <-ctx.Done():
				return
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			// Per-worker font cache; avoids cross-goroutine map races.
			fonts := make(map[string]*pdf.Font)
			for pageNum := range jobs {
				if ctx.Err() != nil {
					return
				}
				p := r.Page(pageNum)
				for _, name := range p.Fonts() {
					if _, ok := fonts[name]; ok {
						continue
					}
					f := p.Font(name)
					fonts[name] = &f
				}
				text, err := p.GetPlainText(fonts)
				select {
				case results <- pageResult{page: pageNum, text: text, err: err}:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var sb strings.Builder
	var approx int64
	nextPage := 1
	pending := make(map[int]string, workers*2)

	for res := range results {
		if res.err != nil {
			cancel()
			return "", res.err
		}
		pending[res.page] = res.text

		for {
			text, ok := pending[nextPage]
			if !ok {
				break
			}
			delete(pending, nextPage)
			nextPage++

			if ctx.Err() != nil {
				return "", ctx.Err()
			}

			remaining := maxBytes - approx
			if remaining <= 0 {
				cancel()
				return sb.String(), nil
			}
			if text != "" {
				if int64(len(text)) > remaining {
					text = text[:remaining]
				}
				sb.WriteString(text)
				approx += int64(len(text))
				if approx >= maxBytes {
					cancel()
					return sb.String(), nil
				}
			}
			if nextPage > pages {
				return sb.String(), nil
			}
		}
	}

	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return sb.String(), nil
}

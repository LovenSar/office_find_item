package extract

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

func pdfMaxFileBytes() int64 {
	// 纯 Go PDF 解析在大文件上可能产生巨量内存/CPU；这里给出保守默认值。
	// Windows 下优先使用 IFilter（若可用），仅在 fallback 时才应用该限制。
	const def = 50 * 1024 * 1024
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

func pdfPureGoFallbackEnabled() bool {
	// 默认策略：
	// - Windows：PDF 依赖系统 IFilter（README 说明）。纯 Go fallback 在部分 PDF 上可能导致严重内存/CPU 暴涨，
	//   因此默认关闭，可通过环境变量显式开启。
	// - 非 Windows：没有 IFilter，保持原有纯 Go 行为（默认开启）。
	if runtime.GOOS != "windows" {
		return true
	}
	v := strings.TrimSpace(os.Getenv("OFIND_PDF_PUREGO"))
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
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

	f, r, err := pdfOpen(path)
	if err != nil {
		return false, "", err
	}
	defer f.Close()

	if ctx.Err() != nil {
		return false, "", ctx.Err()
	}

	reader, err := r.GetPlainText()
	if err != nil {
		return false, "", err
	}
	// 使用流式查找，避免一次性读取整个PDF文本
	return pdfFindFirstStream(ctx, reader, q, contextLen)
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

	f, r, err := pdfOpen(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// 只读取 maxBytes，避免 pdf.GetPlainText 的输出过大造成内存暴涨。
	reader, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	b, err := readAllLimit(reader, maxBytes)
	if err != nil {
		return "", err
	}
	text := string(b)

	return text, nil
}

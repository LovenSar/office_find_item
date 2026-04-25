//go:build windows

package extract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var (
	errPdftotextNotFound  = errors.New("未找到 pdftotext.exe（请安装 Poppler 或将 pdftotext.exe 放在 exe 同目录，或设置 OFIND_PDFTOTEXT_PATH）")
	errPdftotextDisabled   = errors.New("已禁用 pdftotext（OFIND_PDFTOTEXT=0）")
	errPdftotextRun        = errors.New("pdftotext 执行失败")
)

func pdftotextFeatureEnabled() bool {
	v := strings.TrimSpace(os.Getenv("OFIND_PDFTOTEXT"))
	if v == "" {
		return true
	}
	if v == "0" || strings.EqualFold(v, "false") || strings.EqualFold(v, "off") {
		return false
	}
	return true
}

func pdftotextMaxOutBytes() int64 {
	const def = 40 * 1024 * 1024
	v := strings.TrimSpace(os.Getenv("OFIND_PDFTOTEXT_MAX_OUT_BYTES"))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	if n > 200*1024*1024 {
		return 200 * 1024 * 1024
	}
	return n
}

// resolvePdftotextExe 查找 Poppler 的 pdftotext.exe。
func resolvePdftotextExe() (string, error) {
	if p := strings.TrimSpace(os.Getenv("OFIND_PDFTOTEXT_PATH")); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
		return "", fmt.Errorf("OFIND_PDFTOTEXT_PATH 无效: %s", p)
	}
	// 嵌入在 ofind.exe 内的 Poppler（首次解压到用户缓存目录）
	if dir, err := materializeBundledPdftotext(); err == nil && dir != "" {
		cand := filepath.Join(dir, "pdftotext.exe")
		if st, e := os.Stat(cand); e == nil && !st.IsDir() {
			return cand, nil
		}
	}
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		cand := filepath.Join(dir, "pdftotext.exe")
		if st, e := os.Stat(cand); e == nil && !st.IsDir() {
			return cand, nil
		}
	}
	if p, err := exec.LookPath("pdftotext.exe"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("pdftotext"); err == nil {
		return p, nil
	}
	return "", errPdftotextNotFound
}

// pdftotextRun 调用 pdftotext 将全文写到 stdout，占用 PDF 并发槽位。
func pdftotextRun(ctx context.Context, pdfPath string) ([]byte, error) {
	if err := acquirePDFSlot(ctx); err != nil {
		return nil, err
	}
	defer releasePDFSlot()

	exe, err := resolvePdftotextExe()
	if err != nil {
		return nil, err
	}
	pdfMemHook("pdf:pdftotext:run", pdfPath)

	abs := pdfPath
	if !filepath.IsAbs(pdfPath) {
		if a, e := filepath.Abs(pdfPath); e == nil {
			abs = a
		}
	}
	if st, e := os.Stat(abs); e == nil && st.Size() > pdfMaxFileBytes() {
		return nil, errTooLarge
	}

	cmd := exec.CommandContext(ctx, exe, "-q", abs, "-")
	cmd.Env = os.Environ()
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%w: %v", errPdftotextRun, string(ee.Stderr))
		}
		return nil, fmt.Errorf("%w: %w", errPdftotextRun, err)
	}
	limit := pdftotextMaxOutBytes()
	if int64(len(out)) > limit {
		out = out[:limit]
	}
	return out, nil
}

func toValidUTF8Text(b []byte) string {
	s := string(b)
	return strings.ToValidUTF8(s, "\uFFFD")
}

func pdftotextFindFirst(ctx context.Context, path string, query string, contextLen int) (bool, string, error) {
	if !pdftotextFeatureEnabled() {
		return false, "", errPdftotextDisabled
	}
	q := stringsTrimSpace(query)
	if q == "" {
		return false, "", errors.New("query 为空")
	}
	raw, err := pdftotextRun(ctx, path)
	if err != nil {
		return false, "", err
	}
	text := toValidUTF8Text(raw)
	snips := FindSnippets(text, q, contextLen, 1)
	if len(snips) == 0 {
		return false, "", nil
	}
	return true, snips[0], nil
}

func pdftotextFindSnippets(ctx context.Context, path string, query string, contextLen int, maxSnippets int) ([]string, error) {
	if !pdftotextFeatureEnabled() {
		return nil, errPdftotextDisabled
	}
	q := stringsTrimSpace(query)
	if q == "" {
		return nil, errors.New("query 为空")
	}
	if maxSnippets <= 0 {
		maxSnippets = 1
	}
	raw, err := pdftotextRun(ctx, path)
	if err != nil {
		return nil, err
	}
	text := toValidUTF8Text(raw)
	return FindSnippets(text, q, contextLen, maxSnippets), nil
}

func pdftotextExtractText(ctx context.Context, path string, maxBytes int64) (string, error) {
	if !pdftotextFeatureEnabled() {
		return "", errPdftotextDisabled
	}
	raw, err := pdftotextRun(ctx, path)
	if err != nil {
		return "", err
	}
	text := toValidUTF8Text(raw)
	if maxBytes > 0 && int64(len(text)) > maxBytes {
		text = text[:maxBytes]
	}
	return text, nil
}

package extract

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

// PDF 内存监测（Go runtime.MemStats，不含 Windows IFilter 等 native 堆）。
//
// OFIND_PDF_MEM_HOOK:
//   - 未设置且 OFIND_DEBUG!=1：关闭
//   - 1 / true / yes：阶段 + 每页 GetPlainText 后一行简要统计
//   - phases：仅阶段边界（避免大文件刷屏）
//
// 与 OFIND_DEBUG=1 同时：未设置 OFIND_PDF_MEM_HOOK 时也开启（等同 1）。

func pdfMemHookEnabled() bool {
	v := strings.TrimSpace(os.Getenv("OFIND_PDF_MEM_HOOK"))
	if v == "" {
		return os.Getenv("OFIND_DEBUG") == "1"
	}
	if strings.EqualFold(v, "0") || strings.EqualFold(v, "false") || strings.EqualFold(v, "off") {
		return false
	}
	return true
}

func pdfMemHookPhasesOnly() bool {
	v := strings.TrimSpace(os.Getenv("OFIND_PDF_MEM_HOOK"))
	return strings.EqualFold(v, "phases")
}

// pdfMemHook 阶段边界：完整 MemStats。
func pdfMemHook(phase string, path string) {
	if !pdfMemHookEnabled() {
		return
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	base := path
	if path != "" {
		base = filepath.Base(path)
	}
	log.Printf("[pdf-mem] phase=%s file=%s | Alloc=%.2fMiB Sys=%.2fMiB HeapInuse=%.2fMiB HeapObjs=%d Mallocs=%d NumGC=%d | activePDF=%d",
		phase, base,
		float64(m.Alloc)/(1024*1024),
		float64(m.Sys)/(1024*1024),
		float64(m.HeapInuse)/(1024*1024),
		m.HeapObjects,
		m.Mallocs,
		m.NumGC,
		atomic.LoadInt32(&activePDFTasks))
}

// pdfMemHookPage 每页 GetPlainText 之后（纯 Go 路径）。
func pdfMemHookPage(path string, page int, plainLen int) {
	if !pdfMemHookEnabled() || pdfMemHookPhasesOnly() {
		return
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	base := filepath.Base(path)
	log.Printf("[pdf-mem] page file=%s page=%d plainLen=%d | Alloc=%.2fMiB HeapInuse=%.2fMiB | activePDF=%d",
		base, page, plainLen,
		float64(m.Alloc)/(1024*1024),
		float64(m.HeapInuse)/(1024*1024),
		atomic.LoadInt32(&activePDFTasks))
}

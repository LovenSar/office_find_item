package extract

import (
	"runtime"
	"testing"
)

func TestPdfPureGoFallbackEnabled_Default(t *testing.T) {
	t.Setenv("OFIND_PDF_PUREGO", "")
	got := pdfPureGoFallbackEnabled()
	if runtime.GOOS == "windows" {
		// 新的默认逻辑：如果系统有PDF IFilter，禁用纯Go回退；如果没有，启用纯Go回退
		hasIFilter := HasPDFIFilter()
		if hasIFilter && got {
			t.Fatalf("系统有PDF IFilter，期望禁用纯Go回退，但得到 %v", got)
		}
		if !hasIFilter && !got {
			t.Fatalf("系统没有PDF IFilter，期望启用纯Go回退，但得到 %v", got)
		}
		// 如果 hasIFilter 与 got 匹配，则测试通过
	} else {
		if !got {
			t.Fatalf("expected default pdf pure-go fallback enabled on non-windows, got %v", got)
		}
	}
}

func TestPdfPureGoFallbackEnabled_Env(t *testing.T) {
	t.Setenv("OFIND_PDF_PUREGO", "1")
	got := pdfPureGoFallbackEnabled()
	if runtime.GOOS == "windows" && !got {
		t.Fatalf("expected pdf pure-go fallback enabled when OFIND_PDF_PUREGO=1 on windows, got %v", got)
	}
	if runtime.GOOS != "windows" && !got {
		t.Fatalf("expected pdf pure-go fallback enabled on non-windows, got %v", got)
	}
}

package extract

import (
	"runtime"
	"testing"
)

func TestPdfPureGoFallbackEnabled_Default(t *testing.T) {
	t.Setenv("OFIND_PDF_PUREGO", "")
	got := pdfPureGoFallbackEnabled()
	if runtime.GOOS == "windows" {
		if got {
			t.Fatalf("expected default pdf pure-go fallback disabled on windows, got %v", got)
		}
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

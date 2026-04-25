//go:build windows

package extract

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMaterializeBundledPdftotext(t *testing.T) {
	dir, err := materializeBundledPdftotext()
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		t.Fatal("empty dir")
	}
	exe := filepath.Join(dir, "pdftotext.exe")
	if st, err := os.Stat(exe); err != nil || st.IsDir() {
		t.Fatalf("pdftotext.exe missing: %v", err)
	}
}

func TestBundledPdftotextRuns(t *testing.T) {
	dir, err := materializeBundledPdftotext()
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "pdftotext.exe")
	// 无参数时一般打印用法并退出非 0；若缺 DLL 则几乎无输出且错误码异常
	out, err := exec.Command(exe).CombinedOutput()
	t.Logf("pdftotext (no args): err=%v out len=%d", err, len(out))
	if len(out) == 0 && err != nil {
		t.Fatalf("pdftotext 无法启动（可能缺少 Poppler DLL，请放入 bundled_pdftotext/ 后重编）: %v", err)
	}
}

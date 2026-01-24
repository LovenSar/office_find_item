package cache

import (
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCache_GetOrExtract_TruncatesAndReadsBack(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "a.txt")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Cache{Root: filepath.Join(tmpDir, "cache"), MaxTextBytes: 10}
	got, err := c.GetOrExtract(context.Background(), tmpFile, func(ctx context.Context, path string) (string, error) {
		return "1234567890ABC", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "1234567890" {
		t.Fatalf("unexpected text: %q", got)
	}

	got2, err := c.GetOrExtract(context.Background(), tmpFile, func(ctx context.Context, path string) (string, error) {
		t.Fatalf("extractor should not be called on cache hit")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got2 != "1234567890" {
		t.Fatalf("unexpected cached text: %q", got2)
	}
}

func TestCache_TryRead_HardCapsOversizedCache(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "b.txt")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatal(err)
	}

	c := &Cache{Root: filepath.Join(tmpDir, "cache"), MaxTextBytes: 8}
	cp := c.cachePath(tmpFile)
	if err := os.MkdirAll(filepath.Dir(cp), 0o755); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(cp)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	hdr := make([]byte, 16)
	putLE64(hdr[0:8], uint64(st.ModTime().UnixNano()))
	putLE64(hdr[8:16], uint64(st.Size()))
	if _, err := f.Write(hdr); err != nil {
		t.Fatal(err)
	}

	zw := gzip.NewWriter(f)
	// write more than MaxTextBytes
	if _, err := zw.Write([]byte("0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	txt, ok := c.tryRead(cp, st.Size(), st.ModTime())
	if !ok {
		t.Fatalf("expected cache read ok")
	}
	if txt != "01234567" {
		t.Fatalf("unexpected truncated text: %q", txt)
	}
}

func TestTruncateUTF8ToBytes(t *testing.T) {
	// "你" is 3 bytes in UTF-8.
	s := "你a"
	if got := truncateUTF8ToBytes(s, 1); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	if got := truncateUTF8ToBytes(s, 3); got != "你" {
		t.Fatalf("expected %q, got %q", "你", got)
	}
	if got := truncateUTF8ToBytes(s, 4); got != "你a" {
		t.Fatalf("expected %q, got %q", "你a", got)
	}
}

package extract

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestStreamFindFirst_Basic(t *testing.T) {
	chunks := []string{"hello world"}
	i := 0
	next := func(ctx context.Context) (string, error) {
		if i >= len(chunks) {
			return "", io.EOF
		}
		s := chunks[i]
		i++
		return s, nil
	}

	found, snip, err := streamFindFirst(context.Background(), next, "world", 2)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if !strings.Contains(snip, "【world】") {
		t.Fatalf("expected highlight, got %q", snip)
	}
}

func TestStreamFindFirst_CrossBoundary(t *testing.T) {
	chunks := []string{"你好世", "界和平"}
	i := 0
	next := func(ctx context.Context) (string, error) {
		if i >= len(chunks) {
			return "", io.EOF
		}
		s := chunks[i]
		i++
		return s, nil
	}

	found, snip, err := streamFindFirst(context.Background(), next, "世界", 1)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if snip != "好【世界】和" {
		t.Fatalf("unexpected snippet: %q", snip)
	}
}

func TestStreamFindFirst_NeedsMoreRightContext(t *testing.T) {
	chunks := []string{"abcwo", "rld", "efg"}
	i := 0
	next := func(ctx context.Context) (string, error) {
		if i >= len(chunks) {
			return "", io.EOF
		}
		s := chunks[i]
		i++
		return s, nil
	}

	found, snip, err := streamFindFirst(context.Background(), next, "world", 2)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if snip != "bc【world】ef" {
		t.Fatalf("unexpected snippet: %q", snip)
	}
}

func TestStreamFindFirst_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	next := func(ctx context.Context) (string, error) { return "hello", nil }

	_, _, err := streamFindFirst(ctx, next, "he", 1)
	if err == nil {
		t.Fatalf("expected err")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func BenchmarkStreamFindFirst(b *testing.B) {
	text := strings.Repeat("a", 64*1024) + "NEEDLE" + strings.Repeat("b", 64*1024)
	chunkSize := 4 * 1024

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		i := 0
		next := func(ctx context.Context) (string, error) {
			if i >= len(text) {
				return "", io.EOF
			}
			end := i + chunkSize
			if end > len(text) {
				end = len(text)
			}
			s := text[i:end]
			i = end
			return s, nil
		}
		found, _, err := streamFindFirst(context.Background(), next, "NEEDLE", 8)
		if err != nil {
			b.Fatalf("unexpected err: %v", err)
		}
		if !found {
			b.Fatalf("expected found=true")
		}
	}
}

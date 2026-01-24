package extract

import "testing"

func TestFindSnippets_UnicodeContext(t *testing.T) {
	text := "你好世界你好"
	snips := FindSnippets(text, "世界", 1, 1)
	if len(snips) != 1 {
		t.Fatalf("expected 1 snippet, got %d", len(snips))
	}
	if snips[0] != "好【世界】你" {
		t.Fatalf("unexpected snippet: %q", snips[0])
	}
}

func TestFindSnippets_MultipleMatchesNonOverlapping(t *testing.T) {
	text := "aaaaa"
	snips := FindSnippets(text, "aa", 0, 2)
	if len(snips) != 2 {
		t.Fatalf("expected 2 snippets, got %d", len(snips))
	}
	if snips[0] != "【aa】" || snips[1] != "【aa】" {
		t.Fatalf("unexpected snippets: %#v", snips)
	}
}


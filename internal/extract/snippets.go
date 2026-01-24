package extract

import (
	"strings"
	"unicode/utf8"
)

// FindSnippets finds up to maxSnippets matches of query in text and returns context snippets.
// Each snippet highlights the matched occurrence by wrapping it with 【】.
func FindSnippets(text string, query string, contextLen int, maxSnippets int) []string {
	if maxSnippets <= 0 {
		maxSnippets = 1
	}
	if contextLen < 0 {
		contextLen = 0
	}
	if query == "" || text == "" {
		return nil
	}

	snips := make([]string, 0, maxSnippets)

	searchFrom := 0
	for len(snips) < maxSnippets && searchFrom <= len(text) {
		idx := strings.Index(text[searchFrom:], query)
		if idx < 0 {
			break
		}
		matchStart := searchFrom + idx
		matchEnd := matchStart + len(query)

		start := moveLeftRunes(text, matchStart, contextLen)
		end := moveRightRunes(text, matchEnd, contextLen)

		var b strings.Builder
		b.Grow((end - start) + 4)
		b.WriteString(text[start:matchStart])
		b.WriteString("【")
		b.WriteString(text[matchStart:matchEnd])
		b.WriteString("】")
		b.WriteString(text[matchEnd:end])
		snips = append(snips, b.String())

		// move forward; avoid infinite loop
		if matchEnd <= searchFrom {
			searchFrom++
		} else {
			searchFrom = matchEnd
		}
	}
	return snips
}

func moveLeftRunes(s string, fromByte int, n int) int {
	if n <= 0 {
		return clampByteIndex(fromByte, len(s))
	}
	i := clampByteIndex(fromByte, len(s))
	for k := 0; k < n && i > 0; k++ {
		_, size := utf8.DecodeLastRuneInString(s[:i])
		if size <= 0 {
			i--
			continue
		}
		i -= size
	}
	return i
}

func moveRightRunes(s string, fromByte int, n int) int {
	if n <= 0 {
		return clampByteIndex(fromByte, len(s))
	}
	i := clampByteIndex(fromByte, len(s))
	for k := 0; k < n && i < len(s); k++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		if size <= 0 {
			break
		}
		i += size
	}
	return i
}

func clampByteIndex(i int, length int) int {
	if i < 0 {
		return 0
	}
	if i > length {
		return length
	}
	return i
}

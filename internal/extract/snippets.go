package extract

// FindSnippets finds up to maxSnippets matches of query in text and returns context snippets.
// It works on rune slices to support Unicode correctly.
// Each snippet highlights the matched occurrence by wrapping it with 【】.
func FindSnippets(text string, query string, contextLen int, maxSnippets int) []string {
	if maxSnippets <= 0 {
		maxSnippets = 1
	}
	if contextLen < 0 {
		contextLen = 0
	}
	tr := []rune(text)
	qr := []rune(query)
	if len(qr) == 0 || len(tr) == 0 {
		return nil
	}

	snips := make([]string, 0, maxSnippets)
	i := 0
	for i+len(qr) <= len(tr) {
		idx := indexRunesFrom(tr, qr, i)
		if idx < 0 {
			break
		}
		start := idx - contextLen
		if start < 0 {
			start = 0
		}
		end := idx + len(qr) + contextLen
		if end > len(tr) {
			end = len(tr)
		}
		left := string(tr[start:idx])
		mid := string(tr[idx : idx+len(qr)])
		right := string(tr[idx+len(qr) : end])
		snips = append(snips, left+"【"+mid+"】"+right)
		if len(snips) >= maxSnippets {
			break
		}
		// move forward; avoid infinite loop
		i = idx + len(qr)
	}
	return snips
}

func indexRunesFrom(hay []rune, needle []rune, from int) int {
	if len(needle) == 0 {
		return from
	}
	if from < 0 {
		from = 0
	}
	if len(needle) > len(hay)-from {
		return -1
	}
	for i := from; i+len(needle) <= len(hay); i++ {
		ok := true
		for j := 0; j < len(needle); j++ {
			if hay[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

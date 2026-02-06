package extract

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

type nextStringChunkFunc func(ctx context.Context) (string, error)

// streamFindFirst scans text chunks incrementally and returns the first match snippet.
// It keeps a bounded tail buffer so matches spanning chunk boundaries can be found.
func streamFindFirst(ctx context.Context, next nextStringChunkFunc, query string, contextLen int) (bool, string, error) {
	if stringsTrimSpace(query) == "" {
		return false, "", errors.New("query 为空")
	}
	if contextLen < 0 {
		contextLen = 0
	}

	// Keep enough runes to cover:
	// - left context
	// - a full query that starts in the tail and ends in the next chunk
	// - a tiny safety margin
	keepRunes := contextLen + utf8.RuneCountInString(query) + 8

	var prevTail string
	for {
		if ctx.Err() != nil {
			return false, "", ctx.Err()
		}
		chunk, err := next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return false, "", nil
			}
			return false, "", err
		}
		if chunk == "" {
			continue
		}

		searchText := prevTail + chunk
		idx := strings.Index(searchText, query)
		if idx < 0 {
			prevTail = tailRunes(searchText, keepRunes)
			continue
		}

		// Found; if right context isn't available yet, pull more chunks until we have
		// enough or hit EOF.
		matchStart := idx
		matchEnd := idx + len(query)

		fullText := searchText
		for !hasEnoughRightContext(fullText, matchEnd, contextLen) {
			if ctx.Err() != nil {
				return false, "", ctx.Err()
			}
			more, err := next(ctx)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return false, "", err
			}
			if more == "" {
				continue
			}
			fullText += more
		}

		start := moveLeftRunes(fullText, matchStart, contextLen)
		end := moveRightRunes(fullText, matchEnd, contextLen)

		var sb strings.Builder
		sb.Grow((end - start) + 4)
		sb.WriteString(fullText[start:matchStart])
		sb.WriteString("【")
		sb.WriteString(fullText[matchStart:matchEnd])
		sb.WriteString("】")
		sb.WriteString(fullText[matchEnd:end])
		return true, sb.String(), nil
	}
}

// streamFindSnippets scans text chunks incrementally and returns up to maxSnippets.
func streamFindSnippets(ctx context.Context, next nextStringChunkFunc, query string, contextLen int, maxSnippets int) ([]string, error) {
	if stringsTrimSpace(query) == "" {
		return nil, errors.New("query 为空")
	}
	if maxSnippets <= 0 {
		maxSnippets = 1
	}
	if contextLen < 0 {
		contextLen = 0
	}

	keepRunes := contextLen + utf8.RuneCountInString(query) + 8
	var prevTail string
	snips := make([]string, 0, maxSnippets)

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		chunk, err := next(ctx)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if chunk == "" && err == nil {
			continue
		}
		if chunk == "" && errors.Is(err, io.EOF) {
			break
		}

		searchText := prevTail + chunk
		searchFrom := 0

		for len(snips) < maxSnippets {
			idx := strings.Index(searchText[searchFrom:], query)
			if idx < 0 {
				break
			}
			realIdx := searchFrom + idx
			matchStart := realIdx
			matchEnd := matchStart + len(query)

			fullText := searchText
			eof := false
			for !hasEnoughRightContext(fullText, matchEnd, contextLen) {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				more, ferr := next(ctx)
				if ferr != nil {
					if errors.Is(ferr, io.EOF) {
						eof = true
						break
					}
					return nil, ferr
				}
				if more == "" {
					continue
				}
				fullText += more
			}
			searchText = fullText

			start := moveLeftRunes(searchText, matchStart, contextLen)
			end := moveRightRunes(searchText, matchEnd, contextLen)

			var sb strings.Builder
			sb.Grow((end - start) + 4)
			sb.WriteString(searchText[start:matchStart])
			sb.WriteString("【")
			sb.WriteString(searchText[matchStart:matchEnd])
			sb.WriteString("】")
			sb.WriteString(searchText[matchEnd:end])
			snips = append(snips, sb.String())

			if matchEnd <= searchFrom {
				searchFrom++
			} else {
				searchFrom = matchEnd
			}

			if eof {
				return snips, nil
			}
		}

		if len(snips) >= maxSnippets {
			return snips, nil
		}

		prevTail = tailRunes(searchText, keepRunes)
	}
	return snips, nil
}

func tailRunes(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return string([]byte(s))
	}
	i := len(s)
	for k := 0; k < n && i > 0; k++ {
		_, size := utf8.DecodeLastRuneInString(s[:i])
		if size <= 0 {
			i--
			continue
		}
		i -= size
	}
	return string([]byte(s[i:]))
}

func hasEnoughRightContext(s string, fromByte int, contextLen int) bool {
	if contextLen <= 0 {
		return true
	}
	if fromByte < 0 {
		fromByte = 0
	}
	if fromByte > len(s) {
		fromByte = len(s)
	}
	return utf8.RuneCountInString(s[fromByte:]) >= contextLen
}

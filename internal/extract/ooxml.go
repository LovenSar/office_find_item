package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"path/filepath"
	"strings"
)

func ooxmlContains(ctx context.Context, path string, query string) (bool, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return false, errors.New("query 为空")
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer zr.Close()

	ext := strings.ToLower(filepath.Ext(path))
	qb := []byte(q)
	for _, f := range zr.File {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		name := strings.ToLower(f.Name)
		if !ooxmlEntryInteresting(ext, name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		ok, rerr := xmlStreamContains(ctx, rc, qb)
		_ = rc.Close()
		if rerr == nil && ok {
			return true, nil
		}
	}
	return false, nil
}

func ooxmlFindFirst(ctx context.Context, path string, query string, contextLen int) (bool, string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return false, "", errors.New("query 为空")
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, "", err
	}
	defer zr.Close()

	ext := strings.ToLower(filepath.Ext(path))
	qb := []byte(q)
	for _, f := range zr.File {
		if ctx.Err() != nil {
			return false, "", ctx.Err()
		}
		name := strings.ToLower(f.Name)
		if !ooxmlEntryInteresting(ext, name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		ok, snip, _ := xmlStreamFindFirst(ctx, rc, q, qb, contextLen)
		_ = rc.Close()
		if ok {
			return true, snip, nil
		}
	}
	return false, "", nil
}

func ooxmlEntryInteresting(ext, name string) bool {
	if !strings.HasSuffix(name, ".xml") {
		return false
	}
	switch ext {
	case ".docx":
		return strings.HasPrefix(name, "word/")
	case ".xlsx":
		return strings.HasPrefix(name, "xl/")
	case ".pptx":
		return strings.HasPrefix(name, "ppt/")
	case ".vsdx":
		// VSDX 内容通常在 visio/pages/pageX.xml
		return strings.HasPrefix(name, "visio/pages/")
	default:
		return false
	}
}

func xmlStreamContains(ctx context.Context, r io.Reader, query []byte) (bool, error) {
	// 防止巨大 XML 节点导致内存暴涨；这里只做“尽力而为”扫描。
	const maxScanBytes = 20 * 1024 * 1024
	r = io.LimitReader(r, maxScanBytes)

	dec := xml.NewDecoder(r)
	for {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return false, nil
			}
			return false, err
		}
		switch v := tok.(type) {
		case xml.CharData:
			if len(query) > 0 && bytes.Contains(v, query) {
				return true, nil
			}
		}
	}
}

func xmlStreamFindFirst(ctx context.Context, r io.Reader, query string, queryBytes []byte, contextLen int) (bool, string, error) {
	// 防止巨大 XML 节点导致内存暴涨；这里只做“尽力而为”扫描。
	const maxScanBytes = 20 * 1024 * 1024
	r = io.LimitReader(r, maxScanBytes)

	dec := xml.NewDecoder(r)
	for {
		if ctx.Err() != nil {
			return false, "", ctx.Err()
		}
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return false, "", nil
			}
			return false, "", err
		}
		switch v := tok.(type) {
		case xml.CharData:
			// 大多数 CharData 不命中，先用 bytes 快速判断，避免 string(v) 大量分配。
			if len(queryBytes) > 0 && !bytes.Contains(v, queryBytes) {
				continue
			}
			text := string(v)
			if snips := FindSnippets(text, query, contextLen, 1); len(snips) > 0 {
				return true, snips[0], nil
			}
		}
	}
}

func ooxmlExtractText(ctx context.Context, path string, maxBytes int64) (string, error) {
	maxBytes = maxBytesOrDefault(maxBytes)
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	ext := strings.ToLower(filepath.Ext(path))
	var sb strings.Builder
	var approx int64
	for _, f := range zr.File {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		name := strings.ToLower(f.Name)
		if !ooxmlEntryInteresting(ext, name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		// 限制每个 entry 的读取量，避免巨大的 XML 节点导致内存暴涨。
		remaining := maxBytes - approx
		if remaining <= 0 {
			_ = rc.Close()
			break
		}
		const overhead = 256 * 1024
		limit := remaining + overhead
		if limit < 64*1024 {
			limit = 64 * 1024
		}
		dec := xml.NewDecoder(io.LimitReader(rc, limit))
		for {
			if ctx.Err() != nil {
				_ = rc.Close()
				return "", ctx.Err()
			}
			tok, err := dec.Token()
			if err != nil {
				break
			}
			if cd, ok := tok.(xml.CharData); ok {
				if len(cd) > 0 {
					remaining = maxBytes - approx
					if remaining <= 0 {
						_ = rc.Close()
						return sb.String(), nil
					}
					toWrite := cd
					if int64(len(toWrite)) > remaining {
						toWrite = toWrite[:remaining]
					}
					_, _ = sb.Write(toWrite)
					sb.WriteByte(' ')
					approx += int64(len(toWrite)) + 1
					if approx >= maxBytes {
						_ = rc.Close()
						return sb.String(), nil
					}
				}
			}
		}
		_ = rc.Close()
		if approx >= maxBytes {
			break
		}
	}
	return sb.String(), nil
}

func ooxmlFindSnippets(ctx context.Context, path string, query string, contextLen int, maxSnippets int) ([]string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, errors.New("query 为空")
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	ext := strings.ToLower(filepath.Ext(path))
	qb := []byte(q)
	
	allSnips := make([]string, 0, maxSnippets)

	for _, f := range zr.File {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		name := strings.ToLower(f.Name)
		if !ooxmlEntryInteresting(ext, name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		found, err := xmlStreamFindSnippets(ctx, rc, q, qb, contextLen, maxSnippets - len(allSnips))
		_ = rc.Close()
		if err == nil && len(found) > 0 {
			allSnips = append(allSnips, found...)
			if len(allSnips) >= maxSnippets {
				return allSnips, nil
			}
		}
	}
	return allSnips, nil
}

func xmlStreamFindSnippets(ctx context.Context, r io.Reader, query string, queryBytes []byte, contextLen int, maxSnippets int) ([]string, error) {
	if maxSnippets <= 0 {
		return nil, nil
	}
	// Limit read per XML file to avoid infinite loops or memory bombs
	const maxScanBytes = 20 * 1024 * 1024
	r = io.LimitReader(r, maxScanBytes)

	dec := xml.NewDecoder(r)
	snips := make([]string, 0, maxSnippets)

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return snips, nil
			}
			return snips, err
		}
		switch v := tok.(type) {
		case xml.CharData:
			if len(queryBytes) > 0 && !bytes.Contains(v, queryBytes) {
				continue
			}
			text := string(v)
			found := FindSnippets(text, query, contextLen, maxSnippets-len(snips))
			if len(found) > 0 {
				snips = append(snips, found...)
				if len(snips) >= maxSnippets {
					return snips, nil
				}
			}
		}
	}
}

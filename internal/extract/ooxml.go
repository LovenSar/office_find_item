package extract

import (
	"archive/zip"
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
		ok, rerr := xmlStreamContains(ctx, rc, q)
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
		ok, snip, _ := xmlStreamFindFirst(ctx, rc, q, contextLen)
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
	default:
		return false
	}
}

func xmlStreamContains(ctx context.Context, r io.Reader, query string) (bool, error) {
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
			if strings.Contains(string(v), query) {
				return true, nil
			}
		}
	}
}

func xmlStreamFindFirst(ctx context.Context, r io.Reader, query string, contextLen int) (bool, string, error) {
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
		dec := xml.NewDecoder(rc)
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
				chunk := string(cd)
				if chunk != "" {
					sb.WriteString(chunk)
					sb.WriteByte(' ')
					approx += int64(len(chunk)) + 1
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

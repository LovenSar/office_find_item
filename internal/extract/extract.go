package extract

import (
	"context"
	"path/filepath"
	"strings"
)

func FileFindFirst(ctx context.Context, path string, query string, contextLen int) (found bool, snippet string, err error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".log", ".csv", ".json", ".xml", ".ini", ".yaml", ".yml":
		return textFileFindFirst(ctx, path, query, contextLen)
	case ".docx", ".xlsx", ".pptx", ".vsdx":
		return ooxmlFindFirst(ctx, path, query, contextLen)
	case ".pdf":
		return pdfFindFirst(ctx, path, query, contextLen)
	default:
		// .doc/.xls/.ppt/.pdf 等：在 Windows 下用 IFilter；非 Windows 则返回不支持
		return ifilterFindFirst(ctx, path, query, contextLen)
	}
}

func FileContains(ctx context.Context, path string, query string) (bool, error) {
	found, _, err := FileFindFirst(ctx, path, query, 0)
	return found, err
}

func FileFindSnippets(ctx context.Context, path string, query string, contextLen int, maxSnippets int) ([]string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".log", ".csv", ".json", ".xml", ".ini", ".yaml", ".yml":
		return textFileFindSnippets(ctx, path, query, contextLen, maxSnippets)
	case ".docx", ".xlsx", ".pptx", ".vsdx":
		return ooxmlFindSnippets(ctx, path, query, contextLen, maxSnippets)
	case ".pdf":
		return PDFFindSnippetsStream(ctx, path, query, contextLen, maxSnippets)
	default:
		return ifilterFindSnippets(ctx, path, query, contextLen, maxSnippets)
	}
}

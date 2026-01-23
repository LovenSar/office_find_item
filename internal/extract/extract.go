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
	case ".docx", ".xlsx", ".pptx":
		return ooxmlFindFirst(ctx, path, query, contextLen)
	default:
		// .doc/.xls/.ppt/.pdf 等：在 Windows 下用 IFilter；非 Windows 则返回不支持
		return ifilterFindFirst(ctx, path, query, contextLen)
	}
}

func FileContains(ctx context.Context, path string, query string) (bool, error) {
	found, _, err := FileFindFirst(ctx, path, query, 0)
	return found, err
}

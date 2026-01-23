package extract

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// FileExtractText extracts readable text from supported files.
// maxBytes is a soft cap; implementations may stop early.
func FileExtractText(ctx context.Context, path string, maxBytes int64) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".log", ".csv", ".json", ".xml", ".ini", ".yaml", ".yml":
		return textFileExtractText(ctx, path, maxBytes)
	case ".docx", ".xlsx", ".pptx":
		return ooxmlExtractText(ctx, path, maxBytes)
	default:
		return ifilterExtractText(ctx, path, maxBytes)
	}
}

func maxBytesOrDefault(maxBytes int64) int64 {
	if maxBytes <= 0 {
		return 2 * 1024 * 1024
	}
	return maxBytes
}

var errTooLarge = errors.New("提取内容超过上限")

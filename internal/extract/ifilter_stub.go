//go:build !windows

package extract

import (
	"context"
	"errors"
)

func ifilterFindFirst(ctx context.Context, path string, query string, contextLen int) (bool, string, error) {
	_ = ctx
	_ = path
	_ = query
	_ = contextLen
	return false, "", errors.New("该格式需要 Windows IFilter 支持（当前非 Windows）")
}

func ifilterExtractText(ctx context.Context, path string, maxBytes int64) (string, error) {
	_ = ctx
	_ = path
	_ = maxBytes
	return "", errors.New("该格式需要 Windows IFilter 支持（当前非 Windows）")
}

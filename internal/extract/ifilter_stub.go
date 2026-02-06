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

func ifilterFindSnippets(ctx context.Context, path string, query string, contextLen int, maxSnippets int) ([]string, error) {
	_ = ctx
	_ = path
	_ = query
	_ = contextLen
	_ = maxSnippets
	return nil, errors.New("该格式需要 Windows IFilter 支持（当前非 Windows）")
}

func HasPDFIFilter() bool {
	// 非Windows平台没有IFilter
	return false
}

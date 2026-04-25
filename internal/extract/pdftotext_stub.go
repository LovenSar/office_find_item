//go:build !windows

package extract

import (
	"context"
	"errors"
)

var errPdftotextUnavailable = errors.New("pdftotext 仅在 Windows 下可用")

func pdftotextFindFirst(ctx context.Context, path string, query string, contextLen int) (bool, string, error) {
	_ = ctx
	_ = path
	_ = query
	_ = contextLen
	return false, "", errPdftotextUnavailable
}

func pdftotextFindSnippets(ctx context.Context, path string, query string, contextLen int, maxSnippets int) ([]string, error) {
	_ = ctx
	_ = path
	_ = query
	_ = contextLen
	_ = maxSnippets
	return nil, errPdftotextUnavailable
}

func pdftotextExtractText(ctx context.Context, path string, maxBytes int64) (string, error) {
	_ = ctx
	_ = path
	_ = maxBytes
	return "", errPdftotextUnavailable
}

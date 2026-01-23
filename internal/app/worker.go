package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"office_find_item/internal/search"
)

// RunWorker 用于 UI 进程启动的子进程：
// - 只输出 JSON Lines（每行一个结果）到 stdout
// - 不打印 banner / progress
func RunWorker(opts CLIOptions) error {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return errors.New("query 为空")
	}

	roots := parseRoots(opts.Roots)
	if len(roots) == 0 {
		return errors.New("roots 为空")
	}
	// Worker 模式建议单 root；这里仍按传入列表处理。
	for i := range roots {
		abs, err := filepath.Abs(strings.TrimSpace(roots[i]))
		if err == nil {
			roots[i] = abs
		}
	}

	cfg := search.Config{
		Roots:      roots,
		Query:      query,
		Workers:    opts.Workers,
		ContextLen: 30,
	}

	enc := json.NewEncoder(os.Stdout)
	return search.Search(cfg, nil, func(r search.Result) {
		_ = enc.Encode(r)
	})
}

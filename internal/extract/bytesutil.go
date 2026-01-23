package extract

import (
	"errors"
	"io"
	"strings"
)

func stringsTrimSpace(s string) string { return strings.TrimSpace(s) }

func readAllLimit(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, errors.New("limit 必须 > 0")
	}
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 64*1024)
	var total int64
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return nil, errors.New("文件过大，超过读取上限")
			}
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return buf, nil
			}
			return nil, err
		}
	}
}

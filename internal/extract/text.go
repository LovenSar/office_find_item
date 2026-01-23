package extract

import (
	"context"
	"errors"
	"os"
	"unicode/utf16"
	"unicode/utf8"
)

func textFileFindFirst(ctx context.Context, path string, query string, contextLen int) (bool, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, "", err
	}
	defer f.Close()

	if stringsTrimSpace(query) == "" {
		return false, "", errors.New("query 为空")
	}

	if ctx.Err() != nil {
		return false, "", ctx.Err()
	}

	// 读取一定上限，避免极端大文件导致内存压力。
	const maxBytes = 20 * 1024 * 1024
	b, err := readAllLimit(f, maxBytes)
	if err != nil {
		return false, "", err
	}

	text, err := decodeTextBytes(b)
	if err != nil {
		return false, "", err
	}

	snips := FindSnippets(text, query, contextLen, 1)
	if len(snips) == 0 {
		return false, "", nil
	}
	return true, snips[0], nil
}

func textFileExtractText(ctx context.Context, path string, maxBytes int64) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	maxBytes = maxBytesOrDefault(maxBytes)
	b, err := readAllLimit(f, maxBytes)
	if err != nil {
		return "", err
	}
	return decodeTextBytes(b)
}

func decodeTextBytes(b []byte) (string, error) {
	// UTF-8 BOM
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
		return string(b), nil
	}
	// UTF-16 LE/BE BOM
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		return decodeUTF16(b[2:], true), nil
	}
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		return decodeUTF16(b[2:], false), nil
	}
	// 默认 UTF-8
	if utf8.Valid(b) {
		return string(b), nil
	}
	// best-effort
	return string(b), nil
}

func decodeUTF16(b []byte, littleEndian bool) string {
	// b 为纯 UTF-16 字节序列（无 BOM）
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u16 := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		var v uint16
		if littleEndian {
			v = uint16(b[i]) | uint16(b[i+1])<<8
		} else {
			v = uint16(b[i+1]) | uint16(b[i])<<8
		}
		u16 = append(u16, v)
	}
	r := utf16.Decode(u16)
	return string(r)
}

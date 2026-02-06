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

func textFileFindSnippets(ctx context.Context, path string, query string, contextLen int, maxSnippets int) ([]string, error) {
	// Detect encoding first
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	// Do not defer close here immediately if we pass f to closure, but we can if closure uses f.
	// But closure lifetime is inside this function.
	defer f.Close()

	head := make([]byte, 4)
	n, _ := f.Read(head)
	_, _ = f.Seek(0, 0)

	isUTF16 := false
	if n >= 2 {
		if head[0] == 0xFF && head[1] == 0xFE {
			isUTF16 = true
		} else if head[0] == 0xFE && head[1] == 0xFF {
			isUTF16 = true
		}
	}

	// For UTF-16, simpler to just read all (with limit) for now,
	// as streaming decode + snippet searching is complex (need to align chunks).
	// Most large CSVs/Logs causing issues are UTF-8.
	if isUTF16 {
		// Fallback to memory load (capped)
		const maxBytes = 10 * 1024 * 1024
		b, err := readAllLimit(f, maxBytes)
		if err != nil {
			return nil, err
		}
		text, err := decodeTextBytes(b)
		if err != nil {
			return nil, err
		}
		return FindSnippets(text, query, contextLen, maxSnippets), nil
	}

	// UTF-8 Streaming
	var leftOver []byte
	firstChunk := true

	next := func(ctx context.Context) (string, error) {
		readBufSize := 32 * 1024
		buf := make([]byte, readBufSize+len(leftOver))
		if len(leftOver) > 0 {
			copy(buf, leftOver)
		}

		n, err := f.Read(buf[len(leftOver):])
		total := len(leftOver) + n

		if total == 0 {
			if err != nil {
				return "", err
			}
			return "", nil
		}

		b := buf[:total]
		leftOver = nil // Reset

		// Check for partial UTF-8 at the end only if NOT EOF
		if err == nil {
			valid, rest := cutPartialUTF8(b)
			b = valid
			if len(rest) > 0 {
				leftOver = make([]byte, len(rest))
				copy(leftOver, rest)
			}
		}

		if firstChunk {
			if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
				b = b[3:]
			}
			firstChunk = false
		}
		return string(b), err
	}

	return streamFindSnippets(ctx, next, query, contextLen, maxSnippets)
}

func cutPartialUTF8(b []byte) (valid, rest []byte) {
	if len(b) == 0 {
		return b, nil
	}
	// UTF-8 max length is 4. Look back up to 3 bytes.
	for i := 0; i < 3 && i < len(b); i++ {
		idx := len(b) - 1 - i
		c := b[idx]
		// Start bytes: 11xxxxxx (0xC0-0xDF => 2), 111xxxxx (0xE0-0xEF => 3), 1111xxxx (0xF0-0xF7 => 4)
		if c >= 0xC0 {
			needed := 0
			if c < 0xE0 {
				needed = 2
			} else if c < 0xF0 {
				needed = 3
			} else if c < 0xF8 {
				needed = 4
			} else {
				return b, nil
			}
			have := i + 1
			if have < needed {
				return b[:idx], b[idx:]
			}
			return b, nil
		}
		if c < 0x80 {
			return b, nil
		}
	}
	return b, nil
}

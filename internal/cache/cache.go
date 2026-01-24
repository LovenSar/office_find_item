package cache

import (
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"
)

type Extractor func(ctx context.Context, path string) (string, error)

type Cache struct {
	Root         string
	MaxTextBytes int64
}

func (c *Cache) effectiveMaxTextBytes() int64 {
	if c.MaxTextBytes > 0 {
		return c.MaxTextBytes
	}
	return 2 * 1024 * 1024
}

func truncateUTF8ToBytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	// Move left to a valid rune boundary (at most 3 bytes).
	for cut > 0 && cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if cut <= 0 {
		return ""
	}
	return s[:cut]
}

func (c *Cache) cachePath(absPath string) string {
	h := sha1.Sum([]byte(absPath))
	hexsum := hex.EncodeToString(h[:])
	// shard by first 2 chars
	shard := hexsum[:2]
	return filepath.Join(c.Root, shard, hexsum+".bin")
}

func (c *Cache) GetOrExtract(ctx context.Context, absPath string, extractor Extractor) (string, error) {
	if extractor == nil {
		return "", errors.New("extractor is nil")
	}
	st, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}

	cp := c.cachePath(absPath)
	if text, ok := c.tryRead(cp, st.Size(), st.ModTime()); ok {
		return text, nil
	}

	text, err := extractor(ctx, absPath)
	if err != nil {
		return "", err
	}
	// Hard cap: avoid extractor bugs causing huge in-memory strings / cache files.
	maxBytes := c.effectiveMaxTextBytes()
	if int64(len(text)) > maxBytes {
		text = truncateUTF8ToBytes(text, int(maxBytes))
	}
	_ = c.write(cp, st.Size(), st.ModTime(), text)
	return text, nil
}

func (c *Cache) tryRead(path string, size int64, mtime time.Time) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	hdr := make([]byte, 16)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return "", false
	}
	cachedM := int64(le64(hdr[0:8]))
	cachedS := int64(le64(hdr[8:16]))
	if cachedS != size || cachedM != mtime.UnixNano() {
		return "", false
	}
	zr, err := gzip.NewReader(f)
	if err != nil {
		return "", false
	}
	defer zr.Close()
	maxBytes := c.effectiveMaxTextBytes()
	lr := io.LimitReader(zr, maxBytes+1)
	b, err := io.ReadAll(lr)
	if err != nil {
		return "", false
	}
	if int64(len(b)) > maxBytes {
		b = b[:maxBytes]
	}
	return string(b), true
}

func (c *Cache) write(path string, size int64, mtime time.Time, text string) error {
	maxBytes := c.effectiveMaxTextBytes()
	if int64(len(text)) > maxBytes {
		text = truncateUTF8ToBytes(text, int(maxBytes))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()

	hdr := make([]byte, 16)
	putLE64(hdr[0:8], uint64(mtime.UnixNano()))
	putLE64(hdr[8:16], uint64(size))
	if _, err := f.Write(hdr); err != nil {
		return err
	}

	zw := gzip.NewWriter(f)
	_, err = zw.Write([]byte(text))
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func le64(b []byte) uint64 {
	_ = b[7]
	return uint64(b[0]) |
		uint64(b[1])<<8 |
		uint64(b[2])<<16 |
		uint64(b[3])<<24 |
		uint64(b[4])<<32 |
		uint64(b[5])<<40 |
		uint64(b[6])<<48 |
		uint64(b[7])<<56
}

func putLE64(dst []byte, v uint64) {
	_ = dst[7]
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 24)
	dst[4] = byte(v >> 32)
	dst[5] = byte(v >> 40)
	dst[6] = byte(v >> 48)
	dst[7] = byte(v >> 56)
}

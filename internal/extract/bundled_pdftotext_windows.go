//go:build windows

package extract

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// 将 Poppler 的 pdftotext.exe（及同目录其它文件）打入 ofind.exe，首次使用时解压到用户缓存目录。
//
//go:embed bundled_pdftotext
var bundledPdftotext embed.FS

var (
	bundleOnce sync.Once
	bundleDir  string
	bundleErr  error
)

// materializeBundledPdftotext 将嵌入文件解压到本地缓存并返回目录；失败时返回错误。
func materializeBundledPdftotext() (string, error) {
	bundleOnce.Do(func() {
		base, err := os.UserCacheDir()
		if err != nil {
			base = os.TempDir()
		}
		destRoot := filepath.Join(base, "office_find_item", "bundled_pdftotext")
		bundleErr = extractEmbedDirToDisk(bundledPdftotext, "bundled_pdftotext", destRoot)
		if bundleErr != nil {
			return
		}
		bundleDir = destRoot
	})
	return bundleDir, bundleErr
}

func extractEmbedDirToDisk(efs embed.FS, rootPrefix, destRoot string) error {
	return fs.WalkDir(efs, rootPrefix, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, rootPrefix+"/")
		if rel == path {
			rel = strings.TrimPrefix(path, rootPrefix)
			rel = strings.TrimPrefix(rel, "/")
		}
		if rel == "" {
			return nil
		}
		data, err := efs.ReadFile(path)
		if err != nil {
			return err
		}
		out := filepath.Join(destRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return err
		}
		if st, e := os.Stat(out); e == nil && st.Size() == int64(len(data)) {
			return nil
		}
		return os.WriteFile(out, data, 0755)
	})
}

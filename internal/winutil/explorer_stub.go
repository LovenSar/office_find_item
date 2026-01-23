//go:build !windows

package winutil

import "errors"

func RevealInExplorer(path string) error {
	_ = path
	return errors.New("仅支持 Windows")
}

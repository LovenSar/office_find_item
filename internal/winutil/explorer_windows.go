//go:build windows

package winutil

import (
	"os/exec"
	"path/filepath"
)

func RevealInExplorer(path string) error {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	// explorer.exe /select,"C:\path\file"
	arg := "/select," + path
	cmd := exec.Command("explorer.exe", arg)
	return cmd.Start()
}

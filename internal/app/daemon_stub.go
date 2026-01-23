//go:build !windows

package app

import "errors"

func RunDaemon(opts CLIOptions) error {
	_ = opts
	return errors.New("daemon 仅支持 Windows")
}

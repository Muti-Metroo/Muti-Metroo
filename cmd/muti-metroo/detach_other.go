//go:build !windows

package main

import "errors"

func detachSelf() error {
	return errors.New("--detach is only supported on Windows (use nohup or setsid on Linux/macOS)")
}

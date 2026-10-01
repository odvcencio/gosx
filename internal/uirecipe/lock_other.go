//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package uirecipe

import (
	"fmt"
	"os"
)

const nonblockingOpenFlag = 0

func tryFileLock(f *os.File) (bool, error) {
	return false, fmt.Errorf("UI recipe installation requires OS-backed file locking on this platform")
}
func releaseFileLock(f *os.File) {}

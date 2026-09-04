//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package uirecipe

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

const nonblockingOpenFlag = unix.O_NONBLOCK

func tryFileLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}
func releaseFileLock(f *os.File) { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }

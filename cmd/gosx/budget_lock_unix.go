//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

const budgetOpenFlags = unix.O_NONBLOCK | unix.O_NOFOLLOW

func acquireBudgetLock(file *os.File) error {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func openBudgetDirectory(root *os.Root, path string) (*os.File, error) {
	return root.Open(path)
}

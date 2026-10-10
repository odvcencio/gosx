//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package main

import (
	"errors"
	"os"
)

const budgetOpenFlags = 0

func acquireBudgetLock(file *os.File) error {
	return errors.New("budget saving requires OS-backed file locking")
}

func openBudgetDirectory(root *os.Root, path string) (*os.File, error) {
	return root.Open(path)
}

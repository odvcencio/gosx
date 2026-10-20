//go:build unix

package main

import (
	"errors"
	"os"
	"syscall"
)

func preserveBudgetOwnership(file *os.File, source os.FileInfo) error {
	original, ok := source.Sys().(*syscall.Stat_t)
	current, err := file.Stat()
	if !ok || err != nil {
		return errors.New("output unavailable")
	}
	staged, ok := current.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("output unavailable")
	}
	// Avoid chown when ownership already matches; unprivileged writers can
	// preserve their own files without permission to change ownership.
	if original.Uid != staged.Uid || original.Gid != staged.Gid {
		if err := file.Chown(int(original.Uid), int(original.Gid)); err != nil {
			return errors.New("output unavailable")
		}
	}
	after, err := file.Stat()
	if err != nil {
		return errors.New("output unavailable")
	}
	verified, ok := after.Sys().(*syscall.Stat_t)
	if !ok || verified.Uid != original.Uid || verified.Gid != original.Gid {
		return errors.New("output unavailable")
	}
	return nil
}

//go:build !unix

package main

import "os"

// These platforms have no POSIX uid/gid in FileInfo or supported File.Chown.
func preserveBudgetOwnership(file *os.File, source os.FileInfo) error { return nil }

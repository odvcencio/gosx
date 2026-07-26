//go:build !unix && !windows

package main

import "syscall"

// This file backs process_unix.go's and process_windows.go's
// exported functions for every GOOS neither of those two files'
// build tags cover: js, wasip1, and plan9.
//
// cmd/gosx never actually launches a child dev-server process under
// any of those targets. But the package must still build there, for
// example under `GOOS=js GOARCH=wasm go build ./cmd/...`. So these
// are no-op stubs, not real process-tree management.

func childProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func interruptProcessTree(pid int) error {
	return nil
}

func terminateProcessTree(pid int) error {
	return nil
}

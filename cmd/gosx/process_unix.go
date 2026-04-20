//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

func childProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func interruptProcessTree(pid int) error {
	return signalProcessTree(pid, syscall.SIGINT)
}

func terminateProcessTree(pid int) error {
	return signalProcessTree(pid, syscall.SIGKILL)
}

func signalProcessTree(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid > 0 {
		if err := syscall.Kill(-pgid, sig); err == nil || errors.Is(err, syscall.ESRCH) {
			return nil
		}
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(sig); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

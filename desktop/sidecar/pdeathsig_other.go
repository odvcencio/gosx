//go:build unix && !linux

package sidecar

import "syscall"

func setParentDeathSignal(*syscall.SysProcAttr) {}

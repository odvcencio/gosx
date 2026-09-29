//go:build linux

package sidecar

import "syscall"

// setParentDeathSignal kills the sidecar if the app dies without calling
// Stop. Linux ties this signal to the thread that started the child, so it
// also fires if that OS thread exits; apps that start sidecars from a
// goroutine locked to a thread should keep the thread alive.
func setParentDeathSignal(attr *syscall.SysProcAttr) {
	attr.Pdeathsig = syscall.SIGKILL
}

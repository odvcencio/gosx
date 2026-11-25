//go:build !linux

package browsertest

import "os/exec"

func configureProcess(*exec.Cmd) {}

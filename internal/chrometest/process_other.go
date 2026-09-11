//go:build !linux

package chrometest

import "os/exec"

func configureProcess(*exec.Cmd) {}

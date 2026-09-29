//go:build !unix && !windows

package sidecar

import (
	"os"
	"os/exec"
)

type platformProcess struct {
	cmd *exec.Cmd
}

func configureCommand(*exec.Cmd, Options) {}

func attachProcess(cmd *exec.Cmd) (*platformProcess, error) {
	return &platformProcess{cmd: cmd}, nil
}

func (p *platformProcess) interrupt() error { return p.cmd.Process.Signal(os.Interrupt) }

func (p *platformProcess) kill() { _ = p.cmd.Process.Kill() }

func (p *platformProcess) release() {}

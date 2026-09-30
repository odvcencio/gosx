//go:build unix

package sidecar

import (
	"os/exec"
	"sync"
	"syscall"
)

type platformProcess struct {
	cmd      *exec.Cmd
	mu       sync.Mutex
	released bool
}

func configureCommand(cmd *exec.Cmd, _ Options) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	setParentDeathSignal(cmd.SysProcAttr)
}

func attachProcess(cmd *exec.Cmd) (*platformProcess, error) {
	return &platformProcess{cmd: cmd}, nil
}

// interrupt sends SIGTERM to the sidecar's process group.
func (p *platformProcess) interrupt() error {
	return syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
}

// kill sends SIGKILL to the sidecar's process group. After release the
// group has already been killed, so kill does nothing.
func (p *platformProcess) kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.released {
		return
	}
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = p.cmd.Process.Kill()
	}
}

// release runs right after the main process is reaped. It sends SIGKILL to
// what is left of the process group, so children do not outlive the sidecar,
// matching the Windows Job Object. The group ID stays reserved while any
// member is alive, so the signal cannot reach an unrelated group.
func (p *platformProcess) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.released {
		p.released = true
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	}
}

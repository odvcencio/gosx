//go:build windows

package ouroboros

import (
	"os/exec"
	"time"
)

func configureFixtureCommand(cmd *exec.Cmd) {}

func terminateFixtureCommand(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	_ = cmd.Process.Kill()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

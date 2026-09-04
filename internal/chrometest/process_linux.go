//go:build linux

package chrometest

import (
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if _, lambda := os.LookupEnv("LAMBDA_TASK_ROOT"); !lambda {
		cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
	}
	// Chrome and launch scripts can leave children holding output pipes or
	// writing into the profile. Kill the owned process group before Wait and
	// profile removal; killing only the direct parent is insufficient on CI.
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
}

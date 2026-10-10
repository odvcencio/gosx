//go:build linux

package budgetci

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func productionSupported() bool { return true }

type productionProcess struct {
	pid   int
	start string
}

func processFields(pid int) []string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return nil
	}
	// comm may contain spaces or parentheses; field 3 starts after the last ).
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return nil
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return nil
	}
	return fields
}

// stopProduction kills individual, identity-checked PIDs in this command's
// isolated process group. Child compilers cannot outlive a cancelled build.
func stopProduction(group int) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		remaining, err := stopProductionGroup(group)
		if err != nil || !remaining {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("production process cleanup timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func stopProductionGroup(group int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	var processes []productionProcess
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fields := processFields(pid)
		if fields != nil && fields[0] != "Z" && fields[2] == strconv.Itoa(group) {
			processes = append(processes, productionProcess{pid, fields[19]})
		}
	}
	var result error
	// Stop the parent last; it may reap children while their tools shut down.
	for _, parent := range []bool{false, true} {
		for _, process := range processes {
			if (process.pid == group) != parent {
				continue
			}
			fields := processFields(process.pid)
			if fields == nil || fields[19] != process.start || fields[2] != strconv.Itoa(group) {
				continue
			}
			if err := syscall.Kill(process.pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				result = err
			}
		}
	}
	return len(processes) > 0, result
}

func prepareProduction(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return stopProduction(cmd.Process.Pid) }
}

func runProduction(cmd *exec.Cmd) error {
	prepareProduction(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	runErr := cmd.Wait()
	// Also close tools abandoned by a failed parent without context cancellation.
	cleanupErr := stopProduction(cmd.Process.Pid)
	return errors.Join(runErr, cleanupErr)
}

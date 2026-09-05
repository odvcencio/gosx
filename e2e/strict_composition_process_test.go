//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStrictCompositionBoundedCommandOutputProcessControls(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		output, err := runBoundedCommandOutput(t, 5*time.Second, t.TempDir(), os.Environ(), "/bin/sh", "-c", "printf success")
		if err != nil {
			t.Fatalf("runBoundedCommandOutput success: %v\n%s", err, output)
		}
		if string(output) != "success" {
			t.Fatalf("output = %q, want success", output)
		}
	})

	t.Run("nonzero exit", func(t *testing.T) {
		output, err := runBoundedCommandOutput(t, 5*time.Second, t.TempDir(), os.Environ(), "/bin/sh", "-c", "printf failure; exit 7")
		if err == nil || !strings.Contains(err.Error(), "exit status 7") {
			t.Fatalf("nonzero error = %v, output=%q, want exit status 7", err, output)
		}
		if string(output) != "failure" {
			t.Fatalf("output = %q, want failure", output)
		}
	})

	t.Run("child survives parent normal exit", func(t *testing.T) {
		dir := t.TempDir()
		pidFile := filepath.Join(dir, "child.pid")
		script := fmt.Sprintf(`(trap '' TERM; while :; do sleep 1; done) & printf "$!" > %s; exit 0`, shellQuote(pidFile))
		output, err := runBoundedCommandOutput(t, 5*time.Second, dir, os.Environ(), "/bin/sh", "-c", script)
		if err == nil || !strings.Contains(err.Error(), "left process group") {
			t.Fatalf("surviving child error = %v, output=%q, want lingering process group failure", err, output)
		}
		childPID := readPIDFile(t, pidFile)
		assertProcessAbsentSoon(t, childPID)
	})

	t.Run("timeout term ignore requires kill", func(t *testing.T) {
		dir := t.TempDir()
		pidFile := filepath.Join(dir, "leader.pid")
		script := fmt.Sprintf(`trap '' TERM; printf "$$" > %s; while :; do sleep 1; done`, shellQuote(pidFile))
		output, err := runBoundedCommandOutput(t, 100*time.Millisecond, dir, os.Environ(), "/bin/sh", "-c", script)
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout error = %v, output=%q, want context deadline exceeded", err, output)
		}
		leaderPID := readPIDFile(t, pidFile)
		assertProcessAbsentSoon(t, leaderPID)
	})
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func readPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var data []byte
	var err error
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(data)) != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("read pid file %s: %v", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("pid file %s = %q, err=%v", path, data, err)
	}
	return pid
}

func assertProcessAbsentSoon(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("owned process %d still exists after bounded helper cleanup", pid)
}

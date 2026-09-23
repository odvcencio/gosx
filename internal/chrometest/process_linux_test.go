//go:build linux

package chrometest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStartupCleanupKillsDescendantHoldingOutputPipe(t *testing.T) {
	testDescendantCleanup(t, false)
}

func TestEarlyExitWithDescendantPipeIsFatalWithoutRetry(t *testing.T) {
	testDescendantCleanup(t, true)
}

func testDescendantCleanup(t *testing.T, earlyExit bool) {
	t.Helper()
	requirePOSIXShell(t)
	root := t.TempDir()
	pidPath := filepath.Join(root, "child.pid")
	logPath := filepath.Join(root, "attempts.log")
	t.Setenv("GOSX_FAKE_CHILD_PID", pidPath)
	t.Setenv("GOSX_FAKE_LOG", logPath)
	body := `
printf 'attempt\n' >> "$GOSX_FAKE_LOG"
tail -f /dev/null &
printf '%s' "$!" > "$GOSX_FAKE_CHILD_PID"
`
	if earlyExit {
		body += "exit 23\n"
	} else {
		body += "wait\n"
	}
	executable := writeFakeChrome(t, body)
	policy := fastPolicy(1)
	if earlyExit {
		policy.attempts = 2
	}
	_, err := startWithPolicy(context.Background(), executable, policy)
	if err == nil {
		t.Fatal("expected launcher to fail")
	}
	if earlyExit && (!strings.Contains(err.Error(), "exit status 23") || countLogLines(t, logPath) != 1) {
		t.Fatalf("early process exit was not fatal: %v", err)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(readFile(t, pidPath)))
	if parseErr != nil || pid <= 1 {
		t.Fatalf("invalid child PID: %d %v", pid, parseErr)
	}
	// A killed orphan can briefly remain as a zombie until the host reaps it;
	// either missing or zombie proves it cannot keep pipes/profile files open.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stat, statErr := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(statErr) {
			return
		}
		if statErr == nil {
			parts := strings.SplitN(string(stat), ") ", 2)
			if len(parts) == 2 && strings.HasPrefix(parts[1], "Z ") {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("descendant %d survived startup cleanup", pid)
}

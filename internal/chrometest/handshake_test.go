package chrometest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryableStartupTimeoutRequiresTypedTimeoutLiveProcessAndCaller(t *testing.T) {
	live := make(chan struct{})
	exited := make(chan struct{})
	close(exited)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"deadline", context.DeadlineExceeded, true},
		{"wrapped deadline", fmt.Errorf("dial: %w", context.DeadlineExceeded), true},
		{"wrapped network timeout", fmt.Errorf("dial: %w", &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}), true},
		{"plain deadline text", errors.New("context deadline exceeded"), false},
		{"plain timeout text", errors.New("dial timeout"), false},
		{"network failure", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, false},
		{"cancellation", fmt.Errorf("dial: %w", context.Canceled), false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryableStartupTimeout(context.Background(), tc.err, live); got != tc.want {
				t.Fatalf("live startup retry=%v, want %v", got, tc.want)
			}
			if retryableStartupTimeout(context.Background(), tc.err, exited) {
				t.Fatal("process exit became retryable")
			}
			if retryableStartupTimeout(canceled, tc.err, live) {
				t.Fatal("caller cancellation became retryable")
			}
		})
	}
}

func TestStartRetriesHandshakeTimeoutWithFreshProcess(t *testing.T) {
	requirePOSIXShell(t)
	var handshakes atomic.Int32
	firstCanceled := make(chan struct{})
	endpoint, _, _ := fakeCDPBeforeUpgrade(t, false, func(w http.ResponseWriter, r *http.Request) bool {
		if handshakes.Add(1) == 1 {
			<-r.Context().Done()
			close(firstCanceled)
			return false
		}
		return true
	})
	executable, logPath := handshakeChrome(t, endpoint)
	browser, err := startWithPolicy(t.Context(), executable, fastPolicy(2))
	if err != nil {
		t.Fatalf("handshake retry failed: %v", err)
	}
	defer browser.Close()
	if handshakes.Load() != 2 {
		t.Fatalf("handshakes=%d, want 2", handshakes.Load())
	}
	select {
	case <-firstCanceled:
	case <-time.After(time.Second):
		t.Fatal("first handshake connection survived retry")
	}
	profiles := strings.Fields(readFile(t, logPath))
	if len(profiles) != 2 || profiles[0] == profiles[1] {
		t.Fatalf("retry profiles=%v, want two fresh profiles", profiles)
	}
	pids := strings.Fields(readFile(t, logPath+".pids"))
	if len(pids) != 2 || pids[0] == pids[1] {
		t.Fatalf("retry process IDs=%v, want two fresh processes", pids)
	}
	if _, err := os.Stat(profiles[0]); !os.IsNotExist(err) {
		t.Fatalf("failed-attempt profile survived retry: %v", err)
	}
	if profiles[1] != browser.process.profileDir {
		t.Fatal("returned browser did not belong to the second process")
	}
	browser.Close()
	if _, err := os.Stat(profiles[1]); !os.IsNotExist(err) {
		t.Fatalf("successful-attempt profile survived close: %v", err)
	}
}

func TestStartDoesNotRetryPermanentHandshakeFailure(t *testing.T) {
	requirePOSIXShell(t)
	var handshakes atomic.Int32
	endpoint, _, _ := fakeCDPBeforeUpgrade(t, false, func(w http.ResponseWriter, r *http.Request) bool {
		handshakes.Add(1)
		http.Error(w, "invalid protocol", http.StatusBadRequest)
		return false
	})
	executable, logPath := handshakeChrome(t, endpoint)
	_, err := startWithPolicy(t.Context(), executable, fastPolicy(2))
	if err == nil || handshakes.Load() != 1 || countLogLines(t, logPath) != 1 {
		t.Fatalf("permanent handshake failure err=%v handshakes=%d", err, handshakes.Load())
	}
}

func TestStartHandshakeUsesRemainingAggregateBudget(t *testing.T) {
	requirePOSIXShell(t)
	var handshakes atomic.Int32
	canceled := make(chan struct{}, 2)
	endpoint, _, _ := fakeCDPBeforeUpgrade(t, false, func(w http.ResponseWriter, r *http.Request) bool {
		attempt := handshakes.Add(1)
		if attempt == 1 {
			<-r.Context().Done()
			canceled <- struct{}{}
			return false
		}
		// This response fits a fresh one-second attempt but cannot fit the
		// ~350ms remaining after the first attempt consumes its full second.
		select {
		case <-time.After(650 * time.Millisecond):
			return true
		case <-r.Context().Done():
			canceled <- struct{}{}
			return false
		}
	})
	executable, logPath := handshakeChrome(t, endpoint)
	policy := fastPolicy(2)
	policy.attemptTimeout = time.Second
	policy.overallTimeout = cleanupAllowance + 1350*time.Millisecond
	browser, err := startWithPolicy(t.Context(), executable, policy)
	if browser != nil {
		browser.Close()
		t.Fatal("second handshake incorrectly received a fresh attempt budget")
	}
	if err == nil || handshakes.Load() != 2 || countLogLines(t, logPath) != 2 {
		t.Fatalf("aggregate deadline err=%v handshakes=%d", err, handshakes.Load())
	}
	for range 2 {
		select {
		case <-canceled:
		case <-time.After(time.Second):
			t.Fatal("expired aggregate budget left a handshake connected")
		}
	}
	for _, profile := range strings.Fields(readFile(t, logPath)) {
		if _, err := os.Stat(profile); !os.IsNotExist(err) {
			t.Fatalf("expired aggregate budget left profile %q: %v", profile, err)
		}
	}
}

func TestStartCancellationDuringHandshakeDoesNotRetry(t *testing.T) {
	requirePOSIXShell(t)
	started := make(chan struct{})
	closed := make(chan struct{})
	endpoint, _, _ := fakeCDPBeforeUpgrade(t, false, func(w http.ResponseWriter, r *http.Request) bool {
		close(started)
		<-r.Context().Done()
		close(closed)
		return false
	})
	executable, logPath := handshakeChrome(t, endpoint)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := Start(ctx, executable)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("did not reach handshake")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled handshake error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not interrupt handshake")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("canceled handshake connection remained open")
	}
	if countLogLines(t, logPath) != 1 {
		t.Fatal("caller cancellation retried")
	}
}

func handshakeChrome(t *testing.T, endpoint string) (string, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "profiles.log")
	t.Setenv("GOSX_FAKE_LOG", logPath)
	t.Setenv("GOSX_FAKE_PIDS", logPath+".pids")
	t.Setenv("GOSX_FAKE_ENDPOINT", endpoint)
	executable := writeFakeChrome(t, `
printf '%s\n' "$$" >> "$GOSX_FAKE_PIDS"
for arg in "$@"; do
  case "$arg" in --user-data-dir=*) printf '%s\n' "${arg#--user-data-dir=}" >> "$GOSX_FAKE_LOG";; esac
done
printf 'DevTools listening on %s\n' "$GOSX_FAKE_ENDPOINT" >&2
exec tail -f /dev/null
`)
	return executable, logPath
}

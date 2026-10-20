package chrometest

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto"
)

func TestStartWaitsForInitialTabReadiness(t *testing.T) {
	requirePOSIXShell(t)
	var creates, activates atomic.Int32
	endpoint, _, _ := fakeCDPWithCommandError(t, false, nil, func(method string) *cdproto.Error {
		switch method {
		case "Target.createTarget":
			if creates.Add(1) == 1 {
				return &cdproto.Error{Code: -32000, Message: "Failed to open new tab - no browser is open"}
			}
		case "Target.activateTarget":
			activates.Add(1)
		}
		return nil
	})
	executable, logPath := handshakeChrome(t, endpoint)
	browser, err := startWithPolicy(t.Context(), executable, fastPolicy(2))
	if err != nil {
		t.Fatalf("initial tab readiness: %v", err)
	}
	defer browser.Close()
	if creates.Load() != 2 || activates.Load() != 1 || countLogLines(t, logPath) != 1 {
		t.Fatalf("creates=%d activates=%d launches=%d, want 2, 1, 1", creates.Load(), activates.Load(), countLogLines(t, logPath))
	}
}

func TestStartTabReadinessHonorsCallerDeadline(t *testing.T) {
	requirePOSIXShell(t)
	endpoint, _, closed := fakeCDPWithCommandError(t, false, nil, func(method string) *cdproto.Error {
		if method == "Target.createTarget" {
			return &cdproto.Error{Code: -32000, Message: "Failed to open new tab - no browser is open"}
		}
		return nil
	})
	executable, logPath := handshakeChrome(t, endpoint)
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	browser, err := startWithPolicy(ctx, executable, fastPolicy(2))
	if browser != nil {
		browser.Close()
		t.Fatal("unready browser returned")
	}
	if err != context.DeadlineExceeded || countLogLines(t, logPath) != 1 {
		t.Fatalf("err=%v launches=%d, want caller deadline and one launch", err, countLogLines(t, logPath))
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("unready tab connection survived cancellation")
	}
}

func TestStartDoesNotRetryOtherTargetErrors(t *testing.T) {
	requirePOSIXShell(t)
	endpoint, _, _ := fakeCDPWithCommandError(t, false, nil, func(method string) *cdproto.Error {
		if method == "Target.createTarget" {
			return &cdproto.Error{Code: -32000, Message: "invalid browser context"}
		}
		return nil
	})
	executable, logPath := handshakeChrome(t, endpoint)
	_, err := startWithPolicy(t.Context(), executable, fastPolicy(2))
	if err == nil || !strings.Contains(err.Error(), "invalid browser context") || countLogLines(t, logPath) != 1 {
		t.Fatalf("err=%v launches=%d, want permanent target failure and one launch", err, countLogLines(t, logPath))
	}
}

//go:build unix

package sidecar

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStopGracefulOnUnix(t *testing.T) {
	var output syncBuffer
	options := helperOptions("graceful", &output)
	options.ReadyLine = regexp.MustCompile(`^ready$`)
	p, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Stop(5 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("Wait after graceful stop = %v, want nil", err)
	}
	if !strings.Contains(output.String(), "graceful exit") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestStopKillsChildrenOnUnix(t *testing.T) {
	options := helperOptions("spawn", nil)
	options.ReadyLine = regexp.MustCompile(`^child (\d+)$`)
	p, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	child, err := strconv.Atoi(p.Ready())
	if err != nil {
		t.Fatalf("child pid %q: %v", p.Ready(), err)
	}
	if err := p.Stop(0); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(child, 0) != nil {
			return
		}
		// A killed child stays a zombie until its parent (the sidecar,
		// now gone) is reaped by init; treat a zombie as stopped.
		if state, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", child)); err == nil && strings.Contains(string(state), ") Z ") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(child, syscall.SIGKILL)
	t.Fatalf("grandchild %d still running after Stop", child)
}

func TestSidecarDiesWhenHostExitsWithoutStopOnLinux(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("needs /proc (Linux parent-death signal)")
	}
	options := helperOptions("host", nil)
	options.ReadyLine = regexp.MustCompile(`^sidecar (\d+)$`)
	host, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start host: %v", err)
	}
	sidecarPID, err := strconv.Atoi(host.Ready())
	if err != nil {
		t.Fatalf("sidecar pid %q: %v", host.Ready(), err)
	}
	_ = host.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", sidecarPID))
		if err != nil || strings.Contains(string(state), ") Z ") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(sidecarPID, syscall.SIGKILL)
	t.Fatalf("sidecar %d still running after its host exited", sidecarPID)
}

func TestStopKillsChildThatIgnoresSIGTERM(t *testing.T) {
	options := helperOptions("graceful-spawn", nil)
	options.ReadyLine = regexp.MustCompile(`^child (\d+)$`)
	p, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	child, err := strconv.Atoi(p.Ready())
	if err != nil {
		t.Fatalf("child pid %q: %v", p.Ready(), err)
	}
	// Let the child install its SIGTERM handler.
	time.Sleep(200 * time.Millisecond)
	if err := p.Stop(3 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("main process should exit cleanly on SIGTERM, got %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", child))
		if err != nil || strings.Contains(string(state), ") Z ") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(child, syscall.SIGKILL)
	t.Fatalf("child %d that ignores SIGTERM survived Stop", child)
}

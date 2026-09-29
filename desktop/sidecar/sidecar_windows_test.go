//go:build windows

package sidecar

import (
	"context"
	"regexp"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestStopKillsJobChildrenOnWindows(t *testing.T) {
	options := helperOptions("spawn", nil)
	options.ReadyLine = regexp.MustCompile(`^child (\d+)\r?$`)
	p, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	child, err := strconv.Atoi(p.Ready())
	if err != nil {
		t.Fatalf("child pid %q: %v", p.Ready(), err)
	}
	const synchronize, queryLimitedInformation = 0x00100000, 0x1000
	handle, err := syscall.OpenProcess(synchronize|queryLimitedInformation, false, uint32(child))
	if err != nil {
		t.Fatalf("OpenProcess(child): %v", err)
	}
	defer syscall.CloseHandle(handle)
	// The helper starts its child at once; it must already be in the job,
	// because the helper was suspended until it joined.
	var inJob int32
	isProcessInJob := syscall.NewLazyDLL("kernel32.dll").NewProc("IsProcessInJob")
	if ok, _, err := isProcessInJob.Call(uintptr(handle), uintptr(p.platform.job), uintptr(unsafe.Pointer(&inJob))); ok == 0 || inJob == 0 {
		t.Fatalf("grandchild %d is not in the sidecar job (ok=%d, err=%v)", child, ok, err)
	}
	if err := p.Stop(time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	event, err := syscall.WaitForSingleObject(handle, 5000)
	if err != nil || event != syscall.WAIT_OBJECT_0 {
		t.Fatalf("grandchild %d still running after Stop (wait=%d, err=%v)", child, event, err)
	}
}

func TestSidecarDiesWhenHostExitsWithoutStop(t *testing.T) {
	options := helperOptions("host", nil)
	options.ReadyLine = regexp.MustCompile(`^sidecar (\d+)\r?$`)
	host, err := Start(context.Background(), options)
	if err != nil {
		t.Fatalf("Start host: %v", err)
	}
	sidecarPID, err := strconv.Atoi(host.Ready())
	if err != nil {
		t.Fatalf("sidecar pid %q: %v", host.Ready(), err)
	}
	const synchronize = 0x00100000
	handle, err := syscall.OpenProcess(synchronize, false, uint32(sidecarPID))
	if err != nil {
		t.Fatalf("OpenProcess(sidecar): %v", err)
	}
	defer syscall.CloseHandle(handle)
	_ = host.Wait()
	event, err := syscall.WaitForSingleObject(handle, 5000)
	if err != nil || event != syscall.WAIT_OBJECT_0 {
		t.Fatalf("sidecar %d still running after its host exited (wait=%d, err=%v)", sidecarPID, event, err)
	}
}

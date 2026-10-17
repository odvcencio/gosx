//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"runtime"
	"testing"
	"unsafe"
)

// The assembly reads every ctrl and nativeEvent field through go_asm.h, so the
// Go declarations are the layout. These checks catch a edit that would change
// what the assembly means without changing what it assembles to.
func TestDarwinCtrlLayout(t *testing.T) {
	var c ctrl
	if got := unsafe.Offsetof(c.head); got != 0 {
		t.Errorf("head at %d, want 0", got)
	}
	if got := unsafe.Offsetof(c.tail); got != 64 {
		t.Errorf("tail at %d, want 64: head and tail must sit on separate cache lines", got)
	}
	if got := unsafe.Offsetof(c.mask); got < 128 {
		t.Errorf("mask at %d, want it past both counter lines", got)
	}
	if unsafe.Sizeof(c)%8 != 0 {
		t.Errorf("ctrl size %d is not a multiple of 8", unsafe.Sizeof(c))
	}

	var ev nativeEvent
	if got := unsafe.Sizeof(ev); got != 64 {
		t.Errorf("nativeEvent is %d bytes, want one 64-byte cache line", got)
	}
	for name, got := range map[string]uintptr{
		"kind": unsafe.Offsetof(ev.kind),
		"sel":  unsafe.Offsetof(ev.sel),
		"args": unsafe.Offsetof(ev.args),
		"mask": unsafe.Offsetof(ev.mask),
	} {
		want := map[string]uintptr{"kind": 0, "sel": 8, "args": 16, "mask": 40}[name]
		if got != want {
			t.Errorf("nativeEvent.%s at %d, want %d", name, got, want)
		}
	}
	if ringEvents&(ringEvents-1) != 0 {
		t.Errorf("ringEvents = %d is not a power of two", ringEvents)
	}
	if retainNone != 0 || retainArg2 != 4 {
		t.Errorf("retainNone = %d, retainArg2 = %d: the stubs retain argument i by bit i", retainNone, retainArg2)
	}
	if evNone != 0 {
		t.Errorf("evNone = %d: a zeroed ring slot must read as no event", evNone)
	}
}

// Every import has a trampoline whose address the assembly publishes, every shim
// and every delegate stub too, and no two differ in name only.
func TestDarwinTrampolinesPublished(t *testing.T) {
	fns := map[string]uintptr{
		"objc_msgSend":                      fnobjc_msgSend,
		"objc_getClass":                     fnobjc_getClass,
		"sel_registerName":                  fnsel_registerName,
		"objc_allocateClassPair":            fnobjc_allocateClassPair,
		"objc_registerClassPair":            fnobjc_registerClassPair,
		"class_addMethod":                   fnclass_addMethod,
		"class_addIvar":                     fnclass_addIvar,
		"class_getInstanceVariable":         fnclass_getInstanceVariable,
		"ivar_getOffset":                    fnivar_getOffset,
		"object_setInstanceVariable":        fnobject_setInstanceVariable,
		"objc_retain":                       fnobjc_retain,
		"objc_release":                      fnobjc_release,
		"objc_autoreleasePoolPush":          fnobjc_autoreleasePoolPush,
		"objc_autoreleasePoolPop":           fnobjc_autoreleasePoolPop,
		"NSApplicationLoad":                 fnNSApplicationLoad,
		"WKStringCreateWithUTF8CString":     fnWKStringCreateWithUTF8CString,
		"CFStringGetLength":                 fnCFStringGetLength,
		"CFStringGetMaximumSizeForEncoding": fnCFStringGetMaximumSizeForEncoding,
		"CFStringGetCString":                fnCFStringGetCString,
		"pthread_main_np":                   fnpthread_main_np,
		"pthread_sigmask":                   fnpthread_sigmask,
		"msgSendRect":                       fnMsgSendRect,
		"msgSend4F":                         fnMsgSend4F,
		"msgSendZeroFP3":                    fnMsgSendZeroFP3,
	}
	seen := map[uintptr]string{}
	for name, addr := range fns {
		if addr == 0 {
			t.Errorf("%s: trampoline address not published", name)
			continue
		}
		if other, dup := seen[addr]; dup {
			t.Errorf("%s and %s share address %#x", name, other, addr)
		}
		seen[addr] = name
	}
	for i, addr := range impStubs {
		if addr == 0 {
			t.Errorf("impStubs[%d]: stub address not published", i)
			continue
		}
		if other, dup := seen[addr]; dup {
			t.Errorf("impStubs[%d] and %s share address %#x", i, other, addr)
		}
		seen[addr] = "imp stub"
	}
	if !frameworksLinked() {
		t.Error("frameworksLinked() = false")
	}
	if got := pthreadMainNP(); got != 0 {
		t.Errorf("pthread_main_np() = %d on a test goroutine, want 0: tests must not run on the main thread", got)
	}
}

// The recorder reads the ctrl pointer from offset 8 of the delegate instance.
// registerDelegateClass asserts that when the class is registered; check it
// registers and that the answer is the one the assembly hard-wires.
func TestDarwinDelegateIvarOffset(t *testing.T) {
	cls, err := delegateClass()
	if err != nil {
		t.Fatalf("delegateClass: %v", err)
	}
	off, err := ivarOffsetOf(cls)
	if err != nil {
		t.Fatal(err)
	}
	if off != 8 {
		t.Fatalf("ctrl instance variable at offset %d, want 8", off)
	}
	for _, selector := range []string{
		"windowShouldClose:",
		"applicationShouldTerminate:",
		"webView:didFinishNavigation:",
		"webView:didFailNavigation:withError:",
		"webView:didFailProvisionalNavigation:withError:",
	} {
		sel := objcSel(selector)
		if sel == 0 {
			t.Errorf("sel_registerName(%q) = 0", selector)
			continue
		}
		// class_respondsToSelector via an instance: +instancesRespondToSelector:
		if !sendBool1(cls, objcSel("instancesRespondToSelector:"), sel) {
			t.Errorf("GoSXDelegate does not respond to %s", selector)
		}
	}
	// A method that was not registered must not be claimed: WebKit takes its
	// defaults for the delegate methods this slice leaves out.
	if sendBool1(cls, objcSel("instancesRespondToSelector:"), objcSel("webView:decidePolicyForNavigationAction:decisionHandler:")) {
		t.Error("GoSXDelegate claims decidePolicyForNavigationAction:decisionHandler:, which needs a block")
	}
}

func TestDarwinNativeRingRejectsBadCapacity(t *testing.T) {
	for _, capacity := range []int{0, -4, 3, 100} {
		if r, err := newNativeRing(capacity); err == nil {
			t.Errorf("newNativeRing(%d) = %v, want an error", capacity, r)
		}
	}
	r, err := newNativeRing(8)
	if err != nil {
		t.Fatal(err)
	}
	if r.c.mask != 7 || r.c.data == 0 || r.c.retain != fnobjc_retain {
		t.Errorf("ring not wired: mask=%d data=%#x retain=%#x", r.c.mask, r.c.data, r.c.retain)
	}
	if r.wakeArmed() {
		t.Error("a new ring has a wake armed")
	}
}

// Run blocks the asynchronous signals on the main thread so that the threads
// AppKit and WebKit start inherit the mask (lifecycle_darwin.go). The synchronous
// faults must stay deliverable, and the mask must come back when Run returns.
func TestDarwinBlockAsyncSignalsOnThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var before uint32
	if st := pthreadSigmask(sigBlock, nil, &before); st != 0 { // SIG_BLOCK with no set only reads
		t.Fatalf("pthread_sigmask read: %d", st)
	}
	defer pthreadSigmask(sigSetmask, &before, nil)

	restore := blockAsyncSignalsOnThread()
	var during uint32
	pthreadSigmask(sigBlock, nil, &during)
	if want := before | blockedAsyncSignals; during != want {
		t.Errorf("mask while blocked = %#x, want %#x", during, want)
	}
	for name, sig := range map[string]int{"SIGINT": 2, "SIGTERM": 15, "SIGURG": 16, "SIGCHLD": 20, "SIGPIPE": 13, "SIGPROF": 27} {
		if before&(1<<(sig-1)) == 0 && during&(1<<(sig-1)) == 0 {
			t.Errorf("%s is not blocked", name)
		}
	}
	for name, sig := range map[string]int{"SIGILL": 4, "SIGTRAP": 5, "SIGABRT": 6, "SIGFPE": 8, "SIGBUS": 10, "SIGSEGV": 11, "SIGSYS": 12} {
		if before&(1<<(sig-1)) == 0 && during&(1<<(sig-1)) != 0 {
			t.Errorf("synchronous signal %s was blocked", name)
		}
	}

	restore()
	var after uint32
	pthreadSigmask(sigBlock, nil, &after)
	if after != before {
		t.Errorf("mask after restore = %#x, want %#x", after, before)
	}
}

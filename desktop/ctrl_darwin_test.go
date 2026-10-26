//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"errors"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
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
	if retainNone != 0 || retainArg0 != 1 || retainArg2 != 4 {
		t.Errorf("retainNone = %d, retainArg0 = %d, retainArg2 = %d: the stubs retain argument i by bit i", retainNone, retainArg0, retainArg2)
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

// ---- window size limits: SetMinSize and SetMaxSize ----

// SetMinSize takes a positive size, and a refusal records nothing.
func TestDarwinSetMinSizeValidation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		ok            bool
	}{
		{"typical", 320, 200, true},
		{"smallest", 1, 1, true},
		{"large", 100000, 100000, true},
		{"zero width", 0, 200, false},
		{"zero height", 320, 0, false},
		{"zero both", 0, 0, false},
		{"negative width", -1, 200, false},
		{"negative height", 320, -1, false},
		{"negative both", -320, -200, false},
	} {
		impl, _ := newPlatformApp(Options{})
		da := impl.(*darwinApp)
		if err := da.SetMinSize(111, 77); err != nil { // an earlier, valid limit
			t.Fatal(err)
		}
		err := da.SetMinSize(tc.width, tc.height)
		if tc.ok {
			if err != nil || da.minWidth != tc.width || da.minHeight != tc.height {
				t.Errorf("%s: SetMinSize(%d, %d) = %v, recorded %dx%d; want nil and the new size", tc.name, tc.width, tc.height, err, da.minWidth, da.minHeight)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("%s: SetMinSize(%d, %d) = %v, want ErrInvalidOptions", tc.name, tc.width, tc.height, err)
		}
		if da.minWidth != 111 || da.minHeight != 77 {
			t.Errorf("%s: a refused SetMinSize changed the recorded limit to %dx%d", tc.name, da.minWidth, da.minHeight)
		}
	}
}

// SetMaxSize takes a size or zero (no limit on that axis), never a negative.
func TestDarwinSetMaxSizeValidation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		ok            bool
	}{
		{"typical", 2560, 1440, true},
		{"zero both removes the limit", 0, 0, true},
		{"zero width", 0, 700, true},
		{"zero height", 900, 0, true},
		{"negative width", -1, 700, false},
		{"negative height", 900, -1, false},
		{"negative both", -900, -700, false},
		{"negative with zero", -1, 0, false},
	} {
		impl, _ := newPlatformApp(Options{})
		da := impl.(*darwinApp)
		if err := da.SetMaxSize(1111, 777); err != nil {
			t.Fatal(err)
		}
		err := da.SetMaxSize(tc.width, tc.height)
		if tc.ok {
			if err != nil || da.maxWidth != tc.width || da.maxHeight != tc.height {
				t.Errorf("%s: SetMaxSize(%d, %d) = %v, recorded %dx%d; want nil and the new size", tc.name, tc.width, tc.height, err, da.maxWidth, da.maxHeight)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("%s: SetMaxSize(%d, %d) = %v, want ErrInvalidOptions", tc.name, tc.width, tc.height, err)
		}
		if da.maxWidth != 1111 || da.maxHeight != 777 {
			t.Errorf("%s: a refused SetMaxSize changed the recorded limit to %dx%d", tc.name, da.maxWidth, da.maxHeight)
		}
	}
}

// Before Run there is no window: both calls record and succeed, without touching
// AppKit from a test goroutine.
func TestDarwinWindowLimitsBeforeRunOnlyRecord(t *testing.T) {
	impl, _ := newPlatformApp(Options{})
	da := impl.(*darwinApp)
	if err := da.SetMinSize(300, 200); err != nil {
		t.Errorf("SetMinSize before Run = %v, want nil", err)
	}
	if err := da.SetMaxSize(900, 0); err != nil {
		t.Errorf("SetMaxSize before Run = %v, want nil", err)
	}
	if da.minWidth != 300 || da.minHeight != 200 || da.maxWidth != 900 || da.maxHeight != 0 {
		t.Errorf("recorded min %dx%d max %dx%d", da.minWidth, da.minHeight, da.maxWidth, da.maxHeight)
	}
}

// windowLimits reads the window's content size limits back from AppKit, by
// key-value coding (a method that returns an NSSize needs a return shim, DT2).
func windowLimits(da *darwinApp) (minSize, maxSize [2]float64) {
	da.onUIThread(func() error {
		withPool(func() {
			read := func(key string) (size [2]float64) {
				getValue(send1(da.window, objcSel("valueForKey:"), nsString(key)), unsafe.Pointer(&size), unsafe.Sizeof(size))
				return size
			}
			minSize, maxSize = read("contentMinSize"), read("contentMaxSize")
		})
		return nil
	})
	return minSize, maxSize
}

// setContentSizeOf resizes the window's content from code and reports what it
// became.
func setContentSizeOf(da *darwinApp, width, height float64) (w, h float64) {
	da.onUIThread(func() error {
		withPool(func() {
			sendSize(da.window, objcSel("setContentSize:"), width, height)
			r := frameOf(send0(da.window, objcSel("contentView")))
			w, h = r.w, r.h
		})
		return nil
	})
	return w, h
}

// The limits reach the NSWindow: set before Run they are in place when the window
// exists, and after it they replace each other, axis by axis.
func TestDarwinWindowLimitsReachTheWindow(t *testing.T) {
	const unbounded = math.MaxFloat32
	var atCreation [2][2]float64
	var daRef atomic.Pointer[darwinApp]

	runTestAppBefore(t, Options{Width: 640, Height: 480, OnWindowCreated: func(*Window) {
		min, max := windowLimits(daRef.Load())
		atCreation = [2][2]float64{min, max}
	}}, func(app *App, da *darwinApp) {
		daRef.Store(da)
		if err := app.SetMinSize(300, 200); err != nil {
			t.Errorf("SetMinSize before Run: %v", err)
		}
		if err := app.SetMaxSize(900, 0); err != nil {
			t.Errorf("SetMaxSize before Run: %v", err)
		}
	}, func(app *App, da *darwinApp) {
		check := func(what string, wantMin, wantMax [2]float64) {
			t.Helper()
			if min, max := windowLimits(da); min != wantMin || max != wantMax {
				t.Errorf("%s: min %v max %v, want min %v max %v", what, min, max, wantMin, wantMax)
			}
		}
		check("limits set before Run", [2]float64{300, 200}, [2]float64{900, unbounded})

		if err := app.SetMinSize(320, 240); err != nil {
			t.Errorf("SetMinSize: %v", err)
		}
		if err := app.SetMaxSize(1024, 768); err != nil {
			t.Errorf("SetMaxSize: %v", err)
		}
		check("both replaced", [2]float64{320, 240}, [2]float64{1024, 768})

		if err := app.SetMaxSize(0, 0); err != nil {
			t.Errorf("SetMaxSize(0, 0): %v", err)
		}
		check("max removed, min kept", [2]float64{320, 240}, [2]float64{unbounded, unbounded})

		if err := app.SetMaxSize(0, 600); err != nil {
			t.Errorf("SetMaxSize(0, 600): %v", err)
		}
		check("max on one axis", [2]float64{320, 240}, [2]float64{unbounded, 600})

		// Refusals leave the window as it was.
		for name, err := range map[string]error{
			"SetMinSize(0, 100)":  app.SetMinSize(0, 100),
			"SetMinSize(-5, 100)": app.SetMinSize(-5, 100),
			"SetMaxSize(-1, 5)":   app.SetMaxSize(-1, 5),
		} {
			if !errors.Is(err, ErrInvalidOptions) {
				t.Errorf("%s = %v, want ErrInvalidOptions", name, err)
			}
		}
		check("after refused calls", [2]float64{320, 240}, [2]float64{unbounded, 600})

		// The limit is enforced, not just stored: the window cannot be made taller
		// than the maximum, from code or by the user.
		if err := app.SetMaxSize(800, 600); err != nil {
			t.Errorf("SetMaxSize: %v", err)
		}
		if w, h := setContentSizeOf(da, 5000, 5000); w > 800 || h > 600 {
			t.Errorf("content size after asking for 5000x5000 with a 800x600 maximum = %vx%v", w, h)
		}
		if w, h := setContentSizeOf(da, 500, 400); w != 500 || h != 400 {
			t.Errorf("content size inside the limits = %vx%v, want 500x400", w, h)
		}

		// From many goroutines at once, through the dispatcher.
		var wg sync.WaitGroup
		var failures atomic.Int32
		for g := 1; g <= 8; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				for i := 0; i < 10; i++ {
					if app.SetMinSize(100+g, 100+g) != nil || app.SetMaxSize(2000+g, 2000+g) != nil {
						failures.Add(1)
					}
				}
			}(g)
		}
		wg.Wait()
		if failures.Load() != 0 {
			t.Errorf("%d concurrent calls failed", failures.Load())
		}
		min, max := windowLimits(da)
		if min != [2]float64{float64(da.minWidth), float64(da.minHeight)} || max != [2]float64{float64(da.maxWidth), float64(da.maxHeight)} {
			t.Errorf("window limits %v %v do not match the last recorded ones %dx%d %dx%d", min, max, da.minWidth, da.minHeight, da.maxWidth, da.maxHeight)
		}
	})
	if want := [2][2]float64{{300, 200}, {900, unbounded}}; atCreation != want {
		t.Errorf("limits when the window was created = %v, want %v: setup must apply what was set before Run", atCreation, want)
	}
}

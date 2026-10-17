//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"sync"
	"testing"
	"unsafe"
)

// fakeSelf is memory laid out the way the recorder sees a GoSXDelegate instance:
// an isa word, then the ctrl pointer at offset 8. The recorder never touches
// anything else of self, so a test can call it with this instead of an object.
type fakeSelf struct {
	isa  uintptr
	ctrl uintptr
}

func newFakeSelf(r *nativeRing) *fakeSelf {
	return &fakeSelf{ctrl: uintptr(unsafe.Pointer(r.c))}
}

// callStub calls a delegate stub through the C ABI, the way AppKit does:
// IMP(self, _cmd, a0, a1, a2). It returns the stub's return value.
func callStub(stub uintptr, self *fakeSelf, sel, a0, a1, a2 uintptr) uintptr {
	r, _, _ := syscall_syscall6(stub, uintptr(unsafe.Pointer(self)), sel, a0, a1, a2, 0)
	return r
}

func newTestRing(t *testing.T, capacity int) *nativeRing {
	t.Helper()
	r, err := newNativeRing(capacity)
	if err != nil {
		t.Fatalf("newNativeRing(%d): %v", capacity, err)
	}
	return r
}

// retainCount is -[NSObject retainCount], which is exact for the plain objects
// these tests make.
func retainCount(obj uintptr) uintptr { return send0(obj, objcSel("retainCount")) }

func drainAll(r *nativeRing) []nativeEvent {
	var got []nativeEvent
	r.drain(func(ev nativeEvent) { got = append(got, ev) })
	return got
}

func TestRecorderRecordsEachStubsEvent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stub   int
		kind   uint64
		mask   uint64
		retain bool
	}{
		{"windowShouldClose", impStubWindowShouldClose, evCloseRequested, 0, false},
		{"applicationShouldTerminate", impStubApplicationShouldTerminate, evQuitRequested, 0, false},
		{"didFinishNavigation", impStubNavigationFinished, evNavigationFinished, 0, false},
		{"didFailNavigation", impStubNavigationFailed, evNavigationFailed, retainArg2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRing(t, 8)
			self := newFakeSelf(r)
			obj := objcNew(objcClass("NSObject"))
			defer objcRelease(obj)
			sel := objcSel("probe:")
			if got := retainCount(obj); got != 1 {
				t.Fatalf("fresh object retainCount = %d, want 1", got)
			}

			// Arguments 0 and 1 are plain integers (the recorder must not
			// retain them, or objc_retain would crash on them); argument 2 is
			// the object the failure stub retains.
			ret := callStub(impStubs[tc.stub], self, sel, 0x1111, 0x2222, obj)
			if ret != 0 {
				t.Errorf("stub returned %#x, want 0 (NO / NSTerminateCancel / void)", ret)
			}
			if got := r.pending(); got != 1 {
				t.Fatalf("pending = %d, want 1", got)
			}
			if r.c.head != 1 || r.c.tail != 0 {
				t.Errorf("head/tail = %d/%d, want 1/0", r.c.head, r.c.tail)
			}
			want := uint64(0)
			if tc.retain {
				want = 1
			}
			if r.c.retains != want {
				t.Errorf("retains = %d, want %d", r.c.retains, want)
			}
			wantCount := uintptr(1 + want)
			if got := retainCount(obj); got != wantCount {
				t.Errorf("retainCount after the call = %d, want %d", got, wantCount)
			}

			events := drainAll(r)
			if len(events) != 1 {
				t.Fatalf("drained %d events, want 1", len(events))
			}
			ev := events[0]
			if ev.kind != tc.kind || ev.sel != sel || ev.mask != tc.mask {
				t.Errorf("event = {kind %d sel %#x mask %#x}, want {kind %d sel %#x mask %#x}", ev.kind, ev.sel, ev.mask, tc.kind, sel, tc.mask)
			}
			if ev.args != [3]uintptr{0x1111, 0x2222, obj} {
				t.Errorf("args = %#x, want {0x1111 0x2222 %#x}", ev.args, obj)
			}
			if got := retainCount(obj); got != 1 {
				t.Errorf("retainCount after drain = %d, want 1: drain must release what the recorder retained", got)
			}
			calls, dropped, retains, releases := r.stats()
			if calls != 1 || dropped != 0 || retains != releases || retains != want {
				t.Errorf("stats calls=%d dropped=%d retains=%d releases=%d", calls, dropped, retains, releases)
			}
			if r.c.tail != 1 {
				t.Errorf("tail = %d after drain, want 1", r.c.tail)
			}
		})
	}
}

func TestRecorderKeepsOrderAndWrapsTheRing(t *testing.T) {
	r := newTestRing(t, 4)
	self := newFakeSelf(r)
	stub := impStubs[impStubNavigationFinished]
	next := uintptr(1)
	for round := 0; round < 10; round++ { // 30 records through a 4-slot ring
		for i := 0; i < 3; i++ {
			callStub(stub, self, objcSel("probe:"), next, 0, 0)
			next++
		}
		events := drainAll(r)
		if len(events) != 3 {
			t.Fatalf("round %d: drained %d events, want 3", round, len(events))
		}
		for i, ev := range events {
			want := next - 3 + uintptr(i)
			if ev.args[0] != want {
				t.Fatalf("round %d event %d: arg0 = %d, want %d", round, i, ev.args[0], want)
			}
		}
	}
	if r.c.head != 30 || r.c.tail != 30 {
		t.Errorf("head/tail = %d/%d, want 30/30", r.c.head, r.c.tail)
	}
}

func TestRecorderFullRingDropsAndCounts(t *testing.T) {
	r := newTestRing(t, 4)
	self := newFakeSelf(r)
	obj := objcNew(objcClass("NSObject"))
	defer objcRelease(obj)
	stub := impStubs[impStubNavigationFailed]
	sel := objcSel("probe:")

	for i := uintptr(1); i <= 4; i++ {
		if ret := callStub(stub, self, sel, i, 0, obj); ret != 0 {
			t.Fatalf("call %d returned %#x", i, ret)
		}
	}
	if got := retainCount(obj); got != 5 {
		t.Fatalf("retainCount with a full ring = %d, want 5 (1 + 4 records)", got)
	}
	// The ring is full. Further calls must drop: no record, no retain, no
	// overwrite of the oldest record, and still return the conservative answer.
	for i := uintptr(100); i < 103; i++ {
		if ret := callStub(stub, self, sel, i, 0, obj); ret != 0 {
			t.Errorf("dropped call returned %#x, want 0", ret)
		}
	}
	if r.c.dropped != 3 {
		t.Errorf("dropped = %d, want 3", r.c.dropped)
	}
	if r.c.head != 4 || r.c.retains != 4 || r.c.calls != 4 {
		t.Errorf("head=%d retains=%d calls=%d after drops, want 4/4/4", r.c.head, r.c.retains, r.c.calls)
	}
	if got := retainCount(obj); got != 5 {
		t.Errorf("retainCount = %d after dropped calls, want still 5: a dropped record must not retain", got)
	}

	events := drainAll(r)
	if len(events) != 4 {
		t.Fatalf("drained %d, want 4", len(events))
	}
	for i, ev := range events {
		if ev.args[0] != uintptr(i+1) {
			t.Errorf("event %d arg0 = %d, want %d: a dropped record overwrote the ring", i, ev.args[0], i+1)
		}
	}
	if got := retainCount(obj); got != 1 {
		t.Errorf("retainCount after drain = %d, want 1", got)
	}

	// Draining frees the ring: records are accepted again and dropped stays put.
	callStub(stub, self, sel, 7, 0, obj)
	if r.pending() != 1 || r.c.dropped != 3 {
		t.Errorf("pending=%d dropped=%d after the ring drained, want 1/3", r.pending(), r.c.dropped)
	}
	drainAll(r)
}

func TestRecorderToleratesAnUnwiredInstance(t *testing.T) {
	// A delegate instance whose ctrl variable is still nil must be answered, not
	// crash AppKit.
	self := &fakeSelf{}
	for i, stub := range impStubs {
		if ret := callStub(stub, self, objcSel("probe:"), 1, 2, 3); ret != 0 {
			t.Errorf("stub %d returned %#x for an unwired instance, want 0", i, ret)
		}
	}
}

func TestRecorderWithoutRetainFunctionRetainsNothing(t *testing.T) {
	r := newTestRing(t, 4)
	r.c.retain = 0
	self := newFakeSelf(r)
	obj := objcNew(objcClass("NSObject"))
	defer objcRelease(obj)
	callStub(impStubs[impStubNavigationFailed], self, objcSel("probe:"), 0, 0, obj)
	if got := retainCount(obj); got != 1 {
		t.Errorf("retainCount = %d, want 1: no retain function, no retain", got)
	}
	events := drainAll(r)
	if len(events) != 1 || events[0].mask != 0 {
		t.Fatalf("events = %+v, want one record with an empty retain mask so Go releases nothing", events)
	}
	if got := retainCount(obj); got != 1 {
		t.Errorf("retainCount after drain = %d, want 1", got)
	}
}

// The recorder wakes the event pump with [NSApp postEvent:atStart:YES]. Point the
// same four registers at an NSMutableArray and check the call it makes:
// objc_msgSend(array, insertObject:atIndex:, object, 1).
func TestRecorderWakeCallsThroughTheControlBlock(t *testing.T) {
	r := newTestRing(t, 4)
	self := newFakeSelf(r)
	array := send0(send0(objcClass("NSMutableArray"), objcSel("alloc")), objcSel("init"))
	defer objcRelease(array)
	first := objcNew(objcClass("NSObject"))
	defer objcRelease(first)
	wake := objcNew(objcClass("NSObject"))
	defer objcRelease(wake)
	send1(array, objcSel("addObject:"), first)

	r.c.wakeSelf = array
	r.c.wakeSel = objcSel("insertObject:atIndex:")
	r.c.wakeEvent = wake
	r.c.wakeFn = fnobjc_msgSend

	callStub(impStubs[impStubWindowShouldClose], self, objcSel("probe:"), 0, 0, 0)

	if got := send0(array, objcSel("count")); got != 2 {
		t.Fatalf("array count = %d, want 2: the recorder did not call wakeFn", got)
	}
	if got := send1(array, objcSel("objectAtIndex:"), 1); got != wake {
		t.Errorf("array[1] = %#x, want the wake event %#x (atIndex: must be YES = 1)", got, wake)
	}

	// A full ring posts no wake: nothing new needs draining.
	for i := 0; i < 8; i++ {
		callStub(impStubs[impStubWindowShouldClose], self, objcSel("probe:"), 0, 0, 0)
	}
	if r.c.dropped == 0 {
		t.Fatal("setup: expected the ring to fill")
	}
	count := send0(array, objcSel("count"))
	if want := uintptr(2 + (4 - 1)); count != want { // 3 more records fit in a 4-slot ring
		t.Errorf("array count = %d after filling the ring, want %d: one wake per record, none for drops", count, want)
	}
	drainAll(r)
}

// End to end through the Objective-C runtime: a real GoSXDelegate instance,
// messaged with objc_msgSend, dispatches to the assembly methods, which record
// into the ring the instance points at.
func TestRecorderRealDelegateInstanceRecordsThroughTheRuntime(t *testing.T) {
	cls, err := delegateClass()
	if err != nil {
		t.Fatal(err)
	}
	r := newTestRing(t, 8)
	delegate, err := newDelegate(cls, r)
	if err != nil {
		t.Fatal(err)
	}
	defer objcRelease(delegate)

	if ret := send1(delegate, objcSel("windowShouldClose:"), 0xAAAA); ret&0xff != 0 {
		t.Errorf("windowShouldClose: = %#x, want NO", ret)
	}
	if ret := send1(delegate, objcSel("applicationShouldTerminate:"), 0xBBBB); ret != 0 {
		t.Errorf("applicationShouldTerminate: = %#x, want NSTerminateCancel (0)", ret)
	}
	send2(delegate, objcSel("webView:didFinishNavigation:"), 0xC1, 0xC2)
	errCode := -999 // NSURLErrorCancelled
	var errObj uintptr
	withPool(func() {
		errObj = objcRetain(send3(objcClass("NSError"), objcSel("errorWithDomain:code:userInfo:"), nsString("gosx.test"), uintptr(errCode), 0))
	})
	defer objcRelease(errObj)
	send3(delegate, objcSel("webView:didFailNavigation:withError:"), 0xD1, 0xD2, errObj)
	send3(delegate, objcSel("webView:didFailProvisionalNavigation:withError:"), 0xE1, 0xE2, errObj)

	if got := retainCount(errObj); got != 3 {
		t.Errorf("NSError retainCount = %d, want 3 (ours + two records)", got)
	}
	var codes []int
	var kinds []uint64
	r.drain(func(ev nativeEvent) {
		kinds = append(kinds, ev.kind)
		if ev.kind == evNavigationFailed {
			codes = append(codes, int(int64(send0(ev.args[2], objcSel("code")))))
		}
	})
	wantKinds := []uint64{evCloseRequested, evQuitRequested, evNavigationFinished, evNavigationFailed, evNavigationFailed}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("kinds = %v, want %v", kinds, wantKinds)
	}
	for i := range wantKinds {
		if kinds[i] != wantKinds[i] {
			t.Errorf("kinds[%d] = %d, want %d", i, kinds[i], wantKinds[i])
		}
	}
	if len(codes) != 2 || codes[0] != errCode || codes[1] != errCode {
		t.Errorf("error codes read through the retained NSError = %v, want two of %d", codes, errCode)
	}
	if got := retainCount(errObj); got != 1 {
		t.Errorf("NSError retainCount after drain = %d, want 1", got)
	}
}

// The delegate's callbacks arrive on one thread in practice, but the ring is a
// proper single-producer, single-consumer queue: a consumer on another thread
// sees every record exactly once and in order.
func TestRecorderRingHandsOffAcrossThreads(t *testing.T) {
	r := newTestRing(t, 64)
	self := newFakeSelf(r)
	const total = 5000
	stub := impStubs[impStubNavigationFinished]

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := uintptr(1); i <= total; {
			// A dropped record is the full-ring path; retry until accepted.
			dropped := r.c.dropped
			callStub(stub, self, objcSel("probe:"), i, 0, 0)
			if r.c.dropped == dropped {
				i++
			}
		}
	}()
	var got uintptr
	for got < total {
		r.drain(func(ev nativeEvent) {
			got++
			if ev.args[0] != got {
				t.Fatalf("record %d carries %d: the ring lost, duplicated or reordered a record", got, ev.args[0])
			}
		})
	}
	wg.Wait()
	if r.pending() != 0 {
		t.Errorf("pending = %d, want 0", r.pending())
	}
}

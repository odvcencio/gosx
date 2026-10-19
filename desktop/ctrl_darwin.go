//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// ctrl is the hand-off block between AppKit's delegate callbacks and Go, and
// the only state the assembly recorder can see.
//
// An Objective-C delegate method is a C-ABI function that AppKit or WebKit
// calls with (self, _cmd, args...) and no refcon. Go cannot be that function:
// when AppKit calls it, the Go main thread (m0) is inside a syscall.syscall
// call on the system stack with the goroutine parked in syscall state, which is
// not a place to run Go code without cgo's cgocallback machinery, and a
// CGO_ENABLED=0 binary has none. So every delegate method is a few lines of
// assembly (imp_darwin_arm64.s, imp_darwin_amd64.s) that appends a fixed-size
// record to a ring, retains the Objective-C arguments Go will read later, and
// returns a conservative answer. Go drains the ring on its own thread right
// after the event pump returns (drainNativeEvents in app_darwin.go).
//
// Nothing the recorder touches may be Go memory the garbage collector could
// move or free. The ctrl block and the ring behind data live in one mmap arena
// outside the Go heap, as Tymbal's Core Audio ring does. The recorder finds the
// block through the delegate object itself: an instance variable "ctrl" at
// offset 8 of the GoSXDelegate instance (objc_darwin.go asserts that offset when
// the class is registered), so the delegate method needs no global symbol.
//
// One producer and one consumer, as in Tymbal's internal/ring:
//
//   - The recorder writes a record, retains its arguments, then publishes it
//     with a release store to head. It reads tail with acquire semantics to see
//     whether the ring has room.
//   - Go reads head with acquire semantics, handles the record, releases what
//     the recorder retained, then publishes tail with a release store.
//
// AppKit delivers every delegate method on the main thread, which is also the
// thread that drains the ring, so in practice both sides share a thread and the
// ordering is free; the acquire and release halves cost nothing on the main
// path and keep the ring correct if a method ever arrives elsewhere.
//
// Positions count records, never wrap, and are reduced with mask. The assembly
// reads every field through go_asm.h, so this declaration is the layout. Keep
// head and tail on separate cache lines.
type ctrl struct {
	head uint64 // records the recorder has published, ever; written only by the recorder
	_    [56]byte
	tail uint64 // records Go has consumed, ever; written only by Go
	_    [56]byte

	mask    uint64  // ring capacity in records, minus one; the capacity is a power of two
	data    uintptr // ring base
	retain  uintptr // objc_retain: the recorder calls it once per retained argument
	dropped uint64  // records dropped because the ring was full; written only by the recorder
	calls   uint64  // records appended; written only by the recorder
	retains uint64  // object retains and Block copies; written only by the recorder

	// Wake. WebKit calls its delegate from a run-loop source, not from an
	// NSEvent, so the event pump that is waiting in AppKit does not return when a
	// record is appended and Go would not see it until some unrelated event came
	// in. After publishing a record the recorder therefore calls
	// wakeFn(wakeSelf, wakeSel, wakeEvent, YES), which is objc_msgSend(NSApp,
	// @selector(postEvent:atStart:), event, YES) with a pre-built
	// application-defined NSEvent: the same thing -[NSApplication stop:] does to
	// make a pump return. wakeFn == 0 posts nothing.
	wakeFn    uintptr
	wakeSelf  uintptr
	wakeSel   uintptr
	wakeEvent uintptr
	blockCopy uintptr // _Block_copy, for asynchronous WKUIDelegate completions
}

// nativeEvent is one ring record: what a delegate method was called with. Its
// original offsets and 64-byte size stay fixed; WKUIDelegate uses one reserved
// word for a copied completion Block. Other events ignore that word.
type nativeEvent struct {
	kind       uint64     // an ev* constant, chosen by the stub that entered the recorder
	sel        uintptr    // the selector the method was invoked for (_cmd)
	args       [3]uintptr // the first three arguments after self and _cmd
	mask       uint64     // bit i set: args[i] was retained by the recorder, and Go must release it
	completion uintptr    // copied native Block; uses one formerly reserved word
	_          uint64
}

// Event kinds. The stubs in imp_darwin_*.s load these through go_asm.h.
const (
	evNone               uint64 = 0
	evCloseRequested     uint64 = 1 // windowShouldClose:
	evQuitRequested      uint64 = 2 // applicationShouldTerminate:
	evNavigationFinished uint64 = 3 // webView:didFinishNavigation:
	evNavigationFailed   uint64 = 4 // webView:didFailNavigation:withError: and ...didFailProvisionalNavigation:withError:
	evMenuAction         uint64 = 5 // menuAction: sent to the GoSXMenuTarget by a menu item (menu_darwin.go)
	evOpenFiles          uint64 = 6 // application:openFiles:
	evOpenURLs           uint64 = 7 // application:openURLs:
	evWebMessage         uint64 = 8 // userContentController:didReceiveScriptMessage:
	evWebFilePanel       uint64 = 9
	evWebAlert           uint64 = 10
	evWebConfirm         uint64 = 11
	evWebPrompt          uint64 = 12
)

// Retain masks: bit i retains argument i, while copyCompletion owns the native
// completion Block. The numeric values appear in the assembly stubs.
const (
	retainNone     uint64 = 0
	retainArg0     uint64 = 1 << 0 // the sender (the NSMenuItem) of menuAction:
	retainArg1     uint64 = 1 << 1 // document arrays and WKScriptMessage
	retainArg2     uint64 = 1 << 2 // the NSError of the didFail methods
	copyCompletion uint64 = 1 << 8 // completion is copied with _Block_copy, not objc_retain
)

// ringEvents is the ring's capacity in records. A page load produces a handful
// of records and Go drains after every event the pump returns, so 256 leaves the
// "ring full" path for a stuck host only.
const ringEvents = 256

// The recorder and its stubs, in assembly. They are never called from Go
// (tests excepted, through syscall_syscall6); each is declared so that go vet
// can check the assembly's frame against it. AppKit reaches them through
// impStubs, because the func value of an assembly function points at an
// ABIInternal wrapper rather than at the C-ABI entry.
func impRecord()
func impWindowShouldClose()
func impApplicationShouldTerminate()
func impNavigationFinished()
func impNavigationFailed()
func impMenuAction()
func impOpenFiles()
func impOpenURLs()
func impWebMessage()
func impWebUIRecord()
func impWebFilePanel()
func impWebAlert()
func impWebConfirm()
func impWebPrompt()

// impStubs holds the C-ABI entry address of each stub, published by the
// assembly: windowShouldClose:, applicationShouldTerminate:,
// webView:didFinishNavigation:, the two didFail methods, and menuAction:.
var impStubs [impStubCount]uintptr

const (
	impStubWindowShouldClose = iota
	impStubApplicationShouldTerminate
	impStubNavigationFinished
	impStubNavigationFailed
	impStubMenuAction
	impStubOpenFiles
	impStubOpenURLs
	impStubWebMessage
	impStubWebFilePanel
	impStubWebAlert
	impStubWebConfirm
	impStubWebPrompt
	impStubCount
)

// nativeRing is the process-wide event ring. It is carved out of one anonymous
// mapping and, once created, never unmapped: a delegate object that outlives
// its Run (an NSApplication keeps its delegate pointer until told otherwise)
// must never point at freed memory.
type nativeRing struct {
	mem []byte
	c   *ctrl
	buf unsafe.Pointer // ring base, == c.data
	// releases counts objects Go released on behalf of the recorder. With
	// c.retains it shows a leak: they must be equal once the ring is drained.
	releases atomic.Uint64
}

var (
	ringOnce sync.Once
	ringVal  *nativeRing
	ringErr  error
)

// sharedRing returns the process-wide ring, creating it on first use.
func sharedRing() (*nativeRing, error) {
	ringOnce.Do(func() { ringVal, ringErr = newNativeRing(ringEvents) })
	return ringVal, ringErr
}

// newNativeRing maps an arena holding a ctrl block and a ring of capacity
// records (a power of two), wires the retain function and returns it.
func newNativeRing(capacity int) (*nativeRing, error) {
	if capacity <= 0 || capacity&(capacity-1) != 0 {
		return nil, fmt.Errorf("desktop: native ring capacity %d is not a power of two", capacity)
	}
	const align = 64
	ctrlSize := (int(unsafe.Sizeof(ctrl{})) + align - 1) &^ (align - 1)
	size := ctrlSize + capacity*int(unsafe.Sizeof(nativeEvent{}))
	page := syscall.Getpagesize()
	size = (size + page - 1) &^ (page - 1)
	mem, err := syscall.Mmap(-1, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_ANON)
	if err != nil {
		return nil, fmt.Errorf("desktop: map native event ring: %w", err)
	}
	r := &nativeRing{mem: mem}
	r.c = (*ctrl)(unsafe.Pointer(&mem[0]))
	r.buf = unsafe.Pointer(&mem[ctrlSize])
	r.c.mask = uint64(capacity - 1)
	r.c.data = uintptr(r.buf)
	r.c.retain = fnobjc_retain
	r.c.blockCopy = fnBlockCopy
	return r, nil
}

// event returns the record at position pos.
func (r *nativeRing) event(pos uint64) *nativeEvent {
	return (*nativeEvent)(unsafe.Add(r.buf, int((pos&r.c.mask)*uint64(unsafe.Sizeof(nativeEvent{})))))
}

// pending reports how many published records Go has not consumed.
func (r *nativeRing) pending() uint64 {
	return atomic.LoadUint64(&r.c.head) - atomic.LoadUint64(&r.c.tail)
}

// drain hands every published record to handle in order, then releases the
// objects the recorder retained for it and publishes the consumed position.
// handle runs after the record is copied out, so it may take as long as it
// needs without holding up the ring. drain returns the number of records
// handled.
func (r *nativeRing) drain(handle func(nativeEvent)) int {
	n := 0
	for {
		tail := atomic.LoadUint64(&r.c.tail)
		if tail == atomic.LoadUint64(&r.c.head) {
			return n
		}
		ev := *r.event(tail)
		if handle != nil {
			handle(ev)
		} else if ev.mask&copyCompletion != 0 {
			// A stale Run or shutdown must resolve WebKit's outstanding request.
			invokeNativeBlock(ev.completion, 0)
		}
		if ev.mask&copyCompletion != 0 {
			releaseNativeBlock(ev.completion)
			r.releases.Add(1)
		}
		for i, arg := range ev.args {
			if ev.mask&(1<<uint(i)) != 0 {
				objcRelease(arg)
				r.releases.Add(1)
			}
		}
		atomic.StoreUint64(&r.c.tail, tail+1)
		n++
	}
}

// armWake points the recorder at [app postEvent:event atStart:YES]. event must
// be an application-defined NSEvent that the caller has retained; the ring keeps
// it for the life of the process. Call it before any delegate can be invoked.
func (r *nativeRing) armWake(app, event uintptr) {
	r.c.wakeSelf = app
	r.c.wakeSel = objcSel("postEvent:atStart:")
	r.c.wakeEvent = event
	r.c.wakeFn = fnobjc_msgSend
}

// wakeArmed reports whether the recorder will post a wake event.
func (r *nativeRing) wakeArmed() bool { return r.c.wakeFn != 0 }

// stats reports the ring's counters.
func (r *nativeRing) stats() (calls, dropped, retains, releases uint64) {
	return atomic.LoadUint64(&r.c.calls), atomic.LoadUint64(&r.c.dropped),
		atomic.LoadUint64(&r.c.retains), r.releases.Load()
}

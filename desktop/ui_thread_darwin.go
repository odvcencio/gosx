//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"fmt"
	"runtime"
)

// The darwin window thread is the process's main thread, so the dispatcher's
// notion of a thread ID is just whether pthread_main_np says "main": the owner
// is 1, every other thread is 0. Calls made on the main thread run directly;
// calls from other goroutines are queued and the main loop is woken with an
// application-defined NSEvent.

const (
	dispatchMainThread  uint32 = 1
	dispatchOtherThread uint32 = 0
)

// currentThreadID is the dispatcher's current(): 1 on the main thread.
func currentThreadID() uint32 {
	if pthreadMainNP() != 0 {
		return dispatchMainThread
	}
	return dispatchOtherThread
}

// onUIThread runs fn on the window thread (see uiDispatcher).
func (a *darwinApp) onUIThread(fn func() error) error {
	return a.dispatch.run(fn)
}

// startDispatch makes the main thread the dispatcher's owner; call it on that
// thread once the window exists.
func (a *darwinApp) startDispatch() {
	a.dispatch.current = currentThreadID
	a.dispatch.wake = a.postWakeEvent
	a.dispatch.start(dispatchMainThread)
}

func (a *darwinApp) stopDispatch() { a.dispatch.stop() }

func (a *darwinApp) drainDispatchQueue() { a.dispatch.drain() }

// newWakeEvent returns an autoreleased application-defined NSEvent with a zero
// location and timestamp: +[NSEvent otherEventWithType:location:modifierFlags:
// timestamp:windowNumber:context:subtype:data1:data2:]. The NSPoint location and
// the timestamp are floating-point arguments that must be zero; msgSendZeroFP3
// clears V0-V2 / XMM0-XMM2, and the nine integer-class arguments travel through
// syscall9: self, _cmd, type, modifierFlags, windowNumber, context, subtype,
// data1, data2. The caller must hold an autorelease pool on its thread.
func newWakeEvent() uintptr {
	event, _, _ := syscall_syscall9(fnMsgSendZeroFP3, objcClass("NSEvent"),
		objcSel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
		nsEventTypeApplicationDefined, 0, 0, 0, 0, 0, 0)
	return event
}

// postWakeEvent asks the main loop to call drain by posting an
// application-defined NSEvent, the shape of glfwPostEmptyEvent. It may run on
// any goroutine: +[NSEvent otherEventWithType:...] and -[NSApplication
// postEvent:atStart:] are thread-safe, and the event is queued even while the
// main thread is deep inside a modal run loop (it is delivered when that loop
// returns to the outer pump, as a PostMessage is during a Windows modal dialog).
//
// The factory returns an autoreleased event. A pool belongs to the thread that
// pushed it, so the goroutine stays on its thread from push to pop.
func (a *darwinApp) postWakeEvent() error {
	a.mu.Lock()
	app := a.nsapp
	a.mu.Unlock()
	if app == 0 {
		return fmt.Errorf("%w: application not running", ErrWindowNotReady)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objcAutoreleasePoolPush()
	defer objcAutoreleasePoolPop(pool)

	event := newWakeEvent()
	if event == 0 {
		return fmt.Errorf("desktop: create wake event")
	}
	send2(app, objcSel("postEvent:atStart:"), event, 1)
	return nil
}

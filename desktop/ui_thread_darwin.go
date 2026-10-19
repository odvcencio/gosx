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

// onMainThread runs fn on the process's main thread, for the package-level
// functions that have no App in hand (ShowMessage). It runs fn directly when the
// caller is on the main thread, which is where the main goroutine is, before Run
// and inside menu handlers. From any other goroutine it goes through the running
// app's dispatcher; with no app running there is no one to wake the main thread,
// and it reports what is needed.
func onMainThread(what string, fn func() error) error {
	if pthreadMainNP() != 0 {
		return fn()
	}
	app := activeApp.Load()
	if app == nil {
		return errNeedsMainThread(what)
	}
	return app.onUIThread(func() error {
		if pthreadMainNP() == 0 {
			// The app stopped between the lookup and the call, and the dispatcher
			// runs calls on their own thread once it is stopped.
			return errNeedsMainThread(what)
		}
		return fn()
	})
}

func errNeedsMainThread(what string) error {
	return fmt.Errorf("%w: %s must run on the process's main thread: call it from the main goroutine, or while App.Run is running", ErrUnsupported, what)
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

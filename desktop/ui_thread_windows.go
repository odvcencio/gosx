//go:build windows && (amd64 || arm64)

package desktop

import "fmt"

var procGetCurrentThreadId = modKernel.NewProc("GetCurrentThreadId")

// wmAppDispatch asks the window thread to run queued calls.
const wmAppDispatch = wmAppTray + 1

func currentThreadID() uint32 {
	id, _, _ := procGetCurrentThreadId.Call()
	return uint32(id)
}

// onUIThread runs fn on the window thread (see uiDispatcher).
func (a *windowsApp) onUIThread(fn func() error) error {
	return a.dispatch.run(fn)
}

// startDispatch records the window thread; call it on that thread once the
// window exists.
func (a *windowsApp) startDispatch(hwnd uintptr) {
	a.dispatch.current = currentThreadID
	a.dispatch.wake = func() error {
		if ok, _, callErr := procPostMessageW.Call(hwnd, wmAppDispatch, 0, 0); ok == 0 {
			return fmt.Errorf("PostMessageW: %w", callErr)
		}
		return nil
	}
	a.dispatch.start(currentThreadID())
}

func (a *windowsApp) stopDispatch() { a.dispatch.stop() }

func (a *windowsApp) drainDispatchQueue() { a.dispatch.drain() }

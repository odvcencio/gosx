//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"
)

type scriptCompletedHandler struct {
	vtbl *environmentCompletedHandlerVtbl
	refs uint32
	done func(string, error)
}

var scriptCompletedVtbl = environmentCompletedHandlerVtbl{
	QueryInterface: syscall.NewCallback(scriptCompletedQueryInterface),
	AddRef:         syscall.NewCallback(scriptCompletedAddRef),
	Release:        syscall.NewCallback(scriptCompletedRelease),
	Invoke:         syscall.NewCallback(scriptCompletedInvoke),
}

func newScriptCompletedHandler(done func(string, error)) *scriptCompletedHandler {
	h := &scriptCompletedHandler{vtbl: &scriptCompletedVtbl, refs: 1, done: done}
	rootCOMHandler(uintptr(unsafe.Pointer(h)), h)
	return h
}

func scriptCompletedQueryInterface(this, iid, ppv uintptr) uintptr {
	return queryInterfaceHandler(this, iid, ppv, iidAddScriptToExecuteOnDocumentCreatedCompletedHandler, scriptCompletedAddRef)
}

func scriptCompletedAddRef(this uintptr) uintptr {
	h := (*scriptCompletedHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddUint32(&h.refs, 1))
}

func scriptCompletedRelease(this uintptr) uintptr {
	h := (*scriptCompletedHandler)(unsafe.Pointer(this))
	refs := atomic.AddUint32(&h.refs, ^uint32(0))
	if refs == 0 {
		unrootCOMHandler(this)
	}
	runtime.KeepAlive(h)
	return uintptr(refs)
}

func scriptCompletedInvoke(this, result, id uintptr) uintptr {
	h := (*scriptCompletedHandler)(unsafe.Pointer(this))
	var err error
	if failedHRESULT(result) {
		err = hresultError{Op: "ICoreWebView2.AddScriptToExecuteOnDocumentCreated completion", Code: result}
	}
	h.done(utf16PtrToString((*uint16)(unsafe.Pointer(id))), err)
	return sOK
}

// installBootstrapOnUI retains one completed registration. In-flight additions
// remove their own ID if a newer completion has already replaced them.
func (a *windowsApp) installBootstrapOnUI(script string, ready func(error)) error {
	a.mu.Lock()
	webview := a.webview
	a.bootstrapGeneration++
	generation := a.bootstrapGeneration
	a.mu.Unlock()
	if a.options.MuteAudio {
		script = muteMediaAudioScript + script
	}
	return webview.addScriptToExecuteOnDocumentCreated(script, func(id string, err error) {
		a.mu.Lock()
		closed := a.webviewReleased || a.webview != webview
		previous := a.bootstrapScriptID
		stale := generation < a.bootstrapInstalledGeneration
		if !closed && err == nil && id != "" && !stale {
			a.bootstrapScriptID = id
			a.bootstrapInstalledGeneration = generation
		}
		a.mu.Unlock()
		if closed {
			return
		}
		if err == nil && id == "" {
			err = fmt.Errorf("%w: bootstrap registration returned an empty ID", ErrWebView2Unavailable)
		}
		if err == nil {
			if stale {
				err = webview.removeScriptToExecuteOnDocumentCreated(id)
			} else if previous != "" {
				err = webview.removeScriptToExecuteOnDocumentCreated(previous)
			}
		}
		if ready != nil {
			ready(err)
		} else if err != nil {
			a.failRun(err)
		}
	})
}

//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestWindowsBootstrapReplacementRetainsOneScript(t *testing.T) {
	type addition struct {
		handler uintptr
		id      string
	}
	var pending []addition
	registered := map[string]bool{}
	reject := false
	vtable := make([]uintptr, 29)
	vtable[webViewAddScriptToExecuteOnDocumentCreated] = syscall.NewCallback(func(this, script, handler uintptr) uintptr {
		if reject {
			return 0x80004005
		}
		scriptCompletedAddRef(handler)
		pending = append(pending, addition{handler, fmt.Sprint(len(pending) + 1)})
		return sOK
	})
	vtable[webViewRemoveScriptToExecuteOnDocumentCreated] = syscall.NewCallback(func(this, id uintptr) uintptr {
		delete(registered, utf16PtrToString((*uint16)(unsafe.Pointer(id))))
		return sOK
	})
	webview := &coreWebView2{vtbl: uintptr(unsafe.Pointer(&vtable[0]))}
	app := &windowsApp{webview: webview}
	complete := func(index int) {
		t.Helper()
		addition := pending[index]
		registered[addition.id] = true
		id, _ := syscall.UTF16PtrFromString(addition.id)
		scriptCompletedInvoke(addition.handler, sOK, uintptr(unsafe.Pointer(id)))
		scriptCompletedRelease(addition.handler)
		runtime.KeepAlive(id)
	}
	ready := false
	if err := app.installBootstrapOnUI("initial", func(err error) {
		if err != nil {
			t.Error(err)
		}
		ready = true
	}); err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("first navigation must wait for script completion")
	}
	complete(0)
	if !ready {
		t.Fatal("initial installation never became ready")
	}
	for i := 0; i < 100; i++ {
		first := len(pending)
		if err := app.prependBootstrapScriptOnUI("older"); err != nil {
			t.Fatal(err)
		}
		if err := app.prependBootstrapScriptOnUI("newest"); err != nil {
			t.Fatal(err)
		}
		// An older asynchronous completion must remove its own registration.
		complete(first + 1)
		complete(first)
		if len(registered) != 1 || !registered[app.bootstrapScriptID] || app.pendingBootstrap != "newest" {
			t.Fatalf("retained registrations = %v, current %q", registered, app.bootstrapScriptID)
		}
	}
	reject = true
	before := app.bootstrapScriptID
	if err := app.prependBootstrapScriptOnUI("rejected"); err == nil {
		t.Fatal("registration rejection lost")
	}
	if app.pendingBootstrap != "newest" || app.bootstrapScriptID != before || len(registered) != 1 {
		t.Fatal("failed installation changed the pending or registered script")
	}
	runtime.KeepAlive(webview)
	runtime.KeepAlive(vtable)
}

package desktop

import (
	"testing"
	"unsafe"
)

func TestDesktopCOMHandlerIIDMatching(t *testing.T) {
	handlers := []struct {
		name string
		iid  comGUID
	}{
		{"environment completed", iidCreateCoreWebView2EnvironmentCompletedHandler},
		{"controller completed", iidCreateCoreWebView2ControllerCompletedHandler},
		{"web message received", iidWebMessageReceivedEventHandler},
		{"web resource requested", iidWebResourceRequestedEventHandler},
		{"process failed", iidProcessFailedEventHandler},
		{"fullscreen changed", iidContainsFullScreenElementChangedEventHandler},
		{"navigation completed", iidNavigationCompletedEventHandler},
	}
	for _, handler := range handlers {
		t.Run(handler.name, func(t *testing.T) {
			if !supportsCOMInterface(iidIUnknown, handler.iid) {
				t.Fatal("handler must support IUnknown")
			}
			if !supportsCOMInterface(handler.iid, handler.iid) {
				t.Fatal("handler must support its own IID")
			}
			for _, other := range handlers {
				if other.iid != handler.iid && supportsCOMInterface(other.iid, handler.iid) {
					t.Fatalf("handler must reject %s IID", other.name)
				}
			}
			if supportsCOMInterface(comGUID{Data1: 0xdeadbeef}, handler.iid) {
				t.Fatal("handler must reject unrelated IIDs")
			}
		})
	}
}

func TestDesktopCOMReferenceBalancesRetainAndReleaseOnce(t *testing.T) {
	var marker int
	value := unsafe.Pointer(&marker)
	addRefs := 0
	releases := 0
	ref := retainCOMReference(value,
		func(got unsafe.Pointer) {
			if got != value {
				t.Fatal("AddRef received a different pointer")
			}
			addRefs++
		},
		func(got unsafe.Pointer) {
			if got != value {
				t.Fatal("Release received a different pointer")
			}
			releases++
		},
	)
	ref.Release()
	ref.Release()
	if addRefs != 1 || releases != 1 {
		t.Fatalf("reference calls = AddRef %d, Release %d; want 1 each", addRefs, releases)
	}
}

//go:build js && wasm

package browser

import (
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/internal/browserdom"
)

func installObserver(t *testing.T) (js.Value, *js.Value, *int) {
	t.Helper()
	g := js.Global()
	host := g.Get("Object").New()
	var callback js.Value
	disconnects := new(int)
	browserMethod(t, host, "observe", func(_ js.Value, args []js.Value) any {
		if len(args) != 2 || !args[1].Get("attributes").Bool() || args[1].Get("attributeFilter").Length() != 1 || args[1].Get("attributeFilter").Index(0).String() != "data-visible" {
			t.Error("observer options changed")
		}
		return nil
	})
	browserMethod(t, host, "disconnect", func(js.Value, []js.Value) any { *disconnects++; return nil })
	constructor := js.FuncOf(func(_ js.Value, args []js.Value) any { callback = args[0]; return host })
	t.Cleanup(constructor.Release)
	replaceBrowserGlobal(t, "MutationObserver", constructor.Value)
	return host, &callback, disconnects
}
func TestObserverDisposeFromCallbackAndRetryDisconnect(t *testing.T) {
	host, callback, disconnects := installObserver(t)
	element := browserdom.FromJS(js.Global().Get("Object").New())
	calls := 0
	var observer *Observer
	observer = ObserveAttributes(element, []string{"data-visible"}, func() { calls++; observer.Dispose() })
	if observer == nil {
		t.Fatal("observer unavailable")
	}
	callback.Invoke()
	observer.Dispose()
	if calls != 1 || *disconnects != 1 || !observer.released {
		t.Fatal("observer self-disposal")
	}
	observer = ObserveAttributes(element, []string{"data-visible"}, func() { calls++ })
	disconnect := host.Get("disconnect")
	host.Set("disconnect", js.Global().Get("Function").New("throw new Error('disconnect failed')"))
	observer.Dispose()
	if observer.released || observer.active || observer.changed != nil {
		t.Fatal("disconnect failure released a live host function or retained app callback")
	}
	callback.Invoke()
	if calls != 1 {
		t.Fatal("failed disconnect allowed app callback")
	}
	host.Set("disconnect", disconnect)
	observer.Dispose()
	observer.Dispose()
	if !observer.released || *disconnects != 2 {
		t.Fatal("disconnect retry failed")
	}
}
func TestObserverObserveFailureDisconnectsBeforeRelease(t *testing.T) {
	host, _, disconnects := installObserver(t)
	host.Set("observe", js.Global().Get("Function").New("throw new Error('observe failed')"))
	observer := ObserveAttributes(browserdom.FromJS(js.Global().Get("Object").New()), []string{"data-visible"}, func() { t.Fatal("failed observer called app") })
	if observer != nil || *disconnects != 1 {
		t.Fatal("observe failure did not disconnect native owner")
	}
	if ObserveAttributes(Element{}, []string{"data-visible"}, func() {}) != nil {
		t.Fatal("invalid element observed")
	}
}
func TestObserverConstructorFailureRetainsInertCallback(t *testing.T) {
	g := js.Global()
	host, callback, disconnects := installObserver(t)
	capture := js.FuncOf(func(_ js.Value, args []js.Value) any { *callback = args[0]; return nil })
	defer capture.Release()
	constructor := g.Get("Function").New("capture", "return function(callback){capture(callback);throw new Error('constructor failed')}").Invoke(capture)
	g.Set("MutationObserver", constructor)
	if ObserveAttributes(browserdom.FromJS(g.Get("Object").New()), []string{"data-visible"}, func() { t.Fatal("failed constructor called app") }) != nil {
		t.Fatal("throwing constructor accepted")
	}
	callback.Invoke() // retained callback stays inert if constructor returned no handle
	if *disconnects != 0 {
		t.Fatal("failed construction returned a disconnectable handle", host)
	}
}

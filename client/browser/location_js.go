//go:build js && wasm

package browser

import (
	"errors"
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

func CurrentLocation() Location {
	v := js.Global().Get("location")
	if !v.Truthy() {
		return Location{}
	}
	return Location{Href: v.Get("href").String(), Origin: v.Get("origin").String(),
		Protocol: v.Get("protocol").String(), Host: v.Get("host").String(),
		Path: v.Get("pathname").String(), Search: v.Get("search").String(), Hash: v.Get("hash").String()}
}

// Navigate assigns url to location, so the browser navigates to it. A URL that
// differs from the current one only in its fragment makes the browser fire
// popstate before Navigate returns, and on a GoSX page the page's popstate
// handler then dispatches gosx:popstate, gosx:navigation:state and
// gosx:navigate in the same call. Go listeners of these events do not run
// inside that call. They run after the browser has delivered the events to all
// its listeners, and before Navigate returns (see Listener). They are too late
// to change what the page does, so a Go gosx:popstate listener cannot cancel the
// soft navigation that the page starts.
//
// A URL for another document makes Chrome and Firefox fire beforeunload before
// Navigate returns (WebKit fires it later). A Go beforeunload listener runs after
// that, so it cannot stop a navigation that Navigate starts, whatever it does
// with PreventDefault. It can still stop one that the user starts. Run the
// unsaved-changes check in Go before you call Navigate.
func Navigate(url string) { jsutil.Call(js.Global().Get("location"), "assign", url) }

func ReplaceHistory(url string) { js.Global().Get("history").Call("replaceState", js.Null(), "", url) }

// Reload reloads the page. Chrome and Firefox fire beforeunload before it
// returns, so a Go beforeunload listener cannot stop a reload that Reload starts
// (see Navigate). Run the unsaved-changes check in Go before you call Reload.
func Reload() { jsutil.Call(js.Global().Get("location"), "reload") }

func Viewport() Size {
	g := js.Global()
	return Size{Width: platformNumber(g.Get("innerWidth")), Height: platformNumber(g.Get("innerHeight"))}
}
func PixelRatio() float64 { return platformNumber(js.Global().Get("devicePixelRatio")) }

// DispatchResize dispatches a resize event on the window. The browser runs every
// listener before it returns. Go listeners run after the browser has delivered
// the event to all its listeners, and before DispatchResize returns (see
// Listener).
func DispatchResize() {
	jsutil.Call(js.Global(), "dispatchEvent", js.Global().Get("Event").New("resize"))
}
func LogError(message string) { js.Global().Get("console").Call("error", message) }

func platformNumber(v js.Value) float64 {
	if v.Type() == js.TypeNumber {
		return v.Float()
	}
	return 0
}

func (c JSONCapture) Enabled() (enabled bool) {
	defer func() {
		if recover() != nil {
			enabled = false
		}
	}()
	if c.GlobalName == "" || c.EnabledFlag == "" {
		return false
	}
	observer := js.Global().Get(c.GlobalName)
	return observer.Truthy() && observer.Get(c.EnabledFlag).Type() == js.TypeBoolean && observer.Get(c.EnabledFlag).Bool()
}

func (c JSONCapture) Publish(data []byte, label string) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("browser diagnostic capture: %v", value)
		}
	}()
	if !c.Enabled() {
		return nil
	}
	if c.PayloadField == "" || c.LabelField == "" || c.SequenceField == "" {
		return errors.New("browser diagnostic capture: missing field name")
	}
	observer := js.Global().Get(c.GlobalName)
	payload := js.Global().Get("JSON").Call("parse", string(data))
	observer.Set(c.PayloadField, payload)
	observer.Set(c.LabelField, label)
	observer.Set(c.SequenceField, platformNumber(observer.Get(c.SequenceField))+1)
	return nil
}

// Package browser provides typed browser services for Go-WASM engines.
//
// Author repeatable DOM structure in server-rendered HTML templates, then use
// Document.InstantiateTemplate to obtain a detached element without parsing
// markup in the engine. Retain its child handles for subsequent text and
// attribute updates, and append the element when a new item is inserted.
//
// # Listeners and the calls that run them
//
// Element.Dispatch, Focus, Blur and Click, and the package functions
// DispatchResize and DispatchError, make the browser run event listeners before
// they return. A Go listener added with On does not run inside that call:
// Go's js/wasm runtime can stop the program when JavaScript enters Go inside a
// call that Go made, even when one goroutine makes the call (see package
// m31labs.dev/gosx/client/jsutil). The Go listener runs after the browser has
// delivered the event to all its listeners, in the order the browser called
// them, and before the method returns. At that point PreventDefault,
// StopPropagation and StopImmediatePropagation can no longer change the
// dispatch, so a Go listener cannot cancel the click that Click performs.
// Event.CurrentTarget still names the element the listener is attached to.
//
// An event that the page raises, such as a user's click, still reaches a Go
// listener inside the browser's own dispatch, and the listener can cancel it.
//
// A call made with js.Value.Call, such as Call("dispatchEvent", event), is not
// covered. Make it with jsutil.Call, and write the Go listener with
// jsutil.NewCallback, when Go listeners are attached.
package browser

import "m31labs.dev/gosx/internal/browserdom"

type Element = browserdom.Element
type Document = browserdom.Document
type WindowTarget = browserdom.WindowTarget
type MediaQuery = browserdom.MediaQuery
type Elements = browserdom.Elements
type Style = browserdom.Style
type Classes = browserdom.Classes
type Dataset = browserdom.Dataset
type Event = browserdom.Event
type Listener = browserdom.Listener
type ListenerOptions = browserdom.ListenerOptions
type EventTarget = browserdom.EventTarget
type Markup = browserdom.Markup
type Rect = browserdom.Rect

func CurrentDocument() Document          { return browserdom.CurrentDocument() }
func Window() WindowTarget               { return browserdom.Window() }
func MatchMedia(query string) MediaQuery { return browserdom.MatchMedia(query) }

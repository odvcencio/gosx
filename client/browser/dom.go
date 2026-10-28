// Package browser provides typed browser services for Go-WASM engines.
//
// Author repeatable DOM structure in server-rendered HTML templates, then use
// Document.InstantiateTemplate to obtain a detached element without parsing
// markup in the engine. Retain its child handles for subsequent text and
// attribute updates, and append the element when a new item is inserted.
//
// # Listeners run inside the call that dispatches the event
//
// Element.Dispatch, Focus, Blur and Click, and the package functions
// DispatchResize and DispatchError, make the browser run event listeners before
// they return. A Go listener added with On runs inside that call, nested in it.
// One goroutine can do this safely. Go's js/wasm runtime can fail when two
// goroutines are inside such calls at once (see package
// m31labs.dev/gosx/client/jsutil), so make these calls from one goroutine, or
// call the Go consumers directly and dispatch the DOM event only for page
// scripts.
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

// Package browser provides typed browser services for Go-WASM engines.
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

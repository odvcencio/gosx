// Package browser provides typed browser services for Go-WASM engines.
//
// Author repeatable DOM structure in server-rendered HTML templates, then use
// Document.InstantiateTemplate to obtain a detached element without parsing
// markup in the engine. Retain its child handles for subsequent text and
// attribute updates, and append the element when a new item is inserted.
//
// # Listeners and the calls that run them
//
// These calls make the browser run event listeners before they return:
//
//   - Element.Dispatch, Focus, Blur and Click.
//   - Element.CheckValidity and ReportValidity. Each fires invalid on every
//     invalid control, and ReportValidity then focuses the first one, which
//     fires its focus and focusin listeners.
//   - Element.Remove, and Element.SetText and ReplaceMarkup, which remove the
//     element's children. In Chrome for Testing 156, removing the node that has
//     focus, or an ancestor of it, fires blur and focusout before the call
//     returns. Firefox 146 and WebKit 26 fire neither.
//   - The package functions DispatchResize, DispatchError, Navigate and Reload.
//     A Navigate to a fragment of the current page fires popstate before it
//     returns, and on a GoSX page the page's popstate handler then dispatches
//     gosx:popstate, gosx:navigation:state and gosx:navigate in the same call.
//     A Navigate to another document, and Reload, fire beforeunload before they
//     return in Chrome and Firefox (WebKit fires it later).
//
// A Go listener added with On does not run inside such a call: Go's js/wasm
// runtime can stop the program when JavaScript enters Go inside a call that Go
// made, even when one goroutine makes the call (see package
// m31labs.dev/gosx/client/jsutil). The Go listener runs after the browser has
// delivered the event to all its listeners, in the order the browser called
// them, and before the method returns. At that point PreventDefault,
// StopPropagation and StopImmediatePropagation can no longer change the
// dispatch, so a Go listener cannot cancel the click that Click performs.
// Event.CurrentTarget still names the element the listener is attached to.
//
// This holds for every event that the browser or page code raises while Go is
// inside one of these calls, not only for the event that Go dispatches. Three
// results follow:
//
//   - A Go listener cannot cancel the default action of such an event. Click on
//     a submit button submits its form, whatever a Go submit listener does, and
//     so does a requestSubmit that a page script makes in reaction to
//     Context.SetSignal or Context.Emit of package m31labs.dev/gosx/engine/wasm.
//     ReportValidity reports the problem to the user, whatever a Go invalid
//     listener does. A GoSX page starts its soft navigation when Navigate fires
//     popstate, whatever a Go gosx:popstate listener does. To let Go code
//     decide whether a form submits, whether the report appears or whether the
//     page navigates, call that code directly instead of making the call.
//     A Go beforeunload listener cannot stop a navigation that Navigate starts,
//     or a Reload, in Chrome and Firefox: the browser fires the event before
//     the call returns, and the Go listener runs after it. It can still stop a
//     navigation that the user starts. Run the unsaved-changes check in Go
//     before you call Navigate or Reload.
//   - When the target is inside a shadow tree, Event.Target and
//     Event.RelatedTarget may not name what they named during the dispatch. If
//     the event's path ended inside a shadow tree, that is, the event never
//     reached the document, the browser clears both after the dispatch, and a
//     cleared one is an invalid Element. That is the case for an event that is
//     not composed, such as every event that Dispatch creates, and for a focus
//     change between two elements of the same shadow tree (also between a
//     shadow tree and one nested in it).
//     Otherwise the browser leaves them as seen from the document: Target names
//     the outermost shadow host, and RelatedTarget names the related element,
//     or its outermost shadow host if it is inside a shadow tree.
//     Event.CurrentTarget is not affected.
//   - A blur or focusout listener that a removal triggers runs after the
//     removal. In Chrome for Testing 156, a page listener of the same event ran
//     before it and saw the element in the document; the Go listener sees an
//     element that is out of the document (Connected reports false), with its
//     state intact, such as the value of a text field. Parent is invalid only
//     if the removed node is the element itself. Firefox 146 and WebKit 26 fire
//     no blur or focusout on a removal, so no Go listener runs there.
//
// An event that the page raises, such as a user's click, still reaches a Go
// listener inside the browser's own dispatch, and the listener can cancel it.
//
// Other calls can also make the browser run listeners before they return, and
// they are not covered. In Chrome for Testing 156, Append and InsertBefore do so
// when they move the focused element (blur and focusout) or insert an iframe
// (load), AppendMarkup does so for an iframe (load), SetHidden does so for the
// focused element (blur and focusout), and so does SetAttr with the name
// "hidden". A Go listener of such an event runs inside the call. Call Blur on
// the focused element before you move or hide it: Blur is covered, and nothing
// is left to blur later. A call made with
// js.Value.Call, such as Call("dispatchEvent", event), is not covered either.
// Make it with jsutil.Call, and write the Go listener with jsutil.NewCallback,
// when Go listeners are attached.
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

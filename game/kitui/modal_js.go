//go:build js && wasm

package kitui

import (
	"m31labs.dev/gosx/client/browser"
	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/focus"
	"m31labs.dev/gosx/markup"
)

// Modal is a dialog in the page that the game writes into: the <dialog>, its
// body and its footer. FindModal finds one made by Dialog; a game may also
// fill the fields from a dialog of its own.
type Modal struct {
	El, Body, Foot browser.Element
	// Write puts markup into a part of the dialog. Nil writes it as it is; a
	// game that rewrites its markup (control names for the device in hand,
	// the focus kept) sets its own.
	Write func(part browser.Element, n markup.Node)
}

// FindModal finds the parts of a dialog made by Dialog with p.
func FindModal(doc browser.Document, p DialogProps) Modal {
	m := Modal{El: doc.ByID(p.ID), Body: doc.ByID(p.BodyID)}
	if p.FootID != "" {
		m.Foot = doc.ByID(p.FootID)
	}
	return m
}

func (m Modal) write(part browser.Element, n markup.Node) {
	if !part.Valid() {
		return
	}
	if m.Write != nil {
		m.Write(part, n)
		return
	}
	part.ReplaceMarkup(browser.Markup(markup.RenderHTML(n)))
}

// Show writes body into the dialog, empties and hides the footer, opens the
// dialog as a modal (the page behind it is inert; an open dialog stays open)
// and gives the body's main control (focus.Main) the focus: the browser
// would put it on the first control, the close button.
func (m Modal) Show(body markup.Node) {
	m.write(m.Body, body)
	if m.Foot.Valid() {
		m.write(m.Foot, markup.Fragment())
		m.Foot.SetHidden(true)
	}
	m.El.ShowModal()
	focus.FocusFirst(m.Body, focus.Main)
}

// Footer puts a dialog's main action below its body, outside the part that
// scrolls, so it is always in reach, and gives the footer's main control the
// focus.
func (m Modal) Footer(n markup.Node) {
	if !m.Foot.Valid() {
		return
	}
	m.write(m.Foot, n)
	m.Foot.SetHidden(false)
	focus.FocusFirst(m.Foot, focus.Main)
}

// Close closes the dialog; a closed dialog stays closed.
func (m Modal) Close() { m.El.CloseDialog() }

// Open reports whether the dialog is open.
func (m Modal) Open() bool { return m.El.Open() }

// AnyOpen reports whether any dialog in doc is open.
func AnyOpen(doc browser.Document) bool { return doc.Query("dialog[open]").Valid() }

// OnAction runs do for each click inside the dialog on a control that
// carries attr (for example "data-do"), with that attribute's value and the
// control.
func (m Modal) OnAction(attr string, do func(name string, el browser.Element)) *browser.Listener {
	return m.El.On("click", func(e browser.Event) {
		if el := e.Target().Closest("[" + attr + "]"); el.Valid() && m.El.Contains(el) {
			do(el.Attr(attr), el)
		}
	})
}

// SwallowEscape withdraws Escape from keys when the browser closes the dialog
// for it (the dialog's cancel event). That press is used: it cannot also
// reach the game, as "pause" for example, however slow the frames are.
func (m Modal) SwallowEscape(keys *actions.Map) *browser.Listener {
	return m.El.On("cancel", func(browser.Event) { keys.Suppress(actions.Key("Escape")) })
}

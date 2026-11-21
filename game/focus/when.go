package focus

import (
	"strconv"
	"strings"

	"m31labs.dev/gosx/client/browser"
)

// Main selects a screen's main button: the control a player expects the focus
// on when a screen appears or the control that had it is gone.
const Main = ".primary:not(:disabled)"

// Ready reports whether el can take the focus now: it is in the page, enabled
// and drawn (it has a width, so neither it nor a parent is hidden). An island
// draws a moment after its signal changes, so a control the engine asks for
// can be missing, disabled or not yet shown for a frame or two.
func Ready(el browser.Element) bool {
	return el.Valid() && !el.Disabled() && el.OffsetWidth() > 0
}

// Pending is a request When made: the focus waits for a control the page has
// not drawn yet.
type Pending struct {
	doc      browser.Document
	selector string
	left     int
}

// waiting is the one request that may still move the focus. A page has one
// focus, so a newer request replaces an older one.
var waiting *Pending

// When asks for the focus on the first control in doc that matches selector,
// as soon as it is Ready. Step tries once per drawn frame, for at most frames
// frames; a frame on a slow machine is longer, so the wait stretches with it.
// A newer request replaces this one, which then does nothing.
//
// The request is done when the control holds the focus. A control the browser
// will not focus (one outside an open modal dialog, or under an inert
// element) is tried again on the next frame, so the request also waits out a
// dialog that covers the screen, for as long as its frames last.
func When(doc browser.Document, selector string, frames int) *Pending {
	waiting.Cancel()
	p := &Pending{doc: doc, selector: selector, left: max(frames, 0)}
	waiting = p
	return p
}

// Step gives the waiting request its try for this frame. Call it once per
// drawn frame, after the page's signals have been sent. It reports whether a
// request is still waiting.
func Step() bool {
	p := waiting
	if !p.Waiting() {
		return false
	}
	p.left--
	matches := p.doc.QueryAll(p.selector)
	for i := 0; i < matches.Len(); i++ {
		el := matches.At(i)
		if !Ready(el) {
			continue
		}
		el.Focus()
		if p.doc.ActiveElement().Equal(el) {
			p.left = 0
		}
		break
	}
	if p.left == 0 && waiting == p {
		waiting = nil
	}
	return p.Waiting()
}

// Waiting reports whether p may still move the focus: it has frames left, has
// not been cancelled or replaced, and its control has not taken the focus.
func (p *Pending) Waiting() bool { return p != nil && p.left > 0 && waiting == p }

// Cancel withdraws the request. It does nothing to a request already done or
// replaced, so cancelling one never withdraws a newer one.
func (p *Pending) Cancel() {
	if p == nil {
		return
	}
	p.left = 0
	if waiting == p {
		waiting = nil
	}
}

// Keep runs write, which rewrites el's markup, and keeps the keyboard focus
// where the player had it. Rewriting markup destroys the control that had the
// focus, and the page's focus falls to the body. When a control inside el had
// the focus, Keep finds the same control again by the name the page knows it
// by (its id, data-control, data-call, data-group or data-do, in that order)
// and focuses it; when that control is gone, disabled or will not take the
// focus, el's main button (Main) takes it, else its first enabled control.
//
// Keep reports whether a control inside el had the focus. When none did,
// write runs and the focus is left alone, so the caller can decide whether a
// screen that appears takes it.
func Keep(el browser.Element, write func()) bool {
	active := browser.CurrentDocument().ActiveElement()
	if !el.Valid() || !el.Contains(active) || el.Equal(active) {
		write()
		return false
	}
	key := KeyOf(active)
	write()
	if key != "" {
		if same := el.Query(key); same.Valid() {
			same.Focus()
			if browser.CurrentDocument().ActiveElement().Equal(same) {
				return true
			}
		}
	}
	FocusFirst(el, Main)
	return true
}

// named is what KeyOf reads of a control.
type named interface {
	Attr(name string) string
	HasAttr(name string) bool
}

// keyNames are the attributes a control is found again by, in order.
var keyNames = [...]string{"id", "data-control", "data-call", "data-group", "data-do"}

// KeyOf is a selector that finds el's control again after its markup is
// written anew: the first of its id, data-control, data-call, data-group and
// data-do that it carries, as an attribute selector, or "" when it carries
// none of them.
func KeyOf(el named) string {
	for _, name := range keyNames {
		if !el.HasAttr(name) {
			continue
		}
		v := el.Attr(name)
		if name == "id" && v == "" {
			continue
		}
		return "[" + name + `="` + cssString(v) + `"]`
	}
	return ""
}

// cssString escapes v for a double-quoted CSS string.
func cssString(v string) string {
	if !strings.ContainsAny(v, "\"\\\n\r\f") {
		return v
	}
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n', '\r', '\f':
			// A newline cannot stand in a CSS string; write its code point.
			b.WriteString(`\` + strconv.FormatInt(int64(r), 16) + " ")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

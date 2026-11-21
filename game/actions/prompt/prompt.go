// Package prompt writes a game's control prompts for the device in hand.
//
// A game writes its texts with each control as its name in braces: "Snap
// ball · {act}", "{move} steer · {sprint} sprint". A Table says what each
// control is called on the keyboard and on a gamepad, and the functions here
// write the text for one hand.Hand: "Snap ball · Space" on the keyboard,
// "Snap ball · ✕" on a PlayStation pad, and "Snap ball" on a touch screen,
// where the control is a button on the screen and has no name.
//
// A text is clauses joined by the Table's separator (" · " unless set). A
// clause that names a control the device has no name for is left out
// whole, so a line made only of controls reads as nothing on a touch
// screen. A name in braces that is not a control is left as written.
//
// The markup functions write a control as a glyph, a kbd element whose data
// attributes tell a stylesheet what to draw:
//
//	<kbd class="glyph" data-kind="face" data-pad="playstation" data-cap="cross" data-slot="south">✕</kbd>
//
// Its text is always the control's name, so a page with no styles, a screen
// reader and a browser check read the same words Expand writes.
package prompt

import (
	"html"
	"strings"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/actions/hand"
)

// Part is one piece of a prompt: words when Kind is "", else one control
// drawn as a glyph.
type Part = actions.Glyph

// Words is a part with no control in it.
func Words(text string) Part { return Part{Text: text} }

// Text joins the parts' texts.
func Text(parts []Part) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// PadKind says what a control is on a gamepad.
type PadKind uint8

const (
	// NoPad is a control with no gamepad name: the player uses its button
	// on the screen, or the keyboard.
	NoPad PadKind = iota
	// PadButton is one button of the standard mapping.
	PadButton
	// PadStick is a stick, or a stick pushed one way.
	PadStick
)

// Entry is one control's name on each device.
type Entry struct {
	// Keys is what the keyboard prompt shows: display text such as
	// "Space", "WASD", "1–5" or "←", not a KeyboardEvent code. "" means
	// the control has no name on the keyboard.
	Keys string
	Pad  PadKind
	// Button is the standard-mapping button, for PadButton.
	Button int
	// Stick, for PadStick: which stick, which way it is pushed ("", "up",
	// "down", "left", "right"), and the words the prompt shows ("Left
	// stick", "Right stick up").
	Right bool
	Dir   string
	Words string
}

// Button is a control on a key and a gamepad button.
func Button(keys string, button int) Entry {
	return Entry{Keys: keys, Pad: PadButton, Button: button}
}

// Stick is a control on keys and a gamepad stick.
func Stick(keys string, right bool, dir, words string) Entry {
	return Entry{Keys: keys, Pad: PadStick, Right: right, Dir: dir, Words: words}
}

// KeysOnly is a control with a key and no gamepad name.
func KeysOnly(keys string) Entry { return Entry{Keys: keys} }

// ButtonOnly is a control on a gamepad button with no keyboard name.
func ButtonOnly(button int) Entry { return Entry{Pad: PadButton, Button: button} }

// Group is a control that stands for several, such as five receivers'
// throw buttons: it returns the parts for the hand (glyphs and the words
// between them), or nil when the device has no name for it. A group's
// parts should be built from leaf glyphs, not by expanding other groups.
type Group func(h hand.Hand) []Part

// Table is a game's controls by the name its texts use in braces.
type Table struct {
	Entries map[string]Entry
	Groups  map[string]Group
	// Separator joins a text's clauses. "" selects " · ".
	Separator string
}

func (t Table) sep() string {
	if t.Separator == "" {
		return " · "
	}
	return t.Separator
}

// Known reports whether control is one of the table's.
func (t Table) Known(control string) bool {
	if _, ok := t.Entries[control]; ok {
		return true
	}
	_, ok := t.Groups[control]
	return ok
}

// Glyphs are the parts of one control for the hand: none on a touch
// screen, or for a control the device has no name for.
func (t Table) Glyphs(h hand.Hand, control string) []Part {
	if h.Device == hand.Touch {
		return nil
	}
	if g, ok := t.Groups[control]; ok {
		return g(h)
	}
	e, ok := t.Entries[control]
	if !ok {
		return nil
	}
	if h.Device == hand.Keys {
		if e.Keys == "" {
			return nil
		}
		return []Part{actions.KeyGlyph(e.Keys)}
	}
	switch e.Pad {
	case PadButton:
		if g := actions.ButtonGlyph(h.Style(), e.Button); g.Kind != "" {
			return []Part{g}
		}
	case PadStick:
		g := Part{Text: e.Words, Kind: "stick", Pad: actions.StyleName(h.Style()), Cap: "LS", Slot: e.Dir}
		if e.Right {
			g.Cap = "RS"
		}
		return []Part{g}
	}
	return nil
}

// Label is one control's name for the hand: "Space", "A", "✕", "Left
// stick", or "" when the device has none.
func (t Table) Label(h hand.Hand, control string) string {
	return Text(t.Glyphs(h, control))
}

// next finds the next control in braces in s. It returns the
// brace positions and the name, or ok false when there is none. A name in
// braces that is not a control is skipped.
func (t Table) next(s string) (open, end int, control string, ok bool) {
	from := 0
	for {
		o := strings.IndexByte(s[from:], '{')
		if o < 0 {
			return 0, 0, "", false
		}
		o += from
		e := strings.IndexByte(s[o:], '}')
		if e < 0 {
			return 0, 0, "", false
		}
		e += o
		if c := s[o+1 : e]; t.Known(c) {
			return o, e, c, true
		}
		from = e + 1
	}
}

// unnamed reports whether a clause names a control the hand has no name for.
func (t Table) unnamed(h hand.Hand, clause string) bool {
	for {
		_, end, control, ok := t.next(clause)
		if !ok {
			return false
		}
		if len(t.Glyphs(h, control)) == 0 {
			return true
		}
		clause = clause[end+1:]
	}
}

// Parts writes a text for the hand as words and glyphs. It keeps and drops
// the same clauses Expand does, so Text(t.Parts(h, s)) == t.Expand(h, s).
func (t Table) Parts(h hand.Hand, text string) []Part {
	if text == "" {
		return nil
	}
	var out []Part
	words := func(s string) {
		if s == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Kind == "" {
			out[n-1].Text += s
			return
		}
		out = append(out, Words(s))
	}
	kept := 0
	for _, clause := range strings.Split(text, t.sep()) {
		if t.unnamed(h, clause) {
			continue
		}
		if kept > 0 {
			words(t.sep())
		}
		kept++
		for {
			open, end, control, ok := t.next(clause)
			if !ok {
				break
			}
			words(clause[:open])
			for _, g := range t.Glyphs(h, control) {
				if g.Kind == "" {
					words(g.Text)
				} else {
					out = append(out, g)
				}
			}
			clause = clause[end+1:]
		}
		words(clause)
	}
	return out
}

// Expand writes a text for the hand: every control in braces becomes its
// name, and a clause naming a control the device has no name for is left
// out.
func (t Table) Expand(h hand.Hand, text string) string {
	if !strings.Contains(text, "{") {
		return text
	}
	return Text(t.Parts(h, text))
}

// ExpandMarkup names the controls in markup a game wrote, as plain text. It
// changes only the braces, so the markup around them stays whole; a
// control the device has no name for goes together with the separator in
// front of it. The markup must be the game's own template, not text a
// player typed: names are escaped, the rest is passed through.
func (t Table) ExpandMarkup(h hand.Hand, markup string) string {
	return t.expandMarkup(h, markup, false)
}

// ExpandGlyphs names the controls in markup a game wrote, as ExpandMarkup
// does, but draws each control as a glyph. A control named inside a tag,
// in an attribute, is written as plain words. A tag is told by the last
// angle bracket before the brace, so an attribute value must not hold a
// bare ">".
func (t Table) ExpandGlyphs(h hand.Hand, markup string) string {
	return t.expandMarkup(h, markup, true)
}

func (t Table) expandMarkup(h hand.Hand, markup string, glyphs bool) string {
	if !strings.Contains(markup, "{") {
		return markup
	}
	var b strings.Builder
	for {
		open, end, control, ok := t.next(markup)
		if !ok {
			break
		}
		before := markup[:open]
		parts := t.Glyphs(h, control)
		if len(parts) == 0 {
			before = strings.TrimSuffix(before, t.sep())
		}
		b.WriteString(before)
		written := b.String()
		inTag := strings.LastIndexByte(written, '<') > strings.LastIndexByte(written, '>')
		for _, g := range parts {
			if glyphs && !inTag && g.Kind != "" {
				b.WriteString(GlyphHTML(g))
			} else {
				b.WriteString(html.EscapeString(g.Text))
			}
		}
		markup = markup[end+1:]
	}
	b.WriteString(markup)
	return b.String()
}

// GlyphHTML is the markup of one part: escaped words, or a kbd element
// whose text is the control's name and whose data attributes tell the
// stylesheet what to draw. data-slot is left out when the glyph has none.
func GlyphHTML(g Part) string {
	if g.Kind == "" {
		return html.EscapeString(g.Text)
	}
	var b strings.Builder
	b.WriteString(`<kbd class="glyph" data-kind="`)
	b.WriteString(html.EscapeString(g.Kind))
	b.WriteString(`" data-pad="`)
	b.WriteString(html.EscapeString(g.Pad))
	b.WriteString(`" data-cap="`)
	b.WriteString(html.EscapeString(g.Cap))
	if g.Slot != "" {
		b.WriteString(`" data-slot="`)
		b.WriteString(html.EscapeString(g.Slot))
	}
	b.WriteString(`">`)
	b.WriteString(html.EscapeString(g.Text))
	b.WriteString(`</kbd>`)
	return b.String()
}

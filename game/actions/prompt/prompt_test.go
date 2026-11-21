package prompt

import (
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/actions/hand"
)

var table = Table{
	Entries: map[string]Entry{
		"move":     Stick("WASD", false, "", "Left stick"),
		"sprint":   Button("Shift", 7),
		"act":      Button("Space", 0),
		"hit":      Stick("V", true, "up", "Right stick up"),
		"spike":    KeysOnly("B"),
		"defkey":   ButtonOnly(6),
		"throw1":   Button("1", 2),
		"throw2":   Button("2", 3),
		"stepback": Button("←", 14),
	},
}

func init() {
	table.Groups = map[string]Group{
		"throw": func(h hand.Hand) []Part {
			if h.Device == hand.Keys {
				return []Part{actions.KeyGlyph("1–2")}
			}
			var out []Part
			for i, c := range []string{"throw1", "throw2"} {
				if i > 0 {
					out = append(out, Words(", "))
				}
				out = append(out, table.Glyphs(h, c)...)
			}
			return out
		},
	}
}

var (
	keys  = hand.Hand{}
	xbox  = hand.Hand{Device: hand.Pad, ID: actions.PadIdentity{Style: actions.PadXbox}}
	ps    = hand.Hand{Device: hand.Pad, ID: actions.PadIdentity{Style: actions.PadPlayStation}}
	nin   = hand.Hand{Device: hand.Pad, ID: actions.PadIdentity{Style: actions.PadNintendo}}
	touch = hand.Hand{Device: hand.Touch}
	every = []hand.Hand{keys, xbox, ps, nin, touch, {Device: hand.Pad}}
)

func TestExpand(t *testing.T) {
	line := "{move} steer · {sprint} sprint · {throw} throw · {act} juke"
	for _, c := range []struct {
		h    hand.Hand
		want string
	}{
		{keys, "WASD steer · Shift sprint · 1–2 throw · Space juke"},
		{xbox, "Left stick steer · RT sprint · X, Y throw · A juke"},
		{ps, "Left stick steer · R2 sprint · □, △ throw · ✕ juke"},
		{nin, "Left stick steer · ZR sprint · Y, X throw · B juke"},
		{touch, ""},
	} {
		if got := table.Expand(c.h, line); got != c.want {
			t.Errorf("%s: %q, want %q", c.h.Name(), got, c.want)
		}
	}
	if got := table.Expand(touch, "Snap ball · {act}"); got != "Snap ball" {
		t.Errorf("touch drops the control's clause: %q", got)
	}
	if got := table.Expand(xbox, "Spike · {spike} · Pick · {defkey}"); got != "Spike · Pick · LT" {
		t.Errorf("a control with no pad name drops its clause: %q", got)
	}
	if got := table.Expand(keys, "Pick · {defkey}"); got != "Pick" {
		t.Errorf("a control with no key name drops its clause: %q", got)
	}
	if got := table.Expand(keys, "{stat} yards · {act"); got != "{stat} yards · {act" {
		t.Errorf("unknown names and an open brace stay as written: %q", got)
	}
}

func TestPartsReadAsExpand(t *testing.T) {
	texts := []string{
		"{move} steer · {sprint} sprint · {throw} throw · {act} juke",
		"Snap ball · {act}", "{act}", "plain text", "", "{hit} to hit · {stat}",
		"Spike · {spike} · Pick · {defkey}",
	}
	for _, h := range every {
		for _, s := range texts {
			if got, want := Text(table.Parts(h, s)), table.Expand(h, s); got != want {
				t.Errorf("%s %q: parts read %q, Expand %q", h.Name(), s, got, want)
			}
		}
	}
	parts := table.Parts(ps, "Throw · {throw}")
	if len(parts) != 4 || parts[1].Cap != "square" || parts[2] != Words(", ") || parts[3].Cap != "triangle" {
		t.Fatalf("PlayStation throw parts: %+v", parts)
	}
	if g := table.Glyphs(xbox, "hit"); len(g) != 1 || g[0] != (Part{Text: "Right stick up", Kind: "stick", Pad: "xbox", Cap: "RS", Slot: "up"}) {
		t.Fatalf("stick glyph: %+v", g)
	}
	if g := table.Glyphs(keys, "stepback"); len(g) != 1 || g[0].Kind != "key" || g[0].Text != "←" {
		t.Fatalf("keycap: %+v", g)
	}
}

var tags = regexp.MustCompile(`<kbd[^>]*>|</kbd>`)

func TestExpandGlyphsReadsAsExpandMarkup(t *testing.T) {
	markup := `<button class="primary" data-control="snap" title="Snap · {act}">Snap ball · {act}</button><button style="{x}">Dive · {sprint}</button><p>{throw} throw</p>`
	for _, h := range every {
		got := tags.ReplaceAllString(table.ExpandGlyphs(h, markup), "")
		if want := table.ExpandMarkup(h, markup); got != want {
			t.Errorf("%s: glyph markup reads %q, want %q", h.Name(), got, want)
		}
	}
	got := table.ExpandGlyphs(ps, `<b title="{act}">Snap · {act}</b>`)
	want := `<b title="✕">Snap · <kbd class="glyph" data-kind="face" data-pad="playstation" data-cap="cross" data-slot="south">✕</kbd></b>`
	if got != want {
		t.Errorf("PlayStation glyph markup = %q, want %q", got, want)
	}
	got = table.ExpandGlyphs(touch, `<button title="Snap · {act}">Snap ball · {act}</button>`)
	if got != `<button title="Snap">Snap ball</button>` {
		t.Errorf("touch markup keeps no separator: %q", got)
	}
	if strings.Contains(table.ExpandGlyphs(xbox, `<p>{sprint}</p>`), "data-slot") {
		t.Error("a trigger has no slot attribute")
	}
}

func TestLabelAndKnown(t *testing.T) {
	if got := table.Label(xbox, "throw"); got != "X, Y" {
		t.Errorf("group label = %q", got)
	}
	if table.Label(touch, "act") != "" || table.Label(keys, "nope") != "" {
		t.Error("touch and unknown controls have no label")
	}
	if !table.Known("throw") || !table.Known("act") || table.Known("stat") {
		t.Error("Known")
	}
	if got := (Table{Entries: table.Entries, Separator: " | "}).Expand(touch, "Snap | {act} | Go"); got != "Snap | Go" {
		t.Errorf("custom separator: %q", got)
	}
}

package actions

import (
	"strconv"
	"strings"
)

// PadMaker is the company that made a gamepad, as far as the browser's id
// for it tells.
type PadMaker uint8

const (
	MakerUnknown PadMaker = iota
	MakerMicrosoft
	MakerSony
	MakerNintendo
	MakerValve
	Maker8BitDo
	MakerLogitech
	MakerPDP
	MakerPowerA
	MakerHori
	MakerRazer
	MakerGoogle
)

// PadIdentity is what a browser's id string says about a gamepad.
type PadIdentity struct {
	// Raw is the browser's id, unchanged. Two pads of the same model have
	// the same Raw: it is a hint for telling pads apart, not an identity.
	Raw string
	// Vendor and Product are the USB numbers, or 0 when the id gives none
	// (Safari, and Chrome's XInput pads on Windows).
	Vendor, Product uint16
	// Name is the id without its vendor and product part, for a menu:
	// "DualSense Wireless Controller".
	Name string
	// Maker is who made the pad, from the vendor number, else from the name.
	Maker PadMaker
	// Style is whose names the buttons wear: the console maker's for a
	// console maker's pad, and for another maker's pad the console its name
	// says it copies ("PowerA Xbox Series X Controller", "HORIPAD for
	// Nintendo Switch"). A pad that says neither wears the standard names,
	// A, B, X and Y in the standard places: a presentation fallback, not a
	// claim about the hardware.
	Style PadStyle
}

// vendors maps a USB vendor number to its maker and the names its pads wear
// when the name says nothing more.
var vendors = map[uint16]struct {
	maker PadMaker
	style PadStyle
}{
	0x045e: {MakerMicrosoft, PadXbox},
	0x054c: {MakerSony, PadPlayStation},
	0x057e: {MakerNintendo, PadNintendo},
	0x28de: {MakerValve, PadGeneric},
	0x2dc8: {Maker8BitDo, PadGeneric},
	0x046d: {MakerLogitech, PadGeneric},
	0x0e6f: {MakerPDP, PadGeneric},
	0x24c6: {MakerPowerA, PadGeneric},
	0x20d6: {MakerPowerA, PadGeneric},
	0x0f0d: {MakerHori, PadGeneric},
	0x1532: {MakerRazer, PadGeneric},
	0x18d1: {MakerGoogle, PadGeneric},
}

// nameWords decide the maker when the id carries no vendor number, and the
// style of a third-party pad whose name says which console it copies (a
// Hori or PDP pad for the Switch or the PlayStation).
var nameWords = []struct {
	word  string
	maker PadMaker
	style PadStyle
}{
	{"xbox", MakerMicrosoft, PadXbox}, {"xinput", MakerMicrosoft, PadXbox},
	{"dualsense", MakerSony, PadPlayStation}, {"dualshock", MakerSony, PadPlayStation},
	{"playstation", MakerSony, PadPlayStation}, {"ps4", MakerSony, PadPlayStation}, {"ps5", MakerSony, PadPlayStation},
	{"nintendo", MakerNintendo, PadNintendo}, {"joy-con", MakerNintendo, PadNintendo},
	{"pro controller", MakerNintendo, PadNintendo}, {"switch", MakerNintendo, PadNintendo},
	{"8bitdo", Maker8BitDo, PadGeneric}, {"steam", MakerValve, PadGeneric}, {"stadia", MakerGoogle, PadGeneric},
}

// ParsePadID reads a browser's id for a gamepad. It understands Chrome's
// form, "Wireless Controller (STANDARD GAMEPAD Vendor: 054c Product: 0ce6)",
// Firefox's, "054c-0ce6-Wireless Controller", and names with no numbers
// (Safari, and "Xbox 360 Controller (XInput STANDARD GAMEPAD)"). The vendor
// number decides before the words, because more than one maker calls its
// pad "Wireless Controller".
func ParsePadID(id string) PadIdentity {
	p := PadIdentity{Raw: id, Name: strings.TrimSpace(id)}
	lower := strings.ToLower(id)
	if v, ok := hexAfter(lower, "vendor: "); ok {
		p.Vendor = v
		p.Product, _ = hexAfter(lower, "product: ")
		if open := strings.LastIndexByte(p.Name, '('); open > 0 {
			p.Name = strings.TrimSpace(p.Name[:open])
		}
	} else if len(lower) >= 10 && lower[4] == '-' && lower[9] == '-' {
		v, err1 := strconv.ParseUint(lower[:4], 16, 16)
		pr, err2 := strconv.ParseUint(lower[5:9], 16, 16)
		if err1 == nil && err2 == nil {
			p.Vendor, p.Product = uint16(v), uint16(pr)
			p.Name = strings.TrimSpace(id[10:])
		}
	} else if open := strings.Index(lower, "(xinput"); open > 0 {
		p.Name = strings.TrimSpace(p.Name[:open])
	} else if open := strings.Index(lower, "(standard gamepad"); open > 0 {
		p.Name = strings.TrimSpace(p.Name[:open])
	}
	words := func() (PadMaker, PadStyle, bool) {
		name := strings.ToLower(p.Name)
		for _, w := range nameWords {
			if strings.Contains(name, w.word) {
				return w.maker, w.style, true
			}
		}
		return MakerUnknown, PadGeneric, false
	}
	if v, ok := vendors[p.Vendor]; ok && p.Vendor != 0 {
		p.Maker, p.Style = v.maker, v.style
		if console := v.maker == MakerMicrosoft || v.maker == MakerSony || v.maker == MakerNintendo; !console {
			if _, style, ok := words(); ok {
				p.Style = style
			}
		}
		return p
	}
	p.Maker, p.Style, _ = words()
	return p
}

// hexAfter reads the four hex digits after label in s.
func hexAfter(s, label string) (uint16, bool) {
	i := strings.Index(s, label)
	if i < 0 || len(s) < i+len(label)+4 {
		return 0, false
	}
	v, err := strconv.ParseUint(s[i+len(label):i+len(label)+4], 16, 16)
	if err != nil {
		return 0, false
	}
	return uint16(v), true
}

// Glyph is how one control is drawn in a prompt: a button cap with its
// short name, a keycap, or a stick. Text is always the words the glyph
// stands for, for a screen reader, a test and a page with no styles.
type Glyph struct {
	// Text is the control's short name: "A", "✕", "LB", "L2", "Space".
	Text string
	// Kind says how the glyph is drawn: "face" (a round face button),
	// "shoulder" and "trigger" (pills), "menu" (Start, View, Options and
	// Home), "stick", "dpad" or "key" (a keycap).
	Kind string
	// Pad is whose buttons the glyph wears: "xbox", "playstation",
	// "nintendo" or "pad", and "keys" for a keycap.
	Pad string
	// Cap is what the glyph shows: a letter ("A"), a short name ("LB",
	// "L2", "Space"), a stick ("LS", "RS"), or a PlayStation face button's
	// shape ("cross", "circle", "square", "triangle").
	Cap string
	// Slot is where a face button sits ("south", "east", "west", "north"),
	// or which way a d-pad or a stick is pushed ("up", "down", "left",
	// "right"); "" otherwise.
	Slot string
}

// shortNames are the names printed on each button of the standard mapping,
// by style. Nintendo's A and B, and X and Y, sit the other way round.
var shortNames = [...][17]string{
	PadGeneric:     {"A", "B", "X", "Y", "LB", "RB", "LT", "RT", "Back", "Start", "LS", "RS", "D-pad up", "D-pad down", "D-pad left", "D-pad right", "Home"},
	PadXbox:        {"A", "B", "X", "Y", "LB", "RB", "LT", "RT", "View", "Menu", "LS", "RS", "D-pad up", "D-pad down", "D-pad left", "D-pad right", "Xbox"},
	PadPlayStation: {"✕", "○", "□", "△", "L1", "R1", "L2", "R2", "Create", "Options", "L3", "R3", "D-pad up", "D-pad down", "D-pad left", "D-pad right", "PS"},
	PadNintendo:    {"B", "A", "Y", "X", "L", "R", "ZL", "ZR", "−", "+", "LS", "RS", "D-pad up", "D-pad down", "D-pad left", "D-pad right", "Home"},
}

// capNames are what a glyph shows for a button that is not a face button or
// the d-pad; a home button shows "Home" on every pad but the PlayStation.
var capNames = [...][17]string{
	PadGeneric:     {4: "LB", 5: "RB", 6: "LT", 7: "RT", 8: "Back", 9: "Start", 10: "LS", 11: "RS", 16: "Home"},
	PadXbox:        {4: "LB", 5: "RB", 6: "LT", 7: "RT", 8: "View", 9: "Menu", 10: "LS", 11: "RS", 16: "Home"},
	PadPlayStation: {4: "L1", 5: "R1", 6: "L2", 7: "R2", 8: "Create", 9: "Options", 10: "L3", 11: "R3", 16: "PS"},
	PadNintendo:    {4: "L", 5: "R", 6: "ZL", 7: "ZR", 8: "−", 9: "+", 10: "LS", 11: "RS", 16: "Home"},
}

var (
	faceSlots = [4]string{"south", "east", "west", "north"}
	psShapes  = [4]string{"cross", "circle", "square", "triangle"}
	dpadSlots = [4]string{"up", "down", "left", "right"}
)

// StyleName is the name of a pad style as a page carries it: "pad",
// "xbox", "playstation" or "nintendo".
func StyleName(style PadStyle) string {
	switch style {
	case PadXbox:
		return "xbox"
	case PadPlayStation:
		return "playstation"
	case PadNintendo:
		return "nintendo"
	}
	return "pad"
}

// ButtonName is the short name printed on button n of the standard mapping
// for a pad style: "A", "✕", "LB", "ZR". It returns "" for a button the
// standard mapping does not have.
func ButtonName(style PadStyle, n int) string {
	if int(style) >= len(shortNames) || n < 0 || n >= len(shortNames[style]) {
		return ""
	}
	return shortNames[style][n]
}

// ButtonGlyph is the glyph of button n of the standard mapping for a pad
// style. It returns the zero Glyph for a button the mapping does not have.
func ButtonGlyph(style PadStyle, n int) Glyph {
	name := ButtonName(style, n)
	if name == "" {
		return Glyph{}
	}
	g := Glyph{Text: name, Pad: StyleName(style)}
	switch {
	case n <= 3:
		g.Kind, g.Slot, g.Cap = "face", faceSlots[n], name
		if style == PadPlayStation {
			g.Cap = psShapes[n]
		}
	case n <= 5:
		g.Kind = "shoulder"
	case n <= 7:
		g.Kind = "trigger"
	case n <= 9 || n == 16:
		g.Kind = "menu"
	case n <= 11:
		g.Kind = "stick"
	default:
		g.Kind, g.Slot = "dpad", dpadSlots[n-12]
	}
	if g.Cap == "" {
		g.Cap = capNames[style][n]
	}
	return g
}

// KeyGlyph is a keycap that shows text: "Space", "WASD", "1–5".
func KeyGlyph(text string) Glyph {
	return Glyph{Text: text, Kind: "key", Pad: "keys", Cap: text}
}

package actions

import "testing"

func TestParsePadID(t *testing.T) {
	for _, c := range []struct {
		id              string
		vendor, product uint16
		name            string
		maker           PadMaker
		style           PadStyle
	}{
		{"Xbox Wireless Controller (STANDARD GAMEPAD Vendor: 045e Product: 0b13)", 0x045e, 0x0b13, "Xbox Wireless Controller", MakerMicrosoft, PadXbox},
		{"DualSense Wireless Controller (STANDARD GAMEPAD Vendor: 054c Product: 0ce6)", 0x054c, 0x0ce6, "DualSense Wireless Controller", MakerSony, PadPlayStation},
		// Sony and others both call a pad "Wireless Controller": the number decides.
		{"Wireless Controller (STANDARD GAMEPAD Vendor: 054c Product: 09cc)", 0x054c, 0x09cc, "Wireless Controller", MakerSony, PadPlayStation},
		{"Wireless Controller (STANDARD GAMEPAD Vendor: 045e Product: 02e0)", 0x045e, 0x02e0, "Wireless Controller", MakerMicrosoft, PadXbox},
		{"Pro Controller (STANDARD GAMEPAD Vendor: 057e Product: 2009)", 0x057e, 0x2009, "Pro Controller", MakerNintendo, PadNintendo},
		// Firefox.
		{"054c-0ce6-DualSense Wireless Controller", 0x054c, 0x0ce6, "DualSense Wireless Controller", MakerSony, PadPlayStation},
		{"045e-0b12-Microsoft Xbox Series S|X Controller", 0x045e, 0x0b12, "Microsoft Xbox Series S|X Controller", MakerMicrosoft, PadXbox},
		// Chrome on Windows through XInput, and Safari: names only.
		{"Xbox 360 Controller (XInput STANDARD GAMEPAD)", 0, 0, "Xbox 360 Controller", MakerMicrosoft, PadXbox},
		{"DualSense Wireless Controller", 0, 0, "DualSense Wireless Controller", MakerSony, PadPlayStation},
		{"Joy-Con L+R", 0, 0, "Joy-Con L+R", MakerNintendo, PadNintendo},
		{"DualSense Wireless Controller Extended Gamepad", 0, 0, "DualSense Wireless Controller Extended Gamepad", MakerSony, PadPlayStation},
		{"Xbox style pad (STANDARD GAMEPAD Vendor: 054c Product: 0001)", 0x054c, 0x0001, "Xbox style pad", MakerSony, PadPlayStation},
		// A licensed third-party pad wears the console's names when its name says so.
		{"HORIPAD for Nintendo Switch (STANDARD GAMEPAD Vendor: 0f0d Product: 00c1)", 0x0f0d, 0x00c1, "HORIPAD for Nintendo Switch", MakerHori, PadNintendo},
		{"PowerA Xbox Series X Controller (STANDARD GAMEPAD Vendor: 20d6 Product: 2001)", 0x20d6, 0x2001, "PowerA Xbox Series X Controller", MakerPowerA, PadXbox},
		// A third-party maker whose name names no console: the standard names.
		{"8BitDo Pro 2 (STANDARD GAMEPAD Vendor: 2dc8 Product: 6006)", 0x2dc8, 0x6006, "8BitDo Pro 2", Maker8BitDo, PadGeneric},
		{"Logitech Gamepad F310 (STANDARD GAMEPAD Vendor: 046d Product: c216)", 0x046d, 0xc216, "Logitech Gamepad F310", MakerLogitech, PadGeneric},
		// A console maker's number decides over misleading words.
		{"Xbox-style Pro Controller (STANDARD GAMEPAD Vendor: 057e Product: 2009)", 0x057e, 0x2009, "Xbox-style Pro Controller", MakerNintendo, PadNintendo},
		// Upper-case hex.
		{"Wireless Controller (STANDARD GAMEPAD Vendor: 054C Product: 0CE6)", 0x054c, 0x0ce6, "Wireless Controller", MakerSony, PadPlayStation},
		// A pad the browser knows nothing about.
		{"Stub Standard Pad (STANDARD GAMEPAD)", 0, 0, "Stub Standard Pad", MakerUnknown, PadGeneric},
		{"Generic USB Joystick (STANDARD GAMEPAD Vendor: 0079 Product: 0006)", 0x0079, 0x0006, "Generic USB Joystick", MakerUnknown, PadGeneric},
		{"", 0, 0, "", MakerUnknown, PadGeneric},
		// Not hex where the numbers should be: no numbers, and no panic.
		{"zzzz-yyyy-Odd pad", 0, 0, "zzzz-yyyy-Odd pad", MakerUnknown, PadGeneric},
		{"Odd (Vendor: 05", 0, 0, "Odd (Vendor: 05", MakerUnknown, PadGeneric},
	} {
		got := ParsePadID(c.id)
		want := PadIdentity{Raw: c.id, Vendor: c.vendor, Product: c.product, Name: c.name, Maker: c.maker, Style: c.style}
		if got != want {
			t.Errorf("ParsePadID(%q) = %+v, want %+v", c.id, got, want)
		}
	}
}

func TestButtonGlyph(t *testing.T) {
	for _, c := range []struct {
		style PadStyle
		n     int
		want  Glyph
	}{
		{PadXbox, 0, Glyph{Text: "A", Kind: "face", Pad: "xbox", Cap: "A", Slot: "south"}},
		{PadPlayStation, 0, Glyph{Text: "✕", Kind: "face", Pad: "playstation", Cap: "cross", Slot: "south"}},
		{PadPlayStation, 3, Glyph{Text: "△", Kind: "face", Pad: "playstation", Cap: "triangle", Slot: "north"}},
		// Nintendo: the south button is B.
		{PadNintendo, 0, Glyph{Text: "B", Kind: "face", Pad: "nintendo", Cap: "B", Slot: "south"}},
		{PadNintendo, 7, Glyph{Text: "ZR", Kind: "trigger", Pad: "nintendo", Cap: "ZR"}},
		{PadGeneric, 4, Glyph{Text: "LB", Kind: "shoulder", Pad: "pad", Cap: "LB"}},
		{PadXbox, 9, Glyph{Text: "Menu", Kind: "menu", Pad: "xbox", Cap: "Menu"}},
		{PadPlayStation, 11, Glyph{Text: "R3", Kind: "stick", Pad: "playstation", Cap: "R3"}},
		{PadXbox, 14, Glyph{Text: "D-pad left", Kind: "dpad", Pad: "xbox", Cap: "", Slot: "left"}},
		{PadXbox, 16, Glyph{Text: "Xbox", Kind: "menu", Pad: "xbox", Cap: "Home"}},
		{PadXbox, 17, Glyph{}},
		{PadXbox, -1, Glyph{}},
		{PadStyle(9), 0, Glyph{}},
	} {
		if got := ButtonGlyph(c.style, c.n); got != c.want {
			t.Errorf("ButtonGlyph(%d, %d) = %+v, want %+v", c.style, c.n, got, c.want)
		}
	}
	if g := KeyGlyph("Space"); g != (Glyph{Text: "Space", Kind: "key", Pad: "keys", Cap: "Space"}) {
		t.Errorf("KeyGlyph = %+v", g)
	}
}

func TestLabelNintendoAndUnknownStyles(t *testing.T) {
	if got := Label(PadButton(0), PadNintendo); got != "B" {
		t.Errorf("Nintendo south = %q, want B", got)
	}
	if got := Label(PadButton(9), PadNintendo); got != "Plus" {
		t.Errorf("Nintendo start = %q, want Plus", got)
	}
	// A style past the table falls back to the generic names.
	if got := Label(PadButton(0), PadStyle(7)); got != "Button 0" {
		t.Errorf("unknown style = %q, want Button 0", got)
	}
}

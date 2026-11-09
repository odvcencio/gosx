package kitui

import (
	"strconv"

	"m31labs.dev/gosx"
)

// SideSelectProps configures SideSelectPanel.
type SideSelectProps struct {
	Model *SideSelect
	// Title defaults to "Choose sides".
	Title string
	// Labels override the English text: "ready", "spectate", "host", "hint",
	// "prev", "next".
	Labels map[string]string
}

var sideLabels = map[string]string{
	"title": "Choose sides", "ready": "Ready", "spectate": "Spectate", "host": "Host",
	"prev": "Previous", "next": "Next",
	"hint": "Left and Right move your icon. Confirm to ready up. Cancel to step back.",
}

func sideLabel(l map[string]string, k string) string {
	if v, ok := l[k]; ok {
		return v
	}
	return sideLabels[k]
}

const glyphKeyboard = `<svg viewBox="0 0 48 32" aria-hidden="true" focusable="false"><rect x="2" y="6" width="44" height="22" rx="4" fill="none" stroke="currentColor" stroke-width="2"/><path d="M8 13h4M16 13h4M24 13h4M32 13h4M40 13h0M10 19h4M18 19h12M34 19h4" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>`
const glyphGamepad = `<svg viewBox="0 0 48 32" aria-hidden="true" focusable="false"><path d="M12 8h24c5 0 8 5 9 12l1 5c.5 3-3 4-5 2l-4-4H11l-4 4c-2 2-5.500 1-5-2l1-5c1-7 4-12 9-12z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/><path d="M14 13v6M11 16h6" stroke="currentColor" stroke-width="2" stroke-linecap="round"/><circle cx="33" cy="14" r="1.600" fill="currentColor"/><circle cx="37" cy="18" r="1.600" fill="currentColor"/></svg>`
const glyphRemote = `<svg viewBox="0 0 48 32" aria-hidden="true" focusable="false"><circle cx="24" cy="16" r="11" fill="none" stroke="currentColor" stroke-width="2"/><path d="M13 16h22M24 5c-5 6-5 16 0 22M24 5c5 6 5 16 0 22" fill="none" stroke="currentColor" stroke-width="2"/></svg>`

func seatKindName(k SeatKind) string {
	switch k {
	case SeatKeyboard:
		return "keyboard"
	case SeatGamepad:
		return "gamepad"
	}
	return "remote"
}

func slotIndex(s Slot) int { return int(s) + 1 } // 0 = A, 1 = middle, 2 = B

func seatNode(m *SideSelect, st Seat, l map[string]string) gosx.Node {
	glyph := glyphGamepad
	switch st.Kind {
	case SeatKeyboard:
		glyph = glyphKeyboard
	case SeatRemote:
		glyph = glyphRemote
	}
	desc := st.Name + ", " + m.sideName(st.Slot)
	if st.Slot != SlotMiddle {
		if st.Ready {
			desc += ", " + sideLabel(l, "ready")
		}
	} else {
		desc += ", " + sideLabel(l, "spectate")
	}
	attrs := []any{
		gosx.Attr("class", "kit-device"), gosx.Attr("role", "listitem"),
		gosx.Attr("data-device", st.ID), gosx.Attr("data-kind", seatKindName(st.Kind)),
		gosx.Attr("data-slot", slotIndex(st.Slot)), gosx.Attr("data-from-slot", slotIndex(st.Prev)),
		gosx.Attr("data-ready", strconv.FormatBool(st.Ready)),
		gosx.Attr("data-focus", strconv.FormatBool(st.Focus >= 0)),
		gosx.Attr("style", "--slot:"+strconv.Itoa(slotIndex(st.Slot))+";--from:"+strconv.Itoa(slotIndex(st.Prev))),
		gosx.Attr("aria-label", desc),
	}
	children := []any{
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "kit-device-icon")), gosx.RawHTML(glyph)),
		gosx.El("span", gosx.Attrs(gosx.Attr("class", "kit-device-name")), gosx.Text(st.Name)),
	}
	if st.Host {
		children = append(children, gosx.El("span", gosx.Attrs(gosx.Attr("class", "kit-device-tag")), gosx.Text(sideLabel(l, "host"))))
	}
	if st.Ready {
		children = append(children, gosx.El("span", gosx.Attrs(gosx.Attr("class", "kit-device-ready")), gosx.Text(sideLabel(l, "ready"))))
	}
	return gosx.El("div", append([]any{gosx.Attrs(attrs...)}, children...)...)
}

// SideSelectPanel renders the side-select screen: each device is an icon in
// one of three columns (side A, the middle, side B), with a settings row
// under it. It is plain HTML with data attributes, so it works in a static
// export. The icon that just moved carries data-from-slot and a CSS variable
// --from, and the kit stylesheet slides it from the old column with a CSS
// animation; the animation is off under prefers-reduced-motion. Re-render the
// panel after each change (kitui.Render).
//
// The settings row is rendered as plain buttons too, so the mouse works:
// they send data-kit-action="sides-field" with value "<field>:<+1|-1>";
// pass that to SideSelect.HandleAction.
func SideSelectPanel(p SideSelectProps) gosx.Node {
	m, l := p.Model, p.Labels
	title := p.Title
	if title == "" {
		title = sideLabel(l, "title")
	}
	names := m.SideNames()
	cols := [3][]gosx.Node{}
	for _, st := range m.Seats() {
		i := slotIndex(st.Slot)
		cols[i] = append(cols[i], seatNode(m, st, l))
	}
	column := func(i int, class, name string) gosx.Node {
		args := []any{gosx.Attrs(gosx.Attr("class", "kit-side "+class), gosx.Attr("role", "list"), gosx.Attr("aria-label", name), gosx.Attr("data-side", i)),
			gosx.El("h3", gosx.Attrs(gosx.Attr("class", "kit-side-name")), gosx.Text(name))}
		for _, n := range cols[i] {
			args = append(args, n)
		}
		return gosx.El("section", args...)
	}
	root := []any{
		gosx.Attrs(gosx.Attr("class", "kit-sides"), gosx.Attr("role", "group"), gosx.Attr("aria-labelledby", "kit-sides-title"),
			gosx.Attr("data-startable", strconv.FormatBool(m.Startable()))),
		gosx.El("h2", gosx.Attrs(gosx.Attr("class", "kit-title"), gosx.Attr("id", "kit-sides-title")), gosx.Text(title)),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "kit-sides-track")),
			column(0, "kit-side--a", names[0]),
			column(1, "kit-side--mid", sideLabel(l, "spectate")),
			column(2, "kit-side--b", names[1])),
	}
	if fields := m.Fields(); len(fields) > 0 {
		focused := map[int]bool{}
		for _, st := range m.Seats() {
			if st.Local() && st.Focus >= 0 {
				focused[st.Focus] = true
			}
		}
		rows := []any{gosx.Attrs(gosx.Attr("class", "kit-sides-settings"), gosx.Attr("role", "list"), gosx.Attr("data-readonly", strconv.FormatBool(m.FieldsReadOnly())))}
		for i, f := range fields {
			value := ""
			if f.Index >= 0 && f.Index < len(f.Options) {
				value = f.Options[f.Index].Label
			}
			dis := m.FieldsReadOnly()
			btn := func(label, step string) gosx.Node {
				a := []any{gosx.Attr("type", "button"), gosx.Attr("class", "kit-button"), gosx.Attr("data-kit-action", "sides-field"),
					gosx.Attr("data-kit-value", f.ID+":"+step), gosx.Attr("aria-label", label+" "+f.Label)}
				if dis {
					a = append(a, gosx.Attr("disabled", true))
				}
				sym := "<"
				if step == "+1" {
					sym = ">"
				}
				return gosx.El("button", gosx.Attrs(a...), gosx.Text(sym))
			}
			rows = append(rows, gosx.El("div", gosx.Attrs(gosx.Attr("class", "kit-sides-field"), gosx.Attr("role", "listitem"),
				gosx.Attr("data-field", f.ID), gosx.Attr("data-focus", strconv.FormatBool(focused[i]))),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "kit-sides-field-label")), gosx.Text(f.Label)),
				btn(sideLabel(l, "prev"), "-1"),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "kit-value kit-sides-field-value")), gosx.Text(value)),
				btn(sideLabel(l, "next"), "+1")))
		}
		root = append(root, gosx.El("div", rows...))
	}
	root = append(root,
		gosx.El("p", gosx.Attrs(gosx.Attr("class", "kit-copy")), gosx.Text(sideLabel(l, "hint"))),
		gosx.El("p", gosx.Attrs(gosx.Attr("class", "kit-sr-only"), gosx.Attr("role", "status"), gosx.Attr("aria-live", "polite")), gosx.Text(m.Status())))
	return gosx.El("div", root...)
}

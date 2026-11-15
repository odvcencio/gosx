package kitui

import (
	"strconv"

	"m31labs.dev/gosx/markup"
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

func seatNode(m *SideSelect, st Seat, l map[string]string) markup.Node {
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
		markup.Attr("class", "kit-device"), markup.Attr("role", "listitem"),
		markup.Attr("data-device", st.ID), markup.Attr("data-kind", seatKindName(st.Kind)),
		markup.Attr("data-slot", slotIndex(st.Slot)), markup.Attr("data-from-slot", slotIndex(st.Prev)),
		markup.Attr("data-ready", strconv.FormatBool(st.Ready)),
		markup.Attr("data-focus", strconv.FormatBool(st.Focus >= 0)),
		markup.Attr("style", "--slot:"+strconv.Itoa(slotIndex(st.Slot))+";--from:"+strconv.Itoa(slotIndex(st.Prev))),
		markup.Attr("aria-label", desc),
	}
	children := []any{
		markup.El("span", markup.Attrs(markup.Attr("class", "kit-device-icon")), markup.RawHTML(glyph)),
		markup.El("span", markup.Attrs(markup.Attr("class", "kit-device-name")), markup.Text(st.Name)),
	}
	if st.Host {
		children = append(children, markup.El("span", markup.Attrs(markup.Attr("class", "kit-device-tag")), markup.Text(sideLabel(l, "host"))))
	}
	if st.Ready {
		children = append(children, markup.El("span", markup.Attrs(markup.Attr("class", "kit-device-ready")), markup.Text(sideLabel(l, "ready"))))
	}
	return markup.El("div", append([]any{markup.Attrs(attrs...)}, children...)...)
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
func SideSelectPanel(p SideSelectProps) markup.Node {
	m, l := p.Model, p.Labels
	title := p.Title
	if title == "" {
		title = sideLabel(l, "title")
	}
	names := m.SideNames()
	cols := [3][]markup.Node{}
	for _, st := range m.Seats() {
		i := slotIndex(st.Slot)
		cols[i] = append(cols[i], seatNode(m, st, l))
	}
	column := func(i int, class, name string) markup.Node {
		args := []any{markup.Attrs(markup.Attr("class", "kit-side "+class), markup.Attr("role", "list"), markup.Attr("aria-label", name), markup.Attr("data-side", i)),
			markup.El("h3", markup.Attrs(markup.Attr("class", "kit-side-name")), markup.Text(name))}
		for _, n := range cols[i] {
			args = append(args, n)
		}
		return markup.El("section", args...)
	}
	root := []any{
		markup.Attrs(markup.Attr("class", "kit-sides"), markup.Attr("role", "group"), markup.Attr("aria-labelledby", "kit-sides-title"),
			markup.Attr("data-startable", strconv.FormatBool(m.Startable()))),
		markup.El("h2", markup.Attrs(markup.Attr("class", "kit-title"), markup.Attr("id", "kit-sides-title")), markup.Text(title)),
		markup.El("div", markup.Attrs(markup.Attr("class", "kit-sides-track")),
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
		rows := []any{markup.Attrs(markup.Attr("class", "kit-sides-settings"), markup.Attr("role", "list"), markup.Attr("data-readonly", strconv.FormatBool(m.FieldsReadOnly())))}
		for i, f := range fields {
			value := ""
			if f.Index >= 0 && f.Index < len(f.Options) {
				value = f.Options[f.Index].Label
			}
			dis := m.FieldsReadOnly()
			btn := func(label, step string) markup.Node {
				a := []any{markup.Attr("type", "button"), markup.Attr("class", "kit-button"), markup.Attr("data-kit-action", "sides-field"),
					markup.Attr("data-kit-value", f.ID+":"+step), markup.Attr("aria-label", label+" "+f.Label)}
				if dis {
					a = append(a, markup.Attr("disabled", true))
				}
				sym := "<"
				if step == "+1" {
					sym = ">"
				}
				return markup.El("button", markup.Attrs(a...), markup.Text(sym))
			}
			rows = append(rows, markup.El("div", markup.Attrs(markup.Attr("class", "kit-sides-field"), markup.Attr("role", "listitem"),
				markup.Attr("data-field", f.ID), markup.Attr("data-focus", strconv.FormatBool(focused[i]))),
				markup.El("span", markup.Attrs(markup.Attr("class", "kit-sides-field-label")), markup.Text(f.Label)),
				btn(sideLabel(l, "prev"), "-1"),
				markup.El("span", markup.Attrs(markup.Attr("class", "kit-value kit-sides-field-value")), markup.Text(value)),
				btn(sideLabel(l, "next"), "+1")))
		}
		root = append(root, markup.El("div", rows...))
	}
	root = append(root,
		markup.El("p", markup.Attrs(markup.Attr("class", "kit-copy")), markup.Text(sideLabel(l, "hint"))),
		markup.El("p", markup.Attrs(markup.Attr("class", "kit-sr-only"), markup.Attr("role", "status"), markup.Attr("aria-live", "polite")), markup.Text(m.Status())))
	return markup.El("div", root...)
}

package kitui

import "m31labs.dev/gosx/markup"

// DialogProps names the parts of a dialog that a game writes into. Each part
// is found again by its id (FindModal in a js/wasm build). The classes are
// extra classes for the game's own stylesheet, beside the kit-dialog ones.
type DialogProps struct {
	// ID is the <dialog> element's id.
	ID string
	// Title is shown in the dialog's head.
	Title string
	// CloseID is the id of the head's close button ("" for none).
	CloseID string
	// BodyID is the id of the part that scrolls.
	BodyID string
	// FootID is the id of the footer below the body ("" for none). It is
	// hidden until a dialog asks for one.
	FootID string
	// HeadClass and FootClass are added to the head's and the footer's
	// classes.
	HeadClass, FootClass string
}

// Dialog is an empty <dialog> that a game writes into: a head with the title
// and a close button, a body that scrolls, and a footer, outside the body,
// that keeps a dialog's main action in reach. Render it with the page; in
// the browser, FindModal finds it and Modal fills, opens and closes it.
func Dialog(p DialogProps) markup.Node {
	head := []any{markup.Attrs(markup.Attr("class", classes("kit-dialog-head", p.HeadClass))),
		markup.El("span", markup.Text(p.Title))}
	if p.CloseID != "" {
		head = append(head, markup.El("button", markup.Attrs(markup.Attr("type", "button"), markup.Attr("id", p.CloseID),
			markup.Attr("class", "kit-dialog-close"), markup.Attr("aria-label", "Close")), markup.Text("×")))
	}
	parts := []any{markup.Attrs(markup.Attr("id", p.ID), markup.Attr("class", "kit-dialog")),
		markup.El("div", head...),
		markup.El("div", markup.Attrs(markup.Attr("id", p.BodyID), markup.Attr("class", "kit-dialog-body")))}
	if p.FootID != "" {
		parts = append(parts, markup.El("div", markup.Attrs(markup.Attr("id", p.FootID),
			markup.Attr("class", classes("kit-dialog-foot", p.FootClass)), markup.Attr("hidden", true))))
	}
	return markup.El("dialog", parts...)
}

func classes(base, extra string) string {
	if extra == "" {
		return base
	}
	return base + " " + extra
}

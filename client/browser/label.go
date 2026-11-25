package browser

import "strconv"

// Label is an element an engine places over the page at a point each frame,
// such as a caption that follows something in a 3D scene (see
// camera.Lens.Project). It touches the DOM only when what it shows changes,
// so a label that stands still costs nothing per frame.
type Label struct {
	el     Element
	hidden bool
	text   string
	x, y   int
	vars   map[string]string
}

// NewLabel takes over el. The label starts hidden.
func NewLabel(el Element) *Label {
	l := &Label{el: el, x: -1 << 31, y: -1 << 31}
	el.SetHidden(true)
	l.hidden = true
	return l
}

// Element is the label's element.
func (l *Label) Element() Element { return l.el }

// Show centres the label on (x, y), in pixels from the top left of its
// positioned container, rounded down to whole pixels, with the given text.
func (l *Label) Show(x, y float64, text string) {
	if l.hidden {
		l.el.SetHidden(false)
		l.hidden = false
	}
	if text != l.text {
		l.el.SetText(text)
		l.text = text
	}
	if px, py := int(x), int(y); px != l.x || py != l.y {
		l.el.Style().Set("transform", "translate("+strconv.Itoa(px)+"px,"+strconv.Itoa(py)+"px) translate(-50%,-50%)")
		l.x, l.y = px, py
	}
}

// SetVar sets a CSS custom property on the label, such as its colour.
func (l *Label) SetVar(name, value string) {
	if l.vars[name] == value {
		return
	}
	if l.vars == nil {
		l.vars = map[string]string{}
	}
	l.el.Style().Set(name, value)
	l.vars[name] = value
}

// Hide hides the label.
func (l *Label) Hide() {
	if !l.hidden {
		l.el.SetHidden(true)
		l.hidden = true
	}
}

// Hidden reports whether the label is hidden.
func (l *Label) Hidden() bool { return l.hidden }

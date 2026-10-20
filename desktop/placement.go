package desktop

// WindowPlacement describes a window's restored (normal) bounds in screen
// pixels and whether it should be shown maximized.
type WindowPlacement struct {
	X, Y, Width, Height int
	Maximized           bool
}

// IsZero reports whether p is the zero placement.
func (p WindowPlacement) IsZero() bool {
	return p == (WindowPlacement{})
}

type screenRect struct {
	Left, Top, Right, Bottom int
}

func clampPlacement(p WindowPlacement, workAreas []screenRect) WindowPlacement {
	if p.Width < 200 || p.Height < 200 {
		return WindowPlacement{}
	}
	if len(workAreas) == 0 {
		return p
	}

	right := p.X + p.Width
	bottom := p.Y + p.Height
	for _, area := range workAreas {
		left := maxInt(p.X, area.Left)
		top := maxInt(p.Y, area.Top)
		intersectionRight := minInt(right, area.Right)
		intersectionBottom := minInt(bottom, area.Bottom)
		if intersectionRight-left >= 100 && intersectionBottom-top >= 50 {
			return p
		}
	}

	area := workAreas[0]
	x := area.Left + 32
	y := area.Top + 32
	p.X = x
	p.Y = y
	p.Width = minInt(p.Width, maxInt(0, area.Right-x))
	p.Height = minInt(p.Height, maxInt(0, area.Bottom-y))
	return p
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

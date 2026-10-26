package desktop

// WindowPlacement describes a window's restored (normal) bounds in screen
// pixels and whether it should be shown maximized.
type WindowPlacement struct {
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Maximized bool   `json:"maximized"`
	Monitor   string `json:"monitor,omitempty"`
}

// IsZero reports whether p is the zero placement.
func (p WindowPlacement) IsZero() bool {
	return p == (WindowPlacement{})
}

type screenRect struct {
	Left, Top, Right, Bottom int
}

type monitorArea struct {
	Name    string
	Primary bool
	Work    screenRect
}

func clampPlacement(p WindowPlacement, workAreas []screenRect) WindowPlacement {
	if p.Width < 200 || p.Height < 200 {
		return WindowPlacement{}
	}
	if len(workAreas) == 0 {
		return p
	}
	right, bottom := p.X+p.Width, p.Y+p.Height
	for _, area := range workAreas {
		left, top := maxInt(p.X, area.Left), maxInt(p.Y, area.Top)
		intersectionRight := minInt(right, area.Right)
		intersectionBottom := minInt(bottom, area.Bottom)
		if intersectionRight-left >= 100 && intersectionBottom-top >= 50 {
			return p
		}
	}
	area := workAreas[0]
	x, y := area.Left+32, area.Top+32
	p.X, p.Y = x, y
	p.Width = minInt(p.Width, maxInt(0, area.Right-x))
	p.Height = minInt(p.Height, maxInt(0, area.Bottom-y))
	return p
}

func restoreSavedPlacement(p WindowPlacement, monitors []monitorArea, width, height int) WindowPlacement {
	if p.Width < 200 || p.Height < 200 || len(monitors) == 0 {
		return p
	}

	selected := -1
	if p.Monitor != "" {
		for i, monitor := range monitors {
			if monitor.Name == p.Monitor {
				selected = i
				break
			}
		}
		if selected >= 0 && placementVisibleOn(p, monitors[selected].Work) {
			return p
		}
		if selected < 0 {
			selected = primaryMonitor(monitors)
		}
	} else {
		for _, monitor := range monitors {
			if placementVisibleOn(p, monitor.Work) {
				return p
			}
		}
		selected = primaryMonitor(monitors)
	}
	if selected < 0 {
		return p
	}

	area := monitors[selected]
	if width <= 0 {
		width = p.Width
	}
	if height <= 0 {
		height = p.Height
	}
	areaWidth, areaHeight := area.Work.Right-area.Work.Left, area.Work.Bottom-area.Work.Top
	width, height = minInt(width, areaWidth), minInt(height, areaHeight)
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	p.X = area.Work.Left + (areaWidth-width)/2
	p.Y = area.Work.Top + (areaHeight-height)/2
	p.Width, p.Height, p.Monitor = width, height, area.Name
	return p
}

func placementVisibleOn(p WindowPlacement, area screenRect) bool {
	left, top := maxInt(p.X, area.Left), maxInt(p.Y, area.Top)
	right, bottom := minInt(p.X+p.Width, area.Right), minInt(p.Y+p.Height, area.Bottom)
	return right-left >= 100 && bottom-top >= 50
}

func primaryMonitor(monitors []monitorArea) int {
	for i, monitor := range monitors {
		if monitor.Primary {
			return i
		}
	}
	if len(monitors) > 0 {
		return 0
	}
	return -1
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

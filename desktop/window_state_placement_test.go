package desktop

import "testing"

func TestRestoreSavedPlacement(t *testing.T) {
	areas := []monitorArea{
		{Name: `\\.\DISPLAY1`, Primary: true, Work: screenRect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}},
		{Name: `\\.\DISPLAY2`, Work: screenRect{Left: -1280, Top: 0, Right: 0, Bottom: 1024}},
	}
	t.Run("visible secondary bounds retained", func(t *testing.T) {
		p := WindowPlacement{X: -1200, Y: 100, Width: 800, Height: 600, Monitor: areas[1].Name}
		if got := restoreSavedPlacement(p, areas, 600, 400); got != p {
			t.Fatalf("placement = %+v, want %+v", got, p)
		}
	})
	t.Run("offscreen bounds centre on saved monitor", func(t *testing.T) {
		p := WindowPlacement{X: 100, Y: 100, Width: 900, Height: 700, Monitor: areas[1].Name}
		got := restoreSavedPlacement(p, areas, 600, 400)
		if got.X != -940 || got.Y != 312 || got.Width != 600 || got.Height != 400 {
			t.Fatalf("placement = %+v", got)
		}
	})
	t.Run("disconnected monitor falls back to primary", func(t *testing.T) {
		p := WindowPlacement{X: -1200, Y: 100, Width: 900, Height: 700, Maximized: true, Monitor: "missing"}
		got := restoreSavedPlacement(p, areas, 600, 400)
		if got.X != 660 || got.Y != 340 || got.Width != 600 || got.Height != 400 || !got.Maximized {
			t.Fatalf("placement = %+v", got)
		}
	})
	t.Run("monitorless visible negative coordinates retained", func(t *testing.T) {
		p := WindowPlacement{X: -1200, Y: 100, Width: 900, Height: 700, Maximized: true}
		if got := restoreSavedPlacement(p, areas, 600, 400); got != p {
			t.Fatalf("placement = %+v, want %+v", got, p)
		}
	})
	t.Run("monitorless offscreen placement centres on primary", func(t *testing.T) {
		p := WindowPlacement{X: -5000, Y: 5000, Width: 900, Height: 700}
		got := restoreSavedPlacement(p, areas, 600, 400)
		if got.X != 660 || got.Y != 340 {
			t.Fatalf("placement = %+v", got)
		}
	})
}

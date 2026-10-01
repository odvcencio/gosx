package desktop

import "testing"

func TestWindowPlacementIsZero(t *testing.T) {
	tests := []struct {
		name      string
		placement WindowPlacement
		want      bool
	}{
		{name: "zero", want: true},
		{name: "bounds", placement: WindowPlacement{X: 1, Width: 200}, want: false},
		{name: "maximized", placement: WindowPlacement{Maximized: true}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.placement.IsZero(); got != tt.want {
				t.Fatalf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClampPlacement(t *testing.T) {
	areas := []screenRect{{Left: 0, Top: 0, Right: 1000, Bottom: 800}}
	tests := []struct {
		name  string
		p     WindowPlacement
		areas []screenRect
		want  WindowPlacement
	}{
		{
			name:  "fully visible stays",
			p:     WindowPlacement{X: 100, Y: 100, Width: 600, Height: 500},
			areas: areas,
			want:  WindowPlacement{X: 100, Y: 100, Width: 600, Height: 500},
		},
		{
			name:  "off all monitors moves to first area",
			p:     WindowPlacement{X: 2000, Y: 1200, Width: 600, Height: 500},
			areas: []screenRect{{Left: -1000, Top: 100, Right: 0, Bottom: 700}},
			want:  WindowPlacement{X: -968, Y: 132, Width: 600, Height: 500},
		},
		{
			name:  "too large shrinks",
			p:     WindowPlacement{X: 2000, Y: 1200, Width: 1200, Height: 900},
			areas: areas,
			want:  WindowPlacement{X: 32, Y: 32, Width: 968, Height: 768},
		},
		{
			name:  "tiny returns zero",
			p:     WindowPlacement{X: 10, Y: 10, Width: 199, Height: 500},
			areas: areas,
			want:  WindowPlacement{},
		},
		{
			name:  "partial overlap threshold stays",
			p:     WindowPlacement{X: 900, Y: 720, Width: 300, Height: 200},
			areas: areas,
			want:  WindowPlacement{X: 900, Y: 720, Width: 300, Height: 200},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampPlacement(tt.p, tt.areas); got != tt.want {
				t.Fatalf("clampPlacement() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestNormalizeOptionsZeroInitialPlacementPreservesDefaults(t *testing.T) {
	got, err := normalizeOptions(Options{})
	if err != nil {
		t.Fatalf("normalizeOptions: %v", err)
	}
	if !got.InitialPlacement.IsZero() || got.Width != defaultWidth || got.Height != defaultHeight {
		t.Fatalf("normalized options = %+v, want default size and zero placement", got)
	}
}

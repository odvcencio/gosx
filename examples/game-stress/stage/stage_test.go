package stage

import "testing"

func TestCrowdPlacesEveryFigureOnce(t *testing.T) {
	for _, total := range []int{0, 1, 10, 2000, 40000} {
		sections := Crowd(total, 8, 50, 30)
		got := 0
		for _, s := range sections {
			got += len(s.Seats)
			for _, seat := range s.Seats {
				if seat.Tint < 0 || seat.Tint >= len(Palette) {
					t.Fatalf("tint %d out of the palette", seat.Tint)
				}
				if (seat.X > -50 && seat.X < 50) && (seat.Z > -30 && seat.Z < 30) {
					t.Fatalf("a seat at (%.1f, %.1f) is on the arena", seat.X, seat.Z)
				}
			}
		}
		if got != total {
			t.Fatalf("Crowd(%d) placed %d figures", total, got)
		}
		if total > 0 && len(sections) != 8 {
			t.Fatalf("Crowd(%d) made %d sections, want 8", total, len(sections))
		}
	}
	a, b := Crowd(5000, 8, 50, 30), Crowd(5000, 8, 50, 30)
	for i := range a {
		for j := range a[i].Seats {
			if a[i].Seats[j] != b[i].Seats[j] {
				t.Fatal("the layout is not deterministic")
			}
		}
	}
}

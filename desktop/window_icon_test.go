package desktop

import "testing"

func TestDesktopWindowIconUsesFirstAvailableResourceForBothSizes(t *testing.T) {
	big, small, ok := windowIconHandles(1, 101, 202)
	if !ok || big != 101 || small != 202 {
		t.Fatalf("icon handles = %d, %d, %v; want 101, 202, true", big, small, ok)
	}
	big, small, ok = windowIconHandles(1, 101, 0)
	if !ok || big != 101 || small != 101 {
		t.Fatalf("single extracted icon handles = %d, %d, %v; want 101, 101, true", big, small, ok)
	}
	if _, _, ok := windowIconHandles(0, 101, 202); ok {
		t.Fatal("window icon must stay unset when the executable has no icon resource")
	}
}

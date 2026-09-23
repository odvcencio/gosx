package input

import "testing"

func TestBoundedCount(t *testing.T) {
	for _, tc := range []struct {
		length, limit, want int
	}{
		{-1, 8, 0},
		{0, 8, 0},
		{3, 8, 3},
		{8, 8, 8},
		{12, 8, 8},
	} {
		if got := boundedCount(tc.length, tc.limit); got != tc.want {
			t.Errorf("boundedCount(%d, %d) = %d, want %d", tc.length, tc.limit, got, tc.want)
		}
	}
}

func TestFirstStandard(t *testing.T) {
	games := Gamepads{Count: 3, Pads: [MaxGamepads]GamepadState{
		{Connected: true, Mapping: ""},
		{Connected: true, Mapping: "standard", ID: "pad-1"},
		{Connected: true, Mapping: "standard", ID: "pad-2"},
	}}
	got, ok := games.FirstStandard()
	if !ok || got.ID != "pad-1" {
		t.Fatalf("FirstStandard() = (%#v, %v), want first standard pad", got, ok)
	}
	games.Count = MaxGamepads + 1
	games.Pads[1].Connected = false
	got, ok = games.FirstStandard()
	if !ok || got.ID != "pad-2" {
		t.Fatalf("FirstStandard() after disconnect = (%#v, %v), want pad-2", got, ok)
	}
	games.Count = 1
	if _, ok := games.FirstStandard(); ok {
		t.Fatal("FirstStandard() found a pad outside Count")
	}
}

func TestPollGamepadsNativeReturnsEmpty(t *testing.T) {
	if got := PollGamepads(); got.Count != 0 {
		t.Fatalf("PollGamepads() = %#v, want empty snapshot", got)
	}
}

package host

import (
	"math"
	"testing"
	"time"
)

func TestRumbleClampsLevelsAndTimes(t *testing.T) {
	got := Rumble{Duration: time.Minute, Strong: 1.8, Weak: -0.2, Delay: -time.Second}.clamped()
	want := Rumble{Duration: MaxRumble, Strong: 1, Weak: 0}
	if got != want {
		t.Fatalf("clamped = %+v, want %+v", got, want)
	}
	if got := (Rumble{Strong: math.NaN(), Weak: 0.4, Duration: 80 * time.Millisecond}).clamped(); got.Strong != 0 || got.Weak != 0.4 || got.Duration != 80*time.Millisecond {
		t.Fatalf("a level that is not a number must become 0 and the rest stay: %+v", got)
	}
}

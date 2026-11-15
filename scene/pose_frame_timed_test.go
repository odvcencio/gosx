package scene

import (
	"testing"
	"time"
)

func TestTimedPoseMillis(t *testing.T) {
	at, step, delay, err := timedPoseMillis(1500*time.Millisecond, time.Second/30, 0)
	if err != nil || at != 1500 || step < 33.33 || step > 33.34 || delay != 0 {
		t.Fatalf("timedPoseMillis = %v, %v, %v, %v", at, step, delay, err)
	}
	if _, _, d, _ := timedPoseMillis(0, time.Millisecond, -time.Second); d != 0 {
		t.Fatalf("a negative delay should select the default (0), got %v", d)
	}
	if _, _, _, err := timedPoseMillis(0, 0, 0); err == nil {
		t.Fatal("a zero step was accepted")
	}
	if _, _, _, err := timedPoseMillis(-time.Millisecond, time.Millisecond, 0); err == nil {
		t.Fatal("a negative time was accepted")
	}
}

package clock

import (
	"testing"
	"time"
)

func TestRealClockAndTickerCoordinates(t *testing.T) {
	c := New()
	before := c.Now()
	ticker := c.NewTicker(time.Millisecond)
	defer ticker.Stop()
	select {
	case tick := <-ticker.C():
		stamp := ticker.Stamp(tick)
		after := c.Now()
		if stamp.Monotonic < before.Monotonic || stamp.Monotonic > after.Monotonic || stamp.Wall.Location() != time.UTC {
			t.Fatalf("coordinates: before=%v stamp=%v after=%v", before, stamp, after)
		}
		if stamp.Wall != tick.UTC() {
			t.Fatal("ticker timestamp was replaced with read time")
		}
	case <-time.After(time.Second):
		t.Fatal("ticker did not fire")
	}
}

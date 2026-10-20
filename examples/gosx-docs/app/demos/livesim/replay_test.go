package livesim

import (
	"testing"
	"time"
)

func TestLiveSimDoesNotRetainReplay(t *testing.T) {
	start := runner.Frame()
	deadline := time.Now().Add(time.Second)
	for runner.Frame() <= start+2 {
		if time.Now().After(deadline) {
			t.Fatal("live simulation did not advance")
		}
		time.Sleep(time.Millisecond)
	}
	if frames := len(runner.Replay().Frames); frames != 0 {
		t.Fatalf("process-lifetime simulation retained %d replay frames", frames)
	}
}

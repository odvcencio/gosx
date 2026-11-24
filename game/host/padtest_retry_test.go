package host

import (
	"testing"
	"time"
)

func TestRetryPolicyStopsAtReadyAndAtDeadline(t *testing.T) {
	t0 := time.Unix(1000, 0)
	p := newPadTestRetry(t0)
	d, ok := p.Failed(t0.Add(100*time.Millisecond), 0)
	if !ok || d != 250*time.Millisecond {
		t.Fatalf("first delay %v %v, want 250ms", d, ok)
	}
	d, ok = p.Failed(t0.Add(500*time.Millisecond), 0)
	if !ok || d != 500*time.Millisecond {
		t.Fatalf("second delay %v %v, want 500ms", d, ok)
	}
	p.Ready()
	if d, ok := p.Failed(t0.Add(time.Second), 0); ok {
		t.Fatalf("policy kept retrying after ready (%v)", d)
	}

	q := newPadTestRetry(t0)
	now := t0
	var last time.Duration
	for n := 1; ; n++ {
		now = now.Add(last + 10*time.Millisecond)
		d, ok := q.Failed(now, 0)
		if !ok {
			if now.Sub(t0) < padTestJoinWindow {
				t.Fatalf("gave up after %v, before the %v window", now.Sub(t0), padTestJoinWindow)
			}
			if q.Attempts() != n {
				t.Fatalf("attempts %d, want %d", q.Attempts(), n)
			}
			break
		}
		if d > 2*time.Second {
			t.Fatalf("delay %v exceeds 2s", d)
		}
		last = d
		if n > 1000 {
			t.Fatal("policy never gave up")
		}
	}
	if _, ok := newPadTestRetry(t0).Failed(t0.Add(padTestJoinWindow), 0); ok {
		t.Fatal("policy retried at exactly the deadline")
	}
}

func TestRetryPolicyNeverRetriesARefusal(t *testing.T) {
	t0 := time.Unix(1000, 0)
	p := newPadTestRetry(t0)
	if d, ok := p.Failed(t0.Add(time.Second), 1008); ok || !p.Refused() {
		t.Fatalf("a policy-violation close was retried (%v) or not marked refused", d)
	}
	if p.Attempts() != 1 {
		t.Fatalf("attempts %d after a refusal, want 1", p.Attempts())
	}
	// Any other close code, and no code at all, is retried.
	for _, code := range []int{0, 1000, 1001, 1006, 1011} {
		q := newPadTestRetry(t0)
		if _, ok := q.Failed(t0.Add(time.Second), code); !ok || q.Refused() {
			t.Fatalf("close code %d was not retried", code)
		}
	}
}

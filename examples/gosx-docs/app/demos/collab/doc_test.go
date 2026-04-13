package collab

import (
	"strings"
	"sync"
	"testing"
)

func TestDocInitialState(t *testing.T) {
	seed := "# Hello"
	d := NewDoc(seed)
	state := d.State()
	if state.Text != seed {
		t.Errorf("initial text = %q; want %q", state.Text, seed)
	}
	if state.Version != 0 {
		t.Errorf("initial version = %d; want 0", state.Version)
	}
}

func TestDocApplyAdvancesVersion(t *testing.T) {
	d := NewDoc("initial")
	state, accepted := d.Apply("updated", 0)
	if !accepted {
		t.Fatal("expected edit to be accepted")
	}
	if state.Version != 1 {
		t.Errorf("version after apply = %d; want 1", state.Version)
	}
	if state.Text != "updated" {
		t.Errorf("text after apply = %q; want %q", state.Text, "updated")
	}
}

func TestDocApplyStaleRejected(t *testing.T) {
	d := NewDoc("initial")
	// Advance to version 1.
	_, accepted := d.Apply("step1", 0)
	if !accepted {
		t.Fatal("first apply should be accepted")
	}
	// Now try to apply with stale version 0.
	state, accepted := d.Apply("stale", 0)
	if accepted {
		t.Fatal("stale edit should be rejected")
	}
	// Returned state must reflect current (version 1).
	if state.Version != 1 {
		t.Errorf("rejected state version = %d; want 1", state.Version)
	}
	if state.Text != "step1" {
		t.Errorf("rejected state text = %q; want %q", state.Text, "step1")
	}
}

func TestDocApplyOversizeRejected(t *testing.T) {
	d := NewDoc("initial")
	big := strings.Repeat("x", maxDocBytes+1)
	state, accepted := d.Apply(big, 0)
	if accepted {
		t.Fatal("oversize edit should be rejected")
	}
	// Doc must remain unchanged.
	if state.Text != "initial" {
		t.Errorf("after oversize reject, text = %q; want %q", state.Text, "initial")
	}
	if state.Version != 0 {
		t.Errorf("after oversize reject, version = %d; want 0", state.Version)
	}
}

func TestDocConcurrentSafe(t *testing.T) {
	d := NewDoc("start")
	var wg sync.WaitGroup
	const n = 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			cur := d.State()
			d.Apply("edit", cur.Version)
		}(i)
	}
	wg.Wait()
	final := d.State()
	if final.Version > n {
		t.Errorf("final version %d exceeds goroutine count %d", final.Version, n)
	}
	if final.Version == 0 {
		t.Error("no edits were accepted; expected at least one")
	}
}

package actions

import "testing"

// reads is a PressReader with sets of pressed and held actions.
type reads struct{ pressed, held []string }

func (r reads) Pressed(n string) bool { return contains(r.pressed, n) }
func (r reads) Down(n string) bool    { return contains(r.held, n) || contains(r.pressed, n) }

func contains(list []string, n string) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

func checkPrefix() *Prefix {
	return &Prefix{Keys: []string{"pad7", "shift"}, Ends: []string{"up", "right", "down", "left", "pad1", "backspace"}}
}

// "RT, Left" and "RT+Left" are the same input: held, or pressed and let go
// before the direction, the prefix applies to the direction and then lets go.
func TestPrefixHeldOrPressedBefore(t *testing.T) {
	held := checkPrefix()
	r := reads{pressed: []string{"left"}, held: []string{"pad7"}}
	if !held.On(r) {
		t.Fatal("a held prefix does not apply")
	}
	held.After(r)
	if held.Latched() {
		t.Fatal("a held prefix used by its direction still waits")
	}

	tapped := checkPrefix()
	tapped.After(reads{pressed: []string{"pad7"}}) // pressed and let go
	if !tapped.Latched() || !tapped.On(reads{}) {
		t.Fatal("a pressed prefix does not wait")
	}
	// Reads with nothing, or another press, keep it waiting: no time window.
	for range 100 {
		tapped.After(reads{pressed: []string{"pad2"}})
	}
	r = reads{pressed: []string{"left"}}
	if !tapped.On(r) {
		t.Fatal("the waiting prefix does not apply to the direction")
	}
	tapped.After(r)
	if tapped.Latched() || tapped.On(reads{pressed: []string{"left"}}) {
		t.Fatal("the prefix still applies after its direction")
	}
}

func TestPrefixCancelAndClear(t *testing.T) {
	p := checkPrefix()
	p.After(reads{pressed: []string{"shift"}})
	p.After(reads{pressed: []string{"backspace"}})
	if p.Latched() {
		t.Fatal("the cancel did not drop the prefix")
	}
	p.After(reads{pressed: []string{"pad7"}})
	p.Clear()
	if p.Latched() || p.On(reads{}) {
		t.Fatal("Clear left the prefix waiting")
	}
	// The prefix and its direction in one read use it at once.
	p.After(reads{pressed: []string{"pad7", "up"}})
	if p.Latched() {
		t.Fatal("a prefix pressed with its direction still waits")
	}
}

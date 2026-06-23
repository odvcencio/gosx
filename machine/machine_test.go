package machine

import "testing"

const (
	off = "off"
	on  = "on"
)

func TestToggleTransitions(t *testing.T) {
	m := New[string, string](off)
	m.On(off, "flip", on).On(on, "flip", off)
	if !m.Send("flip") || m.State() != on {
		t.Fatalf("expected on, got %s", m.State())
	}
	if !m.Send("flip") || m.State() != off {
		t.Fatalf("expected off, got %s", m.State())
	}
}

func TestUndefinedEventIsNoOp(t *testing.T) {
	m := New[string, string](off)
	m.On(off, "flip", on)
	if m.Send("explode") {
		t.Fatal("undefined event should not transition")
	}
	if m.State() != off {
		t.Fatalf("state should be unchanged, got %s", m.State())
	}
}

func TestGuardBlocksTransition(t *testing.T) {
	allow := false
	m := New[string, string](off)
	m.On(off, "flip", on, Guard[string](func() bool { return allow }))
	if m.Send("flip") || m.State() != off {
		t.Fatal("guard=false should block the transition")
	}
	if m.Can("flip") {
		t.Fatal("Can should be false while the guard fails")
	}
	allow = true
	if !m.Can("flip") {
		t.Fatal("Can should be true once the guard passes")
	}
	if !m.Send("flip") || m.State() != on {
		t.Fatal("guard=true should allow the transition")
	}
}

func TestActionAndEntryExitRunInOrder(t *testing.T) {
	var log []string
	m := New[string, string](off)
	m.OnExit(off, func(s string) { log = append(log, "exit:"+s) })
	m.OnEnter(on, func(s string) { log = append(log, "enter:"+s) })
	m.On(off, "flip", on, Do[string](func() { log = append(log, "action") }))
	m.Send("flip")
	want := []string{"exit:off", "action", "enter:on"}
	if len(log) != len(want) {
		t.Fatalf("hooks ran out of order: %v", log)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("hook order = %v, want %v", log, want)
		}
	}
}

func TestStateSignalNotifiesSubscribers(t *testing.T) {
	m := New[string, string](off)
	m.On(off, "flip", on)
	var got string
	unsub := m.Subscribe(func(s string) { got = s })
	defer unsub()
	m.Send("flip")
	if got != on {
		t.Fatalf("subscriber should see new state, got %q", got)
	}
	if m.Signal().Get() != on {
		t.Fatal("Signal() should expose the live state for islands/computed")
	}
}

func TestIsReflectsCurrentState(t *testing.T) {
	m := New[string, string](off)
	if !m.Is(off) || m.Is(on) {
		t.Fatal("Is() does not reflect the current state")
	}
}

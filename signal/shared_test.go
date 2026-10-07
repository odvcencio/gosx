package signal

import "testing"

func TestSharedSignalsAreNativeRequestLocal(t *testing.T) {
	for _, constructor := range []struct {
		name string
		new  func(string, int) *Signal[int]
	}{
		{"NewShared", NewShared[int]},
		{"Shared", Shared[int]},
	} {
		t.Run(constructor.name, func(t *testing.T) {
			first := constructor.new("selection", 7)
			second := constructor.new("selection", 11)
			if first == second || first.Get() != 7 || second.Get() != 11 {
				t.Fatal("native calls must keep independent initial values")
			}
			calls := 0
			dispose := first.Subscribe(func() { calls++ })
			first.Set(7)
			first.Update(func(value int) int { return value + 1 })
			if first.Get() != 8 || second.Get() != 11 || calls != 1 || first.Revision() != 1 {
				t.Fatal("shared constructors must retain ordinary signal semantics")
			}
			dispose()
			first.Set(9)
			if calls != 1 {
				t.Fatal("subscription was not disposed")
			}
		})
	}
}

func TestSharedSignalTypedValues(t *testing.T) {
	type view struct{ Revision int }
	s := NewShared[view]("$view", view{Revision: 3})
	s.Set(view{Revision: 4})
	if s.Get().Revision != 4 {
		t.Fatal("typed value was not retained")
	}
	items := Shared("items", []string{"a"})
	items.Set([]string{"b"})
	if items.Get()[0] != "b" || items.Revision() != 1 {
		t.Fatal("non-comparable signal values must remain supported")
	}
}

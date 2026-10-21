package bridge

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

func TestChangedSignalBatchReadsLiveStateAndPreservesPulses(t *testing.T) {
	b := New()
	counts := map[string]int{}
	b.SetSharedSignalCallback(func(name, _ string) { counts[name]++ })
	initial := `{"$view":{"turn":"east","hand":[1,2]},"$meta":{"title":"Table"},"$selected":""}`
	if err := b.SetChangedSharedSignalBatchJSON(initial); err != nil {
		t.Fatal(err)
	}
	// JSON object order is irrelevant, including inside the view.
	if err := b.SetChangedSharedSignalBatchJSON(`{"$selected":"","$meta":{"title":"Table"},"$view":{"hand":[1,2],"turn":"east"}}`); err != nil {
		t.Fatal(err)
	}
	for name, count := range counts {
		if count != 1 {
			t.Fatalf("unchanged %s notified %d times", name, count)
		}
	}
	if err := b.SetChangedSharedSignalBatchJSON(`{"$view":{"turn":"west","hand":[1,2]},"$meta":{"title":"Table"},"$selected":"tile-2"}`); err != nil {
		t.Fatal(err)
	}
	if counts["$view"] != 2 || counts["$meta"] != 1 || counts["$selected"] != 2 {
		t.Fatalf("dirty counts=%v", counts)
	}
	// An island can mutate a map/array in place. Sender identity or raw JSON
	// caching must not conceal that edit when a later snapshot restores it.
	current, _ := b.store.Get("$view")
	current.Map()["turn"] = vm.StringVal("north")
	current.Map()["hand"].List()[0] = vm.IntVal(9)
	if err := b.SetChangedSharedSignalBatchJSON(`{"$view":{"turn":"west","hand":[1,2]}}`); err != nil {
		t.Fatal(err)
	}
	if counts["$view"] != 3 {
		t.Fatal("in-place edit was hidden")
	}
	if err := b.SetSharedSignalBatchJSON(`{"$selected":"tile-2"}`); err != nil {
		t.Fatal(err)
	}
	if counts["$selected"] != 3 {
		t.Fatal("explicit pulse delivery was suppressed")
	}
}

func TestChangedSignalBatchIsAtomicAndDoesNotCoerce(t *testing.T) {
	b := New()
	if err := b.SetSharedSignalBatchJSON(`{"$left":1,"$right":2}`); err != nil {
		t.Fatal(err)
	}
	observed := 0
	b.store.Signal("$left", vm.IntVal(0)).Subscribe(func() {
		right, _ := b.store.Get("$right")
		if right.Number() != 4 {
			t.Error("subscriber saw partial batch")
		}
		observed++
	})
	if err := b.SetChangedSharedSignalBatchJSON(`{"$left":"1","$right":4}`); err != nil {
		t.Fatal(err)
	}
	if observed != 1 {
		t.Fatal("number/string edit was coerced away")
	}
	for _, invalid := range []string{`{"$left":9,"":true}`, `{"$left":9,"$right":`, `[]`, `{"$left":1e400}`} {
		if err := b.SetChangedSharedSignalBatchJSON(invalid); err == nil {
			t.Fatalf("invalid batch accepted: %s", invalid)
		}
	}
	left, _ := b.store.Get("$left")
	if observed != 1 || left.Text() != "1" {
		t.Fatal("invalid batch partially committed")
	}
}

func TestSignalBatchCreationObserversSeeAllValues(t *testing.T) {
	for _, changedOnly := range []bool{false, true} {
		b := New()
		calls := 0
		b.SetSharedSignalCallback(func(string, string) {
			calls++
			for name, want := range map[string]float64{"$left": 1, "$right": 2} {
				value, exists := b.store.Get(name)
				if !exists || value.Number() != want {
					t.Errorf("changedOnly=%v: partial creation batch: %s", changedOnly, name)
				}
			}
		})
		if err := b.setSharedSignalBatchJSON(`{"$left":1,"$right":2}`, changedOnly); err != nil {
			t.Fatal(err)
		}
		if calls != 2 {
			t.Fatalf("creation notifications=%d", calls)
		}
	}
}

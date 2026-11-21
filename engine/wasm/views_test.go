package wasm

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

type scoreView struct{ Home, Away int }

func TestViewsSendOnlyWhatChanged(t *testing.T) {
	var batches []map[string]any
	vs := NewViewsTo(func(m map[string]any) error { batches = append(batches, m); return nil })
	score := scoreView{}
	hand := "keys"
	hint := "{act} snap"
	list := []string{"a"}
	Value(vs, "score", func() scoreView { return score })
	Keyed(vs, "hint", func() [2]string { return [2]string{hint, hand} }, func(k [2]string) string { return k[1] + ":" + k[0] })
	Equal(vs, "list", func() []string { return list }, slices.Equal[[]string])
	final := Stale(vs, "final", func() int { return len(list) })

	if n, err := vs.Flush(); n != 3 || err != nil {
		t.Fatalf("first Flush sent %d, %v; want the three views that are not stale-only", n, err)
	}
	if n, _ := vs.Flush(); n != 0 || len(batches) != 1 {
		t.Fatalf("a Flush with nothing changed sent %d views in %d batches", n, len(batches))
	}
	score.Home, hand = 7, "pad"
	list = []string{"a"} // a new slice, same contents
	final.MarkStale()
	vs.Flush()
	want := map[string]any{"$score": scoreView{7, 0}, "$hint": "pad:{act} snap", "$final": 1}
	if got := batches[len(batches)-1]; !reflect.DeepEqual(got, want) {
		t.Fatalf("second batch = %v, want %v", got, want)
	}
	if n, _ := vs.Flush(); n != 0 {
		t.Fatalf("a stale view was sent again (%d views) after it went out", n)
	}
}

func TestViewsRetryAFailedSend(t *testing.T) {
	fail := true
	var got map[string]any
	vs := NewViewsTo(func(m map[string]any) error {
		if fail {
			return errors.New("page not ready")
		}
		got = m
		return nil
	})
	n := 1
	Value(vs, "n", func() int { return n })
	if _, err := vs.Flush(); err == nil {
		t.Fatal("Flush hid the send error")
	}
	fail = false
	if c, _ := vs.Flush(); c != 1 || got["$n"] != 1 {
		t.Fatalf("after a failed send Flush sent %d (%v); want the view again", c, got)
	}
}

func TestQuietViewWaitsUntilFirstOn(t *testing.T) {
	var last map[string]any
	vs := NewViewsTo(func(m map[string]any) error { last = m; return nil })
	mode := "off"
	Value(vs, "seats", func() string { return mode }).Quiet(func(m string) bool { return m == "off" })
	if c, _ := vs.Flush(); c != 0 {
		t.Fatal("a quiet view went out before it was ever on")
	}
	mode = "on"
	vs.Flush()
	mode = "off"
	if c, _ := vs.Flush(); c != 1 || last["$seats"] != "off" {
		t.Fatalf("turning the layer off sent %d (%v); once sent, it must follow every change", c, last)
	}
}

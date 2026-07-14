package collab

import (
	"sync"
	"testing"

	"m31labs.dev/gosx/examples/gosx-docs/app/demos/democtl"
)

func TestIdentityForClientIDIsDeterministic(t *testing.T) {
	id := "collab-aabbccdd"
	first := identityForClientID(id)
	second := identityForClientID(id)
	if first != second {
		t.Fatalf("identityForClientID(%q) not stable: %+v vs %+v", id, first, second)
	}
}

func TestIdentityForClientIDFromPools(t *testing.T) {
	names := make(map[string]bool)
	for _, n := range democtl.NamePool() {
		names[n] = true
	}
	colors := make(map[string]bool)
	for _, c := range democtl.ColorPool() {
		colors[c] = true
	}

	for _, id := range []string{"collab-1", "collab-2", "collab-deadbeef", "collab-0000000000000000"} {
		got := identityForClientID(id)
		if !names[got.Name] {
			t.Errorf("identityForClientID(%q).Name = %q; not in democtl.NamePool()", id, got.Name)
		}
		if !colors[got.Color] {
			t.Errorf("identityForClientID(%q).Color = %q; not in democtl.ColorPool()", id, got.Color)
		}
	}
}

func TestIdentityForClientIDVariesWithID(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 50; i++ {
		id := identityForClientID("collab-" + string(rune('a'+i%26)) + string(rune('0'+i%10)))
		seen[id.Name+"|"+id.Color] = struct{}{}
	}
	if len(seen) < 5 {
		t.Errorf("expected varied identities across distinct client IDs, got %d unique combos", len(seen))
	}
}

func TestRosterJoinAssignsAndCounts(t *testing.T) {
	r := newRoster()
	if r.count() != 0 {
		t.Fatalf("new roster count = %d; want 0", r.count())
	}

	idA := r.join("client-a")
	if r.count() != 1 {
		t.Fatalf("after one join, count = %d; want 1", r.count())
	}
	idA2 := r.join("client-a")
	if idA != idA2 {
		t.Errorf("re-joining same client ID changed identity: %+v vs %+v", idA, idA2)
	}
	if r.count() != 1 {
		t.Fatalf("re-joining an existing client ID should not increase count; got %d", r.count())
	}

	r.join("client-b")
	if r.count() != 2 {
		t.Fatalf("after two distinct joins, count = %d; want 2", r.count())
	}
}

func TestRosterLeaveRemovesMemberAndCursor(t *testing.T) {
	r := newRoster()
	r.join("client-a")
	if _, ok := r.updateCursor("client-a", 3, 5); !ok {
		t.Fatal("updateCursor for a joined client should succeed")
	}
	if got := len(r.snapshot()); got != 1 {
		t.Fatalf("snapshot before leave = %d entries; want 1", got)
	}

	r.leave("client-a")
	if r.count() != 0 {
		t.Fatalf("after leave, count = %d; want 0", r.count())
	}
	if got := len(r.snapshot()); got != 0 {
		t.Fatalf("snapshot after leave = %d entries; want 0", got)
	}
}

func TestRosterUpdateCursorRejectsUnknownClient(t *testing.T) {
	r := newRoster()
	if _, ok := r.updateCursor("ghost", 0, 0); ok {
		t.Fatal("updateCursor for a client that never joined should return ok=false")
	}
}

func TestRosterUpdateCursorReflectsIdentity(t *testing.T) {
	r := newRoster()
	id := r.join("client-a")
	evt, ok := r.updateCursor("client-a", 7, 9)
	if !ok {
		t.Fatal("expected updateCursor to succeed")
	}
	if evt.ID != "client-a" || evt.Name != id.Name || evt.Color != id.Color {
		t.Errorf("cursor event identity mismatch: got %+v, want name=%q color=%q", evt, id.Name, id.Color)
	}
	if evt.Offset != 7 || evt.SelEnd != 9 {
		t.Errorf("cursor event position = (%d,%d); want (7,9)", evt.Offset, evt.SelEnd)
	}
}

func TestRosterSnapshotOnlyIncludesReportedCursors(t *testing.T) {
	r := newRoster()
	r.join("client-a") // never reports a cursor
	r.join("client-b")
	r.updateCursor("client-b", 1, 1)

	snap := r.snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot length = %d; want 1 (only client-b reported a cursor)", len(snap))
	}
	if snap[0].ID != "client-b" {
		t.Errorf("snapshot entry ID = %q; want %q", snap[0].ID, "client-b")
	}
}

func TestRosterConcurrentSafe(t *testing.T) {
	r := newRoster()
	var wg sync.WaitGroup
	const n = 100
	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			id := "client-" + string(rune('a'+i%26))
			r.join(id)
			r.updateCursor(id, i, i)
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = r.count()
			_ = r.snapshot()
		}(i)
	}
	wg.Wait()
}

func TestClampInt(t *testing.T) {
	cases := []struct{ v, lo, hi, want int }{
		{-5, 0, 10, 0},
		{15, 0, 10, 10},
		{5, 0, 10, 5},
		{0, 0, 0, 0},
	}
	for _, c := range cases {
		if got := clampInt(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("clampInt(%d, %d, %d) = %d; want %d", c.v, c.lo, c.hi, got, c.want)
		}
	}
}

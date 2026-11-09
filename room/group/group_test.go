package group

import (
	"errors"
	"testing"
	"time"

	"m31labs.dev/gosx/room/store"
)

func newReg(t *testing.T, dir string) (*Registry, *store.Store) {
	t.Helper()
	st, err := store.Open(dir, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return New(st, Options{MaxMembers: 3, Now: func() time.Time { return time.Unix(1000, 0) }}), st
}

func TestGroupLifecycleSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	r, st := newReg(t, dir)
	if _, err := r.Create("Bad ID", "x", nil); !errors.Is(err, ErrBadID) {
		t.Fatalf("err = %v", err)
	}
	g, err := r.Create("spring-league", "Spring League", map[string]string{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create("spring-league", "dup", nil); !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v", err)
	}
	for _, id := range []string{"t1", "t2", "t3"} {
		if _, err := r.AddMember(g.ID, Member{ID: id, Name: "Team " + id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.AddMember(g.ID, Member{ID: "t4"}); !errors.Is(err, ErrFull) {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.AddMember(g.ID, Member{ID: "t1"}); !errors.Is(err, ErrMemberDup) {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.AddMember("nope", Member{ID: "t1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	must(t, r.RemoveMember(g.ID, "t3"))
	must(t, r.SetState(g.ID, []byte(`{"round":1}`)))
	if _, err := r.AppendEvent(g.ID, []byte("started")); err != nil {
		t.Fatal(err)
	}
	st.Close()

	r, st = newReg(t, dir)
	defer st.Close()
	got, err := r.Get("spring-league")
	if err != nil || got.Name != "Spring League" || got.Meta["k"] != "v" || !got.Created.Equal(time.Unix(1000, 0)) {
		t.Fatalf("group = %+v %v", got, err)
	}
	ms := r.Members(g.ID)
	if len(ms) != 2 || ms[0].ID != "t1" || ms[1].Name != "Team t2" {
		t.Fatalf("members = %+v", ms)
	}
	if !r.HasMember(g.ID, "t2") || r.HasMember(g.ID, "t3") {
		t.Fatal("HasMember wrong")
	}
	if string(r.State(g.ID)) != `{"round":1}` {
		t.Fatalf("state = %s", r.State(g.ID))
	}
	if ev := r.Events(g.ID, 0, 0); len(ev) != 1 || string(ev[0].Data) != "started" {
		t.Fatalf("events = %+v", ev)
	}
	if n := len(r.List()); n != 1 {
		t.Fatalf("list = %d", n)
	}
}

func TestDeleteRemovesMembersAndState(t *testing.T) {
	r, st := newReg(t, t.TempDir())
	defer st.Close()
	r.Create("g", "G", nil)
	r.AddMember("g", Member{ID: "a"})
	r.SetState("g", []byte("s"))
	r.AppendEvent("g", []byte("e"))
	must(t, r.Delete("g"))
	if _, err := r.Get("g"); !errors.Is(err, ErrNotFound) {
		t.Fatal("group still there")
	}
	if len(st.Keys("group/")) != 0 {
		t.Fatalf("leftover keys %v", st.Keys("group/"))
	}
	if err := r.Delete("g"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, next := st.LogRange(EventsKey("g")); next != 1 {
		t.Fatal("event log not dropped")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

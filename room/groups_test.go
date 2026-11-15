package room

import (
	"testing"

	"m31labs.dev/gosx/room/mode"
	"m31labs.dev/gosx/room/store"
)

func TestServerGroupsNeedStore(t *testing.T) {
	cfg := Config{Modes: []mode.Mode{sumMode("duel", 2)}, Manual: true, Logf: func(string, ...any) {}}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if srv.Groups() != nil {
		t.Fatal("Groups should be nil without a store")
	}
	dir := t.TempDir()
	st, err := store.Open(dir, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Store = st
	srv, err = NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Groups().Create("league", "League", nil); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, _ = store.Open(dir, store.Options{})
	defer st.Close()
	cfg.Store = st
	srv, _ = NewServer(cfg)
	if _, err := srv.Groups().Get("league"); err != nil {
		t.Fatalf("group lost over restart: %v", err)
	}
}

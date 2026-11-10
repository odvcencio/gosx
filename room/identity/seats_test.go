package identity

import (
	"errors"
	"sync"
	"testing"

	"m31labs.dev/gosx/room/group"
	"m31labs.dev/gosx/room/store"
)

// wired opens a store with a group "g" (members red and blue) and an
// identity service tied to it through Config.Groups.
func wired(t *testing.T, dir string) (*Service, *group.Registry, *store.Store) {
	t.Helper()
	st, err := store.Open(dir, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	reg := group.New(st, group.Options{})
	if _, err := reg.Get("g"); err != nil {
		must(t, errOnly(reg.Create("g", "G", nil)))
		must(t, errOnly(reg.AddMember("g", group.Member{ID: "red"})))
		must(t, errOnly(reg.AddMember("g", group.Member{ID: "blue"})))
	}
	svc, err := New(st, Config{Keys: testKeys, Groups: reg})
	if err != nil {
		t.Fatal(err)
	}
	return svc, reg, st
}

func errOnly[T any](_ T, err error) error { return err }

func join(t *testing.T, svc *Service, seat string) Session {
	t.Helper()
	inv, err := svc.CreateInvite(InviteSpec{Group: "g", Seat: seat})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.Redeem(inv.Code, RedeemOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

// Removing a member frees its seat in the same commit: the old device no
// longer authenticates, also after a restart and after the member is added
// back, and a new invite for the seat redeems.
func TestRemovedMemberLosesSeat(t *testing.T) {
	dir := t.TempDir()
	svc, reg, st := wired(t, dir)
	red := join(t, svc, "red")
	if _, err := svc.Authenticate(red.Token, "g"); err != nil {
		t.Fatal(err)
	}
	must(t, reg.RemoveMember("g", "red"))
	if _, err := svc.Authenticate(red.Token, "g"); !errors.Is(err, ErrNotBound) {
		t.Fatalf("removed member authenticated: %v", err)
	}
	if _, ok := svc.DeviceAt("g", "red"); ok {
		t.Fatal("seat still bound after RemoveMember")
	}
	if _, ok := svc.SeatOf(red.Device.ID, "g"); ok {
		t.Fatal("device still holds the seat after RemoveMember")
	}

	st.Close()
	svc, reg, st = wired(t, dir)
	defer st.Close()
	if _, err := svc.Authenticate(red.Token, "g"); !errors.Is(err, ErrNotBound) {
		t.Fatalf("removed member authenticated after restart: %v", err)
	}
	// Added back, the seat needs a new invite: the old binding does not revive.
	must(t, errOnly(reg.AddMember("g", group.Member{ID: "red"})))
	if _, err := svc.Authenticate(red.Token, "g"); !errors.Is(err, ErrNotBound) {
		t.Fatalf("old device regained a re-added seat: %v", err)
	}
	again := join(t, svc, "red")
	if _, err := svc.Authenticate(again.Token, "g"); err != nil {
		t.Fatalf("new invite for the re-added seat: %v", err)
	}
}

// Deleting a group frees every seat in it, so no old device authenticates
// against a group created again under the same ID.
func TestDeletedGroupFreesSeats(t *testing.T) {
	svc, reg, st := wired(t, t.TempDir())
	defer st.Close()
	red, blue := join(t, svc, "red"), join(t, svc, "blue")
	must(t, reg.Delete("g"))
	for _, s := range []Session{red, blue} {
		if _, err := svc.Authenticate(s.Token, "g"); !errors.Is(err, ErrNotBound) {
			t.Fatalf("device of deleted group authenticated: %v", err)
		}
		if _, ok := svc.SeatOf(s.Device.ID, "g"); ok {
			t.Fatal("binding survived group deletion")
		}
	}
	if keys := st.Keys(keySeat + "g/"); len(keys) != 0 {
		t.Fatalf("seat keys left: %v", keys)
	}
	must(t, errOnly(reg.Create("g", "G again", nil)))
	must(t, errOnly(reg.AddMember("g", group.Member{ID: "red"})))
	if _, err := svc.Authenticate(red.Token, "g"); !errors.Is(err, ErrNotBound) {
		t.Fatalf("old device authenticated in the recreated group: %v", err)
	}
	join(t, svc, "red")
}

// Without Config.Groups, Authenticate still re-checks ValidateSeat, so a
// binding left behind by a removal cannot authenticate.
func TestAuthenticateRechecksSeat(t *testing.T) {
	st, err := store.Open(t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var mu sync.Mutex
	seats := map[string]bool{"g/red": true}
	svc, err := New(st, Config{Keys: testKeys, ValidateSeat: func(g, s string) bool {
		mu.Lock()
		defer mu.Unlock()
		return seats[g+"/"+s]
	}})
	if err != nil {
		t.Fatal(err)
	}
	red := join(t, svc, "red")
	mu.Lock()
	delete(seats, "g/red")
	mu.Unlock()
	if _, err := svc.Authenticate(red.Token, "g"); !errors.Is(err, ErrNotBound) {
		t.Fatalf("binding to a seat ValidateSeat rejects authenticated: %v", err)
	}
}

// A redemption racing a removal of the same seat never leaves a binding to
// a seat that is not a member.
func TestRemoveMemberRacesRedeem(t *testing.T) {
	svc, reg, st := wired(t, t.TempDir())
	defer st.Close()
	for i := range 50 {
		if !reg.HasMember("g", "red") {
			must(t, errOnly(reg.AddMember("g", group.Member{ID: "red"})))
		}
		inv, err := svc.CreateInvite(InviteSpec{Group: "g", Seat: "red"})
		must(t, err)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = svc.Redeem(inv.Code, RedeemOptions{}) }()
		go func() { defer wg.Done(); _ = reg.RemoveMember("g", "red") }()
		wg.Wait()
		if _, bound := svc.DeviceAt("g", "red"); bound && !reg.HasMember("g", "red") {
			t.Fatalf("round %d: seat bound to a removed member", i)
		}
		_ = svc.Unbind("g", "red")
	}
}

func TestGroupsMustShareStore(t *testing.T) {
	a, err := store.Open(t.TempDir(), store.Options{})
	must(t, err)
	defer a.Close()
	b, err := store.Open(t.TempDir(), store.Options{})
	must(t, err)
	defer b.Close()
	if _, err := New(a, Config{Keys: testKeys, Groups: group.New(b, group.Options{})}); err == nil {
		t.Fatal("a registry on another store was accepted")
	}
}

package identity

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/room/store"
)

type clock struct{ t atomic.Int64 }

func (c *clock) Now() time.Time          { return time.Unix(c.t.Load(), 0) }
func (c *clock) Advance(d time.Duration) { c.t.Add(int64(d / time.Second)) }

var testKeys = []Key{{ID: "k2", Secret: []byte("0123456789abcdef-new")}, {ID: "k1", Secret: []byte("0123456789abcdef-old")}}

func newSvc(t *testing.T, dir string, keys []Key, clk *clock) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(dir, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	seats := map[string]bool{"g/t1": true, "g/t2": true, "g/t3": true}
	svc, err := New(st, Config{Keys: keys, Now: clk.Now, ValidateSeat: func(g, s string) bool { return seats[g+"/"+s] }})
	if err != nil {
		t.Fatal(err)
	}
	return svc, st
}

func TestInviteCodeShapeAndNormalize(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		c := NewInviteCode()
		if len(c) != 9 || c[4] != '-' {
			t.Fatalf("code %q", c)
		}
		if strings.ContainsAny(c, "01ILOU") {
			t.Fatalf("ambiguous character in %q", c)
		}
		if NormalizeCode(strings.ToLower(c)) != strings.ReplaceAll(c, "-", "") {
			t.Fatalf("normalize %q", c)
		}
		seen[c] = true
	}
	if len(seen) < 495 {
		t.Fatalf("only %d distinct codes of 500", len(seen))
	}
	for _, bad := range []string{"", "ABC", "ABCD-EFG0", "ABCD-EFGHJ", "ABCD EFG!"} {
		if NormalizeCode(bad) != "" {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestRedeemBindsDeviceAndSurvivesRestart(t *testing.T) {
	dir, clk := t.TempDir(), &clock{}
	clk.t.Store(1_000_000)
	svc, st := newSvc(t, dir, testKeys, clk)
	inv, err := svc.CreateInvite(InviteSpec{Group: "g", Seat: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.Redeem(strings.ToLower(inv.Code), RedeemOptions{Name: "  Ada\x00 "})
	if err != nil {
		t.Fatal(err)
	}
	if sess.Seat != "t1" || sess.Device.Name != "Ada" || sess.Token == "" {
		t.Fatalf("session %+v", sess)
	}
	if _, err := svc.Redeem(inv.Code, RedeemOptions{}); !errors.Is(err, ErrBadInvite) {
		t.Fatalf("second redeem err = %v, want invite gone", err)
	}
	st.Close()

	svc, st = newSvc(t, dir, testKeys, clk)
	defer st.Close()
	got, err := svc.Authenticate(sess.Token, "g")
	if err != nil || got.Seat != "t1" || got.Device.ID != sess.Device.ID || got.Device.Name != "Ada" {
		t.Fatalf("authenticate after restart: %+v %v", got, err)
	}
	if _, err := svc.Authenticate(sess.Token, "other"); !errors.Is(err, ErrNotBound) {
		t.Fatalf("err = %v", err)
	}
	if d, ok := svc.DeviceAt("g", "t1"); !ok || d != sess.Device.ID {
		t.Fatal("DeviceAt")
	}
}

func TestInviteExpiryAndUses(t *testing.T) {
	clk := &clock{}
	clk.t.Store(5000)
	svc, st := newSvc(t, t.TempDir(), testKeys, clk)
	defer st.Close()
	inv, _ := svc.CreateInvite(InviteSpec{Group: "g", Uses: 2, TTL: time.Hour})
	if _, err := svc.Redeem(inv.Code, RedeemOptions{}); !errors.Is(err, ErrSeatRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.Redeem(inv.Code, RedeemOptions{Seat: "nope"}); !errors.Is(err, ErrNoSuchSeat) {
		t.Fatalf("err = %v", err)
	}
	a, err := svc.Redeem(inv.Code, RedeemOptions{Seat: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Redeem(inv.Code, RedeemOptions{Seat: "t1"}); !errors.Is(err, ErrSeatTaken) {
		t.Fatalf("err = %v", err)
	}
	if left, _ := svc.Invite(inv.Code); left.Uses != 1 {
		t.Fatalf("uses left = %d (a failed redeem spent a use)", left.Uses)
	}
	if _, err := svc.Redeem(inv.Code, RedeemOptions{Seat: "t2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Redeem(inv.Code, RedeemOptions{Seat: "t3"}); !errors.Is(err, ErrBadInvite) {
		t.Fatalf("used-up invite err = %v", err)
	}
	_ = a

	exp, _ := svc.CreateInvite(InviteSpec{Group: "g", TTL: time.Minute})
	clk.Advance(2 * time.Minute)
	if _, err := svc.Redeem(exp.Code, RedeemOptions{Seat: "t3"}); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("err = %v", err)
	}
	if n, _ := svc.PurgeExpired(); n != 1 {
		t.Fatalf("purged %d", n)
	}
	if len(svc.Invites("g")) != 0 {
		t.Fatal("live invites remain")
	}
}

func TestInviteSpecValidation(t *testing.T) {
	svc, st := newSvc(t, t.TempDir(), testKeys, &clock{})
	defer st.Close()
	for _, spec := range []InviteSpec{
		{Group: ""}, {Group: "a/b"}, {Group: "g", Uses: -1}, {Group: "g", Uses: 1000},
		{Group: "g", Seat: "t1", Uses: 2}, {Group: "g", TTL: 365 * 24 * time.Hour},
	} {
		if _, err := svc.CreateInvite(spec); !errors.Is(err, ErrBadSpec) {
			t.Fatalf("%+v: err = %v", spec, err)
		}
	}
	if _, err := svc.CreateInvite(InviteSpec{Group: "g", Seat: "ghost"}); !errors.Is(err, ErrNoSuchSeat) {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenTamperAndKeyRotation(t *testing.T) {
	dir, clk := t.TempDir(), &clock{}
	oldRing := testKeys[1:] // only k1
	svc, st := newSvc(t, dir, oldRing, clk)
	inv, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "t1"})
	sess, _ := svc.Redeem(inv.Code, RedeemOptions{})
	st.Close()

	// Tamper: flip a payload character.
	parts := strings.Split(sess.Token, ".")
	bad := strings.Join([]string{parts[0], parts[1], parts[2][:len(parts[2])-1] + "A", parts[3]}, ".")
	svc, st = newSvc(t, dir, testKeys, clk) // k2 now signs, k1 still verifies
	defer st.Close()
	if bad != sess.Token {
		if _, err := svc.Verify(bad); !errors.Is(err, ErrBadToken) {
			t.Fatalf("tampered err = %v", err)
		}
	}
	if _, err := svc.Verify("garbage"); !errors.Is(err, ErrBadToken) {
		t.Fatal("garbage accepted")
	}
	if _, err := svc.Verify(sess.Token); err != nil {
		t.Fatalf("old-key token rejected while key in ring: %v", err)
	}
	fresh, err := svc.Refresh(sess.Token)
	if err != nil || !strings.HasPrefix(fresh, "v1.k2.") {
		t.Fatalf("refresh = %q %v", fresh, err)
	}
	// Retire k1: the refreshed token still works, the old one does not.
	st.Close()
	svc, st = newSvc(t, dir, testKeys[:1], clk)
	defer st.Close()
	if _, err := svc.Verify(fresh); err != nil {
		t.Fatalf("refreshed token: %v", err)
	}
	if _, err := svc.Verify(sess.Token); !errors.Is(err, ErrBadToken) {
		t.Fatalf("retired-key token err = %v", err)
	}
	// A token signed with a wrong secret under a known key ID fails.
	other, _ := New(st, Config{Keys: []Key{{ID: "k2", Secret: []byte("different-secret-16+")}}, Now: clk.Now})
	forged, _ := other.Refresh(fresh)
	_ = forged // other cannot verify fresh, so Refresh errors; sign directly:
	forged = other.sign(sess.Device, clk.Now())
	if _, err := svc.Verify(forged); !errors.Is(err, ErrBadToken) {
		t.Fatalf("forged err = %v", err)
	}
}

func TestRevokeRotateAndFreeSeat(t *testing.T) {
	clk := &clock{}
	svc, st := newSvc(t, t.TempDir(), testKeys, clk)
	defer st.Close()
	inv, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "t1"})
	sess, _ := svc.Redeem(inv.Code, RedeemOptions{})

	rotated, err := svc.Rotate(sess.Device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(sess.Token); !errors.Is(err, ErrBadToken) {
		t.Fatalf("old token after rotate: %v", err)
	}
	if _, err := svc.Verify(rotated); err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(sess.Device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(rotated); !errors.Is(err, ErrRevoked) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := svc.DeviceAt("g", "t1"); ok {
		t.Fatal("revoke left the seat bound")
	}
	// The seat can be claimed again.
	inv2, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "t1"})
	if _, err := svc.Redeem(inv2.Code, RedeemOptions{Name: "Bo"}); err != nil {
		t.Fatalf("re-invite: %v", err)
	}
}

func TestSecondInviteWithExistingToken(t *testing.T) {
	svc, st := newSvc(t, t.TempDir(), testKeys, &clock{})
	defer st.Close()
	a, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "t1"})
	sess, _ := svc.Redeem(a.Code, RedeemOptions{Name: "Ada"})
	b, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "t2"})
	// Same device, same group: refused (one seat per device per group).
	if _, err := svc.Redeem(b.Code, RedeemOptions{Token: sess.Token}); !errors.Is(err, ErrAlreadyBound) {
		t.Fatalf("err = %v", err)
	}
	if left, _ := svc.Invite(b.Code); left.Uses != 1 {
		t.Fatal("refused redeem spent the invite")
	}
	if _, err := svc.Redeem(b.Code, RedeemOptions{Token: "nope"}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestBindUnbindSetName(t *testing.T) {
	svc, st := newSvc(t, t.TempDir(), testKeys, &clock{})
	defer st.Close()
	a, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "t1"})
	s1, _ := svc.Redeem(a.Code, RedeemOptions{})
	b, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "t2"})
	s2, _ := svc.Redeem(b.Code, RedeemOptions{})
	if err := svc.Bind(s2.Device.ID, "g", "t1"); !errors.Is(err, ErrSeatTaken) {
		t.Fatalf("err = %v", err)
	}
	must(t, svc.Unbind("g", "t1"))
	if _, err := svc.Authenticate(s1.Token, "g"); !errors.Is(err, ErrNotBound) {
		t.Fatalf("err = %v", err)
	}
	must(t, svc.Bind(s1.Device.ID, "g", "t3"))
	must(t, svc.SetName(s1.Device.ID, strings.Repeat("x", 40)))
	d, _ := svc.Device(s1.Device.ID)
	if len(d.Name) != 24 {
		t.Fatalf("name %q", d.Name)
	}
}

func TestMaxTokenAge(t *testing.T) {
	clk := &clock{}
	clk.t.Store(1000)
	st, _ := store.Open(t.TempDir(), store.Options{})
	defer st.Close()
	svc, _ := New(st, Config{Keys: testKeys, Now: clk.Now, MaxTokenAge: time.Hour})
	inv, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "t1"})
	s, _ := svc.Redeem(inv.Code, RedeemOptions{})
	clk.Advance(2 * time.Hour)
	if _, err := svc.Verify(s.Token); !errors.Is(err, ErrTokenTooOld) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseKeys(t *testing.T) {
	keys, err := ParseKeys("a:MDEyMzQ1Njc4OWFiY2RlZg==, b:MDEyMzQ1Njc4OWFiY2RlZg")
	if err != nil || len(keys) != 2 || keys[1].ID != "b" || len(keys[0].Secret) != 16 {
		t.Fatalf("%+v %v", keys, err)
	}
	if _, err := ParseKeys(""); !errors.Is(err, ErrNoKeys) {
		t.Fatal("empty accepted")
	}
	if _, err := ParseKeys("nocolon"); err == nil {
		t.Fatal("accepted")
	}
	st, _ := store.Open(t.TempDir(), store.Options{})
	defer st.Close()
	if _, err := New(st, Config{Keys: []Key{{ID: "x", Secret: []byte("short")}}}); err == nil {
		t.Fatal("short secret accepted")
	}
}

// Concurrent redemptions of a single-use invite: exactly one wins.
func TestRedeemRace(t *testing.T) {
	svc, st := newSvc(t, t.TempDir(), testKeys, &clock{})
	defer st.Close()
	inv, _ := svc.CreateInvite(InviteSpec{Group: "g"})
	var wg sync.WaitGroup
	var wins atomic.Int32
	seats := []string{"t1", "t2", "t3"}
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Redeem(inv.Code, RedeemOptions{Seat: seats[i%3]}); err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d redemptions of a single-use invite", wins.Load())
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

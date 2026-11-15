package identity

import (
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"m31labs.dev/gosx/room/store"
)

func budgetSvc(t *testing.T, clk *clock, cfg Config) *Service {
	t.Helper()
	st, err := store.Open(t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg.Keys, cfg.Now = testKeys, clk.Now
	svc, err := New(st, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// wrongCode returns a well-formed code that names no invite.
func wrongCode(t *testing.T, svc *Service) string {
	t.Helper()
	for {
		c := NewInviteCode()
		if _, err := svc.Invite(c); errors.Is(err, ErrBadInvite) {
			return c
		}
	}
}

// One caller who spends its budget on guesses is refused, even for a correct
// code, without spending the invite. Another caller redeems that invite
// meanwhile, and the first caller's budget refills.
func TestRedeemCallerBudget(t *testing.T) {
	clk := &clock{}
	clk.t.Store(1_000_000)
	// Shared budget off, so only the per-caller default (10 per 10 min) acts.
	svc := budgetSvc(t, clk, Config{RedeemBudget: Budget{Failures: -1}})
	mine, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "a"})
	theirs, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "b"})

	for i := range 10 {
		if _, err := svc.Redeem(wrongCode(t, svc), RedeemOptions{Caller: "attacker"}); !errors.Is(err, ErrBadInvite) {
			t.Fatalf("guess %d: %v", i, err)
		}
	}
	if _, err := svc.Redeem(mine.Code, RedeemOptions{Caller: "attacker"}); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("correct code during lockout: %v, want ErrTooManyAttempts", err)
	}
	if inv, _ := svc.Invite(mine.Code); inv.Uses != 1 {
		t.Fatalf("a refused attempt spent the invite: %d uses left", inv.Uses)
	}

	// A caller in another slot is unaffected and redeems a real invite.
	other := "player"
	for i := 0; svc.attempts.slot(other) == svc.attempts.slot("attacker"); i++ {
		other = fmt.Sprintf("player-%d", i)
	}
	if _, err := svc.Redeem(theirs.Code, RedeemOptions{Caller: other}); err != nil {
		t.Fatalf("another caller during the lockout: %v", err)
	}

	clk.Advance(time.Minute) // refills one attempt
	if _, err := svc.Redeem(mine.Code, RedeemOptions{Caller: "attacker"}); err != nil {
		t.Fatalf("after one refill period: %v", err)
	}
}

// The shared budget protects a caller who passes no key (no limiter of its
// own) and cannot be bypassed with fresh caller keys. It refills.
func TestRedeemSharedBudgetDefault(t *testing.T) {
	clk := &clock{}
	clk.t.Store(1_000_000)
	svc := budgetSvc(t, clk, Config{})
	if svc.cfg.RedeemBudget != (Budget{Failures: 100, Window: time.Minute}) {
		t.Fatalf("default shared budget %+v", svc.cfg.RedeemBudget)
	}
	inv, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "a"})
	for i := range 100 {
		if _, err := svc.Redeem(wrongCode(t, svc), RedeemOptions{}); !errors.Is(err, ErrBadInvite) {
			t.Fatalf("guess %d: %v", i, err)
		}
	}
	if _, err := svc.Redeem(inv.Code, RedeemOptions{}); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("correct code after 100 failures: %v, want ErrTooManyAttempts", err)
	}
	if _, err := svc.Redeem(wrongCode(t, svc), RedeemOptions{Caller: "fresh-key"}); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("fresh caller key during a shared lockout: %v", err)
	}
	clk.Advance(time.Minute)
	if _, err := svc.Redeem(inv.Code, RedeemOptions{}); err != nil {
		t.Fatalf("after the budget refilled: %v", err)
	}
}

// Successful and non-guess outcomes spend no budget: a lockout needs real
// misses.
func TestRedeemValidOutcomesSpendNothing(t *testing.T) {
	clk := &clock{}
	svc := budgetSvc(t, clk, Config{RedeemBudget: Budget{Failures: 2, Window: time.Hour}})
	open, _ := svc.CreateInvite(InviteSpec{Group: "g", Uses: 50})
	for i := range 20 {
		// The code is valid; only the seat is missing.
		if _, err := svc.Redeem(open.Code, RedeemOptions{}); !errors.Is(err, ErrSeatRequired) {
			t.Fatalf("attempt %d: %v", i, err)
		}
		if _, err := svc.Redeem(open.Code, RedeemOptions{Seat: fmt.Sprintf("s%d", i)}); err != nil {
			t.Fatalf("redeem %d: %v", i, err)
		}
	}
}

// Unknown, expired and used-up codes give a guesser the same error.
func TestRedeemFailuresIndistinguishable(t *testing.T) {
	clk := &clock{}
	clk.t.Store(1_000_000)
	svc := budgetSvc(t, clk, Config{})
	used, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "a"})
	owner, err := svc.Redeem(used.Code, RedeemOptions{})
	if err != nil {
		t.Fatal(err)
	}
	expired, _ := svc.CreateInvite(InviteSpec{Group: "g", Seat: "b", TTL: time.Minute})
	clk.Advance(2 * time.Minute)

	_, errUnknown := svc.Redeem(wrongCode(t, svc), RedeemOptions{})
	_, errExpired := svc.Redeem(expired.Code, RedeemOptions{})
	_, errUsed := svc.Redeem(used.Code, RedeemOptions{})
	for _, err := range []error{errUnknown, errExpired, errUsed} {
		if err != ErrBadInvite {
			t.Fatalf("errors %v / %v / %v, want ErrBadInvite for all three", errUnknown, errExpired, errUsed)
		}
	}
	// A caller with a valid device token learns which.
	if _, err := svc.Redeem(expired.Code, RedeemOptions{Token: owner.Token}); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("authenticated, expired: %v", err)
	}
}

// Many distinct caller keys do not grow memory: reserving for 100,000 new
// keys allocates nothing. (About 24 keys share each of the 4096 slots, so
// later keys in a slot find it spent; that is the price of fixed memory.)
func TestRedeemCallerBudgetMemoryBounded(t *testing.T) {
	a := newAttempts(Budget{Failures: -1}, Budget{Failures: 10, Window: time.Minute}, time.Unix(0, 0))
	keys := make([]string, 100_000)
	for i := range keys {
		keys[i] = fmt.Sprintf("2001:db8:%x::/64", i)
	}
	now := time.Unix(1, 0)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	admitted := 0
	for _, k := range keys {
		if _, ok := a.reserve(k, now); ok {
			admitted++
		}
	}
	runtime.ReadMemStats(&after)
	if n := after.Mallocs - before.Mallocs; n > 100 {
		t.Fatalf("%d allocations for 100,000 caller keys; the table must not grow", n)
	}
	// Each slot admits at most its budget; nearly every slot gets 10 keys.
	if admitted > callerSlots*10 || admitted < callerSlots*9 {
		t.Fatalf("admitted %d first attempts, want just under %d", admitted, callerSlots*10)
	}
}

// A failed guess does not wait for the service lock, which a real
// redemption holds through its commit and fsync.
func TestRedeemGuessDoesNotTakeServiceLock(t *testing.T) {
	svc := budgetSvc(t, &clock{}, Config{})
	code := wrongCode(t, svc)
	svc.mu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := svc.Redeem(code, RedeemOptions{Caller: "x"})
		done <- err
	}()
	select {
	case err := <-done:
		svc.mu.Unlock()
		if !errors.Is(err, ErrBadInvite) {
			t.Fatalf("guess: %v", err)
		}
	case <-time.After(2 * time.Second):
		svc.mu.Unlock()
		<-done
		t.Fatal("a failed guess waited for the service lock")
	}
}

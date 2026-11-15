package identity

import (
	"hash/maphash"
	"sync"
	"time"
)

// Budget is an allowance of failed attempts that refills evenly over Window.
// With Failures 10 and Window 10 minutes, a caller who spent it all gets one
// more attempt each minute. Failures below zero turns the limit off.
type Budget struct {
	Failures int
	Window   time.Duration
}

func (b Budget) withDefault(failures int, window time.Duration) Budget {
	if b.Failures == 0 {
		b.Failures = failures
	}
	if b.Window <= 0 {
		b.Window = window
	}
	return b
}

// callerSlots is the size of the per-caller table. Caller keys hash into it
// with a per-process random seed, so memory stays fixed however many keys an
// attacker invents, and an attacker cannot choose a key that lands on a
// chosen user's slot.
const callerSlots = 4096

// slack absorbs floating-point error in refills, so a caller who waited
// exactly one refill period gets the attempt.
const slack = 1e-9

// attempts holds the failed-redemption budgets: one shared by every caller
// and a fixed table of per-caller slots. Each operation holds mu for a few
// arithmetic steps, never across a store access.
type attempts struct {
	mu      sync.Mutex
	seed    maphash.Seed
	base    time.Time
	shared  debt
	callers [callerSlots]debt

	sharedMax, callerMax   float64 // zero: no limit
	sharedRate, callerRate float64 // budget regained per nanosecond
}

// debt is a token bucket stored as the amount spent, so its zero value is a
// full budget.
type debt struct {
	spent float64
	at    int64 // nanoseconds after attempts.base, at the last settle
}

func (d *debt) settle(now int64, rate float64) {
	if now > d.at {
		d.spent = max(0, d.spent-float64(now-d.at)*rate)
		d.at = now
	}
}

func newAttempts(shared, caller Budget, now time.Time) *attempts {
	a := &attempts{seed: maphash.MakeSeed(), base: now}
	if shared.Failures > 0 {
		a.sharedMax = float64(shared.Failures)
		a.sharedRate = a.sharedMax / float64(shared.Window)
	}
	if caller.Failures > 0 {
		a.callerMax = float64(caller.Failures)
		a.callerRate = a.callerMax / float64(caller.Window)
	}
	return a
}

func (a *attempts) slot(caller string) int {
	return int(maphash.String(a.seed, caller) % callerSlots)
}

// reserve takes one attempt from the caller's slot and the shared budget
// before a code is looked up, and refuses when either is spent. Reserving
// first keeps concurrent guesses from overshooting the budget. slot is -1
// when no caller slot applies; pass it to refund.
func (a *attempts) reserve(caller string, t time.Time) (slot int, ok bool) {
	now := int64(t.Sub(a.base))
	a.mu.Lock()
	defer a.mu.Unlock()
	slot = -1
	if a.callerMax > 0 && caller != "" {
		slot = a.slot(caller)
		c := &a.callers[slot]
		c.settle(now, a.callerRate)
		if c.spent+1 > a.callerMax+slack {
			return -1, false
		}
	}
	if a.sharedMax > 0 {
		a.shared.settle(now, a.sharedRate)
		if a.shared.spent+1 > a.sharedMax+slack {
			return -1, false
		}
		a.shared.spent++
	}
	if slot >= 0 {
		a.callers[slot].spent++
	}
	return slot, true
}

// refund returns a reserved attempt that was not a failed guess.
func (a *attempts) refund(slot int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sharedMax > 0 {
		a.shared.spent = max(0, a.shared.spent-1)
	}
	if slot >= 0 {
		a.callers[slot].spent = max(0, a.callers[slot].spent-1)
	}
}

package host

import "time"

// The page's join policy. A page on a loaded machine can hold its first
// hello for many seconds, and a socket that closes before the page is ready
// says nothing about the server, so the page opens a new socket and keeps
// trying until this window ends. The policy is plain Go with no browser code,
// so a native test can drive it with a fake clock.
const (
	// padTestJoinWindow is how long, from the first attempt, the page keeps
	// trying to reach ready.
	padTestJoinWindow = 60 * time.Second
	// padTestRetryFirst is the wait after the first failure; each later wait
	// doubles up to padTestRetryMax.
	padTestRetryFirst = 250 * time.Millisecond
	padTestRetryMax   = 2 * time.Second

	// padTestRefusalCode is the WebSocket close code (policy violation) the
	// server sends after a bad hello or when a page is already connected.
	padTestRefusalCode = 1008
)

// padTestRetry decides whether and when to open another socket. It holds no
// timers and reads no clock: the caller passes the time of each event.
type padTestRetry struct {
	start    time.Time
	attempts int
	next     time.Duration
	ready    bool
	refused  bool
	hello    bool
}

// newPadTestRetry starts the policy at the moment of the first attempt.
func newPadTestRetry(first time.Time) *padTestRetry {
	return &padTestRetry{start: first, attempts: 1, next: padTestRetryFirst}
}

// HelloSent records that the current attempt's socket is open and its hello
// has gone out. From then on the server may already have claimed the page, and
// it admits one page only, so closing the socket would turn every later
// attempt into a refusal. The attempt then waits for ready until the join
// window ends.
func (r *padTestRetry) HelloSent() { r.hello = true }

// AttemptTimerEnds reports whether the per-attempt timer may end the current
// attempt. It may only until hello is sent: a page that cannot open a socket
// in time tries again, but a page whose hello is out waits for the server.
func (r *padTestRetry) AttemptTimerEnds() bool { return !r.hello && !r.ready }

// Ready records that the server answered hello. No retry follows.
func (r *padTestRetry) Ready() { r.ready = true }

// Failed records that an attempt ended before ready at time now. closeCode is
// the WebSocket close code the socket ended with, or 0 when it ended some
// other way (an error event, a timeout, a socket that never opened). It
// returns how long to wait before the next attempt, or false when the page
// should give up: it is ready already, the server refused it (a refusal is
// final: a retry would be refused again), or the join window has passed.
func (r *padTestRetry) Failed(now time.Time, closeCode int) (time.Duration, bool) {
	if closeCode == padTestRefusalCode {
		r.refused = true
		return 0, false
	}
	if r.ready || now.Sub(r.start) >= padTestJoinWindow {
		return 0, false
	}
	r.hello = false
	d := r.next
	r.next *= 2
	if r.next > padTestRetryMax {
		r.next = padTestRetryMax
	}
	r.attempts++
	return d, true
}

// Refused reports whether the last failure was a refusal by the server.
func (r *padTestRetry) Refused() bool { return r.refused }

// Attempts is the number of sockets opened so far, the first one included.
func (r *padTestRetry) Attempts() int { return r.attempts }

package telemetry

import (
	"strings"

	"m31labs.dev/gosx/server"
)

// The App installs this adapter once. Mounted routers keep their application
// observers but never install another built-in telemetry subscriber.
type requestObserver struct{ owner *Telemetry }

func (o requestObserver) ObserveRequestStart() { o.owner.requestDelta(1) }

func (o requestObserver) Observe(e server.RequestEvent) {
	o.owner.requestDelta(-1)
	pattern := strings.TrimSpace(e.Pattern)
	if i := strings.IndexAny(pattern, " \t"); i >= 0 {
		pattern = strings.TrimSpace(pattern[i+1:])
	}
	o.owner.observeRequest(e.Kind, pattern, e.Method, e.Status, e.ResponseBytes, e.Duration, e.Hijacked)
}

func (t *Telemetry) requestDelta(delta int64) {
	if !t.Enabled() || t.requests == nil {
		return
	}
	s := t.requests
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if !t.Enabled() || delta < 0 && s.live == 0 {
		return
	}
	s.live += delta
	// Serialize the count and its gauge publication so concurrent callbacks
	// cannot publish an older count after a newer one.
	_ = s.inFlight.Set(float64(s.live))
}

// Called after admission stops. Late request callbacks cannot restore a live
// contribution, even when the worker exits before the application's handlers.
func (t *Telemetry) releaseRequests() {
	s := t.requests
	if s == nil {
		return
	}
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	s.live = 0
	_ = s.inFlight.Set(0)
}

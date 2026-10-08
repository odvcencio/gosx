package hub

import (
	"errors"
	"log"
	"time"
)

// SlowClientPolicy opts into eviction based on newly dropped queued messages.
// Zero DropThreshold disables eviction. Configure before the first upgrade.
type SlowClientPolicy struct {
	DropThreshold int
	CheckInterval time.Duration
}

var ErrInvalidSlowClient = errors.New("hub: invalid slow-client policy")

// Validate checks the policy without starting a timer or changing it.
func (p SlowClientPolicy) Validate() error {
	_, err := p.normalized()
	return err
}

func (p SlowClientPolicy) normalized() (SlowClientPolicy, error) {
	if p.DropThreshold < 0 || p.CheckInterval < 0 {
		return p, ErrInvalidSlowClient
	}
	if p.DropThreshold == 0 {
		return SlowClientPolicy{}, nil
	}
	if p.CheckInterval == 0 {
		p.CheckInterval = time.Second
	}
	if p.CheckInterval < 100*time.Millisecond || p.CheckInterval > time.Minute {
		return p, ErrInvalidSlowClient
	}
	return p, nil
}

func (s *transportState) interval() time.Duration {
	if s.slow.DropThreshold > 0 && s.slow.CheckInterval < pingPeriod {
		steps := (pingPeriod + s.slow.CheckInterval - 1) / s.slow.CheckInterval
		return pingPeriod / steps
	}
	return pingPeriod
}

func (h *Hub) warnInvalidSlowClient(now time.Time) {
	h.observerWarningMu.Lock()
	warn := h.policyWarningAt.IsZero() || now.Sub(h.policyWarningAt) >= time.Minute
	if warn {
		h.policyWarningAt = now
	}
	h.observerWarningMu.Unlock()
	if warn {
		log.Print("[gosx hub] configuration refused; class=invalid_slow_client")
	}
}

// slowClient is called by the writer's existing timer, outside connection/hub
// locks. Each interval uses a fresh delta, never the lifetime drop total. A
// tick that lands within half a tick of CheckInterval counts as due, so checks
// follow the shared tick without skipping every second one.
func (c *Client) slowClient(now time.Duration) bool {
	s := c.transport
	elapsed := now - s.lastCheck
	if s.slow.DropThreshold == 0 || elapsed+s.interval()/2 <= s.slow.CheckInterval {
		return false
	}
	drops := c.DropStats()
	text, binary := drops.Text-s.lastDrops.Text, drops.Binary-s.lastDrops.Binary
	s.lastDrops, s.lastCheck = drops, now
	// A window shorter than CheckInterval needs proportionally fewer drops.
	limit := float64(s.slow.DropThreshold) * float64(min(elapsed, s.slow.CheckInterval)) / float64(s.slow.CheckInterval)
	return float64(text)+float64(binary) >= limit
}

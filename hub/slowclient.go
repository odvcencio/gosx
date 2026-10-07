package hub

import (
	"errors"
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
		return s.slow.CheckInterval
	}
	return pingPeriod
}

// slowClient is called by the writer's existing timer, outside connection/hub
// locks. Each interval uses a fresh delta, never the lifetime drop total.
func (c *Client) slowClient(now time.Duration) bool {
	s := c.transport
	if s.slow.DropThreshold == 0 || now-s.lastCheck < s.slow.CheckInterval {
		return false
	}
	drops := c.DropStats()
	text, binary := drops.Text-s.lastDrops.Text, drops.Binary-s.lastDrops.Binary
	s.lastDrops, s.lastCheck = drops, now
	threshold := uint64(s.slow.DropThreshold)
	// Comparing before summing avoids overflow even at saturated totals.
	return text >= threshold || binary >= threshold || text >= threshold-binary
}

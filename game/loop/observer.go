package loop

import (
	"errors"
	"time"
)

// FrameEvent measures the existing Step, error callback and render work.
// Interval is the host frame gap; it is zero on first visibility or restart.
type FrameEvent struct{ Duration, Interval time.Duration }

// Observer receives visible frames synchronously outside all Driver locks.
// Return promptly. An admitted frame may finish after Stop; hidden frames do
// not produce events. Measure an authoritative sim runner at that owner only.
type Observer interface{ ObserveFrame(FrameEvent) }

var ErrObserverStarted = errors.New("loop: observer cannot change after Start")

// SetObserver installs an observer, including nil, before the first Start.
// Once started, replacement is rejected even while the driver is stopped.
func (d *Driver) SetObserver(o Observer) error {
	if d == nil {
		return errors.New("loop: nil driver")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started {
		return ErrObserverStarted
	}
	d.observer = o
	return nil
}

package loop

import "sync"

// ManualFrameSource is a FrameSource a test (or a non-browser host that
// wants to drive frames on its own schedule) advances explicitly by calling
// Fire. It never schedules anything on its own.
type ManualFrameSource struct {
	mu     sync.Mutex
	handle int
	cb     func(float64)
	hidden bool
}

// NewManualFrameSource creates a ManualFrameSource. The zero value is also
// ready to use; the constructor exists for symmetry with the rest of the
// package.
func NewManualFrameSource() *ManualFrameSource {
	return &ManualFrameSource{}
}

// RequestFrame implements FrameSource.
func (m *ManualFrameSource) RequestFrame(cb func(timestampMS float64)) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handle++
	m.cb = cb
	return m.handle
}

// CancelFrame implements FrameSource.
func (m *ManualFrameSource) CancelFrame(handle int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handle == handle {
		m.cb = nil
	}
}

// Hidden implements FrameSource.
func (m *ManualFrameSource) Hidden() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hidden
}

// SetHidden changes what Hidden reports on the next call.
func (m *ManualFrameSource) SetHidden(hidden bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hidden = hidden
}

// Fire invokes the currently pending RequestFrame callback, if any, with
// timestampMS, simulating one animation frame. It reports whether a
// callback was pending.
func (m *ManualFrameSource) Fire(timestampMS float64) bool {
	m.mu.Lock()
	cb := m.cb
	m.cb = nil
	m.mu.Unlock()
	if cb == nil {
		return false
	}
	cb(timestampMS)
	return true
}

// Pending reports whether a RequestFrame call is currently outstanding.
func (m *ManualFrameSource) Pending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cb != nil
}

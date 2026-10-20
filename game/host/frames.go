package host

// FrameSource is an animation-frame clock. FrameSourceJS (built only
// under GOOS=js/GOARCH=wasm) wraps window.requestAnimationFrame and
// document.hidden. Native tests can supply a fake, including
// game/loop.ManualFrameSource when they also use the simulation driver.
type FrameSource interface {
	// RequestFrame schedules cb to run exactly once, at the next animation
	// frame, with the host timestamp in milliseconds. It returns a handle
	// CancelFrame can use to cancel the request before it fires.
	RequestFrame(cb func(timestampMS float64)) int
	// CancelFrame cancels a pending RequestFrame call. Canceling a handle
	// that already fired, or was never issued, is a no-op.
	CancelFrame(handle int)
	// Hidden reports whether the host surface is currently hidden, for
	// example a backgrounded browser tab.
	Hidden() bool
}

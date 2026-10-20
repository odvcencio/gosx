// Package browserdom implements the internal DOM bridge used by typed browser services.
package browserdom

// Markup is application-authored HTML. Escape untrusted text before constructing it.
type Markup string

// Rect is a snapshot of an element's layout in CSS pixels.
type Rect struct{ Left, Top, Right, Bottom, Width, Height float64 }

// ListenerOptions controls native event delivery without eagerly decoding events.
type ListenerOptions struct{ Capture, Passive, Once bool }

// EventTarget owns native event listeners.
type EventTarget interface {
	On(string, func(Event), ...ListenerOptions) *Listener
}

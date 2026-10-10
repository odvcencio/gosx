//go:build js && wasm

package loop

import "m31labs.dev/gosx/game/host"

// FrameSourceJS owns browser animation-frame callbacks. Presentation-only
// engines can import game/host directly without pulling in game.Runtime.
type FrameSourceJS = host.FrameSourceJS

// NewFrameSourceJS creates a browser animation-frame source.
func NewFrameSourceJS() *FrameSourceJS { return host.NewFrameSourceJS() }

//go:build js && wasm

package gamepad

import "m31labs.dev/gosx/game/host"

// NavigatorSource reads browser gamepads. Engines that do not need game.Input
// can import game/host directly without pulling in game.Runtime.
type NavigatorSource = host.NavigatorSource

// ForwardedInput is the page input forwarded to an engine worker. It
// implements Source, so a Poller works in a worker as on the main thread.
type ForwardedInput = host.ForwardedInput

// NewForwardedInput subscribes to forwarded page input; ok is false outside an
// engine worker.
func NewForwardedInput() (*ForwardedInput, bool) { return host.NewForwardedInput() }

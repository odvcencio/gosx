//go:build js && wasm

package gamepad

import "m31labs.dev/gosx/game/host"

// NavigatorSource reads browser gamepads. Engines that do not need game.Input
// can import game/host directly without pulling in game.Runtime.
type NavigatorSource = host.NavigatorSource

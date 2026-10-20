//go:build js && wasm

package main

import (
	"fmt"

	"m31labs.dev/gosx/game/host"
)

func main() {
	frames := host.NewFrameSourceJS()
	id := frames.RequestFrame(func(now float64) {
		pads := (host.NavigatorSource{}).Gamepads()
		fmt.Println(now, frames.Hidden(), len(pads))
	})
	frames.CancelFrame(id)
}

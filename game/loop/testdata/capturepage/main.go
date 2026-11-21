//go:build js && wasm

// Command capturepage drives a game.Runtime with a loop.Driver on the browser
// frame source, the way a game does. capture_build_test.go builds it with and
// without the gosx_capture tag.
package main

import (
	"time"

	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/game/loop"
)

func main() {
	rt := game.New(game.Config{FixedStep: time.Second / 60})
	d := loop.New(rt, loop.NewFrameSourceJS())
	d.Start(nil)
	select {}
}

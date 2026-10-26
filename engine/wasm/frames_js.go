//go:build js && wasm

package wasm

import (
	"context"
	"errors"
	"fmt"
)

// EachFrame calls step once per visible animation frame until ctx is done or
// step returns an error. It is the way to write a loop that runs on the page.
//
// A Go WASM engine on the page's main thread shares one thread with the
// browser. The browser draws, handles input and runs timers only when Go is
// blocked on something that ends in a browser event; a promise that is already
// resolved does not count, because the browser runs it before it returns to its
// own work. A loop that sends Scene3D updates and waits for each result, with
// nothing slower in it, therefore never gives the browser a turn, and the page
// freezes. EachFrame waits for requestAnimationFrame between calls, so the
// browser draws and answers input between every two iterations. In an engine
// worker it waits on the worker's frame clock instead.
//
// While the page is hidden no frame arrives and step does not run. The first
// frame after the page is shown has Delta zero.
//
// EachFrame blocks, so call it from a goroutine, never from the Factory. It
// returns nil when step returns StopFrames, the error step returns, or the
// error of ctx when ctx is done.
func EachFrame(ctx context.Context, step func(Frame) error) error {
	if step == nil {
		return fmt.Errorf("EachFrame requires a step function")
	}
	if ctx == nil {
		return fmt.Errorf("EachFrame requires a context")
	}
	waiter := newSceneFrameWaiter()
	defer waiter.dispose()
	previous := 0.0
	for {
		at, paused, err := waiter.next(ctx)
		if err != nil {
			return err
		}
		frame := Frame{Time: at}
		if previous > 0 && !paused {
			frame.Delta = max(0, at-previous)
		}
		previous = at
		if err = step(frame); err != nil {
			if errors.Is(err, StopFrames) {
				return nil
			}
			return err
		}
	}
}

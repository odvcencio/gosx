//go:build js && wasm

// Command fixture is the Go WASM engine the real-browser worker tests run. The
// page chooses its behavior through engine props, so one binary covers every
// scenario. It reports to the test through ctx.Emit("report", ...), which works
// in a worker (the port relays it) and on the main thread (fallback).
package main

import (
	"syscall/js"
	"time"

	"m31labs.dev/gosx/engine/wasm"
	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/game/loop"
)

func perfNow() float64 { return js.Global().Get("performance").Call("now").Float() }

func report(ctx wasm.Context, kind string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["kind"] = kind
	if err := ctx.Emit("report", data); err != nil {
		js.Global().Get("console").Call("error", "report failed: "+err.Error())
	}
}

func main() {
	err := wasm.Register("WorkerFixture", func(ctx wasm.Context) (wasm.Handle, error) {
		var props struct {
			Scenario  string `json:"scenario"`
			Tag       string `json:"tag"`
			RunHidden bool   `json:"runHidden"`
		}
		_ = ctx.DecodeProps(&props)
		_, hasPort := ctx.Port()
		report(ctx, "mounted", map[string]any{
			"thread":      string(ctx.Thread()),
			"fallback":    ctx.ThreadFallbackReason(),
			"hasDocument": !js.Global().Get("document").IsUndefined(),
			"hasPort":     hasPort,
			"scenario":    props.Scenario,
		})
		var stop func()
		switch props.Scenario {
		case "clock":
			stop = startClock(ctx, props.Tag, props.RunHidden)
		}
		return wasm.HandleFunc(func() {
			if stop != nil {
				stop()
			}
			report(ctx, "disposed", nil)
		}), nil
	})
	if err != nil {
		js.Global().Get("console").Call("error", err.Error())
	}
	select {}
}

// countingSource counts every clock callback, whether or not the driver steps,
// so a paused driver (hidden page, RunWhileHidden false) is still observable.
type countingSource struct {
	loop.FrameSource
	onTick func(timestampMS float64)
}

func (c countingSource) RequestFrame(cb func(float64)) int {
	return c.FrameSource.RequestFrame(func(ts float64) { c.onTick(ts); cb(ts) })
}

// startClock runs a 60 Hz game.Runtime on loop.Driver over the browser frame
// source and reports its cadence every 30 clock ticks. In a worker the frame
// source is the worker's timer clock; on the main thread it is
// requestAnimationFrame.
func startClock(ctx wasm.Context, tag string, runHidden bool) (stop func()) {
	steps, frames, ticks := 0, 0, 0
	begin := perfNow()
	var last, gapMax, gapSum float64
	var lastTick, tickGapMax float64
	rt := game.New(game.Config{
		FixedStep: time.Second / 60, MaxSubsteps: 8, MaxDelta: time.Second,
		Systems: []game.System{game.Func("count", game.PhaseFixedUpdate, func(*game.Context) error { steps++; return nil })},
	})
	inner := loop.NewFrameSourceJS()
	source := countingSource{FrameSource: inner}
	source.onTick = func(float64) {
		now := perfNow()
		if lastTick != 0 && now-lastTick > tickGapMax {
			tickGapMax = now - lastTick
		}
		lastTick = now
		ticks++
		if ticks%30 != 0 {
			return
		}
		data := map[string]any{
			"tag": tag, "steps": steps, "frames": frames, "ticks": ticks, "gapMaxMs": gapMax, "tickGapMaxMs": tickGapMax,
			"hidden": inner.Hidden(), "elapsedMs": now - begin, "thread": string(ctx.Thread()), "runHidden": runHidden,
		}
		if frames > 1 {
			data["gapAvgMs"] = gapSum / float64(frames-1)
		}
		if port, ok := ctx.Port(); ok {
			data["clock"] = port.FrameStats()
		}
		report(ctx, "clock", data)
		gapMax, tickGapMax = 0, 0
	}
	driver := loop.New(rt, source)
	driver.RunWhileHidden = runHidden
	driver.Start(func(float64, game.Frame) {
		now := perfNow()
		if last != 0 {
			gap := now - last
			gapSum += gap
			if gap > gapMax {
				gapMax = gap
			}
		}
		last = now
		frames++
	})
	return driver.Stop
}

//go:build js && wasm

// Command fixture is the Go WASM engine the real-browser worker tests run. The
// page chooses its behavior through engine props, so one binary covers every
// scenario. It reports to the test through ctx.Emit("report", ...), which works
// in a worker (the port relays it) and on the main thread (fallback).
package main

import (
	"context"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/engine/wasm"
	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/game/host"
	"m31labs.dev/gosx/game/loop"
	"m31labs.dev/gosx/scene"
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
			Target    string `json:"target"`
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
		case "scene":
			stop = startScene(ctx, props.Target)
		case "input":
			stop = startInput(ctx)
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

// startScene waits for the page's Scene3D mount, then drives one instanced box
// across the view at 60 Hz with GSXI instance-stream frames, writes the frame
// counter to a shared signal, and sends one pose, motion and clip frame so the
// test can see all four binary formats reach the page.
func startScene(ctx wasm.Context, target string) (stop func()) {
	const totalFrames = 240
	done := make(chan struct{})
	stream := scene.NewStreamWriter(target)
	source := loop.NewFrameSourceJS()
	var handle int
	go func() {
		readyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := ctx.Scene3DByID(target).Ready(readyCtx); err != nil {
			report(ctx, "scene-error", map[string]any{"error": err.Error()})
			return
		}
		// On the main thread Prepare loads the stream codec and reports false
		// until it arrives; in a worker it is ready at once.
		for deadline := time.Now().Add(10 * time.Second); !stream.Prepare(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				report(ctx, "scene-error", map[string]any{"error": "stream writer not ready"})
				return
			}
		}
		report(ctx, "scene-ready", nil)
		rev, applied, rejected := uint64(0), 0, 0
		var step func(float64)
		step = func(float64) {
			select {
			case <-done:
				return
			default:
			}
			rev++
			x := float32(-3 + 6*float64(rev)/totalFrames)
			ok, err := stream.Apply(scene.InstanceStreamFrame{
				BatchID: "crowd", Revision: rev, Kind: scene.InstanceStreamTransform, Count: 1,
				Data: []float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, x, 0, 0, 1},
			})
			if err != nil || !ok {
				rejected++
			} else {
				applied++
			}
			if rev%10 == 0 {
				_ = ctx.SetChangedSignals(map[string]any{"$sim": map[string]any{"rev": rev, "x": x}})
			}
			if rev == 3 {
				sendOtherFrames(target)
			}
			if rev%30 == 0 {
				report(ctx, "scene-progress", map[string]any{"rev": rev, "applied": applied, "rejected": rejected})
			}
			if rev >= totalFrames {
				report(ctx, "scene-done", map[string]any{"rev": rev, "applied": applied, "rejected": rejected})
				return
			}
			handle = source.RequestFrame(step)
		}
		handle = source.RequestFrame(step)
	}()
	return func() {
		close(done)
		source.CancelFrame(handle)
		stream.Dispose()
	}
}

// sendOtherFrames sends one frame of each remaining binary format. The page has
// no model batches mounted, so the renderer may reject them; the test checks
// only that the bytes reach the page's dispatch entry points.
func sendOtherFrames(target string) {
	_ = scene.DispatchPoseFrame(target, scene.PoseFrame{Batches: []scene.PoseBatch{{ID: "actors", Instances: []scene.MeshInstanceIR{
		{ID: "a0", X: 1, ScaleX: 1, ScaleY: 1, ScaleZ: 1, Animation: "Run", AnimationTime: .25, AnimationLoop: true}}}}}, nil)
	_ = scene.DispatchMotionFrame(target, scene.MotionFrame{Batches: []scene.MotionBatch{{ID: "crowd2", Instances: []scene.MotionInstanceIR{
		{ID: "m0", Animation: "Run", TPrev: 0, TNext: 1, PrevScaleX: 1, PrevScaleY: 1, PrevScaleZ: 1, NextScaleX: 1, NextScaleY: 1, NextScaleZ: 1}}}}}, nil)
	_ = scene.DispatchClipFrame(target, scene.ClipFrame{Models: []scene.ClipModel{{ID: "hero", Layers: []scene.ClipLayer{{Name: "Run", Time: .5, Weight: 1, Loop: true}}}}})
}

// startInput reports every forwarded input event with its measured delay, and
// the pad state a game/gamepad.Poller-style reader would see.
func startInput(ctx wasm.Context) (stop func()) {
	input, ok := host.NewForwardedInput()
	if !ok {
		report(ctx, "input-error", map[string]any{"error": "no forwarded input in this context"})
		return nil
	}
	cancel := input.Subscribe(func(e host.InputEvent) {
		data := map[string]any{
			"k": e.Kind, "code": e.Code, "down": e.Down, "type": e.PointerType, "x": e.X, "y": e.Y, "mask": e.Mask,
			"delay": e.Delay(), "at": e.AtMS, "recv": e.ReceivedMS, "pageEpoch": e.PageEpochMS,
			"nowMS": perfNow(), "keyA": input.KeyDown("KeyA"),
		}
		if e.Kind == "pad" {
			data["index"], data["connected"], data["buttons"], data["axes"] = e.PadIndex, e.Connected, e.Buttons, e.Axes
			data["padsHeld"] = len(input.Gamepads())
		}
		report(ctx, "input", data)
	})
	report(ctx, "input-ready", nil)
	return func() { cancel(); input.Close() }
}

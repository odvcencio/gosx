//go:build js && wasm

// Command engine is the game-stress example's Go WASM engine. It runs the
// stress.Game workload (22 agents at 30 Hz with replays), animates the 22
// skinned figures, starts the 40,000-figure crowd on the GPU, and records what
// every frame cost.
//
// On the main thread it rides the Scene3D renderer's frame
// (host.SceneFrameSource): each display frame steps the game and writes the
// figures' poses before the renderer draws. In a Web Worker it runs on the
// worker's clock and sends one timed pose frame per tick; the page
// interpolates them (scene.DispatchTimedPoseFrame).
package main

import (
	"context"
	"encoding/binary"
	"math"
	"runtime/metrics"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/engine/wasm"
	"m31labs.dev/gosx/examples/game-stress/stage"
	"m31labs.dev/gosx/examples/game-stress/stress"
	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/game/host"
	"m31labs.dev/gosx/game/loop"
	"m31labs.dev/gosx/scene"
)

var perf = js.Global().Get("performance")

func now() float64 { return perf.Call("now").Float() }

func main() {
	if err := wasm.Register("GameStress", mount); err != nil {
		js.Global().Get("console").Call("error", err.Error())
	}
	select {}
}

func report(ctx wasm.Context, kind string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["kind"] = kind
	_ = ctx.Emit("report", data)
}

func mount(ctx wasm.Context) (wasm.Handle, error) {
	var props stage.EngineProps
	if err := ctx.DecodeProps(&props); err != nil {
		return nil, err
	}
	if props.Target == "" {
		props.Target = stage.MountID
	}
	_, inWorker := ctx.Port()
	e := &stressEngine{ctx: ctx, props: props, worker: inWorker, done: make(chan struct{})}
	e.marker = newMarker(inWorker)
	scripted := stress.ScriptedInput(7)
	e.game = stress.NewGame(stress.GameConfig{
		Sim: props.SimConfig(), ReplayEvery: props.ReplayEvery, ReplayTicks: props.ReplayTicks, Clock: now,
		Checkpoints: []uint64{300, 600},
		Input: func(tick uint64, seat int) byte {
			in := scripted(tick, seat)
			if seat == 0 && e.marker.held() {
				in |= stress.InputMarker
			}
			return in
		},
	})
	report(ctx, "mounted", map[string]any{"thread": string(ctx.Thread()), "fallback": ctx.ThreadFallbackReason(), "worker": inWorker})
	go e.run()
	return wasm.HandleFunc(e.stop), nil
}

type stressEngine struct {
	ctx    wasm.Context
	props  stage.EngineProps
	worker bool
	game   *stress.Game
	marker *marker
	driver *loop.Driver
	done   chan struct{}

	anim    animator
	ids     []string
	poses   []scene.MeshInstanceIR
	frame   scene.PoseFrame
	rec     recorder
	gc      gcReader
	started float64
	posted  float64
}

func (e *stressEngine) stop() {
	select {
	case <-e.done:
	default:
		close(e.done)
	}
	if e.driver != nil {
		e.driver.Stop()
	}
}

func (e *stressEngine) run() {
	readyCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := e.ctx.Scene3DByID(e.props.Target).Ready(readyCtx); err != nil {
		report(e.ctx, "error", map[string]any{"error": "scene not ready: " + err.Error()})
		return
	}
	n := e.props.Players
	e.ids = make([]string, n)
	for i := range e.ids {
		e.ids[i] = "p" + itoa(i)
	}
	e.poses = make([]scene.MeshInstanceIR, n)
	e.frame = scene.PoseFrame{Batches: []scene.PoseBatch{{ID: stage.PlayersBatch, Instances: e.poses}}}
	e.anim = newAnimator(n)
	e.rec = newRecorder(e.ctx, e.worker)
	e.gc = newGCReader()
	if err := e.startCrowd(); err != nil {
		report(e.ctx, "error", map[string]any{"error": "crowd motion frame: " + err.Error()})
	}
	var source loop.FrameSource
	if e.worker {
		source = loop.NewFrameSourceJS() // the worker's own 60 Hz clock
	} else {
		source = host.NewSceneFrameSource(e.props.Target) // the renderer's frame
	}
	timed := &timedSource{FrameSource: source}
	e.driver = loop.New(e.game.Runtime(), timed)
	e.driver.RunWhileHidden = e.worker
	e.driver.OnError = func(err error) { report(e.ctx, "error", map[string]any{"error": err.Error()}) }
	report(e.ctx, "running", map[string]any{"players": n, "crowd": e.props.Crowd})
	e.driver.Start(func(alpha float64, f game.Frame) { e.frameDone(timed.start, alpha, f) })
}

// frameDone runs after Step in every frame: animate, write the poses and
// record the frame.
func (e *stressEngine) frameDone(start, alpha float64, f game.Frame) {
	st := e.game.Stats()
	t0 := now()
	dt := f.Delta.Seconds()
	var postMS float64
	if e.worker {
		// One timed frame per new tick; the page interpolates between them.
		if st.Ticks > 0 {
			e.writePoses(1, float64(st.Ticks)/float64(e.props.SimConfig().TickHz))
			p0 := now()
			step := e.game.Runtime().FixedStep()
			at := time.Duration(e.game.Sim().Tick()) * step
			if err := scene.DispatchTimedPoseFrame(e.props.Target, e.frame, at, step); err != nil {
				e.rec.fail(err)
			}
			postMS = now() - p0
		}
	} else {
		e.writePoses(alpha, dt)
		if err := scene.DispatchPoseFrame(e.props.Target, e.frame, nil); err != nil {
			e.rec.fail(err)
		}
	}
	end := now()
	var r [stage.GoRecordFloats]float64
	r[stage.RecTimeMS] = start
	r[stage.RecHookMS] = end - start
	r[stage.RecSimMS] = st.SimMS
	r[stage.RecAIMS] = st.AIMS
	r[stage.RecReplayMS] = st.ReplayMS
	r[stage.RecAnimMS] = end - t0 - postMS
	r[stage.RecPostMS] = postMS
	r[stage.RecTicks] = float64(st.Ticks)
	r[stage.RecReplayed] = float64(st.Replayed)
	r[stage.RecSimTick] = float64(e.game.Sim().Tick())
	r[stage.RecMismatches] = float64(st.Mismatches)
	e.gc.read(&r)
	for _, c := range [...]uint64{300, 600} {
		if h, ok := e.game.Checkpoint(c); ok && !e.rec.hashSent[c] {
			e.rec.hashSent[c] = true
			r[stage.RecHashHi], r[stage.RecHashLo], r[stage.RecHashTick] = float64(h>>32), float64(h&0xffffffff), float64(c)
			break
		}
	}
	e.rec.push(&r)
}

// writePoses fills the pose frame: each figure at its interpolated position,
// facing its heading, with its animation clip and time.
func (e *stressEngine) writePoses(alpha, dt float64) {
	for i := range e.poses {
		p := e.game.Pose(i, alpha)
		clip, t := e.anim.update(i, p.Speed, dt)
		e.poses[i] = scene.MeshInstanceIR{ID: e.ids[i], X: p.X, Y: p.Y, Z: p.Z, RotationY: p.Heading,
			ScaleX: 1, ScaleY: 1, ScaleZ: 1, Animation: clip, AnimationTime: t, AnimationLoop: true}
	}
}

// startCrowd sends one motion frame with a batch per stand section: every
// figure holds its seat and loops its clip from its own start time. The
// renderer then animates the whole crowd on the GPU with no per-frame work
// for it on any thread. It is one frame because the page keeps only the newest
// pending full motion frame per mount: eight frames sent back to back would
// supersede each other.
func (e *stressEngine) startCrowd() error {
	if e.props.Crowd <= 0 {
		return nil
	}
	cfg := e.props.SimConfig()
	var frame scene.MotionFrame
	for _, sec := range stage.Crowd(e.props.Crowd, e.props.Sections, cfg.HalfX, cfg.HalfZ) {
		instances := make([]scene.MotionInstanceIR, len(sec.Seats))
		for i, s := range sec.Seats {
			clip, rate := stage.ClipSway, 1.0
			if s.Cheer {
				clip, rate = stage.ClipCheer, 0.85+0.3*s.Phase/1.1
			}
			instances[i] = scene.MotionInstanceIR{ID: stage.SeatID(i),
				PrevX: s.X, PrevY: s.Y, PrevZ: s.Z, PrevRotationY: s.Yaw, PrevScaleX: 1, PrevScaleY: 1, PrevScaleZ: 1, TPrev: 0,
				NextX: s.X, NextY: s.Y, NextZ: s.Z, NextRotationY: s.Yaw, NextScaleX: 1, NextScaleY: 1, NextScaleZ: 1, TNext: 1e6,
				Animation: clip, ClipStartTime: -s.Phase, AnimationLoop: true, PlaybackRate: rate}
		}
		frame.Batches = append(frame.Batches, scene.MotionBatch{ID: sec.ID, Instances: instances})
	}
	return scene.DispatchMotionFrame(e.props.Target, frame, nil)
}

// timedSource records when each frame callback starts, so a frame's whole
// engine time is measured, Step included.
type timedSource struct {
	loop.FrameSource
	start float64
}

func (t *timedSource) RequestFrame(cb func(float64)) int {
	return t.FrameSource.RequestFrame(func(ts float64) { t.start = now(); cb(ts) })
}

func (t *timedSource) SetFrameAlpha(alpha float64) {
	if sink, ok := t.FrameSource.(host.FrameAlphaSink); ok {
		sink.SetFrameAlpha(alpha)
	}
}

// animator picks each figure's clip from its speed and advances the clip
// time. Times only grow; the renderer wraps a looping clip.
type animator struct {
	running []bool
	run     []float64
	idle    []float64
}

func newAnimator(n int) animator {
	return animator{running: make([]bool, n), run: make([]float64, n), idle: make([]float64, n)}
}

func (a *animator) update(i int, speed, dt float64) (string, float64) {
	switch {
	case speed > 1.2:
		a.running[i] = true
	case speed < 0.8:
		a.running[i] = false
	}
	if a.running[i] {
		a.run[i] += dt * math.Max(0.6, math.Min(1.6, speed/5.5))
		return stage.ClipRun, a.run[i]
	}
	a.idle[i] += dt
	return stage.ClipIdle, a.idle[i]
}

// recorder hands frame records to the page: on the main thread it writes each
// one into a slot of the harness's ring buffer (one call per frame); in a
// worker it posts them in batches of 30.
type recorder struct {
	worker   bool
	port     wasm.Port
	slots    []js.Value
	frame    uint64
	bytes    []byte
	batch    []byte
	hashSent map[uint64]bool
	failures int
	ctx      wasm.Context
}

func newRecorder(ctx wasm.Context, worker bool) recorder {
	r := recorder{worker: worker, ctx: ctx, bytes: make([]byte, stage.GoRecordFloats*8), hashSent: map[uint64]bool{}}
	if worker {
		r.port, _ = ctx.Port()
		r.batch = make([]byte, 0, 30*stage.GoRecordFloats*8)
		return r
	}
	ring := js.Global().Get("__stressGoSlots")
	if ring.Truthy() {
		r.slots = make([]js.Value, ring.Length())
		for i := range r.slots {
			r.slots[i] = ring.Index(i)
		}
	}
	return r
}

func (r *recorder) push(rec *[stage.GoRecordFloats]float64) {
	r.frame++
	rec[stage.RecFrame] = float64(r.frame)
	for i, v := range rec {
		binary.LittleEndian.PutUint64(r.bytes[i*8:], math.Float64bits(v))
	}
	if r.worker {
		r.batch = append(r.batch, r.bytes...)
		if len(r.batch) >= 30*len(r.bytes) {
			r.port.PostBytes("stress.frames", r.batch)
			r.batch = r.batch[:0]
		}
		return
	}
	if len(r.slots) > 0 {
		js.CopyBytesToJS(r.slots[int(r.frame)%len(r.slots)], r.bytes)
	}
}

func (r *recorder) fail(err error) {
	r.failures++
	if r.failures <= 3 {
		report(r.ctx, "error", map[string]any{"error": err.Error()})
	}
}

// gcReader reports the Go garbage collector's work since the last frame from
// runtime/metrics: its CPU time, its stop-the-world pauses, cycles, and bytes
// allocated.
type gcReader struct {
	samples                  []metrics.Sample
	lastCPU, lastPause       float64
	lastCycles, lastAllocKiB float64
}

func newGCReader() gcReader {
	g := gcReader{samples: []metrics.Sample{
		{Name: "/cpu/classes/gc/total:cpu-seconds"},
		{Name: "/sched/pauses/total/gc:seconds"},
		{Name: "/gc/cycles/total:gc-cycles"},
		{Name: "/gc/heap/allocs:bytes"},
		{Name: "/gc/heap/live:bytes"},
	}}
	var r [stage.GoRecordFloats]float64
	g.read(&r)
	return g
}

func (g *gcReader) read(r *[stage.GoRecordFloats]float64) {
	metrics.Read(g.samples)
	cpu := value(g.samples[0])
	pause := 0.0
	if g.samples[1].Value.Kind() == metrics.KindFloat64Histogram {
		h := g.samples[1].Value.Float64Histogram()
		for i, c := range h.Counts {
			lo, hi := h.Buckets[i], h.Buckets[i+1]
			if math.IsInf(hi, 1) {
				hi = lo
			}
			if math.IsInf(lo, -1) {
				lo = hi
			}
			pause += float64(c) * (lo + hi) / 2
		}
	}
	cycles := value(g.samples[2])
	alloc := value(g.samples[3]) / 1024
	r[stage.RecGCCPUMS] = (cpu - g.lastCPU) * 1000
	r[stage.RecGCPauseMS] = (pause - g.lastPause) * 1000
	r[stage.RecGCCycles] = cycles - g.lastCycles
	r[stage.RecAllocKB] = alloc - g.lastAllocKiB
	r[stage.RecHeapMB] = value(g.samples[4]) / (1 << 20)
	g.lastCPU, g.lastPause, g.lastCycles, g.lastAllocKiB = cpu, pause, cycles, alloc
}

func value(s metrics.Sample) float64 {
	switch s.Value.Kind() {
	case metrics.KindUint64:
		return float64(s.Value.Uint64())
	case metrics.KindFloat64:
		return s.Value.Float64()
	}
	return 0
}

// marker reports whether KeyM is held: the input the browser test uses to
// measure input-to-display latency. In a worker it reads the page's forwarded
// keyboard; on the main thread it listens to the window.
type marker struct {
	forwarded *host.ForwardedInput
	down      bool
}

func newMarker(worker bool) *marker {
	m := &marker{}
	if worker {
		m.forwarded, _ = host.NewForwardedInput()
		return m
	}
	key := func(down bool) js.Func {
		return js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 && args[0].Get("code").String() == "KeyM" {
				m.down = down
			}
			return nil
		})
	}
	js.Global().Call("addEventListener", "keydown", key(true))
	js.Global().Call("addEventListener", "keyup", key(false))
	return m
}

func (m *marker) held() bool {
	if m.forwarded != nil {
		return m.forwarded.KeyDown("KeyM")
	}
	return m.down
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}

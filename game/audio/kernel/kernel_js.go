//go:build js && wasm

package kernel

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"syscall/js"
	"time"

	"m31labs.dev/gosx/client/jsutil"
	"m31labs.dev/gosx/game/audio"
	audiohost "m31labs.dev/gosx/game/audio/host"
	"m31labs.dev/gosx/game/audio/worklet"
)

// Config describes one kernel node.
type Config struct {
	// Mixer supplies the AudioContext and the bus the kernel plays on. Its host
	// must be the browser host (audio.HostJS).
	Mixer *audio.Mixer
	// Bus is the mixer bus the kernel output feeds. Default "music".
	Bus string
	// Backend is the kernel's record layout and capabilities.
	Backend worklet.Backend
	// WASM is the kernel module, a gosx_audio reactor.
	WASM []byte
	// Image is the kernel's data blob, for example a compiled score. Info is
	// the metadata the kernel's own tooling extracted from it.
	Image []byte
	Info  worklet.ImageInfo
	// ProcessorPrefix is the URL prefix where worklet.Handler is mounted, with
	// a trailing slash, for example "/assets/". The processor loads from
	// ProcessorPrefix + worklet.Path(). Required.
	ProcessorPrefix string
	// LatencyMS is the output latency the processor adds to its gap budget.
	LatencyMS float64
	// ReadyTimeout bounds initialization. Default 10 seconds.
	ReadyTimeout time.Duration
}

// Node is a running kernel.
type Node struct {
	host *audiohost.HostJS
	node *audiohost.WorkletNodeJS
	b    worklet.Backend

	mu        sync.Mutex
	state     worklet.State
	onRecords func(records []byte)
	onFault   func(code uint16)
	onTiming  func(worklet.Event)
	nextReq   int
	ready     chan struct{}
	failed    chan struct{}
}

// Start loads the processor module, compiles the kernel, creates the node,
// waits until the kernel has initialized, and connects it to the bus.
// Call Start from a goroutine and after a user gesture has resumed the context.
func Start(cfg Config) (*Node, error) {
	if cfg.Mixer == nil {
		return nil, errors.New("audio/kernel: Config.Mixer is required")
	}
	host, ok := cfg.Mixer.Host().(*audiohost.HostJS)
	if !ok {
		return nil, audiohost.ErrUnsupported
	}
	if cfg.ProcessorPrefix == "" {
		return nil, errors.New("audio/kernel: Config.ProcessorPrefix is required")
	}
	if err := cfg.Backend.Validate(); err != nil {
		return nil, err
	}
	if cfg.Bus == "" {
		cfg.Bus = audio.MusicBus
	}
	bus := cfg.Mixer.Bus(cfg.Bus)
	if bus == nil {
		return nil, fmt.Errorf("audio/kernel: mixer has no bus %q", cfg.Bus)
	}
	timeout := cfg.ReadyTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if err := host.AddWorkletModule(cfg.ProcessorPrefix + worklet.Path()); err != nil {
		return nil, err
	}
	module, err := host.CompileWASM(cfg.WASM)
	if err != nil {
		return nil, err
	}
	n := &Node{host: host, b: cfg.Backend, ready: make(chan struct{}), failed: make(chan struct{})}
	node, err := host.NewWASMWorklet(audiohost.WASMWorkletOptions{
		Module: module, Image: cfg.Image, Backend: cfg.Backend, Info: cfg.Info,
		ID: "kernel", LatencyMS: cfg.LatencyMS,
	})
	if err != nil {
		return nil, err
	}
	n.node = node
	node.OnMessage(n.onMessage)
	node.OnError(func(err error) { n.fail(err.Error()) })
	select {
	case <-n.ready:
	case <-n.failed:
		_ = node.Close()
		return nil, errors.New("audio/kernel: " + n.State().Error)
	case <-time.After(timeout):
		_ = node.Close()
		return nil, errors.New("audio/kernel: kernel did not initialize in time")
	}
	node.Connect(bus.Input())
	return n, nil
}

func (n *Node) fail(text string) {
	n.mu.Lock()
	n.state.Apply(worklet.Event{Type: worklet.MsgError, Err: text})
	n.mu.Unlock()
	select {
	case <-n.failed:
	default:
		close(n.failed)
	}
}

// decode turns a processor message into an Event. Binary payloads are copied
// out of the transferred buffer before the buffer goes back to the processor.
func decode(data js.Value) worklet.Event {
	ev := worklet.Event{Type: data.Get("t").String()}
	switch ev.Type {
	case worklet.MsgMessage:
		count := data.Get("n").Int()
		ev.Frames = data.Get("f").Float()
		if count > 0 {
			ev.Records = make([]byte, count)
			js.CopyBytesToGo(ev.Records, js.Global().Get("Uint8Array").New(data.Get("bytes"), 0, count))
		}
	case worklet.MsgState:
		ev.Playing = data.Get("p").Truthy()
	case worklet.MsgFault:
		ev.Fault = uint16(data.Get("a").Int())
	case worklet.MsgError, worklet.MsgReject:
		ev.Err = data.Get("e").String()
		if r := data.Get("r"); r.Type() == js.TypeString {
			ev.Request = r.String()
		}
	case worklet.MsgTiming:
		ev.Underruns = data.Get("u").Int()
		ev.MemoryBytes = data.Get("m").Int()
		ev.Frames = data.Get("f").Float()
	}
	return ev
}

func (n *Node) onMessage(data js.Value) {
	ev := decode(data)
	n.mu.Lock()
	giveBack := n.state.Apply(ev)
	records, fault, timing := n.onRecords, n.onFault, n.onTiming
	n.mu.Unlock()

	switch ev.Type {
	case worklet.MsgReady:
		close(n.ready)
	case worklet.MsgError:
		select {
		case <-n.failed:
		default:
			close(n.failed)
		}
	case worklet.MsgMessage:
		if giveBack {
			// The buffer must go back before the processor drains again.
			buf := data.Get("bytes")
			_ = n.node.PostMessage(map[string]any{"t": "b", "bytes": buf}, buf)
		}
		if records != nil && len(ev.Records) > 0 {
			records(ev.Records)
		}
	case worklet.MsgFault:
		if fault != nil {
			fault(ev.Fault)
		}
	case worklet.MsgTiming:
		if timing != nil {
			timing(ev)
		}
	}
}

// OnMessages sets the handler for engine message records. fn runs inside a
// JavaScript callback, so it must not block; copy what it needs. The records
// argument is a whole number of Backend.MessageBytes records.
func (n *Node) OnMessages(fn func(records []byte)) {
	n.mu.Lock()
	n.onRecords = fn
	n.mu.Unlock()
}

// OnFault sets the handler for reactor faults. Playback has stopped by then.
func (n *Node) OnFault(fn func(code uint16)) {
	n.mu.Lock()
	n.onFault = fn
	n.mu.Unlock()
}

// OnTiming sets the handler for RequestTiming replies.
func (n *Node) OnTiming(fn func(worklet.Event)) {
	n.mu.Lock()
	n.onTiming = fn
	n.mu.Unlock()
}

// Send posts a batch of command records, a whole number of Backend.CommandBytes
// records. The processor rejects the whole batch if any record is invalid.
func (n *Node) Send(batch []byte) error {
	if err := n.b.CheckCommands(batch); err != nil {
		return err
	}
	return n.node.PostMessage(map[string]any{"t": "c", "bytes": jsutil.NewUint8ArrayFromBytes(batch)})
}

// Replace swaps the data blob (for example a new score) without a new module.
// For a kernel with a Transport, the swap lands on the next bar line.
func (n *Node) Replace(image []byte, info worklet.ImageInfo) error {
	if err := n.b.ValidateImage(info); err != nil {
		return err
	}
	n.mu.Lock()
	n.nextReq++
	id := strconv.Itoa(n.nextReq)
	n.mu.Unlock()
	return n.node.PostMessage(map[string]any{
		"t": "i", "r": id,
		"i":    jsutil.NewUint8ArrayFromBytes(image).Get("buffer"),
		"info": map[string]any{"tempoMilli": info.TempoMilli, "requiredCapabilities": float64(info.RequiredCapabilities)},
	})
}

// RequestTiming asks the processor for its CPU-time histogram and underrun
// count; the reply goes to the OnTiming handler.
func (n *Node) RequestTiming() error {
	return n.node.PostMessage(map[string]any{"t": "q", "l": 0})
}

// ResetTiming clears the processor's CPU-time histogram.
func (n *Node) ResetTiming() error { return n.node.PostMessage(map[string]any{"t": "p"}) }

// State returns a copy of the node's state.
func (n *Node) State() worklet.State {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.state
	if s.Fault != nil {
		f := *s.Fault
		s.Fault = &f
	}
	return s
}

// WaitFor blocks until pred holds for the node's state, or ctx ends. Call it
// from a goroutine.
func (n *Node) WaitFor(ctx context.Context, pred func(worklet.State) bool) error {
	for {
		if pred(n.State()) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Close stops the kernel and disconnects the node.
func (n *Node) Close() error {
	n.mu.Lock()
	n.state.Closed = true
	n.mu.Unlock()
	return n.node.Close()
}

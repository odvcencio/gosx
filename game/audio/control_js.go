//go:build js && wasm

package audio

import (
	"errors"
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

func recoverAudio(operation string, err *error) {
	if recovered := recover(); recovered != nil {
		*err = fmt.Errorf("audio: %s: %v", operation, recovered)
	}
}

func audioNumber(value js.Value) float64 {
	if value.Type() == js.TypeNumber {
		return value.Float()
	}
	return 0
}

// await lets disposal unblock the Go caller even if a browser's pending fetch
// or decode has not finished. The promise retains its handlers until settling,
// so late resolution/rejection cannot call a released Go function or restart
// audio after Close. There is no goroutine waiting on that late completion.
func (h *HostJS) await(promise js.Value) (value js.Value, err error) {
	defer recoverAudio("await", &err)
	type result struct {
		value js.Value
		err   error
	}
	results := make(chan result, 1)
	var resolved, rejected js.Func
	settled := false
	finish := func(r result) {
		if settled {
			return
		}
		settled = true
		resolved.Release()
		rejected.Release()
		results <- r
	}
	resolved = js.FuncOf(func(_ js.Value, args []js.Value) any {
		value := js.Undefined()
		if len(args) > 0 {
			value = args[0]
		}
		finish(result{value: value})
		return nil
	})
	rejected = js.FuncOf(func(_ js.Value, args []js.Value) any {
		message := "unknown rejection"
		if len(args) > 0 {
			message = args[0].String()
			if args[0].Type() == js.TypeObject && !args[0].IsNull() {
				message = jsutil.Describe(args[0])
			}
		}
		finish(result{err: fmt.Errorf("audio: promise rejected: %s", message)})
		return nil
	})
	registered := false
	defer func() {
		if !registered && !settled {
			resolved.Release()
			rejected.Release()
		}
	}()
	promise.Call("then", resolved, rejected)
	registered = true
	select {
	case r := <-results:
		if h.closed {
			return js.Undefined(), ErrClosed
		}
		return r.value, r.err
	case <-h.closing:
		return js.Undefined(), ErrClosed
	}
}

func (h *HostJS) SampleRate() float64    { return audioNumber(h.ctx.Get("sampleRate")) }
func (h *HostJS) BaseLatency() float64   { return audioNumber(h.ctx.Get("baseLatency")) }
func (h *HostJS) OutputLatency() float64 { return audioNumber(h.ctx.Get("outputLatency")) }

// ResumeAndWait waits for the context to resume and reports promise rejection.
// Like DecodeAudioData and AddWorkletModule, call it from a goroutine rather
// than blocking a syscall/js event callback. Resume remains gesture-safe.
func (h *HostJS) ResumeAndWait() error { return h.transition("resume") }

// Suspend waits until the context is suspended. Call it from a goroutine.
func (h *HostJS) Suspend() error { return h.transition("suspend") }

func (h *HostJS) transition(method string) (err error) {
	defer recoverAudio(method, &err)
	if h.closed {
		return ErrClosed
	}
	_, err = h.await(h.ctx.Call(method))
	if err == nil && h.closed {
		return ErrClosed
	}
	return err
}

// Close immediately disables owned worklet ports and releases their handlers,
// then waits for the context to close. It is idempotent. Call from a goroutine.
func (h *HostJS) Close() (err error) {
	defer recoverAudio("close", &err)
	if h.closed {
		return nil
	}
	h.closed = true
	close(h.closing)
	for node := range h.worklets {
		_ = node.Close()
	}
	for source := range h.sources {
		source.clearEnded()
		source.Disconnect()
		delete(h.sources, source)
	}
	if h.ctx.Get("state").String() == "closed" {
		return nil
	}
	_, err = jsutil.AwaitPromise(h.ctx.Call("close"))
	return err
}

// BufferDuration returns decoded buffer seconds, or zero for an invalid handle.
func BufferDuration(buffer Buffer) float64 {
	if value, ok := buffer.(js.Value); ok && value.Type() == js.TypeObject && !value.IsNull() {
		return audioNumber(value.Get("duration"))
	}
	return 0
}

func (n *sourceNodeJS) StartRegion(at, offset, duration float64) (err error) {
	defer recoverAudio("start region", &err)
	if n.host.closed {
		return ErrClosed
	}
	if !nonnegative(at, offset, duration) {
		return errors.New("audio: invalid source region")
	}
	if duration == 0 {
		n.value.Call("start", at, offset)
	} else {
		n.value.Call("start", at, offset, duration)
	}
	return nil
}

func (n *sourceNodeJS) SetLoopRegion(start, end float64) (err error) {
	defer recoverAudio("loop region", &err)
	if n.host.closed {
		return ErrClosed
	}
	if !nonnegative(start, end) || end <= start {
		return errors.New("audio: invalid loop region")
	}
	n.value.Set("loopStart", start)
	n.value.Set("loopEnd", end)
	return nil
}

func (n *gainNodeJS) SetGainTarget(value, at, timeConstant float64) (err error) {
	defer recoverAudio("gain target", &err)
	if !nonnegative(value, at, timeConstant) {
		return errors.New("audio: invalid gain automation")
	}
	param := n.value.Get("gain")
	param.Call("cancelScheduledValues", at)
	if timeConstant == 0 {
		param.Call("setValueAtTime", value, at)
	} else {
		param.Call("setTargetAtTime", value, at, timeConstant)
	}
	return nil
}

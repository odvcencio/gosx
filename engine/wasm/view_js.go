//go:build js && wasm

package wasm

import (
	"errors"
	"fmt"
	"sync"
	"syscall/js"
)

// jsViewSink sends batches through the engine context. On a worker the
// context posts the batch with a sequence number and calls back once the page
// acknowledges that number. On the main thread it applies the batch at once.
type jsViewSink struct {
	ctx      Context
	mu       sync.Mutex
	next     uint64
	waiting  map[uint64]func(bool)
	callback js.Func
	hasCB    bool
}

func (c Context) viewSink() viewSink { return &jsViewSink{ctx: c, waiting: map[uint64]func(bool){}} }

func (s *jsViewSink) send(batch []byte, done func(bool)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("send view batch: %v", r)
		}
	}()
	if s.ctx.value.Get("sendView").Type() != js.TypeFunction {
		result := s.ctx.value.Call("setSignalsJSON", string(batch), true)
		if result.Type() == js.TypeString && result.String() != "" {
			return errors.New(result.String())
		}
		done(true)
		return nil
	}
	s.mu.Lock()
	if !s.hasCB {
		s.callback = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			token := uint64(args[0].Float())
			s.mu.Lock()
			done := s.waiting[token]
			delete(s.waiting, token)
			s.mu.Unlock()
			if done != nil {
				// The page answers (token, ok); ok is false after it rejected the batch.
				done(len(args) < 2 || args[1].Type() != js.TypeBoolean || args[1].Bool())
			}
			return nil
		})
		s.hasCB = true
	}
	s.next++
	token := s.next
	s.waiting[token] = done
	callback := s.callback
	s.mu.Unlock()
	result := s.ctx.value.Call("sendView", string(batch), true, float64(token), callback)
	if result.Type() == js.TypeString && result.String() != "" {
		s.mu.Lock()
		delete(s.waiting, token)
		s.mu.Unlock()
		return errors.New(result.String())
	}
	return nil
}

func (s *jsViewSink) hidden() bool {
	if port, ok := s.ctx.Port(); ok {
		return port.Hidden()
	}
	doc := js.Global().Get("document")
	return doc.Truthy() && doc.Get("hidden").Truthy()
}

func (s *jsViewSink) onVisibility(fn func()) HandleFunc {
	if port, ok := s.ctx.Port(); ok {
		return port.OnVisibility(func(bool) { fn() })
	}
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return func() {}
	}
	callback := js.FuncOf(func(js.Value, []js.Value) any { fn(); return nil })
	doc.Call("addEventListener", "visibilitychange", callback)
	return func() {
		doc.Call("removeEventListener", "visibilitychange", callback)
		callback.Release()
	}
}

func (s *jsViewSink) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasCB {
		s.callback.Release()
		s.hasCB = false
	}
}

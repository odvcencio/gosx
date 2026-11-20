//go:build js && wasm

// Command padpage is the page the Chrome check drives. It reads pads through
// host.GamepadReader and actions.PadPoller on the engine's frame dispatcher
// (host.FrameSourceJS), the way a game does, and writes what it saw into
// document.title: the frame count, the connected slots and the held buttons.
// It holds no pad logic of its own.
package main

import (
	"fmt"
	"sort"
	"strings"
	"syscall/js"

	"m31labs.dev/gosx/game/actions"
	"m31labs.dev/gosx/game/host"
)

type heldSink struct {
	frame int
	held  map[string]float64
	log   []string
}

func (s *heldSink) Feed(e actions.Event) {
	key := e.Device + "/" + e.Source.Code
	if e.Value != 0 {
		s.held[key] = e.Value
	} else {
		delete(s.held, key)
	}
	s.log = append(s.log, fmt.Sprintf("%d:%s=%g", s.frame, key, e.Value))
	if len(s.log) > 12 {
		s.log = s.log[1:]
	}
}

func (s *heldSink) ReleaseAll() { s.held = map[string]float64{} }

func main() {
	reader := host.NewGamepadReader()
	poller := actions.NewPadPoller()
	sink := &heldSink{held: map[string]float64{}}
	frames := host.NewFrameSourceJS()
	doc := js.Global().Get("document")
	last := ""
	var step func(float64)
	step = func(float64) {
		sink.frame++
		poller.Poll(reader, sink)
		var slots, held []string
		for _, p := range reader.Gamepads() {
			slots = append(slots, fmt.Sprint(p.Index))
		}
		for k := range sink.held {
			held = append(held, k)
		}
		sort.Strings(held)
		title := fmt.Sprintf("pads=[%s] held=[%s] log=[%s]", strings.Join(slots, ","), strings.Join(held, ","), strings.Join(sink.log, ","))
		if title != last {
			last = title
			doc.Set("title", title)
		}
		doc.Get("documentElement").Call("setAttribute", "data-frames", fmt.Sprint(sink.frame))
		frames.RequestFrame(step)
	}
	frames.RequestFrame(step)
	select {}
}

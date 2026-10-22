//go:build !js || !wasm

package wasm

import (
	"encoding/json"
	"errors"

	"m31labs.dev/gosx/engine"
)

// Thread is always the main thread on native targets.
func (Context) Thread() engine.Thread { return engine.ThreadMain }

// ThreadFallbackReason is empty on native targets.
func (Context) ThreadFallbackReason() string { return "" }

// Port is unavailable on native targets.
type Port struct{}

// Message is one page-to-engine application message.
type Message struct {
	Name  string
	JSON  json.RawMessage
	Bytes []byte
}

// ErrNoPort reports use of Port outside a worker-thread engine.
var ErrNoPort = errors.New("gosx engine/wasm: the engine port exists only in a worker-thread engine")

// Port returns ok false on native targets.
func (Context) Port() (Port, bool) { return Port{}, false }

func (Port) Post(string, any) error             { return ErrUnsupported }
func (Port) PostBytes(string, []byte) error     { return ErrUnsupported }
func (Port) OnMessage(func(Message)) HandleFunc { return func() {} }
func (Port) Hidden() bool                       { return false }
func (Port) OnVisibility(func(bool)) HandleFunc { return func() {} }
func (Port) SetFrameRate(float64) error         { return ErrUnsupported }
func (Port) Now() float64                       { return 0 }

// FrameStats reports the worker clock's measured cadence.
type FrameStats struct {
	Hz           float64 `json:"hz"`
	Frames       int     `json:"frames"`
	LateMaxMS    float64 `json:"lateMaxMs"`
	GapMaxMS     float64 `json:"gapMaxMs"`
	GapAvgMS     float64 `json:"gapAvgMs"`
	SkippedSlots int     `json:"skippedSlots"`
}

func (Port) FrameStats() FrameStats { return FrameStats{} }

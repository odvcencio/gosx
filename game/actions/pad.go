package actions

import (
	"math"
	"strconv"

	"m31labs.dev/gosx/game/host"
)

const maxPadInputs = 32

var padButtonCodes, padAxisCodes [maxPadInputs]string

func init() {
	for i := range padButtonCodes {
		padButtonCodes[i] = "button" + strconv.Itoa(i)
		padAxisCodes[i] = "axis" + strconv.Itoa(i)
	}
}

// PadPoller reads a host.GamepadSource and feeds changes into a Sink, one
// device per pad ("pad:0", "pad:1"). Stick pairs get a radial deadzone, so a
// resting stick reads exactly 0. After its buffers are warm, Poll does not
// allocate when nothing changes.
type PadPoller struct {
	// Deadzone is the radial stick deadzone in [0,1). Zero selects 0.15.
	Deadzone float64
	// AllowNonStandard includes pads whose mapping is not "standard".
	AllowNonStandard bool

	pads map[int]*padMemory
	seen map[int]bool
}

type padMemory struct {
	id      string
	device  string
	buttons []float64
	axes    []float64
	scratch []float64
}

// NewPadPoller creates a poller with the default 0.15 deadzone.
func NewPadPoller() *PadPoller {
	return &PadPoller{pads: map[int]*padMemory{}, seen: map[int]bool{}}
}

const padEpsilon = 1e-3

// Poll reads source once and feeds every changed value to sink. Pads that
// disappear get zero events for everything they held.
func (p *PadPoller) Poll(source host.GamepadSource, sink Sink) {
	if source == nil || sink == nil {
		return
	}
	dz := p.Deadzone
	if dz <= 0 {
		dz = 0.15
	}
	for k := range p.seen {
		delete(p.seen, k)
	}
	for _, pad := range source.Gamepads() {
		if !p.AllowNonStandard && pad.Mapping != "standard" {
			continue
		}
		p.seen[pad.Index] = true
		mem := p.pads[pad.Index]
		if mem == nil {
			mem = &padMemory{device: PadDevice(pad.Index)}
			p.pads[pad.Index] = mem
		}
		device := mem.device
		if mem.id != pad.ID {
			p.release(device, mem, sink)
			mem.id = pad.ID
		}
		mem.buttons = p.diff(device, KindPadButton, padButtonCodes[:], mem.buttons, pad.Buttons, sink)
		mem.scratch = append(mem.scratch[:0], pad.Axes...)
		for i := 0; i+1 < len(mem.scratch); i += 2 {
			mem.scratch[i], mem.scratch[i+1] = radial(mem.scratch[i], mem.scratch[i+1], dz)
		}
		if n := len(mem.scratch); n%2 == 1 {
			mem.scratch[n-1] = applyDeadzone(mem.scratch[n-1], dz)
		}
		mem.axes = p.diff(device, KindPadAxis, padAxisCodes[:], mem.axes, mem.scratch, sink)
	}
	for idx, mem := range p.pads {
		if !p.seen[idx] {
			p.release(mem.device, mem, sink)
			delete(p.pads, idx)
		}
	}
}

func (p *PadPoller) release(device string, mem *padMemory, sink Sink) {
	for i, v := range mem.buttons {
		if v != 0 && i < maxPadInputs {
			sink.Feed(Event{Device: device, Source: Source{KindPadButton, padButtonCodes[i]}})
		}
	}
	for i, v := range mem.axes {
		if v != 0 && i < maxPadInputs {
			sink.Feed(Event{Device: device, Source: Source{KindPadAxis, padAxisCodes[i]}})
		}
	}
	mem.buttons, mem.axes = mem.buttons[:0], mem.axes[:0]
}

func (p *PadPoller) diff(device string, kind Kind, codes []string, prev []float64, next []float64, sink Sink) []float64 {
	for i, v := range next {
		if i >= maxPadInputs {
			break
		}
		old := 0.0
		if i < len(prev) {
			old = prev[i]
		}
		if math.Abs(v-old) >= padEpsilon {
			sink.Feed(Event{Device: device, Source: Source{kind, codes[i]}, Value: v})
		}
	}
	return append(prev[:0], next...)
}

func radial(x, y, dz float64) (float64, float64) {
	mag := math.Hypot(x, y)
	if mag < dz {
		return 0, 0
	}
	if mag > 1 {
		mag = 1
	}
	s := (mag - dz) / (1 - dz) / mag
	return x * s, y * s
}

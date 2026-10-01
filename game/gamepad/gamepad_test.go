package gamepad

import (
	"math"
	"testing"

	"m31labs.dev/gosx/game"
)

type fakeSource struct {
	pads []State
}

func (f fakeSource) Gamepads() []State { return f.pads }

func standardPad(index int, id string, buttons, axes []float64) State {
	return State{Index: index, ID: id, Mapping: "standard", Buttons: buttons, Axes: axes}
}

func TestPollAppliesButtonPressAsBoundAction(t *testing.T) {
	input := game.NewInput(game.Button("jump", "button0"))
	poller := NewPoller(Config{})

	src := fakeSource{pads: []State{standardPad(0, "pad-1", []float64{1}, nil)}}
	poller.Poll(src, input)
	if !input.Down("jump") || !input.Pressed("jump") {
		t.Fatalf("expected jump down+pressed, got %+v", input.Action("jump"))
	}

	input.EndFrame()
	// Held steady: no change, so Pressed should not re-trigger.
	poller.Poll(src, input)
	if !input.Down("jump") || input.Pressed("jump") {
		t.Fatalf("expected jump held without re-pressing, got %+v", input.Action("jump"))
	}

	input.EndFrame()
	src.pads[0].Buttons = []float64{0}
	poller.Poll(src, input)
	if input.Down("jump") || !input.Released("jump") {
		t.Fatalf("expected jump released, got %+v", input.Action("jump"))
	}
}

func TestPollSkipsUnchangedValuesBelowEpsilon(t *testing.T) {
	input := game.NewInput(game.Button("jump", "button0"))
	poller := NewPoller(Config{})
	src := fakeSource{pads: []State{standardPad(0, "pad-1", []float64{0.5}, nil)}}
	poller.Poll(src, input)
	input.EndFrame()

	// A change smaller than changeEpsilon must not re-fire the binding.
	src.pads[0].Buttons = []float64{0.5 + changeEpsilon/2}
	poller.Poll(src, input)
	if len(input.Events()) != 0 {
		t.Fatalf("expected no events for a sub-epsilon change, got %v", input.Events())
	}
}

func TestPollAppliesRadialDeadzoneToStickPair(t *testing.T) {
	input := game.NewInput(game.Axis("move.x", "axis0", 1), game.Axis("move.y", "axis1", 1))
	poller := NewPoller(Config{Deadzone: 0.2})

	// Inside the deadzone: both axes should read as exactly zero, so no
	// event should fire at all past the initial zero baseline.
	src := fakeSource{pads: []State{standardPad(0, "pad-1", nil, []float64{0.1, 0.1})}}
	poller.Poll(src, input)
	if len(input.Events()) != 0 {
		t.Fatalf("expected no events inside the deadzone, got %v", input.Events())
	}

	input.EndFrame()
	src.pads[0].Axes = []float64{1, 0}
	poller.Poll(src, input)
	action := input.Action("move.x")
	if !action.Down || action.Value <= 0.99 {
		t.Fatalf("expected move.x near full deflection, got %+v", action)
	}
	if v := input.Action("move.y").Value; v != 0 {
		t.Fatalf("expected move.y to stay at 0, got %v", v)
	}
}

func TestPollRampsSmoothlyAcrossDeadzoneBoundary(t *testing.T) {
	x, y := applyRadialDeadzone(0.15, 0, 0.15)
	if x != 0 || y != 0 {
		t.Fatalf("expected exactly zero at the deadzone boundary, got (%v,%v)", x, y)
	}
	x, _ = applyRadialDeadzone(1, 0, 0.15)
	if math.Abs(x-1) > 1e-9 {
		t.Fatalf("expected full deflection to remain 1, got %v", x)
	}
}

func TestPollIgnoresNonStandardMappingByDefault(t *testing.T) {
	input := game.NewInput(game.Button("jump", "button0"))
	poller := NewPoller(Config{})
	pad := standardPad(0, "weird-pad", []float64{1}, nil)
	pad.Mapping = ""
	poller.Poll(fakeSource{pads: []State{pad}}, input)
	if input.Down("jump") {
		t.Fatal("expected non-standard mapping to be ignored")
	}
}

func TestPollAllowNonStandardOptIn(t *testing.T) {
	input := game.NewInput(game.Button("jump", "button0"))
	poller := NewPoller(Config{AllowNonStandard: true})
	pad := standardPad(0, "weird-pad", []float64{1}, nil)
	pad.Mapping = ""
	poller.Poll(fakeSource{pads: []State{pad}}, input)
	if !input.Down("jump") {
		t.Fatal("expected non-standard mapping to be honored with AllowNonStandard")
	}
}

func TestPollResetsEdgeStateOnGamepadSwap(t *testing.T) {
	input := game.NewInput(game.Button("jump", "button0"))
	poller := NewPoller(Config{})
	src := fakeSource{pads: []State{standardPad(0, "pad-a", []float64{1}, nil)}}
	poller.Poll(src, input)
	input.EndFrame()

	// A different pad takes slot 0. Its resting value happens to be 0, which
	// must not be reported as a "release" edge left over from pad-a.
	src.pads[0] = standardPad(0, "pad-b", []float64{0}, nil)
	poller.Poll(src, input)
	if len(input.Events()) != 0 {
		t.Fatalf("expected no spurious release on gamepad swap, got %v", input.Events())
	}
}

func TestPollDropsDisconnectedSlotState(t *testing.T) {
	input := game.NewInput(game.Button("jump", "button0"))
	poller := NewPoller(Config{})
	poller.Poll(fakeSource{pads: []State{standardPad(0, "pad-a", []float64{1}, nil)}}, input)
	if len(poller.buttons) != 1 {
		t.Fatalf("expected tracked state for one pad, got %d", len(poller.buttons))
	}
	poller.Poll(fakeSource{pads: nil}, input)
	if len(poller.buttons) != 0 || len(poller.connectedID) != 0 {
		t.Fatalf("expected disconnect to clear tracked state, got buttons=%d ids=%d", len(poller.buttons), len(poller.connectedID))
	}
}

func TestPollStampsConfiguredPlayerID(t *testing.T) {
	input := game.NewInput(game.Button("jump", "button0"))
	poller := NewPoller(Config{PlayerID: "p1"})
	poller.Poll(fakeSource{pads: []State{standardPad(0, "pad-a", []float64{1}, nil)}}, input)
	if got := input.Action("jump").Source; got != "button0" {
		t.Fatalf("action source = %q, want button0", got)
	}
	events := input.Events()
	if len(events) != 1 || events[0].PlayerID != "p1" {
		t.Fatalf("expected event stamped with player p1, got %+v", events)
	}
}

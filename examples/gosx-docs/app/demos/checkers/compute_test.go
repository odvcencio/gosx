package checkers

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestComputeHintFieldIsBoundedDeterministicAndDestinationOnly(t *testing.T) {
	step := Move{From: 5, Len: 1, Kind: Step}
	step.Landings[0] = 10
	hop := Move{From: 7, Len: 3, Kind: Hop}
	hop.Landings[0], hop.Landings[1], hop.Landings[2] = 11, 12, 13
	field := ComputeHintField([]Move{step, hop})
	if field != ComputeHintField([]Move{step, hop}) {
		t.Fatal("hint field is nondeterministic")
	}
	if math.Abs(float64(field[13]-1)) > 1e-6 || field[10] <= 0 || field[10] >= field[13] {
		t.Fatalf("unexpected destination weights: step=%v hop=%v", field[10], field[13])
	}
	if field[5] != 0 || field[7] != 0 || field[11] != 0 || field[12] != 0 {
		t.Fatalf("non-destination holes received authority-like hints: %+v", field)
	}
	if err := validateHintField(field); err != nil {
		t.Fatal(err)
	}
}

func TestComputeHintFieldAllocatesNothing(t *testing.T) {
	moves := hintProbeMoves()
	if allocations := testing.AllocsPerRun(1000, func() { _ = ComputeHintField(moves) }); allocations != 0 {
		t.Fatalf("CPU hint field allocations = %v, want 0", allocations)
	}
}

func BenchmarkComputeHintField121(b *testing.B) {
	state, err := NewMatch(0, 3)
	if err != nil {
		b.Fatal(err)
	}
	moves := GenerateMoves(nil, state, state.Active)
	b.ReportAllocs()
	for b.Loop() {
		_ = ComputeHintField(moves)
	}
}

func TestElioRequiresOptInCapabilitiesInitializationAndParity(t *testing.T) {
	computer := NewHintComputer()
	adapter := &fakeElioHintAdapter{}
	for name, caps := range map[string]HintRuntimeCapabilities{
		"not-opted-in":     {WebGPU: true, ExternalComputePass: true},
		"no-webgpu":        {OptIn: true, ExternalComputePass: true},
		"no-external-pass": {OptIn: true, WebGPU: true},
	} {
		t.Run(name, func(t *testing.T) {
			status := computer.Initialize(context.Background(), caps, adapter)
			if status.Active || status.Label != "Go CPU hints" || status.Backend != "go-cpu" {
				t.Fatalf("dishonest status: %+v", status)
			}
		})
	}
	status := computer.Initialize(context.Background(), HintRuntimeCapabilities{OptIn: true, WebGPU: true, ExternalComputePass: true}, adapter)
	if !status.Active || status.Label != "Elio GPU hints" || status.Backend != "elio-webgpu" {
		t.Fatalf("initialized status: %+v", status)
	}
	if adapter.descriptor != ElioHintDescriptor() {
		t.Fatalf("descriptor = %+v", adapter.descriptor)
	}
}

func TestElioMismatchFallsBackToCanonicalCPUField(t *testing.T) {
	adapter := &fakeElioHintAdapter{}
	computer := NewHintComputer()
	if status := computer.Initialize(context.Background(), HintRuntimeCapabilities{OptIn: true, WebGPU: true, ExternalComputePass: true}, adapter); !status.Active {
		t.Fatalf("init: %+v", status)
	}
	move := Move{From: 1, Len: 1, Kind: Step}
	move.Landings[0] = 2
	adapter.corrupt = true
	got := computer.Compute(context.Background(), []Move{move})
	want := ComputeHintField([]Move{move})
	if got != want {
		t.Fatalf("fallback differs from CPU: got=%v want=%v", got[2], want[2])
	}
	status := computer.Status()
	if status.Active || status.Label != "Go CPU hints" || status.Reason == "" {
		t.Fatalf("fallback status: %+v", status)
	}
}

func TestElioInitializationFailureNeverClaimsActive(t *testing.T) {
	computer := NewHintComputer()
	adapter := &fakeElioHintAdapter{initializeErr: errors.New("pipeline validation failed")}
	status := computer.Initialize(context.Background(), HintRuntimeCapabilities{OptIn: true, WebGPU: true, ExternalComputePass: true}, adapter)
	if status.Active || status.Label != "Go CPU hints" || status.Reason != "pipeline validation failed" {
		t.Fatalf("status = %+v", status)
	}
}

type fakeElioHintAdapter struct {
	descriptor    HintComputeDescriptor
	initializeErr error
	corrupt       bool
}

func (f *fakeElioHintAdapter) Initialize(_ context.Context, descriptor HintComputeDescriptor) (ElioInitialization, error) {
	f.descriptor = descriptor
	if f.initializeErr != nil {
		return ElioInitialization{}, f.initializeErr
	}
	return ElioInitialization{PassInitialized: true, Backend: "webgpu", Kernel: "fixture.elio.wgsl"}, nil
}

func (f *fakeElioHintAdapter) Dispatch(_ context.Context, legal []Move) (HintField, error) {
	field := ComputeHintField(legal)
	if f.corrupt {
		field[0] = float32(math.NaN())
	}
	return field, nil
}

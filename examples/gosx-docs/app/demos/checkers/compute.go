package checkers

import (
	"context"
	"fmt"
	"math"
	"sync"
)

const HintFieldVersion = "checkers.hint-field.v1"

type HintField [HoleCount]float32

// ComputeHintField is the canonical, allocation-free CPU implementation. It
// consumes already-legal moves and cannot affect legality or move selection.
func ComputeHintField(legal []Move) HintField {
	var field HintField
	var max float32
	for i := range legal {
		move := legal[i]
		to := move.To()
		if int(to) >= HoleCount || move.Len == 0 {
			continue
		}
		value := float32(1) + float32(move.Len-1)*0.12
		if move.Kind == Hop {
			value += 0.25
		}
		field[to] += value
		if field[to] > max {
			max = field[to]
		}
	}
	if max > 0 {
		inverse := float32(1) / max
		for i := range field {
			field[i] *= inverse
		}
	}
	return field
}

type HintComputeDescriptor struct {
	ID              string
	Version         string
	HoleCount       int
	OutputBytes     int
	WorkgroupSize   int
	RequiredBackend string
	Operation       string
}

func ElioHintDescriptor() HintComputeDescriptor {
	return HintComputeDescriptor{
		ID: "checkers-legal-hints", Version: HintFieldVersion, HoleCount: HoleCount,
		OutputBytes: HoleCount * 4, WorkgroupSize: 64, RequiredBackend: "webgpu",
		Operation: "normalize weighted legal destinations; no rule or search authority",
	}
}

type HintRuntimeCapabilities struct {
	OptIn               bool
	WebGPU              bool
	ExternalComputePass bool
}

type ElioInitialization struct {
	PassInitialized bool
	Backend         string
	Kernel          string
}

// ElioHintAdapter is implemented by a host that actually links Elio and owns
// the WebGPU external-compute pass. Initialization alone is insufficient: the
// engine also verifies a probe dispatch against ComputeHintField.
type ElioHintAdapter interface {
	Initialize(context.Context, HintComputeDescriptor) (ElioInitialization, error)
	Dispatch(context.Context, []Move) (HintField, error)
}

type HintStatus struct {
	Active  bool
	Backend string
	Label   string
	Reason  string
	Kernel  string
}

type HintComputer struct {
	mu      sync.RWMutex
	adapter ElioHintAdapter
	status  HintStatus
}

func NewHintComputer() *HintComputer {
	return &HintComputer{status: cpuHintStatus("Elio GPU hints not initialized")}
}

func (h *HintComputer) Initialize(ctx context.Context, caps HintRuntimeCapabilities, adapter ElioHintAdapter) HintStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.adapter = nil
	if !caps.OptIn {
		h.status = cpuHintStatus("Elio GPU hints are opt-in")
		return h.status
	}
	if !caps.WebGPU || !caps.ExternalComputePass {
		h.status = cpuHintStatus("WebGPU external compute pass unavailable")
		return h.status
	}
	if adapter == nil {
		h.status = cpuHintStatus("Elio adapter unavailable")
		return h.status
	}
	initialized, err := adapter.Initialize(ctx, ElioHintDescriptor())
	if err != nil || !initialized.PassInitialized || initialized.Backend != "webgpu" {
		reason := "Elio pass did not initialize"
		if err != nil {
			reason = err.Error()
		}
		h.status = cpuHintStatus(reason)
		return h.status
	}
	probe := hintProbeMoves()
	got, err := adapter.Dispatch(ctx, probe)
	if err != nil || validateHintField(got) != nil || !hintFieldsNear(got, ComputeHintField(probe), 1e-5) {
		h.status = cpuHintStatus("Elio probe did not match Go CPU hints")
		return h.status
	}
	h.adapter = adapter
	h.status = HintStatus{Active: true, Backend: "elio-webgpu", Label: "Elio GPU hints", Kernel: initialized.Kernel}
	return h.status
}

func (h *HintComputer) Compute(ctx context.Context, legal []Move) HintField {
	reference := ComputeHintField(legal)
	h.mu.RLock()
	adapter, active := h.adapter, h.status.Active
	h.mu.RUnlock()
	if !active || adapter == nil {
		return reference
	}
	got, err := adapter.Dispatch(ctx, legal)
	if err == nil && validateHintField(got) == nil && hintFieldsNear(got, reference, 1e-5) {
		return got
	}
	h.mu.Lock()
	h.adapter = nil
	h.status = cpuHintStatus("Elio output failed CPU parity validation")
	h.mu.Unlock()
	return reference
}

func (h *HintComputer) Status() HintStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.status
}

func cpuHintStatus(reason string) HintStatus {
	return HintStatus{Backend: "go-cpu", Label: "Go CPU hints", Reason: reason}
}

func validateHintField(field HintField) error {
	for i, value := range field {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < 0 || value > 1 {
			return fmt.Errorf("hint[%d]=%v outside [0,1]", i, value)
		}
	}
	return nil
}

func hintFieldsNear(a, b HintField, tolerance float32) bool {
	for i := range a {
		if float32(math.Abs(float64(a[i]-b[i]))) > tolerance {
			return false
		}
	}
	return true
}

func hintProbeMoves() []Move {
	step := Move{From: 0, Len: 1, Kind: Step}
	step.Landings[0] = 1
	hop := Move{From: 2, Len: 2, Kind: Hop}
	hop.Landings[0], hop.Landings[1] = 3, 4
	return []Move{step, hop}
}

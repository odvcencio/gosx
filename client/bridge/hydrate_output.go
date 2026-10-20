package bridge

import (
	"fmt"

	rootengine "m31labs.dev/gosx/engine"
)

const (
	HydrateEnvelopeVersion       uint8 = 1
	HydrateOutputScene3DCommands       = "scene3d.commands"
	HydrateModeInitial                 = "initial"
)

// HydrateEnvelope is the versioned output of a generic hydration call that
// must be consumed by the browser before it publishes a mounted surface.
type HydrateEnvelope struct {
	Version     uint8                `json:"version"`
	SurfaceKind string               `json:"surfaceKind"`
	OutputKind  string               `json:"outputKind"`
	TargetID    string               `json:"targetId"`
	Mode        string               `json:"mode"`
	Commands    []rootengine.Command `json:"commands"`
}

// HydrateReconcilerOutput is the output-bearing generic hydration entry point.
// DOM and Canvas2D preserve their existing behavior and return no envelope.
func (b *Bridge) HydrateReconcilerOutput(surfaceKind, id, componentName, propsJSON string, programData []byte, format string) (*HydrateEnvelope, error) {
	switch surfaceKind {
	case SurfaceKindDOM:
		return nil, b.HydrateIsland(id, componentName, propsJSON, programData, format)
	case SurfaceKindScene3D:
		return b.hydrateScene3DOutput(id, componentName, propsJSON, programData, format)
	case SurfaceKindCanvas2D:
		return nil, b.hydrateCanvas2D(id, componentName, propsJSON, programData, format)
	default:
		return nil, unknownSurfaceKindError(surfaceKind)
	}
}

func unknownSurfaceKindError(surfaceKind string) error {
	return fmt.Errorf("unknown surfaceKind %q (expected one of: %q, %q, %q)",
		surfaceKind, SurfaceKindDOM, SurfaceKindScene3D, SurfaceKindCanvas2D)
}

//go:build !gosx_tiny_islands_only

package bridge

import rootengine "m31labs.dev/gosx/engine"

// hydrateScene3DOutput preserves the initial command stream produced by the
// engine adapter. Build-tag-paired with bridge_reconciler_islands.go.
func (b *Bridge) hydrateScene3DOutput(id, componentName, propsJSON string, programData []byte, format string) (*HydrateEnvelope, error) {
	commands, err := b.HydrateEngine(id, componentName, propsJSON, programData, format)
	if err != nil {
		return nil, err
	}
	if commands == nil {
		commands = make([]rootengine.Command, 0)
	}
	return &HydrateEnvelope{
		Version:     HydrateEnvelopeVersion,
		SurfaceKind: SurfaceKindScene3D,
		OutputKind:  HydrateOutputScene3DCommands,
		TargetID:    id,
		Mode:        HydrateModeInitial,
		Commands:    commands,
	}, nil
}

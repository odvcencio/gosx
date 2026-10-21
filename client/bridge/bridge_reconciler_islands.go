//go:build gosx_tiny_islands_only || gosx_runtime_core || gosx_runtime_collab

package bridge

import "errors"

// hydrateScene3DOutput is the islands-only stub: scene3d is unavailable in builds
// that strip the engine reconciler. Build-tag-paired with
// bridge_reconciler_full.go.
func (b *Bridge) hydrateScene3DOutput(id, componentName, propsJSON string, programData []byte, format string) (*HydrateEnvelope, error) {
	return nil, errors.New("scene3d surface unavailable in islands-only build")
}

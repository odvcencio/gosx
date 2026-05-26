//go:build gosx_tiny_islands_only

package bridge

import "fmt"

// hydrateScene3D is the islands-only stub: scene3d is unavailable in builds
// that strip the engine reconciler. Build-tag-paired with
// bridge_reconciler_full.go.
func (b *Bridge) hydrateScene3D(id, componentName, propsJSON string, programData []byte, format string) error {
	return fmt.Errorf("scene3d surface unavailable in islands-only build")
}

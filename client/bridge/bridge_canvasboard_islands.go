//go:build gosx_tiny_islands_only

package bridge

import "fmt"

// hydrateCanvas2D is the islands-only stub. canvas2d is unavailable in WASM
// builds that strip the engine reconciler. Build-tag-paired with
// bridge_canvasboard_full.go.
func (b *Bridge) hydrateCanvas2D(id, componentName, propsJSON string, programData []byte, format string) error {
	return fmt.Errorf("canvas2d surface unavailable in islands-only build")
}

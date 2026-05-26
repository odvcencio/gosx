//go:build !gosx_tiny_islands_only

package bridge

// hydrateScene3D routes a scene3d surface through HydrateEngine and discards
// the returned command stream. Build-tag-paired with bridge_reconciler_islands.go
// — the tiny islands-only build provides a stub that returns an error so the
// linker can drop the scene reconciler entirely.
func (b *Bridge) hydrateScene3D(id, componentName, propsJSON string, programData []byte, format string) error {
	_, err := b.HydrateEngine(id, componentName, propsJSON, programData, format)
	return err
}

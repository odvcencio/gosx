package scene

import "m31labs.dev/gosx/physics"

// ColliderConfig returns a static physics heightfield of the terrain's
// surface: the same samples, the same world placement and the same triangle
// split as HeightAt and the finest render level. Add it with
// physics.World.AddCollider; bodies then rest on the ground the player sees,
// and World.Raycast hits it.
func (t *Terrain) ColliderConfig() (physics.ColliderConfig, error) {
	if err := t.Validate(); err != nil {
		return physics.ColliderConfig{}, err
	}
	f, _ := t.frame()
	h := f.heightmap
	heights := make([]float64, len(h.Data))
	for i, value := range h.Data {
		heights[i] = f.baseY + float64(float64(value)/65535*f.scale)
	}
	return physics.ColliderConfig{
		Shape:   physics.ShapeHeightfield,
		Offset:  physics.Vec3{X: f.minX + f.sizeX/2, Z: f.minZ + f.sizeZ/2},
		Heights: heights,
		Columns: h.Columns,
		Rows:    h.Rows,
		Width:   f.sizeX,
		Depth:   f.sizeZ,
	}, nil
}

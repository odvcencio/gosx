package scene

import "m31labs.dev/gosx/det"

// InstanceBatch streams one InstancedMesh's instances each frame: it keeps
// the frame's data, the batch's revision and its StreamWriter, so a game
// places its instances (Place, Color) and calls Send once per frame.
//
// Instances keep their place between frames until placed again. Truncate
// shortens a TransformColor batch, whose count may change each frame; a
// Transform batch must send the count it was declared with.
type InstanceBatch struct {
	ID       string
	Kind     InstanceStreamKind
	writer   *StreamWriter
	data     []float32
	revision uint64
}

// NewInstanceBatch streams the batch batchID of the Scene3D mounted on the
// element whose id is mount.
func NewInstanceBatch(mount, batchID string, kind InstanceStreamKind) *InstanceBatch {
	return &InstanceBatch{ID: batchID, Kind: kind, writer: NewStreamWriter(mount)}
}

// Len is the number of instances the batch holds.
func (b *InstanceBatch) Len() int { return len(b.data) / b.Kind.Stride() }

// Truncate keeps the first n instances.
func (b *InstanceBatch) Truncate(n int) {
	b.data = b.data[:min(len(b.data), max(0, n)*b.Kind.Stride())]
}

// grow makes room for instance i.
func (b *InstanceBatch) grow(i int) []float32 {
	stride := b.Kind.Stride()
	if need := (i + 1) * stride; len(b.data) < need {
		b.data = append(b.data, make([]float32, need-len(b.data))...)
	}
	return b.data[i*stride : (i+1)*stride]
}

// Place sets instance i's transform: scaled by scale, tilted by tilt radians
// about its own X axis, turned by yaw radians about the world's Y axis, then
// moved to at. The batch grows to hold i.
func (b *InstanceBatch) Place(i int, at, scale Vector3, yaw, tilt float64) {
	PlaceInstance(b.grow(i), at, scale, yaw, tilt)
}

// Color sets instance i's RGBA color, for a TransformColor batch.
func (b *InstanceBatch) Color(i int, rgba [4]float64) {
	if b.Kind != InstanceStreamTransformColor {
		return
	}
	row := b.grow(i)
	for k, v := range rgba {
		row[16+k] = float32(v)
	}
}

// Send streams the batch to the renderer. It reports false, sending nothing,
// while the renderer is not ready; the next Send tries again.
func (b *InstanceBatch) Send() (bool, error) {
	if !b.writer.Prepare() {
		return false, nil
	}
	b.revision++
	return b.writer.Apply(InstanceStreamFrame{BatchID: b.ID, Revision: b.revision, Kind: b.Kind, Count: b.Len(), Data: b.data})
}

// Dispose releases the batch's writer.
func (b *InstanceBatch) Dispose() { b.writer.Dispose() }

// PlaceInstance writes one column-major instance matrix into dst[:16]: scale,
// then a tilt about the instance's own X axis, then a turn about the world's
// Y axis, then the position.
func PlaceInstance(dst []float32, at, scale Vector3, yaw, tilt float64) {
	cy, sy := det.Cos(yaw), det.Sin(yaw)
	c, s := det.Cos(tilt), det.Sin(tilt)
	m := [16]float64{
		float64(cy * scale.X), 0, float64(-sy * scale.X), 0,
		float64(sy * s * scale.Y), float64(c * scale.Y), float64(cy * s * scale.Y), 0,
		float64(sy * c * scale.Z), float64(-s * scale.Z), float64(cy * c * scale.Z), 0,
		at.X, at.Y, at.Z, 1,
	}
	for k, v := range m {
		dst[k] = float32(v)
	}
}

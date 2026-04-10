// Package field provides a 3D vector field type with trilinear sampling,
// standard operators (advection, curl, divergence, gradient, blur, resample),
// per-component scalar quantization with delta encoding, and hub streaming.
//
// Fields are independent of any renderer. They are the foundation for
// volumetric rendering, particle advection, fluid simulation, and any other
// consumer that needs structured 3D data.
package field

// AABB is an axis-aligned bounding box in world space.
type AABB struct {
	Min, Max [3]float32
}

// Field is a 3D grid of N-component vectors with world-space bounds.
//
// Components is 1 (scalar density), 2 (flow), 3 (velocity/color/normal),
// or 4 (color+alpha). Data is stored interleaved: for a 2x2x2 vec3 field,
// indices are [v0.x, v0.y, v0.z, v1.x, v1.y, v1.z, ...] in voxel order
// X-fastest, then Y, then Z.
type Field struct {
	Resolution [3]int
	Components int
	Bounds     AABB
	Data       []float32
	Time       float64 // optional timestamp for streaming/replay
}

// New allocates an uninitialized field.
func New(resolution [3]int, components int, bounds AABB) *Field {
	if components < 1 || components > 4 {
		panic("field: components must be 1..4")
	}
	if resolution[0] < 1 || resolution[1] < 1 || resolution[2] < 1 {
		panic("field: resolution must be >= 1 on every axis")
	}
	n := resolution[0] * resolution[1] * resolution[2] * components
	return &Field{
		Resolution: resolution,
		Components: components,
		Bounds:     bounds,
		Data:       make([]float32, n),
	}
}

// FromFunc samples fn at every voxel center and stores the result.
// fn must return a slice of length components.
func FromFunc(resolution [3]int, components int, bounds AABB,
	fn func(x, y, z float32) []float32) *Field {
	f := New(resolution, components, bounds)
	dx := (bounds.Max[0] - bounds.Min[0]) / float32(resolution[0])
	dy := (bounds.Max[1] - bounds.Min[1]) / float32(resolution[1])
	dz := (bounds.Max[2] - bounds.Min[2]) / float32(resolution[2])
	idx := 0
	for k := 0; k < resolution[2]; k++ {
		zc := bounds.Min[2] + (float32(k)+0.5)*dz
		for j := 0; j < resolution[1]; j++ {
			yc := bounds.Min[1] + (float32(j)+0.5)*dy
			for i := 0; i < resolution[0]; i++ {
				xc := bounds.Min[0] + (float32(i)+0.5)*dx
				v := fn(xc, yc, zc)
				if len(v) != components {
					panic("field: fn returned wrong component count")
				}
				for c := 0; c < components; c++ {
					f.Data[idx] = v[c]
					idx++
				}
			}
		}
	}
	return f
}

// SampleScalar returns the trilinearly interpolated scalar value at world (x,y,z).
// Panics if Components != 1. Out-of-bounds samples are clamped to the nearest
// in-bounds voxel.
func (f *Field) SampleScalar(x, y, z float32) float32 {
	if f.Components != 1 {
		panic("field: SampleScalar requires Components == 1")
	}
	return f.sampleAt(x, y, z, 0)
}

// sampleAt returns the trilinearly interpolated value of component c at (x,y,z).
func (f *Field) sampleAt(x, y, z float32, c int) float32 {
	rx := f.Resolution[0]
	ry := f.Resolution[1]
	rz := f.Resolution[2]
	// Normalized 0..1 inside bounds, then scale to voxel space (centered grid).
	u := (x-f.Bounds.Min[0])/(f.Bounds.Max[0]-f.Bounds.Min[0])*float32(rx) - 0.5
	v := (y-f.Bounds.Min[1])/(f.Bounds.Max[1]-f.Bounds.Min[1])*float32(ry) - 0.5
	w := (z-f.Bounds.Min[2])/(f.Bounds.Max[2]-f.Bounds.Min[2])*float32(rz) - 0.5

	i0 := clampInt(int(floor32(u)), 0, rx-1)
	j0 := clampInt(int(floor32(v)), 0, ry-1)
	k0 := clampInt(int(floor32(w)), 0, rz-1)
	i1 := clampInt(i0+1, 0, rx-1)
	j1 := clampInt(j0+1, 0, ry-1)
	k1 := clampInt(k0+1, 0, rz-1)

	tx := clampF(u-float32(i0), 0, 1)
	ty := clampF(v-float32(j0), 0, 1)
	tz := clampF(w-float32(k0), 0, 1)

	c000 := f.at(i0, j0, k0, c)
	c100 := f.at(i1, j0, k0, c)
	c010 := f.at(i0, j1, k0, c)
	c110 := f.at(i1, j1, k0, c)
	c001 := f.at(i0, j0, k1, c)
	c101 := f.at(i1, j0, k1, c)
	c011 := f.at(i0, j1, k1, c)
	c111 := f.at(i1, j1, k1, c)

	c00 := c000*(1-tx) + c100*tx
	c10 := c010*(1-tx) + c110*tx
	c01 := c001*(1-tx) + c101*tx
	c11 := c011*(1-tx) + c111*tx
	c0 := c00*(1-ty) + c10*ty
	c1 := c01*(1-ty) + c11*ty
	return c0*(1-tz) + c1*tz
}

// at returns the raw stored value of component c at voxel (i,j,k).
func (f *Field) at(i, j, k, c int) float32 {
	idx := ((k*f.Resolution[1]+j)*f.Resolution[0] + i) * f.Components
	return f.Data[idx+c]
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func floor32(x float32) float32 {
	i := float32(int32(x))
	if i > x {
		return i - 1
	}
	return i
}

// Sample returns the interpolated vector at world-space (x, y, z).
// The returned slice has length f.Components. Out-of-bounds samples
// are clamped to the nearest edge.
func (f *Field) Sample(x, y, z float32) []float32 {
	out := make([]float32, f.Components)
	for c := 0; c < f.Components; c++ {
		out[c] = f.sampleAt(x, y, z, c)
	}
	return out
}

// SampleVec3 returns the vec3 at (x,y,z) for Components==3 fields.
// Panics if Components != 3.
func (f *Field) SampleVec3(x, y, z float32) [3]float32 {
	if f.Components != 3 {
		panic("field: SampleVec3 requires Components == 3")
	}
	return [3]float32{
		f.sampleAt(x, y, z, 0),
		f.sampleAt(x, y, z, 1),
		f.sampleAt(x, y, z, 2),
	}
}

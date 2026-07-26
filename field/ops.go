package field

import "math"

// Advect moves particles through a velocity field by dt seconds using
// semi-Lagrangian RK2 (midpoint) integration. The particles slice is
// laid out as [x0,y0,z0, x1,y1,z1, ...] and modified in place.
//
// The velocity field's Components must equal 3. Callers must ensure
// len(particles) is a multiple of 3; trailing 1 or 2 elements are
// silently ignored.
func Advect(velocity *Field, particles []float32, dt float32) error {
	if err := validateFieldComponents("field.Advect", velocity, 3); err != nil {
		return err
	}
	half := dt * 0.5
	for i := 0; i+2 < len(particles); i += 3 {
		x, y, z := particles[i], particles[i+1], particles[i+2]
		v1 := velocity.SampleVec3(x, y, z)
		mx, my, mz := x+v1[0]*half, y+v1[1]*half, z+v1[2]*half
		v2 := velocity.SampleVec3(mx, my, mz)
		particles[i] = x + v2[0]*dt
		particles[i+1] = y + v2[1]*dt
		particles[i+2] = z + v2[2]*dt
	}
	return nil
}

// atClamp is like at but clamps voxel indices to bounds.
func (f *Field) atClamp(i, j, k, c int) float32 {
	i = clampInt(i, 0, f.Resolution[0]-1)
	j = clampInt(j, 0, f.Resolution[1]-1)
	k = clampInt(k, 0, f.Resolution[2]-1)
	return f.at(i, j, k, c)
}

// Gradient returns a vec3 field equal to the gradient of a scalar field,
// computed via central differences. Edge voxels use one-sided differences.
// Invalid input returns nil; callers that need diagnostics should use
// GradientChecked.
func Gradient(scalar *Field) *Field {
	out, _ := GradientChecked(scalar)
	return out
}

// GradientChecked returns the gradient or a validation error when scalar is
// not a scalar field. It allocates the output. Callers that run every frame
// should use GradientInto with a buffer they keep.
func GradientChecked(scalar *Field) (*Field, error) {
	if err := validateFieldComponents("field.Gradient", scalar, 1); err != nil {
		return nil, err
	}
	out, err := NewChecked(scalar.Resolution, 3, scalar.Bounds)
	if err != nil {
		return nil, err
	}
	if err := GradientInto(out, scalar); err != nil {
		return nil, err
	}
	return out, nil
}

// GradientInto writes the gradient of scalar into dst. dst must be a vec3 field
// with the resolution of scalar. dst must not share memory with scalar, because
// the central difference reads neighbor voxels after they would be overwritten.
//
// GradientInto allocates nothing. It is the form a per-frame loop should call.
func GradientInto(dst, scalar *Field) error {
	if err := validateFieldComponents("field.Gradient", scalar, 1); err != nil {
		return err
	}
	if err := validateOutput("field.Gradient", dst, scalar.Resolution, 3); err != nil {
		return err
	}
	if sameBuffer(dst, scalar) {
		return fieldError("field.Gradient", "output must not alias the input")
	}
	rx, ry, rz := scalar.Resolution[0], scalar.Resolution[1], scalar.Resolution[2]
	dx := (scalar.Bounds.Max[0] - scalar.Bounds.Min[0]) / float32(rx)
	dy := (scalar.Bounds.Max[1] - scalar.Bounds.Min[1]) / float32(ry)
	dz := (scalar.Bounds.Max[2] - scalar.Bounds.Min[2]) / float32(rz)
	out := dst
	for k := 0; k < rz; k++ {
		for j := 0; j < ry; j++ {
			for i := 0; i < rx; i++ {
				gx := centralDiff(scalar, i, j, k, 0, rx) / (2 * dx)
				gy := centralDiff(scalar, i, j, k, 1, ry) / (2 * dy)
				gz := centralDiff(scalar, i, j, k, 2, rz) / (2 * dz)
				idx := ((k*ry+j)*rx + i) * 3
				out.Data[idx] = gx
				out.Data[idx+1] = gy
				out.Data[idx+2] = gz
			}
		}
	}
	return nil
}

// centralDiff returns f[neighbor+] - f[neighbor-] of component 0 along axis (0=x,1=y,2=z).
// At boundaries returns 2*(forward or backward difference) so caller's /2d becomes /d.
func centralDiff(f *Field, i, j, k, axis, n int) float32 {
	var iP, jP, kP, iM, jM, kM int = i, j, k, i, j, k
	switch axis {
	case 0:
		iP, iM = i+1, i-1
	case 1:
		jP, jM = j+1, j-1
	case 2:
		kP, kM = k+1, k-1
	}
	if (axis == 0 && i == 0) || (axis == 1 && j == 0) || (axis == 2 && k == 0) {
		return 2 * (f.atClamp(iP, jP, kP, 0) - f.atClamp(i, j, k, 0))
	}
	if (axis == 0 && i == n-1) || (axis == 1 && j == n-1) || (axis == 2 && k == n-1) {
		return 2 * (f.atClamp(i, j, k, 0) - f.atClamp(iM, jM, kM, 0))
	}
	return f.atClamp(iP, jP, kP, 0) - f.atClamp(iM, jM, kM, 0)
}

// Divergence returns a scalar field equal to the divergence of a vec3 field.
// Invalid input returns nil; callers that need diagnostics should use
// DivergenceChecked.
func Divergence(velocity *Field) *Field {
	out, _ := DivergenceChecked(velocity)
	return out
}

// DivergenceChecked returns the divergence or a validation error when velocity
// is not a vec3 field. It allocates the output. Callers that run every frame
// should use DivergenceInto with a buffer they keep.
func DivergenceChecked(velocity *Field) (*Field, error) {
	if err := validateFieldComponents("field.Divergence", velocity, 3); err != nil {
		return nil, err
	}
	out, err := NewChecked(velocity.Resolution, 1, velocity.Bounds)
	if err != nil {
		return nil, err
	}
	if err := DivergenceInto(out, velocity); err != nil {
		return nil, err
	}
	return out, nil
}

// DivergenceInto writes the divergence of velocity into dst. dst must be a
// scalar field with the resolution of velocity, and must not share memory with
// velocity.
//
// DivergenceInto allocates nothing.
func DivergenceInto(dst, velocity *Field) error {
	if err := validateFieldComponents("field.Divergence", velocity, 3); err != nil {
		return err
	}
	if err := validateOutput("field.Divergence", dst, velocity.Resolution, 1); err != nil {
		return err
	}
	if sameBuffer(dst, velocity) {
		return fieldError("field.Divergence", "output must not alias the input")
	}
	rx, ry, rz := velocity.Resolution[0], velocity.Resolution[1], velocity.Resolution[2]
	dx := (velocity.Bounds.Max[0] - velocity.Bounds.Min[0]) / float32(rx)
	dy := (velocity.Bounds.Max[1] - velocity.Bounds.Min[1]) / float32(ry)
	dz := (velocity.Bounds.Max[2] - velocity.Bounds.Min[2]) / float32(rz)
	for k := 0; k < rz; k++ {
		for j := 0; j < ry; j++ {
			for i := 0; i < rx; i++ {
				dvxdx := componentDiff(velocity, i, j, k, 0, 0, rx) / (2 * dx)
				dvydy := componentDiff(velocity, i, j, k, 1, 1, ry) / (2 * dy)
				dvzdz := componentDiff(velocity, i, j, k, 2, 2, rz) / (2 * dz)
				idx := (k*ry+j)*rx + i
				dst.Data[idx] = dvxdx + dvydy + dvzdz
			}
		}
	}
	return nil
}

// componentDiff returns the central difference of component c along axis,
// with one-sided differences at the boundary (premultiplied by 2 for caller convenience).
func componentDiff(f *Field, i, j, k, c, axis, n int) float32 {
	var iP, jP, kP, iM, jM, kM int = i, j, k, i, j, k
	switch axis {
	case 0:
		iP, iM = i+1, i-1
	case 1:
		jP, jM = j+1, j-1
	case 2:
		kP, kM = k+1, k-1
	}
	if (axis == 0 && i == 0) || (axis == 1 && j == 0) || (axis == 2 && k == 0) {
		return 2 * (f.atClamp(iP, jP, kP, c) - f.atClamp(i, j, k, c))
	}
	if (axis == 0 && i == n-1) || (axis == 1 && j == n-1) || (axis == 2 && k == n-1) {
		return 2 * (f.atClamp(i, j, k, c) - f.atClamp(iM, jM, kM, c))
	}
	return f.atClamp(iP, jP, kP, c) - f.atClamp(iM, jM, kM, c)
}

// Curl returns a vec3 field equal to the curl of a vec3 input field,
// computed via central differences. Invalid input returns nil; callers that
// need diagnostics should use CurlChecked.
func Curl(velocity *Field) *Field {
	out, _ := CurlChecked(velocity)
	return out
}

// CurlChecked returns the curl or a validation error when velocity is not a
// vec3 field. It allocates the output. Callers that run every frame should use
// CurlInto with a buffer they keep.
func CurlChecked(velocity *Field) (*Field, error) {
	if err := validateFieldComponents("field.Curl", velocity, 3); err != nil {
		return nil, err
	}
	out, err := NewChecked(velocity.Resolution, 3, velocity.Bounds)
	if err != nil {
		return nil, err
	}
	if err := CurlInto(out, velocity); err != nil {
		return nil, err
	}
	return out, nil
}

// CurlInto writes the curl of velocity into dst. dst must be a vec3 field with
// the resolution of velocity, and must not share memory with velocity.
//
// CurlInto allocates nothing.
func CurlInto(dst, velocity *Field) error {
	if err := validateFieldComponents("field.Curl", velocity, 3); err != nil {
		return err
	}
	if err := validateOutput("field.Curl", dst, velocity.Resolution, 3); err != nil {
		return err
	}
	if sameBuffer(dst, velocity) {
		return fieldError("field.Curl", "output must not alias the input")
	}
	rx, ry, rz := velocity.Resolution[0], velocity.Resolution[1], velocity.Resolution[2]
	dx := (velocity.Bounds.Max[0] - velocity.Bounds.Min[0]) / float32(rx)
	dy := (velocity.Bounds.Max[1] - velocity.Bounds.Min[1]) / float32(ry)
	dz := (velocity.Bounds.Max[2] - velocity.Bounds.Min[2]) / float32(rz)
	out := dst
	for k := 0; k < rz; k++ {
		for j := 0; j < ry; j++ {
			for i := 0; i < rx; i++ {
				dvzdy := componentDiff(velocity, i, j, k, 2, 1, ry) / (2 * dy)
				dvydz := componentDiff(velocity, i, j, k, 1, 2, rz) / (2 * dz)
				dvxdz := componentDiff(velocity, i, j, k, 0, 2, rz) / (2 * dz)
				dvzdx := componentDiff(velocity, i, j, k, 2, 0, rx) / (2 * dx)
				dvydx := componentDiff(velocity, i, j, k, 1, 0, rx) / (2 * dx)
				dvxdy := componentDiff(velocity, i, j, k, 0, 1, ry) / (2 * dy)
				idx := ((k*ry+j)*rx + i) * 3
				out.Data[idx] = dvzdy - dvydz
				out.Data[idx+1] = dvxdz - dvzdx
				out.Data[idx+2] = dvydx - dvxdy
			}
		}
	}
	return nil
}

// Blur applies a separable Gaussian blur with the given radius, in voxels.
// It returns a new field and does not change the input. Invalid input returns
// nil; callers that need diagnostics should use BlurInto.
//
// Blur allocates one output field and one scratch field per call. A per-frame
// loop should call BlurInto and keep both buffers.
func Blur(f *Field, radius float32) *Field {
	if err := validateField("field.Blur", f); err != nil {
		return nil
	}
	out := New(f.Resolution, f.Components, f.Bounds)
	if err := BlurInto(out, f, radius, nil); err != nil {
		return nil
	}
	return out
}

// BlurInto writes a separable Gaussian blur of src into dst. dst must have the
// shape of src. dst may alias src exactly, which gives an in-place blur; a
// partial overlap is not supported.
//
// scratch supplies the one temporary buffer the three passes need. Pass nil to
// let BlurInto allocate a temporary for this call only. Pass a reused Scratch to
// make a steady-state loop allocation free.
//
// A separable Gaussian needs one scratch buffer, not three. The passes
// ping-pong between dst and the scratch buffer, so the third pass lands the
// result in dst.
func BlurInto(dst, src *Field, radius float32, scratch *Scratch) error {
	if err := validateField("field.Blur", src); err != nil {
		return err
	}
	if err := validateOutput("field.Blur", dst, src.Resolution, src.Components); err != nil {
		return err
	}
	if radius <= 0 {
		if !sameBuffer(dst, src) {
			copy(dst.Data, src.Data)
		}
		return nil
	}
	if scratch == nil {
		scratch = &Scratch{}
	}
	kernel := scratch.gaussian(radius)
	tmp := scratch.fieldLike(src)
	if sameBuffer(dst, src) {
		// dst holds the input. Move the data out first, then ping-pong back.
		blurAxis(src, tmp, kernel, 0)
		blurAxis(tmp, dst, kernel, 1)
		blurAxis(dst, tmp, kernel, 2)
		copy(dst.Data, tmp.Data)
		return nil
	}
	blurAxis(src, dst, kernel, 0)
	blurAxis(dst, tmp, kernel, 1)
	blurAxis(tmp, dst, kernel, 2)
	return nil
}

func gaussianKernel(radius float32) []float32 {
	r := int(math.Ceil(float64(radius * 3)))
	if r < 1 {
		r = 1
	}
	size := 2*r + 1
	k := make([]float32, size)
	sigma := float64(radius)
	twoSigmaSq := 2 * sigma * sigma
	var sum float32
	for i := 0; i < size; i++ {
		x := float64(i - r)
		w := float32(math.Exp(-x * x / twoSigmaSq))
		k[i] = w
		sum += w
	}
	for i := range k {
		k[i] /= sum
	}
	return k
}

func blurAxis(src, dst *Field, kernel []float32, axis int) {
	r := len(kernel) / 2
	rx, ry, rz := src.Resolution[0], src.Resolution[1], src.Resolution[2]
	for k := 0; k < rz; k++ {
		for j := 0; j < ry; j++ {
			for i := 0; i < rx; i++ {
				for c := 0; c < src.Components; c++ {
					var sum float32
					for t := -r; t <= r; t++ {
						ii, jj, kk := i, j, k
						switch axis {
						case 0:
							ii = i + t
						case 1:
							jj = j + t
						case 2:
							kk = k + t
						}
						sum += kernel[t+r] * src.atClamp(ii, jj, kk, c)
					}
					idx := ((k*ry+j)*rx + i) * src.Components
					dst.Data[idx+c] = sum
				}
			}
		}
	}
}

// Resample produces a field at a new resolution using trilinear filtering
// from the source field. Invalid input returns nil; callers that need
// diagnostics should use ResampleInto.
//
// Resample allocates the output. A per-frame loop should call ResampleInto with
// a buffer it keeps.
func Resample(f *Field, newResolution [3]int) *Field {
	if err := validateField("field.Resample", f); err != nil {
		return nil
	}
	out, err := NewChecked(newResolution, f.Components, f.Bounds)
	if err != nil {
		return nil
	}
	if err := ResampleInto(out, f); err != nil {
		return nil
	}
	return out
}

// ResampleInto writes src, trilinearly filtered to the resolution of dst, into
// dst. dst carries the target resolution and must have the component count of
// src. dst must not share memory with src.
//
// ResampleInto allocates nothing.
func ResampleInto(dst, src *Field) error {
	if err := validateField("field.Resample", src); err != nil {
		return err
	}
	if dst == nil {
		return fieldError("field.Resample", "output field is nil")
	}
	if err := validateOutput("field.Resample", dst, dst.Resolution, src.Components); err != nil {
		return err
	}
	if sameBuffer(dst, src) {
		return fieldError("field.Resample", "output must not alias the input")
	}
	// The resampled grid covers the same world volume as the source, so dst
	// takes the source bounds. Only the sample count changes.
	dst.Bounds = src.Bounds
	newResolution := dst.Resolution
	dx := (src.Bounds.Max[0] - src.Bounds.Min[0]) / float32(newResolution[0])
	dy := (src.Bounds.Max[1] - src.Bounds.Min[1]) / float32(newResolution[1])
	dz := (src.Bounds.Max[2] - src.Bounds.Min[2]) / float32(newResolution[2])
	idx := 0
	for k := 0; k < newResolution[2]; k++ {
		zc := src.Bounds.Min[2] + (float32(k)+0.5)*dz
		for j := 0; j < newResolution[1]; j++ {
			yc := src.Bounds.Min[1] + (float32(j)+0.5)*dy
			for i := 0; i < newResolution[0]; i++ {
				xc := src.Bounds.Min[0] + (float32(i)+0.5)*dx
				for c := 0; c < src.Components; c++ {
					dst.Data[idx] = src.sampleAt(xc, yc, zc, c)
					idx++
				}
			}
		}
	}
	return nil
}

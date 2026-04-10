package field

import "math"

// Advect moves particles through a velocity field by dt seconds using
// semi-Lagrangian RK2 (midpoint) integration. The particles slice is
// laid out as [x0,y0,z0, x1,y1,z1, ...] and modified in place.
//
// The velocity field's Components must equal 3.
func Advect(velocity *Field, particles []float32, dt float32) {
	if velocity.Components != 3 {
		panic("field.Advect: velocity field must have Components == 3")
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
func Gradient(scalar *Field) *Field {
	if scalar.Components != 1 {
		panic("field.Gradient: input must have Components == 1")
	}
	rx, ry, rz := scalar.Resolution[0], scalar.Resolution[1], scalar.Resolution[2]
	dx := (scalar.Bounds.Max[0] - scalar.Bounds.Min[0]) / float32(rx)
	dy := (scalar.Bounds.Max[1] - scalar.Bounds.Min[1]) / float32(ry)
	dz := (scalar.Bounds.Max[2] - scalar.Bounds.Min[2]) / float32(rz)
	out := New(scalar.Resolution, 3, scalar.Bounds)
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
	return out
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
func Divergence(velocity *Field) *Field {
	if velocity.Components != 3 {
		panic("field.Divergence: input must have Components == 3")
	}
	rx, ry, rz := velocity.Resolution[0], velocity.Resolution[1], velocity.Resolution[2]
	dx := (velocity.Bounds.Max[0] - velocity.Bounds.Min[0]) / float32(rx)
	dy := (velocity.Bounds.Max[1] - velocity.Bounds.Min[1]) / float32(ry)
	dz := (velocity.Bounds.Max[2] - velocity.Bounds.Min[2]) / float32(rz)
	out := New(velocity.Resolution, 1, velocity.Bounds)
	for k := 0; k < rz; k++ {
		for j := 0; j < ry; j++ {
			for i := 0; i < rx; i++ {
				dvxdx := componentDiff(velocity, i, j, k, 0, 0, rx) / (2 * dx)
				dvydy := componentDiff(velocity, i, j, k, 1, 1, ry) / (2 * dy)
				dvzdz := componentDiff(velocity, i, j, k, 2, 2, rz) / (2 * dz)
				idx := (k*ry+j)*rx + i
				out.Data[idx] = dvxdx + dvydy + dvzdz
			}
		}
	}
	return out
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
// computed via central differences.
func Curl(velocity *Field) *Field {
	if velocity.Components != 3 {
		panic("field.Curl: input must have Components == 3")
	}
	rx, ry, rz := velocity.Resolution[0], velocity.Resolution[1], velocity.Resolution[2]
	dx := (velocity.Bounds.Max[0] - velocity.Bounds.Min[0]) / float32(rx)
	dy := (velocity.Bounds.Max[1] - velocity.Bounds.Min[1]) / float32(ry)
	dz := (velocity.Bounds.Max[2] - velocity.Bounds.Min[2]) / float32(rz)
	out := New(velocity.Resolution, 3, velocity.Bounds)
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
	return out
}

// Blur applies a separable Gaussian blur with the given radius (in voxels).
// Returns a new field; the input is not modified.
func Blur(f *Field, radius float32) *Field {
	if radius <= 0 {
		out := New(f.Resolution, f.Components, f.Bounds)
		copy(out.Data, f.Data)
		return out
	}
	kernel := gaussianKernel(radius)
	tmp := New(f.Resolution, f.Components, f.Bounds)
	out := New(f.Resolution, f.Components, f.Bounds)
	tmp2 := New(f.Resolution, f.Components, f.Bounds)
	blurAxis(f, tmp, kernel, 0)
	blurAxis(tmp, tmp2, kernel, 1)
	blurAxis(tmp2, out, kernel, 2)
	return out
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
// from the source field.
func Resample(f *Field, newResolution [3]int) *Field {
	out := New(newResolution, f.Components, f.Bounds)
	dx := (f.Bounds.Max[0] - f.Bounds.Min[0]) / float32(newResolution[0])
	dy := (f.Bounds.Max[1] - f.Bounds.Min[1]) / float32(newResolution[1])
	dz := (f.Bounds.Max[2] - f.Bounds.Min[2]) / float32(newResolution[2])
	idx := 0
	for k := 0; k < newResolution[2]; k++ {
		zc := f.Bounds.Min[2] + (float32(k)+0.5)*dz
		for j := 0; j < newResolution[1]; j++ {
			yc := f.Bounds.Min[1] + (float32(j)+0.5)*dy
			for i := 0; i < newResolution[0]; i++ {
				xc := f.Bounds.Min[0] + (float32(i)+0.5)*dx
				for c := 0; c < f.Components; c++ {
					out.Data[idx] = f.sampleAt(xc, yc, zc, c)
					idx++
				}
			}
		}
	}
	return out
}

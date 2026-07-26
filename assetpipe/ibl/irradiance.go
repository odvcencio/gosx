package ibl

import "math"

// SH9 holds nine spherical-harmonic coefficients per colour channel. The
// order is the standard band order: l=0, then l=1 (m=-1,0,1), then l=2
// (m=-2,-1,0,1,2).
type SH9 [9]Vec3

// shBasis evaluates the first nine real spherical harmonics for a direction.
func shBasis(d Vec3) [9]float64 {
	x, y, z := d.X, d.Y, d.Z
	return [9]float64{
		0.282095,
		0.488603 * y,
		0.488603 * z,
		0.488603 * x,
		1.092548 * x * y,
		1.092548 * y * z,
		0.315392 * (3*z*z - 1),
		1.092548 * x * z,
		0.546274 * (x*x - y*y),
	}
}

// texelSolidAngle returns the solid angle a cubemap texel covers. The formula
// integrates the projected area of the texel on the unit sphere.
func texelSolidAngle(x, y, size int) float64 {
	inv := 1.0 / float64(size)
	u := 2*(float64(x)+0.5)*inv - 1
	v := 2*(float64(y)+0.5)*inv - 1
	x0, x1 := u-inv, u+inv
	y0, y1 := v-inv, v+inv
	return areaElement(x0, y0) - areaElement(x0, y1) - areaElement(x1, y0) + areaElement(x1, y1)
}

func areaElement(x, y float64) float64 {
	return math.Atan2(x*y, math.Sqrt(x*x+y*y+1))
}

// ProjectSH projects a cubemap onto the first nine spherical harmonics. The
// projection weights every texel by its solid angle, so the result does not
// depend on the cubemap size.
func ProjectSH(cube *Cube) SH9 {
	var sh SH9
	if cube == nil {
		return sh
	}
	size := cube.Size
	for face := 0; face < 6; face++ {
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				dir := FaceDirection(face, x, y, size)
				weight := texelSolidAngle(x, y, size)
				colour := cube.Get(face, x, y)
				basis := shBasis(dir)
				for i := 0; i < 9; i++ {
					sh[i] = sh[i].add(colour.scale(basis[i] * weight))
				}
			}
		}
	}
	return sh
}

// Cosine convolution factors for the first three bands. Ramamoorthi and
// Hanrahan give A0 = pi, A1 = 2*pi/3, and A2 = pi/4.
var shCosineConvolution = [9]float64{
	math.Pi,
	2 * math.Pi / 3, 2 * math.Pi / 3, 2 * math.Pi / 3,
	math.Pi / 4, math.Pi / 4, math.Pi / 4, math.Pi / 4, math.Pi / 4,
}

// IrradianceFromSH evaluates the diffuse irradiance for a normal and divides
// by pi, so the result is the radiance a Lambert surface reflects when its
// albedo is 1. A constant environment therefore returns that same constant.
func IrradianceFromSH(sh SH9, normal Vec3) Vec3 {
	basis := shBasis(normal)
	var sum Vec3
	for i := 0; i < 9; i++ {
		sum = sum.add(sh[i].scale(basis[i] * shCosineConvolution[i]))
	}
	return sum.scale(1 / math.Pi)
}

// IrradianceCube renders a small diffuse cubemap from spherical-harmonic
// coefficients. Three bands reproduce a cosine convolution to about one
// percent, which is the accepted trade for a map this small.
func IrradianceCube(sh SH9, size int) *Cube {
	cube := NewCube(size)
	for face := 0; face < 6; face++ {
		for y := 0; y < cube.Size; y++ {
			for x := 0; x < cube.Size; x++ {
				normal := FaceDirection(face, x, y, cube.Size)
				value := IrradianceFromSH(sh, normal)
				value.X = math.Max(0, value.X)
				value.Y = math.Max(0, value.Y)
				value.Z = math.Max(0, value.Z)
				cube.Set(face, x, y, value)
			}
		}
	}
	return cube
}

// IrradianceCubeBruteForce convolves a cubemap against the cosine lobe by
// direct summation. It costs O(faces^2) and exists as the reference the
// spherical-harmonic path is checked against.
func IrradianceCubeBruteForce(source *Cube, size int) *Cube {
	cube := NewCube(size)
	srcSize := source.Size
	weights := make([]float64, srcSize*srcSize)
	for y := 0; y < srcSize; y++ {
		for x := 0; x < srcSize; x++ {
			weights[y*srcSize+x] = texelSolidAngle(x, y, srcSize)
		}
	}
	parallelRows(6*cube.Size, func(row int) {
		face := row / cube.Size
		y := row % cube.Size
		for x := 0; x < cube.Size; x++ {
			normal := FaceDirection(face, x, y, cube.Size)
			var sum Vec3
			var total float64
			for srcFace := 0; srcFace < 6; srcFace++ {
				for sy := 0; sy < srcSize; sy++ {
					for sx := 0; sx < srcSize; sx++ {
						dir := FaceDirection(srcFace, sx, sy, srcSize)
						cosine := normal.dot(dir)
						if cosine <= 0 {
							continue
						}
						weight := cosine * weights[sy*srcSize+sx]
						sum = sum.add(source.Get(srcFace, sx, sy).scale(weight))
						total += weight
					}
				}
			}
			if total > 0 {
				sum = sum.scale(1 / total)
			}
			cube.Set(face, x, y, sum)
		}
	})
	return cube
}

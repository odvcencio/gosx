// Package ibl builds image-based lighting products from a linear high dynamic
// range environment image. It produces a GGX-prefiltered specular cubemap, a
// diffuse irradiance cubemap with matching spherical-harmonic coefficients,
// and the scene-independent split-sum BRDF lookup table.
//
// The package uses the standard library only, so a build machine needs no
// native texture tools. All math runs in float64 and stores float32.
package ibl

import (
	"fmt"
	"math"
	"runtime"
	"sync"
)

// Cube face order matches Vulkan, WebGPU, and OpenGL: +X, -X, +Y, -Y, +Z, -Z.
const (
	FacePosX = 0
	FaceNegX = 1
	FacePosY = 2
	FaceNegY = 3
	FacePosZ = 4
	FaceNegZ = 5
)

// Vec3 is a direction or a colour triple in float64.
type Vec3 struct{ X, Y, Z float64 }

func (v Vec3) add(o Vec3) Vec3      { return Vec3{v.X + o.X, v.Y + o.Y, v.Z + o.Z} }
func (v Vec3) sub(o Vec3) Vec3      { return Vec3{v.X - o.X, v.Y - o.Y, v.Z - o.Z} }
func (v Vec3) scale(s float64) Vec3 { return Vec3{v.X * s, v.Y * s, v.Z * s} }
func (v Vec3) dot(o Vec3) float64   { return v.X*o.X + v.Y*o.Y + v.Z*o.Z }
func (v Vec3) length() float64      { return math.Sqrt(v.dot(v)) }

func (v Vec3) normalize() Vec3 {
	length := v.length()
	if length == 0 {
		return Vec3{0, 0, 1}
	}
	return v.scale(1 / length)
}

// Cube holds one cubemap level. Each face stores Size*Size RGB texels in row
// order, with row 0 at the top of the face.
type Cube struct {
	Size  int
	Faces [6][]float32
}

// NewCube allocates a cubemap of the given edge length.
func NewCube(size int) *Cube {
	if size < 1 {
		size = 1
	}
	cube := &Cube{Size: size}
	for face := range cube.Faces {
		cube.Faces[face] = make([]float32, size*size*3)
	}
	return cube
}

// Texels returns the number of texels in one face.
func (c *Cube) Texels() int { return c.Size * c.Size }

// Get reads one texel.
func (c *Cube) Get(face, x, y int) Vec3 {
	i := (y*c.Size + x) * 3
	data := c.Faces[face]
	return Vec3{float64(data[i]), float64(data[i+1]), float64(data[i+2])}
}

// Set writes one texel.
func (c *Cube) Set(face, x, y int, value Vec3) {
	i := (y*c.Size + x) * 3
	data := c.Faces[face]
	data[i] = float32(value.X)
	data[i+1] = float32(value.Y)
	data[i+2] = float32(value.Z)
}

// Clone copies the cubemap.
func (c *Cube) Clone() *Cube {
	out := NewCube(c.Size)
	for face := range c.Faces {
		copy(out.Faces[face], c.Faces[face])
	}
	return out
}

// FaceDirection returns the unit direction of texel (x, y) on a face.
func FaceDirection(face, x, y, size int) Vec3 {
	u := 2*(float64(x)+0.5)/float64(size) - 1
	v := 2*(float64(y)+0.5)/float64(size) - 1
	return faceVector(face, u, v).normalize()
}

func faceVector(face int, u, v float64) Vec3 {
	switch face {
	case FacePosX:
		return Vec3{1, -v, -u}
	case FaceNegX:
		return Vec3{-1, -v, u}
	case FacePosY:
		return Vec3{u, 1, v}
	case FaceNegY:
		return Vec3{u, -1, -v}
	case FacePosZ:
		return Vec3{u, -v, 1}
	default:
		return Vec3{-u, -v, -1}
	}
}

// DirectionToFace maps a direction to a face index and to face coordinates in
// the range [0, 1].
func DirectionToFace(d Vec3) (face int, s, t float64) {
	absX, absY, absZ := math.Abs(d.X), math.Abs(d.Y), math.Abs(d.Z)
	var major float64
	var u, v float64
	switch {
	case absX >= absY && absX >= absZ:
		major = absX
		if d.X > 0 {
			face, u, v = FacePosX, -d.Z, -d.Y
		} else {
			face, u, v = FaceNegX, d.Z, -d.Y
		}
	case absY >= absZ:
		major = absY
		if d.Y > 0 {
			face, u, v = FacePosY, d.X, d.Z
		} else {
			face, u, v = FaceNegY, d.X, -d.Z
		}
	default:
		major = absZ
		if d.Z > 0 {
			face, u, v = FacePosZ, d.X, -d.Y
		} else {
			face, u, v = FaceNegZ, -d.X, -d.Y
		}
	}
	if major == 0 {
		major = 1
	}
	return face, 0.5 * (u/major + 1), 0.5 * (v/major + 1)
}

// Sample reads the cubemap with bilinear filtering inside the selected face.
// The filter clamps at the face border, so a seam texel blends with its own
// face instead of the neighbouring face. The error stays below one texel and
// disappears as the face size grows.
func (c *Cube) Sample(d Vec3) Vec3 {
	face, s, t := DirectionToFace(d)
	return c.sampleFace(face, s, t)
}

func (c *Cube) sampleFace(face int, s, t float64) Vec3 {
	size := c.Size
	x := s*float64(size) - 0.5
	y := t*float64(size) - 0.5
	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	fx := x - float64(x0)
	fy := y - float64(y0)
	x1, y1 := clampInt(x0+1, size), clampInt(y0+1, size)
	x0, y0 = clampInt(x0, size), clampInt(y0, size)

	c00 := c.Get(face, x0, y0)
	c10 := c.Get(face, x1, y0)
	c01 := c.Get(face, x0, y1)
	c11 := c.Get(face, x1, y1)
	top := c00.scale(1 - fx).add(c10.scale(fx))
	bottom := c01.scale(1 - fx).add(c11.scale(fx))
	return top.scale(1 - fy).add(bottom.scale(fy))
}

func clampInt(v, size int) int {
	if v < 0 {
		return 0
	}
	if v >= size {
		return size - 1
	}
	return v
}

// Chain is a cubemap mip chain. Level 0 holds the full size cubemap and each
// following level halves the edge length down to 1x1.
type Chain []*Cube

// BuildChain box-filters cube down to a 1x1 level and returns the chain.
func BuildChain(cube *Cube) Chain {
	chain := Chain{cube}
	current := cube
	for current.Size > 1 {
		next := NewCube(current.Size / 2)
		for face := 0; face < 6; face++ {
			for y := 0; y < next.Size; y++ {
				for x := 0; x < next.Size; x++ {
					sum := current.Get(face, x*2, y*2).
						add(current.Get(face, x*2+1, y*2)).
						add(current.Get(face, x*2, y*2+1)).
						add(current.Get(face, x*2+1, y*2+1))
					next.Set(face, x, y, sum.scale(0.25))
				}
			}
		}
		chain = append(chain, next)
		current = next
	}
	return chain
}

// SampleLevel reads the chain with linear blending between the two nearest
// levels.
func (c Chain) SampleLevel(d Vec3, level float64) Vec3 {
	if len(c) == 0 {
		return Vec3{}
	}
	if level <= 0 {
		return c[0].Sample(d)
	}
	top := float64(len(c) - 1)
	if level >= top {
		return c[len(c)-1].Sample(d)
	}
	low := int(math.Floor(level))
	frac := level - float64(low)
	a := c[low].Sample(d)
	b := c[low+1].Sample(d)
	return a.scale(1 - frac).add(b.scale(frac))
}

// EquirectSource is the linear environment image the projector reads.
type EquirectSource interface {
	// Size returns the pixel width and height.
	Size() (int, int)
	// RGB returns the linear colour of one pixel.
	RGB(x, y int) (float32, float32, float32)
}

// EquirectToCube projects an equirectangular environment image onto a
// cubemap. The projector averages samples*samples positions per texel, so a
// small cubemap keeps the energy of a large source.
//
// The longitude mapping matches the common shader convention:
// u = 0.5 + atan2(z, x) / (2*pi) and v = acos(y) / pi, with row 0 of the
// source at y = +1.
func EquirectToCube(src EquirectSource, size, samples int) (*Cube, error) {
	if src == nil {
		return nil, fmt.Errorf("ibl: nil equirectangular source")
	}
	width, height := src.Size()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("ibl: source is %dx%d", width, height)
	}
	if size < 1 {
		return nil, fmt.Errorf("ibl: cube size %d", size)
	}
	if samples < 1 {
		samples = 1
	}
	cube := NewCube(size)
	step := 1.0 / float64(samples)
	parallelRows(6*size, func(row int) {
		face := row / size
		y := row % size
		for x := 0; x < size; x++ {
			var sum Vec3
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					u := 2*(float64(x)+(float64(sx)+0.5)*step)/float64(size) - 1
					v := 2*(float64(y)+(float64(sy)+0.5)*step)/float64(size) - 1
					dir := faceVector(face, u, v).normalize()
					sum = sum.add(sampleEquirect(src, dir))
				}
			}
			cube.Set(face, x, y, sum.scale(1/float64(samples*samples)))
		}
	})
	return cube, nil
}

func sampleEquirect(src EquirectSource, d Vec3) Vec3 {
	width, height := src.Size()
	u := 0.5 + math.Atan2(d.Z, d.X)/(2*math.Pi)
	v := math.Acos(math.Max(-1, math.Min(1, d.Y))) / math.Pi
	fx := u*float64(width) - 0.5
	fy := v*float64(height) - 0.5
	x0 := int(math.Floor(fx))
	y0 := int(math.Floor(fy))
	tx := fx - float64(x0)
	ty := fy - float64(y0)

	get := func(x, y int) Vec3 {
		// Longitude wraps; latitude clamps at the poles.
		x = ((x % width) + width) % width
		if y < 0 {
			y = 0
		}
		if y >= height {
			y = height - 1
		}
		r, g, b := src.RGB(x, y)
		return Vec3{float64(r), float64(g), float64(b)}
	}
	top := get(x0, y0).scale(1 - tx).add(get(x0+1, y0).scale(tx))
	bottom := get(x0, y0+1).scale(1 - tx).add(get(x0+1, y0+1).scale(tx))
	return top.scale(1 - ty).add(bottom.scale(ty))
}

// parallelRows runs body for every row index, spread over the available CPUs.
func parallelRows(rows int, body func(row int)) {
	workers := runtime.NumCPU()
	if workers > rows {
		workers = rows
	}
	if workers < 1 {
		workers = 1
	}
	if workers == 1 {
		for row := 0; row < rows; row++ {
			body(row)
		}
		return
	}
	var wg sync.WaitGroup
	next := make(chan int, workers)
	go func() {
		for row := 0; row < rows; row++ {
			next <- row
		}
		close(next)
	}()
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range next {
				body(row)
			}
		}()
	}
	wg.Wait()
}

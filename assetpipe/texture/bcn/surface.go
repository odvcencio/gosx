package bcn

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
)

// Surface holds linear-light RGBA pixels as float32, four values per pixel in
// row-major order.
//
// The layout repeats texture.Image so a caller can share the pixel slice
// without a copy. See the package doc for the reason the type exists.
type Surface struct {
	Width, Height int
	Pix           []float32
}

// NewSurface allocates a transparent black surface.
func NewSurface(width, height int) *Surface {
	if width < 1 || height < 1 {
		return &Surface{}
	}
	return &Surface{Width: width, Height: height, Pix: make([]float32, width*height*4)}
}

// At returns the four linear channels of one pixel.
func (s *Surface) At(x, y int) (float32, float32, float32, float32) {
	i := (y*s.Width + x) * 4
	return s.Pix[i], s.Pix[i+1], s.Pix[i+2], s.Pix[i+3]
}

// Set writes the four linear channels of one pixel.
func (s *Surface) Set(x, y int, r, g, b, a float32) {
	i := (y*s.Width + x) * 4
	s.Pix[i], s.Pix[i+1], s.Pix[i+2], s.Pix[i+3] = r, g, b, a
}

// validate checks the size and the pixel slice length.
func (s *Surface) validate() error {
	if s == nil || s.Width < 1 || s.Height < 1 {
		return fmt.Errorf("%w: empty surface", ErrShape)
	}
	want := s.Width * s.Height * 4
	if len(s.Pix) < want {
		return fmt.Errorf("%w: %dx%d needs %d values, slice holds %d",
			ErrShape, s.Width, s.Height, want, len(s.Pix))
	}
	return nil
}

// clampX returns a column index inside the surface.
//
// A block that reaches past the right or the bottom edge repeats the last row or
// column. Repeating the edge keeps the real texels in charge of the endpoint
// fit. Padding with black would drag the endpoints toward black and would show
// as a dark seam on the last block of a texture whose size is not a multiple of
// four.
func (s *Surface) clampX(x int) int {
	if x >= s.Width {
		return s.Width - 1
	}
	return x
}

func (s *Surface) clampY(y int) int {
	if y >= s.Height {
		return s.Height - 1
	}
	return y
}

// gatherRGBA copies one 4x4 block as stored 8-bit codes.
//
// The colour channels pass through t. Alpha never does, because the sRGB
// specification applies the transfer function to the three colour channels only.
func (s *Surface) gatherRGBA(bx, by int, t Transfer, out *[16]RGBA8) {
	for row := 0; row < 4; row++ {
		y := s.clampY(by*4 + row)
		for col := 0; col < 4; col++ {
			x := s.clampX(bx*4 + col)
			i := (y*s.Width + x) * 4
			out[row*4+col] = RGBA8{
				R: t.encode(s.Pix[i]),
				G: t.encode(s.Pix[i+1]),
				B: t.encode(s.Pix[i+2]),
				A: encodeUnorm8(s.Pix[i+3]),
			}
		}
	}
}

// gatherChannel copies one channel of a 4x4 block as stored codes, held as
// float64 so the fit arithmetic never rounds twice.
func (s *Surface) gatherChannel(bx, by int, ch Channel, out *[16]float64) {
	for row := 0; row < 4; row++ {
		y := s.clampY(by*4 + row)
		for col := 0; col < 4; col++ {
			x := s.clampX(bx*4 + col)
			i := (y*s.Width+x)*4 + int(ch)
			out[row*4+col] = float64(encodeUnorm8(s.Pix[i]))
		}
	}
}

// gatherNormal copies one 4x4 block of tangent-space normals as stored codes.
//
// The function reads the three colour channels as an encoded normal, which the
// usual convention stores as n*0.5+0.5. It normalizes the vector before it
// quantizes, so a source whose texels drifted off the unit sphere still encodes
// a unit normal. An authoring tool produces that drift whenever it resamples a
// normal map with any filter wider than a box.
func (s *Surface) gatherNormal(bx, by int, xs, ys *[16]float64) {
	for row := 0; row < 4; row++ {
		y := s.clampY(by*4 + row)
		for col := 0; col < 4; col++ {
			x := s.clampX(bx*4 + col)
			i := (y*s.Width + x) * 4
			nx, ny, _ := decodeNormal(s.Pix[i], s.Pix[i+1], s.Pix[i+2])
			xs[row*4+col] = float64(encodeUnorm8(float32(nx*0.5 + 0.5)))
			ys[row*4+col] = float64(encodeUnorm8(float32(ny*0.5 + 0.5)))
		}
	}
}

// NormalizeNormals rewrites every texel of s so it encodes a unit normal.
//
// The function reads and writes the usual n*0.5+0.5 encoding in the three colour
// channels and leaves alpha alone. EncodeBC5Normal normalizes each block on its
// own, so a caller needs this function only to normalize a surface it keeps for
// another purpose, such as a reference measurement.
func NormalizeNormals(s *Surface) {
	if s == nil {
		return
	}
	for i := 0; i+3 < len(s.Pix); i += 4 {
		x, y, z := decodeNormal(s.Pix[i], s.Pix[i+1], s.Pix[i+2])
		s.Pix[i] = float32(x*0.5 + 0.5)
		s.Pix[i+1] = float32(y*0.5 + 0.5)
		s.Pix[i+2] = float32(z*0.5 + 0.5)
	}
}

// decodeNormal turns one encoded texel into a unit vector.
//
// A texel of exactly (0.5, 0.5, 0.5) decodes to a zero vector, which has no
// direction. The function returns the flat normal (0, 0, 1) for that case,
// because a flat normal is what a normal map means by "no perturbation".
func decodeNormal(r, g, b float32) (float64, float64, float64) {
	x := float64(r)*2 - 1
	y := float64(g)*2 - 1
	z := float64(b)*2 - 1
	length := math.Sqrt(x*x + y*y + z*z)
	if length < 1e-9 {
		return 0, 0, 1
	}
	return x / length, y / length, z / length
}

// ReferenceCodes returns the 8-bit codes an uncompressed upload of s would hold.
//
// It is the reference of every PSNR this package reports. Comparing a decoded
// payload against it isolates the block error from the 8-bit quantization that
// both an uncompressed and a compressed upload pay.
func ReferenceCodes(s *Surface, t Transfer) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if !t.valid() {
		return nil, fmt.Errorf("%w: got %d", ErrTransfer, int(t))
	}
	out := make([]byte, s.Width*s.Height*4)
	for pixel := 0; pixel < s.Width*s.Height; pixel++ {
		src := pixel * 4
		out[src] = t.encode(s.Pix[src])
		out[src+1] = t.encode(s.Pix[src+1])
		out[src+2] = t.encode(s.Pix[src+2])
		out[src+3] = encodeUnorm8(s.Pix[src+3])
	}
	return out, nil
}

// workerCount resolves the Workers option.
//
// Zero or one runs on the calling goroutine. A negative value asks for one
// worker for each processor. The default stays single-threaded because an asset
// pipeline usually runs many textures at once already, and nested parallelism
// only fights over the same cores.
func workerCount(workers, rows int) int {
	if workers < 0 {
		workers = runtime.NumCPU()
	}
	if workers < 1 {
		workers = 1
	}
	if workers > rows {
		workers = rows
	}
	return workers
}

// encodeBlocks walks every block of s and lets encode fill its payload.
//
// The output does not depend on the worker count, because each block writes only
// its own bytes and reads only the source.
func encodeBlocks(s *Surface, block, workers int, encode func(bx, by int, dst []byte)) []byte {
	across := BlocksAcross(s.Width)
	down := BlocksAcross(s.Height)
	stride := across * block
	out := make([]byte, stride*down)

	run := func(by int) {
		row := out[by*stride : (by+1)*stride]
		for bx := 0; bx < across; bx++ {
			encode(bx, by, row[bx*block:(bx+1)*block])
		}
	}

	count := workerCount(workers, down)
	if count == 1 {
		for by := 0; by < down; by++ {
			run(by)
		}
		return out
	}

	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(count)
	for w := 0; w < count; w++ {
		go func() {
			defer wg.Done()
			for {
				by := int(next.Add(1)) - 1
				if by >= down {
					return
				}
				run(by)
			}
		}()
	}
	wg.Wait()
	return out
}

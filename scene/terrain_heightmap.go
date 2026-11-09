package scene

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/png" // DecodeHeightmapPNG reads 8- and 16-bit PNG files.
	"io"
	"math"
	"sync"
)

// Heightmap is a grid of 16-bit height samples. Sample value 0 is the lowest
// height and 65535 the highest; Terrain.HeightScale turns the normalized value
// into meters. Data is row-major: Data[row*Columns+column]. Column 0 lies at
// the terrain's minimum X and row 0 at its minimum Z.
//
// Treat a Heightmap as immutable once a scene has lowered it. The lowering
// caches the wire encoding and the LOD error table on the value; build a new
// Heightmap to change the terrain.
type Heightmap struct {
	Columns int
	Rows    int
	Data    []uint16

	mu         sync.Mutex
	cache      *heightmapCache
	shorelines map[shorelineKey]*OceanBathymetry
}

// MaxHeightmapSamples bounds Columns*Rows. 4097x4097 is the largest grid a
// browser height texture can hold on every WebGL2 and WebGPU device.
const MaxHeightmapSamples = 4097 * 4097

var (
	errHeightmapSize = errors.New("scene: heightmap needs at least 2x2 samples and at most 4097x4097")
	errHeightmapData = errors.New("scene: heightmap data length does not match Columns*Rows")
)

// Validate reports whether the grid can describe a terrain.
func (h *Heightmap) Validate() error {
	if h == nil {
		return errors.New("scene: nil heightmap")
	}
	if h.Columns < 2 || h.Rows < 2 || h.Columns*h.Rows > MaxHeightmapSamples {
		return errHeightmapSize
	}
	if len(h.Data) != h.Columns*h.Rows {
		return errHeightmapData
	}
	return nil
}

// At returns the normalized height (0..1) of one sample. Coordinates outside
// the grid clamp to the edge.
func (h *Heightmap) At(column, row int) float64 {
	return float64(h.Data[h.index(column, row)]) / 65535
}

func (h *Heightmap) index(column, row int) int {
	if column < 0 {
		column = 0
	} else if column >= h.Columns {
		column = h.Columns - 1
	}
	if row < 0 {
		row = 0
	} else if row >= h.Rows {
		row = h.Rows - 1
	}
	return row*h.Columns + column
}

// interpolate returns the normalized height at fractional grid coordinates
// (gx along columns, gz along rows). Each grid cell is split into two
// triangles along the diagonal from (i, j) to (i+1, j+1); the browser mesh,
// the physics heightfield and this function all use the same split, so a
// height query matches what the player stands on.
func (h *Heightmap) interpolate(gx, gz float64) float64 {
	maxX := float64(h.Columns - 1)
	maxZ := float64(h.Rows - 1)
	gx = clampFloat(gx, 0, maxX)
	gz = clampFloat(gz, 0, maxZ)
	i := int(math.Floor(gx))
	j := int(math.Floor(gz))
	if i >= h.Columns-1 {
		i = h.Columns - 2
	}
	if j >= h.Rows-1 {
		j = h.Rows - 2
	}
	fx := gx - float64(i)
	fz := gz - float64(j)
	h00 := h.At(i, j)
	h10 := h.At(i+1, j)
	h01 := h.At(i, j+1)
	h11 := h.At(i+1, j+1)
	return triangleHeight(h00, h10, h01, h11, fx, fz)
}

// triangleHeight interpolates one grid cell with the shared diagonal split.
func triangleHeight(h00, h10, h01, h11, fx, fz float64) float64 {
	if fx >= fz {
		return h00 + float64(fx*(h10-h00)) + float64(fz*(h11-h10))
	}
	return h00 + float64(fz*(h01-h00)) + float64(fx*(h11-h01))
}

// sampleNormal returns the unit normal at one sample from central
// differences, given the world spacing between samples and the height scale.
// Edge samples use one-sided differences.
func (h *Heightmap) sampleNormal(column, row int, spacingX, spacingZ, heightScale float64) Vector3 {
	left, right := column-1, column+1
	if left < 0 {
		left = 0
	}
	if right >= h.Columns {
		right = h.Columns - 1
	}
	down, up := row-1, row+1
	if down < 0 {
		down = 0
	}
	if up >= h.Rows {
		up = h.Rows - 1
	}
	dx := float64(right-left) * spacingX
	dz := float64(up-down) * spacingZ
	dhdx := float64((h.At(right, row)-h.At(left, row))*heightScale) / dx
	dhdz := float64((h.At(column, up)-h.At(column, down))*heightScale) / dz
	return terrainNormalize(Vector3{X: -dhdx, Y: 1, Z: -dhdz})
}

// HeightmapFromFunc samples f over a columns x rows grid. u and v run from 0
// at the first sample to 1 at the last. f returns a normalized height; values
// outside 0..1 clamp.
func HeightmapFromFunc(columns, rows int, f func(u, v float64) float64) (*Heightmap, error) {
	h := &Heightmap{Columns: columns, Rows: rows}
	if columns < 2 || rows < 2 || columns*rows > MaxHeightmapSamples {
		return nil, errHeightmapSize
	}
	h.Data = make([]uint16, columns*rows)
	for row := 0; row < rows; row++ {
		v := float64(row) / float64(rows-1)
		for column := 0; column < columns; column++ {
			u := float64(column) / float64(columns-1)
			h.Data[row*columns+column] = quantizeHeight(f(u, v))
		}
	}
	return h, nil
}

func quantizeHeight(value float64) uint16 {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if value >= 1 {
		return 65535
	}
	return uint16(math.Round(value * 65535))
}

// HeightmapFromRaw reads a headerless 16-bit heightmap (the ".r16" / ".raw"
// export of most terrain tools): columns*rows samples, two bytes each, in the
// given byte order.
func HeightmapFromRaw(data []byte, columns, rows int, order binary.ByteOrder) (*Heightmap, error) {
	if columns < 2 || rows < 2 || columns*rows > MaxHeightmapSamples {
		return nil, errHeightmapSize
	}
	if len(data) != columns*rows*2 {
		return nil, fmt.Errorf("scene: raw heightmap is %d bytes, want %d for %dx%d 16-bit samples", len(data), columns*rows*2, columns, rows)
	}
	if order == nil {
		order = binary.LittleEndian
	}
	h := &Heightmap{Columns: columns, Rows: rows, Data: make([]uint16, columns*rows)}
	for i := range h.Data {
		h.Data[i] = order.Uint16(data[i*2:])
	}
	return h, nil
}

// DecodeHeightmapPNG reads a grayscale PNG. A 16-bit file keeps every bit; an
// 8-bit or color file uses its luminance scaled to 16 bits. The image's top
// row becomes the terrain's minimum-Z row.
func DecodeHeightmapPNG(r io.Reader) (*Heightmap, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("scene: decode heightmap: %w", err)
	}
	bounds := img.Bounds()
	columns, rows := bounds.Dx(), bounds.Dy()
	if columns < 2 || rows < 2 || columns*rows > MaxHeightmapSamples {
		return nil, errHeightmapSize
	}
	h := &Heightmap{Columns: columns, Rows: rows, Data: make([]uint16, columns*rows)}
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			var value uint16
			switch typed := img.(type) {
			case *image.Gray16:
				value = typed.Gray16At(bounds.Min.X+column, bounds.Min.Y+row).Y
			case *image.Gray:
				value = uint16(typed.GrayAt(bounds.Min.X+column, bounds.Min.Y+row).Y) * 257
			default:
				value = color.Gray16Model.Convert(img.At(bounds.Min.X+column, bounds.Min.Y+row)).(color.Gray16).Y
			}
			h.Data[row*columns+column] = value
		}
	}
	return h, nil
}

// TerrainNoise describes a seeded fractal heightmap. The same values produce
// the same samples on every platform: the generator uses integer hashing and
// rounds every product explicitly, so no compiler can fuse a multiply-add.
type TerrainNoise struct {
	Seed uint64
	// Columns and Rows are the sample counts. Zero means 257.
	Columns int
	Rows    int
	// Frequency is the number of base features across the grid. Zero means 3.
	Frequency float64
	// Octaves is the number of noise layers, 1..10. Zero means 6.
	Octaves int
	// Lacunarity is the frequency ratio between octaves. Zero means 2.
	Lacunarity float64
	// Gain is the amplitude ratio between octaves, 0..1. Zero means 0.5.
	Gain float64
	// Ridged blends toward ridged noise (sharp crests), 0..1.
	Ridged float64
	// Island lowers the edges toward zero, 0..1. 1 gives a single island
	// whose shore lies about two thirds of the way to the border.
	Island float64
	// Exponent reshapes the normalized heights: values above 1 widen valleys
	// and sharpen peaks. Zero means 1.
	Exponent float64
}

// GenerateHeightmap builds a deterministic fractal heightmap normalized to
// the full 16-bit range.
func GenerateHeightmap(n TerrainNoise) *Heightmap {
	columns := n.Columns
	if columns <= 0 {
		columns = 257
	}
	rows := n.Rows
	if rows <= 0 {
		rows = 257
	}
	if columns < 2 {
		columns = 2
	}
	if rows < 2 {
		rows = 2
	}
	for columns*rows > MaxHeightmapSamples {
		columns = (columns + 1) / 2
		rows = (rows + 1) / 2
	}
	frequency := n.Frequency
	if frequency <= 0 {
		frequency = 3
	}
	octaves := n.Octaves
	if octaves <= 0 {
		octaves = 6
	}
	if octaves > 10 {
		octaves = 10
	}
	lacunarity := n.Lacunarity
	if lacunarity <= 0 {
		lacunarity = 2
	}
	gain := n.Gain
	if gain <= 0 || gain > 1 {
		gain = 0.5
	}
	ridged := clampFloat(n.Ridged, 0, 1)
	island := clampFloat(n.Island, 0, 1)
	exponent := n.Exponent
	if exponent <= 0 {
		exponent = 1
	}
	values := make([]float64, columns*rows)
	low, high := math.Inf(1), math.Inf(-1)
	for row := 0; row < rows; row++ {
		v := float64(row) / float64(rows-1)
		for column := 0; column < columns; column++ {
			u := float64(column) / float64(columns-1)
			sum, amplitude, total := 0.0, 1.0, 0.0
			freq := frequency
			for octave := 0; octave < octaves; octave++ {
				sample := valueNoise2D(n.Seed, uint64(octave), float64(u*freq), float64(v*freq))
				if ridged > 0 {
					crest := 1 - math.Abs(float64(2*sample)-1)
					sample = sample + float64(ridged*(float64(crest*crest)-sample))
				}
				sum += float64(sample * amplitude)
				total += amplitude
				amplitude = float64(amplitude * gain)
				freq = float64(freq * lacunarity)
			}
			value := sum / total
			if island > 0 {
				dx := float64(2*u) - 1
				dz := float64(2*v) - 1
				distance := math.Sqrt(float64(dx*dx) + float64(dz*dz))
				falloff := smoothstepFloat(0.35, 1.0, distance)
				value = float64(value * (1 - float64(island*falloff)))
			}
			values[row*columns+column] = value
			low = math.Min(low, value)
			high = math.Max(high, value)
		}
	}
	h := &Heightmap{Columns: columns, Rows: rows, Data: make([]uint16, columns*rows)}
	span := high - low
	for i, value := range values {
		normalized := 0.0
		if span > 0 {
			normalized = (value - low) / span
		}
		if exponent != 1 {
			normalized = math.Pow(normalized, exponent)
		}
		h.Data[i] = quantizeHeight(normalized)
	}
	return h
}

// valueNoise2D is smooth value noise in [0, 1) with a seeded integer lattice.
func valueNoise2D(seed, salt uint64, x, z float64) float64 {
	ix := math.Floor(x)
	iz := math.Floor(z)
	fx := x - ix
	fz := z - iz
	ux := float64(fx*fx) * (3 - float64(2*fx))
	uz := float64(fz*fz) * (3 - float64(2*fz))
	cx, cz := int64(ix), int64(iz)
	a := latticeValue(seed, salt, cx, cz)
	b := latticeValue(seed, salt, cx+1, cz)
	c := latticeValue(seed, salt, cx, cz+1)
	d := latticeValue(seed, salt, cx+1, cz+1)
	top := a + float64(ux*(b-a))
	bottom := c + float64(ux*(d-c))
	return top + float64(uz*(bottom-top))
}

func latticeValue(seed, salt uint64, x, z int64) float64 {
	h := mix64(seed ^ mix64(salt+0x9e3779b97f4a7c15) ^ mix64(uint64(x)*0xbf58476d1ce4e5b9) ^ mix64(uint64(z)*0x94d049bb133111eb+0x2545f4914f6cdd1d))
	return float64(h>>11) / float64(uint64(1)<<53)
}

// mix64 is the splitmix64 finalizer.
func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

func clampFloat(value, minimum, maximum float64) float64 {
	if math.IsNaN(value) {
		return minimum
	}
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func smoothstepFloat(edge0, edge1, x float64) float64 {
	if edge1 == edge0 {
		if x < edge0 {
			return 0
		}
		return 1
	}
	t := clampFloat((x-edge0)/(edge1-edge0), 0, 1)
	return float64(t*t) * (3 - float64(2*t))
}

// terrainNormalize returns v with unit length, or straight up for a zero
// vector, so a terrain normal is never zero.
func terrainNormalize(v Vector3) Vector3 {
	length := math.Sqrt(float64(v.X*v.X) + float64(v.Y*v.Y) + float64(v.Z*v.Z))
	if length == 0 {
		return Vector3{Y: 1}
	}
	return Vector3{X: v.X / length, Y: v.Y / length, Z: v.Z / length}
}

// heightmapCache holds the derived data one lowering computes for a
// heightmap and a world size.
type heightmapCache struct {
	key     *uint16
	length  int
	sizeX   float64
	sizeZ   float64
	heights string
	minimum uint16
	maximum uint16
	lod     terrainLODTable
}

// derived returns the cached wire encoding, value range and LOD error table
// for one world size, computing them on first use.
func (h *Heightmap) derived(sizeX, sizeZ float64) *heightmapCache {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := &h.Data[0]
	if c := h.cache; c != nil && c.key == key && c.length == len(h.Data) && c.sizeX == sizeX && c.sizeZ == sizeZ {
		return c
	}
	c := &heightmapCache{key: key, length: len(h.Data), sizeX: sizeX, sizeZ: sizeZ}
	raw := make([]byte, len(h.Data)*2)
	c.minimum, c.maximum = 65535, 0
	for i, value := range h.Data {
		binary.LittleEndian.PutUint16(raw[i*2:], value)
		if value < c.minimum {
			c.minimum = value
		}
		if value > c.maximum {
			c.maximum = value
		}
	}
	c.heights = base64.StdEncoding.EncodeToString(raw)
	c.lod = computeTerrainLOD(h, sizeX, sizeZ)
	h.cache = c
	return c
}

package scene

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
)

// OceanShoreline turns on the terrain-aware shore. Set it on Ocean.Shoreline
// with Environment.Terrain set and Ocean.Bathymetry left nil. At lowering time
// the scene bakes, from the terrain and Ocean.Level, one RGBA texture over the
// terrain rectangle and installs it as Ocean.Bathymetry:
//
//   - R is the seabed height (the terrain), signed-sqrt encoded, so the ocean's
//     existing shallows, breakers and run-up follow the real coast;
//   - G is the signed distance to the waterline: 0.5 at the shore, 0 at
//     Distance meters out to sea and 1 at Distance meters inland.
//
// The ocean then draws a foam band FoamWidth meters wide that hugs the
// waterline at any water level, makes the surf zone murkier (its absorption
// length shrinks to Turbidity times Ocean.Clarity near the shore, recovering
// over three foam widths), and draws no water over dry land. Both renderers
// read the same texture. BakeShoreline exposes the bake directly.
type OceanShoreline struct {
	// Resolution is the texture side in texels, 32..1024. Zero means 256.
	Resolution int `json:"resolution,omitempty"`
	// Distance is how far the distance field reaches on each side of the
	// shore, in meters. Zero means 32.
	Distance float64 `json:"distance,omitempty"`
	// FoamWidth is the width of the waterline foam band in meters. Zero means 3.
	FoamWidth float64 `json:"foamWidth,omitempty"`
	// Turbidity is the surf zone's clarity factor, 0.05..1; lower is murkier.
	// Zero means 0.35.
	Turbidity float64 `json:"turbidity,omitempty"`
}

func normalizeOceanShoreline(s *OceanShoreline) *OceanShoreline {
	if s == nil {
		return nil
	}
	out := *s
	if out.Resolution <= 0 {
		out.Resolution = 256
	}
	out.Resolution = min(max(out.Resolution, 32), 1024)
	if !(out.Distance > 0) || math.IsInf(out.Distance, 0) {
		out.Distance = 32
	}
	if !(out.FoamWidth > 0) || math.IsInf(out.FoamWidth, 0) {
		out.FoamWidth = 3
	}
	if !(out.Turbidity > 0) || math.IsInf(out.Turbidity, 0) {
		out.Turbidity = 0.35
	}
	out.Turbidity = clampFloat(out.Turbidity, 0.05, 1)
	return &out
}

// shorelineOcean installs the baked shoreline as the ocean's bathymetry when
// the ocean asks for one, has no authored bathymetry, and a terrain is set.
func shorelineOcean(ocean *Ocean, terrain *Terrain) *Ocean {
	if ocean == nil || ocean.Shoreline == nil || ocean.Bathymetry != nil || terrain == nil {
		return ocean
	}
	if baked, err := BakeShoreline(terrain, ocean.Level, *ocean.Shoreline); err == nil {
		ocean.Bathymetry = baked
	}
	return ocean
}

type shorelineKey struct {
	level, distance, foam, turbidity float64
	resolution                       int
	sizeX, sizeZ, baseY, scale       float64
}

// BakeShoreline bakes the shoreline bathymetry for a terrain and water level.
// See OceanShoreline for the texture contract.
func BakeShoreline(t *Terrain, level float64, shoreline OceanShoreline) (*OceanBathymetry, error) {
	f, ok := t.frame()
	if !ok {
		return nil, errTerrainHeightmap
	}
	if math.IsNaN(level) || math.IsInf(level, 0) {
		return nil, errors.New("scene: shoreline water level must be finite")
	}
	s := normalizeOceanShoreline(&shoreline)
	key := shorelineKey{level: level, distance: s.Distance, foam: s.FoamWidth, turbidity: s.Turbidity, resolution: s.Resolution,
		sizeX: f.sizeX, sizeZ: f.sizeZ, baseY: f.baseY, scale: f.scale}
	h := f.heightmap
	h.mu.Lock()
	if cached, found := h.shorelines[key]; found {
		h.mu.Unlock()
		out := *cached
		out.MinX, out.MinZ, out.MaxX, out.MaxZ = f.minX, f.minZ, f.minX+f.sizeX, f.minZ+f.sizeZ
		return &out, nil
	}
	h.mu.Unlock()

	n := s.Resolution
	stepX, stepZ := f.sizeX/float64(n), f.sizeZ/float64(n)
	heights := make([]float64, n*n)
	land := make([]bool, n*n)
	low, high := math.Inf(1), math.Inf(-1)
	for j := 0; j < n; j++ {
		z := f.minZ + float64((float64(j)+0.5)*stepZ)
		for i := 0; i < n; i++ {
			x := f.minX + float64((float64(i)+0.5)*stepX)
			y := f.height(x, z)
			heights[j*n+i] = y
			land[j*n+i] = y > level
			low, high = math.Min(low, y), math.Max(high, y)
		}
	}
	toLand := squaredDistanceField(land, n, stepX, stepZ, true)
	toWater := squaredDistanceField(land, n, stepX, stepZ, false)
	rangeY := math.Max(math.Max(math.Abs(low), math.Abs(high)), 1e-6)
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			k := j*n + i
			// Positive on land, negative at sea; half a texel puts the zero on
			// the boundary between a land and a water texel.
			d := -math.Sqrt(toLand[k]) + float64(0.5*math.Min(stepX, stepZ))
			if land[k] {
				d = math.Sqrt(toWater[k]) - float64(0.5*math.Min(stepX, stepZ))
			}
			g := clampFloat(0.5+d/(2*s.Distance), 0, 1)
			sq := math.Copysign(math.Sqrt(math.Abs(heights[k])/rangeY), heights[k])
			img.SetNRGBA(i, j, color.NRGBA{
				R: uint8(math.Round(float64(clampFloat((sq+1)/2, 0, 1) * 255))),
				G: uint8(math.Round(float64(g * 255))),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
		return nil, err
	}
	out := &OceanBathymetry{
		Src:            "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
		MinX:           f.minX,
		MinZ:           f.minZ,
		MaxX:           f.minX + f.sizeX,
		MaxZ:           f.minZ + f.sizeZ,
		MinHeight:      -rangeY,
		MaxHeight:      rangeY,
		Encoding:       "signed-sqrt",
		ShoreDistance:  s.Distance,
		ShoreFoam:      s.FoamWidth,
		ShoreTurbidity: s.Turbidity,
	}
	h.mu.Lock()
	if h.shorelines == nil {
		h.shorelines = map[shorelineKey]*OceanBathymetry{}
	}
	if len(h.shorelines) > 8 {
		clear(h.shorelines)
	}
	cached := *out
	h.shorelines[key] = &cached
	h.mu.Unlock()
	return out, nil
}

// squaredDistanceField returns, per texel, the squared distance in meters to
// the nearest texel whose land flag equals target (zero on such texels). It is
// the exact two-pass transform of Felzenszwalb and Huttenlocher (2012).
func squaredDistanceField(land []bool, n int, stepX, stepZ float64, target bool) []float64 {
	const far = 1e20
	grid := make([]float64, n*n)
	for k, isLand := range land {
		if isLand == target {
			grid[k] = 0
		} else {
			grid[k] = far
		}
	}
	line := make([]float64, n)
	out := make([]float64, n)
	v := make([]int, n)
	z := make([]float64, n+1)
	for j := 0; j < n; j++ {
		copy(line, grid[j*n:(j+1)*n])
		distanceTransform1D(line, out, v, z, stepX)
		copy(grid[j*n:(j+1)*n], out)
	}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			line[j] = grid[j*n+i]
		}
		distanceTransform1D(line, out, v, z, stepZ)
		for j := 0; j < n; j++ {
			grid[j*n+i] = out[j]
		}
	}
	return grid
}

// distanceTransform1D is the lower envelope of parabolas f(q) + (p-q)^2 with
// sample positions q * step.
func distanceTransform1D(f, out []float64, v []int, z []float64, step float64) {
	n := len(f)
	pos := func(q int) float64 { return float64(float64(q) * step) }
	intersect := func(q, r int) float64 {
		return ((f[q] + float64(pos(q)*pos(q))) - (f[r] + float64(pos(r)*pos(r)))) / (2 * (pos(q) - pos(r)))
	}
	k := 0
	v[0] = 0
	z[0], z[1] = math.Inf(-1), math.Inf(1)
	for q := 1; q < n; q++ {
		s := intersect(q, v[k])
		for s <= z[k] {
			k--
			s = intersect(q, v[k])
		}
		k++
		v[k] = q
		z[k] = s
		z[k+1] = math.Inf(1)
	}
	k = 0
	for q := 0; q < n; q++ {
		for z[k+1] < pos(q) {
			k++
		}
		d := pos(q) - pos(v[k])
		out[q] = float64(d*d) + f[v[k]]
	}
}

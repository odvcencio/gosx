package scene

import (
	"encoding/base64"
	"errors"
	"math"
	"strings"

	"m31labs.dev/gosx/det"
)

// Terrain is a heightmap ground surface for large outdoor scenes. Set it on
// Environment.Terrain; a scene has at most one.
//
// The browser draws it as a quadtree of grid patches (CDLOD). Each frame it
// picks, per patch, the coarsest level whose geometric error projects to at
// most PixelError pixels, and vertices near a level boundary morph onto the
// coarser grid, so neighbouring patches always share their edge vertices and
// no cracks open while the camera moves. Lighting uses a per-texel normal
// map, so slopes shade the same at every level. Terrain receives the scene's
// shadows, fog and image-based lighting on WebGPU and WebGL2; WebGL2 uses a
// coarser error target.
//
// HeightAt, NormalAt and Sample answer height queries for gameplay on the same
// triangulated surface the physics collider (ColliderConfig) and the finest
// render level use.
type Terrain struct {
	// ID names the terrain. Empty means "terrain".
	ID string
	// Heightmap is the height source: decode a 16-bit PNG
	// (DecodeHeightmapPNG), read raw samples (HeightmapFromRaw), sample a
	// function (HeightmapFromFunc), or generate one (GenerateHeightmap).
	// Required.
	Heightmap *Heightmap
	// Src optionally names a URL of the same samples as a headerless
	// little-endian 16-bit file (HeightmapFromRaw's input). The browser then
	// fetches that file instead of reading the samples inline from the page,
	// which keeps large terrains out of the scene payload. Heightmap is still
	// required: the server uses it for queries, LOD and shoreline baking.
	Src string
	// Position is the world position of the terrain's center at sample value
	// zero. Position.Y is therefore the height of the lowest possible sample.
	Position Vector3
	// Width and Depth are the world extent along X and Z in meters. Zero
	// Width means 256; zero Depth means Width.
	Width float64
	Depth float64
	// HeightScale is the world height, in meters, of sample value 65535.
	// Zero means 32.
	HeightScale float64
	// PixelError is the largest screen-space geometric error, in pixels, the
	// browser accepts before it refines a patch. Zero means 1. WebGL2 doubles
	// it.
	PixelError float64
	// Layers are up to four surface materials painted in order: layer 0
	// covers everything, and each later layer covers the earlier ones where
	// its height, slope and splat masks allow. Nil gives one grass-colored
	// layer.
	Layers []TerrainLayer
	// Splat optionally paints the masks of layers 1, 2 and 3 from its R, G
	// and B channels. The splat grid spans the whole terrain at any
	// resolution.
	Splat *SplatMap
	// TriplanarSlope is the slope, in degrees, above which layer textures
	// switch from top-down to triplanar projection so cliffs do not stretch.
	// Zero means 35. A negative value keeps top-down projection everywhere.
	TriplanarSlope float64
	// CastShadow and ReceiveShadow default to true.
	CastShadow    *bool
	ReceiveShadow *bool
}

// TerrainLayer is one splat material. Masks multiply: a layer shows where the
// height mask, the slope mask and (when a SplatMap is set) its splat channel
// are all non-zero.
type TerrainLayer struct {
	// Color is an sRGB hex tint. It multiplies Albedo when both are set.
	// Empty means white with a texture and mid grey without one.
	Color string
	// Albedo is an optional sRGB color texture URL.
	Albedo string
	// Normal is an optional tangent-space normal map URL.
	Normal string
	// Roughness is 0..1. Zero means 0.9.
	Roughness float64
	// Scale is the world size, in meters, of one texture repeat. Zero means 4.
	Scale float64
	// MinHeight and MaxHeight bound the layer in world Y. Nil leaves that side
	// open.
	MinHeight *float64
	MaxHeight *float64
	// MinSlope and MaxSlope bound the layer by slope in degrees, 0 (flat) to
	// 90 (vertical). Zero MaxSlope means 90.
	MinSlope float64
	MaxSlope float64
	// HeightBlend is the width, in meters, of the fade at a height bound.
	// Zero means 2.
	HeightBlend float64
	// SlopeBlend is the width, in degrees, of the fade at a slope bound.
	// Zero means 6.
	SlopeBlend float64
}

// SplatMap holds RGBA8 layer masks over the whole terrain: R paints layer 1,
// G layer 2 and B layer 3. A is reserved. Data is row-major with four bytes
// per texel, row 0 at the terrain's minimum Z.
type SplatMap struct {
	Columns int
	Rows    int
	Data    []byte
}

// TerrainMaxLayers is the number of splat layers a terrain draws.
const TerrainMaxLayers = 4

// terrainNodeQuads is the grid size of one CDLOD node. The browser draws a
// node as four quarter patches of terrainNodeQuads/2 quads. It must match
// SCENE_TERRAIN_NODE_QUADS in client/runtime/scene3d/terrain.ts.
const terrainNodeQuads = 32

// terrainMaxLevels bounds the quadtree depth.
const terrainMaxLevels = 12

const (
	defaultTerrainSize           = 256.0
	defaultTerrainHeightScale    = 32.0
	defaultTerrainPixelError     = 1.0
	defaultTerrainTriplanarSlope = 35.0
	defaultTerrainLayerColor     = "#6f7d4c"
	terrainOpenHeight            = 1e6
)

// TerrainSample is the result of one height query.
type TerrainSample struct {
	// Height is the world Y of the surface.
	Height float64
	// Normal is the smooth unit surface normal (interpolated from per-sample
	// normals), the one the browser shades with.
	Normal Vector3
	// Slope is the angle between Normal and straight up, in degrees.
	Slope float64
	// Inside reports whether (x, z) lies on the terrain. Outside, the query
	// clamps to the nearest edge.
	Inside bool
}

// terrainFrame is a terrain with every default resolved.
type terrainFrame struct {
	heightmap *Heightmap
	minX      float64
	minZ      float64
	sizeX     float64
	sizeZ     float64
	texelX    float64
	texelZ    float64
	baseY     float64
	scale     float64
}

var errTerrainHeightmap = errors.New("scene: terrain needs a Heightmap")

// Validate reports why the terrain cannot be drawn or queried.
func (t *Terrain) Validate() error {
	if t == nil || t.Heightmap == nil {
		return errTerrainHeightmap
	}
	if err := t.Heightmap.Validate(); err != nil {
		return err
	}
	for _, value := range []float64{t.Position.X, t.Position.Y, t.Position.Z, t.Width, t.Depth, t.HeightScale} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("scene: terrain position and size must be finite")
		}
	}
	if t.Width < 0 || t.Depth < 0 || t.HeightScale < 0 {
		return errors.New("scene: terrain Width, Depth and HeightScale must not be negative")
	}
	if t.Splat != nil {
		if t.Splat.Columns < 1 || t.Splat.Rows < 1 || len(t.Splat.Data) != t.Splat.Columns*t.Splat.Rows*4 {
			return errors.New("scene: terrain splat data must hold Columns*Rows RGBA texels")
		}
	}
	return nil
}

func (t *Terrain) frame() (terrainFrame, bool) {
	if t.Validate() != nil {
		return terrainFrame{}, false
	}
	sizeX := t.Width
	if sizeX <= 0 {
		sizeX = defaultTerrainSize
	}
	sizeZ := t.Depth
	if sizeZ <= 0 {
		sizeZ = sizeX
	}
	scale := t.HeightScale
	if scale <= 0 {
		scale = defaultTerrainHeightScale
	}
	h := t.Heightmap
	return terrainFrame{
		heightmap: h,
		minX:      t.Position.X - sizeX/2,
		minZ:      t.Position.Z - sizeZ/2,
		sizeX:     sizeX,
		sizeZ:     sizeZ,
		texelX:    sizeX / float64(h.Columns-1),
		texelZ:    sizeZ / float64(h.Rows-1),
		baseY:     t.Position.Y,
		scale:     scale,
	}, true
}

// Bounds returns the terrain's world XZ rectangle and its world height range
// (the lowest and highest sample actually present).
func (t *Terrain) Bounds() (minimum, maximum Vector3) {
	f, ok := t.frame()
	if !ok {
		return Vector3{}, Vector3{}
	}
	low, high := uint16(65535), uint16(0)
	for _, value := range f.heightmap.Data {
		low = min(low, value)
		high = max(high, value)
	}
	minimum = Vector3{X: f.minX, Y: f.baseY + float64(float64(low)/65535*f.scale), Z: f.minZ}
	maximum = Vector3{X: f.minX + f.sizeX, Y: f.baseY + float64(float64(high)/65535*f.scale), Z: f.minZ + f.sizeZ}
	return minimum, maximum
}

// Contains reports whether the world point (x, z) lies on the terrain.
func (t *Terrain) Contains(x, z float64) bool {
	f, ok := t.frame()
	return ok && x >= f.minX && x <= f.minX+f.sizeX && z >= f.minZ && z <= f.minZ+f.sizeZ
}

// HeightAt returns the world Y of the surface at (x, z). Outside the terrain
// it clamps to the nearest edge. An invalid terrain returns 0.
func (t *Terrain) HeightAt(x, z float64) float64 {
	f, ok := t.frame()
	if !ok {
		return 0
	}
	return f.height(x, z)
}

// NormalAt returns the smooth unit surface normal at (x, z).
func (t *Terrain) NormalAt(x, z float64) Vector3 {
	f, ok := t.frame()
	if !ok {
		return Vector3{Y: 1}
	}
	return f.normal(x, z)
}

// Sample returns the height, normal and slope at (x, z) in one query.
func (t *Terrain) Sample(x, z float64) TerrainSample {
	f, ok := t.frame()
	if !ok {
		return TerrainSample{Normal: Vector3{Y: 1}}
	}
	normal := f.normal(x, z)
	return TerrainSample{
		Height: f.height(x, z),
		Normal: normal,
		Slope:  slopeDegrees(normal),
		Inside: x >= f.minX && x <= f.minX+f.sizeX && z >= f.minZ && z <= f.minZ+f.sizeZ,
	}
}

func (f terrainFrame) grid(x, z float64) (float64, float64) {
	return (x - f.minX) / f.texelX, (z - f.minZ) / f.texelZ
}

func (f terrainFrame) height(x, z float64) float64 {
	gx, gz := f.grid(x, z)
	return f.baseY + float64(f.heightmap.interpolate(gx, gz)*f.scale)
}

// normal interpolates the four surrounding per-sample normals bilinearly and
// renormalizes, which is what the browser's filtered normal texture does.
func (f terrainFrame) normal(x, z float64) Vector3 {
	h := f.heightmap
	gx, gz := f.grid(x, z)
	gx = clampFloat(gx, 0, float64(h.Columns-1))
	gz = clampFloat(gz, 0, float64(h.Rows-1))
	i := min(int(math.Floor(gx)), h.Columns-2)
	j := min(int(math.Floor(gz)), h.Rows-2)
	fx := gx - float64(i)
	fz := gz - float64(j)
	n00 := h.sampleNormal(i, j, f.texelX, f.texelZ, f.scale)
	n10 := h.sampleNormal(i+1, j, f.texelX, f.texelZ, f.scale)
	n01 := h.sampleNormal(i, j+1, f.texelX, f.texelZ, f.scale)
	n11 := h.sampleNormal(i+1, j+1, f.texelX, f.texelZ, f.scale)
	lerp := func(a, b Vector3, t float64) Vector3 {
		return Vector3{X: a.X + float64(t*(b.X-a.X)), Y: a.Y + float64(t*(b.Y-a.Y)), Z: a.Z + float64(t*(b.Z-a.Z))}
	}
	return terrainNormalize(lerp(lerp(n00, n10, fx), lerp(n01, n11, fx), fz))
}

func slopeDegrees(normal Vector3) float64 {
	return float64(det.Acos(clampFloat(normal.Y, -1, 1))*180) / math.Pi
}

// TerrainIR is the wire form of Terrain under environment.terrain. Heights
// carries the samples as base64 little-endian uint16 unless Src names a file.
type TerrainIR struct {
	ID             string           `json:"id"`
	Columns        int              `json:"columns"`
	Rows           int              `json:"rows"`
	Heights        string           `json:"heights,omitempty"`
	Src            string           `json:"src,omitempty"`
	MinX           float64          `json:"minX"`
	MinZ           float64          `json:"minZ"`
	SizeX          float64          `json:"sizeX"`
	SizeZ          float64          `json:"sizeZ"`
	BaseY          float64          `json:"baseY"`
	HeightScale    float64          `json:"heightScale"`
	MinHeight      float64          `json:"minHeight"`
	MaxHeight      float64          `json:"maxHeight"`
	Levels         int              `json:"levels"`
	Spacing        float64          `json:"spacing"`
	LODErrors      []float64        `json:"lodErrors"`
	PixelError     float64          `json:"pixelError"`
	TriplanarSlope float64          `json:"triplanarSlope"`
	Layers         []TerrainLayerIR `json:"layers"`
	Splat          *TerrainSplatIR  `json:"splat,omitempty"`
	CastShadow     bool             `json:"castShadow"`
	ReceiveShadow  bool             `json:"receiveShadow"`
}

// TerrainLayerIR is one resolved layer: every default applied, open height
// bounds widened to ±1e6 m.
type TerrainLayerIR struct {
	Color       string  `json:"color"`
	Albedo      string  `json:"albedo,omitempty"`
	Normal      string  `json:"normal,omitempty"`
	Roughness   float64 `json:"roughness"`
	Scale       float64 `json:"scale"`
	MinHeight   float64 `json:"minHeight"`
	MaxHeight   float64 `json:"maxHeight"`
	MinSlope    float64 `json:"minSlope"`
	MaxSlope    float64 `json:"maxSlope"`
	HeightBlend float64 `json:"heightBlend"`
	SlopeBlend  float64 `json:"slopeBlend"`
}

// TerrainSplatIR carries a SplatMap as base64 RGBA8.
type TerrainSplatIR struct {
	Columns int    `json:"columns"`
	Rows    int    `json:"rows"`
	Data    string `json:"data"`
}

// SceneIR lowers the terrain to its wire form. An invalid terrain lowers to
// nil, so the browser draws nothing instead of a broken surface.
func (t *Terrain) SceneIR() *TerrainIR {
	f, ok := t.frame()
	if !ok {
		return nil
	}
	h := f.heightmap
	derived := h.derived(f.sizeX, f.sizeZ)
	id := strings.TrimSpace(t.ID)
	if id == "" {
		id = "terrain"
	}
	pixelError := t.PixelError
	if pixelError <= 0 || math.IsNaN(pixelError) {
		pixelError = defaultTerrainPixelError
	}
	pixelError = clampFloat(pixelError, 0.25, 16)
	triplanar := t.TriplanarSlope
	switch {
	case triplanar == 0 || math.IsNaN(triplanar):
		triplanar = defaultTerrainTriplanarSlope
	case triplanar < 0:
		triplanar = 90
	default:
		triplanar = clampFloat(triplanar, 1, 90)
	}
	errorsMeters := make([]float64, len(derived.lod.errors))
	for i, value := range derived.lod.errors {
		errorsMeters[i] = float64(value * f.scale)
	}
	out := &TerrainIR{
		ID:             id,
		Columns:        h.Columns,
		Rows:           h.Rows,
		MinX:           f.minX,
		MinZ:           f.minZ,
		SizeX:          f.sizeX,
		SizeZ:          f.sizeZ,
		BaseY:          f.baseY,
		HeightScale:    f.scale,
		MinHeight:      f.baseY + float64(float64(derived.minimum)/65535*f.scale),
		MaxHeight:      f.baseY + float64(float64(derived.maximum)/65535*f.scale),
		Levels:         derived.lod.levels,
		Spacing:        derived.lod.spacing,
		LODErrors:      errorsMeters,
		PixelError:     pixelError,
		TriplanarSlope: triplanar,
		Layers:         lowerTerrainLayers(t.Layers),
		CastShadow:     t.CastShadow == nil || *t.CastShadow,
		ReceiveShadow:  t.ReceiveShadow == nil || *t.ReceiveShadow,
	}
	if src := strings.TrimSpace(t.Src); src != "" {
		out.Src = src
	} else {
		out.Heights = derived.heights
	}
	if t.Splat != nil {
		out.Splat = &TerrainSplatIR{Columns: t.Splat.Columns, Rows: t.Splat.Rows, Data: base64.StdEncoding.EncodeToString(t.Splat.Data)}
	}
	return out
}

func lowerTerrainLayers(layers []TerrainLayer) []TerrainLayerIR {
	if len(layers) == 0 {
		layers = []TerrainLayer{{Color: defaultTerrainLayerColor}}
	}
	if len(layers) > TerrainMaxLayers {
		layers = layers[:TerrainMaxLayers]
	}
	out := make([]TerrainLayerIR, 0, len(layers))
	for _, layer := range layers {
		color := strings.TrimSpace(layer.Color)
		albedo := strings.TrimSpace(layer.Albedo)
		if color == "" {
			if albedo != "" {
				color = "#ffffff"
			} else {
				color = "#808080"
			}
		}
		roughness := layer.Roughness
		if roughness <= 0 || math.IsNaN(roughness) {
			roughness = 0.9
		}
		scale := layer.Scale
		if scale <= 0 || math.IsNaN(scale) {
			scale = 4
		}
		minHeight, maxHeight := -terrainOpenHeight, terrainOpenHeight
		if layer.MinHeight != nil && !math.IsNaN(*layer.MinHeight) {
			minHeight = clampFloat(*layer.MinHeight, -terrainOpenHeight, terrainOpenHeight)
		}
		if layer.MaxHeight != nil && !math.IsNaN(*layer.MaxHeight) {
			maxHeight = clampFloat(*layer.MaxHeight, -terrainOpenHeight, terrainOpenHeight)
		}
		maxSlope := layer.MaxSlope
		if maxSlope <= 0 || math.IsNaN(maxSlope) {
			maxSlope = 90
		}
		heightBlend := layer.HeightBlend
		if heightBlend <= 0 || math.IsNaN(heightBlend) {
			heightBlend = 2
		}
		slopeBlend := layer.SlopeBlend
		if slopeBlend <= 0 || math.IsNaN(slopeBlend) {
			slopeBlend = 6
		}
		out = append(out, TerrainLayerIR{
			Color:       color,
			Albedo:      albedo,
			Normal:      strings.TrimSpace(layer.Normal),
			Roughness:   clampFloat(roughness, 0.02, 1),
			Scale:       scale,
			MinHeight:   minHeight,
			MaxHeight:   maxHeight,
			MinSlope:    clampFloat(layer.MinSlope, 0, 90),
			MaxSlope:    clampFloat(maxSlope, 0, 90),
			HeightBlend: heightBlend,
			SlopeBlend:  slopeBlend,
		})
	}
	return out
}

// terrainLODTable is the CDLOD level layout for one heightmap and world size.
type terrainLODTable struct {
	// levels is the quadtree depth; level 0 is the finest.
	levels int
	// spacing is the vertex spacing of level 0 in meters. Level i uses
	// spacing * 2^i. The root node spans terrainNodeQuads * spacing *
	// 2^(levels-1) meters from (minX, minZ).
	spacing float64
	// errors[i] is the largest vertical gap, in normalized height units,
	// between the full-resolution surface and the level-i grid.
	errors []float64
}

// computeTerrainLOD picks the level count so the finest grid is at least as
// dense as the heightmap, then measures each level's geometric error against
// the full-resolution surface at every sample.
func computeTerrainLOD(h *Heightmap, sizeX, sizeZ float64) terrainLODTable {
	texelX := sizeX / float64(h.Columns-1)
	texelZ := sizeZ / float64(h.Rows-1)
	texel := math.Min(texelX, texelZ)
	root := math.Max(sizeX, sizeZ)
	levels := 1
	for levels < terrainMaxLevels && root/(terrainNodeQuads*math.Ldexp(1, levels-1)) > texel*1.0001 {
		levels++
	}
	spacing := root / (terrainNodeQuads * math.Ldexp(1, levels-1))
	table := terrainLODTable{levels: levels, spacing: spacing, errors: make([]float64, levels)}
	surface := func(x, z float64) float64 {
		return h.interpolate(x/texelX, z/texelZ)
	}
	for level := 0; level < levels; level++ {
		step := float64(spacing * math.Ldexp(1, level))
		nx := int(math.Ceil(sizeX/step)) + 1
		nz := int(math.Ceil(sizeZ/step)) + 1
		grid := make([]float64, nx*nz)
		for b := 0; b < nz; b++ {
			for a := 0; a < nx; a++ {
				grid[b*nx+a] = surface(float64(float64(a)*step), float64(float64(b)*step))
			}
		}
		worst := 0.0
		for row := 0; row < h.Rows; row++ {
			z := float64(float64(row) * texelZ)
			gz := z / step
			b := min(int(math.Floor(gz)), nz-2)
			fz := gz - float64(b)
			for column := 0; column < h.Columns; column++ {
				x := float64(float64(column) * texelX)
				gx := x / step
				a := min(int(math.Floor(gx)), nx-2)
				fx := gx - float64(a)
				coarse := triangleHeight(grid[b*nx+a], grid[b*nx+a+1], grid[(b+1)*nx+a], grid[(b+1)*nx+a+1], fx, fz)
				if gap := math.Abs(h.At(column, row) - coarse); gap > worst {
					worst = gap
				}
			}
		}
		table.errors[level] = worst
	}
	return table
}

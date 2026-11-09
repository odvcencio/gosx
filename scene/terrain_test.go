package scene

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

// terrainNoiseGolden is the FNV-64a digest of the samples of the TerrainNoise
// in TestGenerateHeightmapIsDeterministic.
const terrainNoiseGolden = 0x84f7084bb2f6f583

func planeHeightmap(t *testing.T, columns, rows int, f func(u, v float64) float64) *Heightmap {
	t.Helper()
	h, err := HeightmapFromFunc(columns, rows, f)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestHeightmapValidate(t *testing.T) {
	cases := map[string]*Heightmap{
		"nil":       nil,
		"too small": {Columns: 1, Rows: 4, Data: make([]uint16, 4)},
		"short":     {Columns: 3, Rows: 3, Data: make([]uint16, 8)},
	}
	for name, h := range cases {
		if h.Validate() == nil {
			t.Errorf("%s: Validate accepted an invalid heightmap", name)
		}
	}
	if err := (&Heightmap{Columns: 2, Rows: 2, Data: make([]uint16, 4)}).Validate(); err != nil {
		t.Fatalf("valid 2x2 heightmap rejected: %v", err)
	}
}

func TestHeightmapFromRawByteOrder(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0xff, 0x00, 0x00, 0xff}
	little, err := HeightmapFromRaw(data, 2, 2, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint16{0x0201, 0x0403, 0x00ff, 0xff00}; !reflect.DeepEqual(little.Data, want) {
		t.Fatalf("little endian = %#x, want %#x", little.Data, want)
	}
	big, err := HeightmapFromRaw(data, 2, 2, binary.BigEndian)
	if err != nil {
		t.Fatal(err)
	}
	if big.Data[0] != 0x0102 {
		t.Fatalf("big endian first sample = %#x", big.Data[0])
	}
	if _, err := HeightmapFromRaw(data[:6], 2, 2, nil); err == nil {
		t.Fatal("short raw data accepted")
	}
}

func TestDecodeHeightmapPNGKeepsSixteenBits(t *testing.T) {
	img := image.NewGray16(image.Rect(0, 0, 3, 2))
	values := []uint16{0, 1, 65535, 1234, 40000, 65534}
	for i, value := range values {
		img.SetGray16(i%3, i/3, color.Gray16{Y: value})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	h, err := DecodeHeightmapPNG(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if h.Columns != 3 || h.Rows != 2 || !reflect.DeepEqual(h.Data, values) {
		t.Fatalf("decoded %dx%d %v, want 3x2 %v", h.Columns, h.Rows, h.Data, values)
	}

	gray := image.NewGray(image.Rect(0, 0, 2, 2))
	gray.SetGray(1, 1, color.Gray{Y: 255})
	buf.Reset()
	if err := png.Encode(&buf, gray); err != nil {
		t.Fatal(err)
	}
	h8, err := DecodeHeightmapPNG(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if h8.Data[3] != 65535 || h8.Data[0] != 0 {
		t.Fatalf("8-bit PNG did not scale to 16 bits: %v", h8.Data)
	}
}

func TestTerrainHeightAtMatchesPlaneExactly(t *testing.T) {
	// A plane is reproduced exactly by triangle interpolation, at samples and
	// between them.
	h := planeHeightmap(t, 65, 33, func(u, v float64) float64 { return 0.25*u + 0.5*v })
	terrain := &Terrain{Heightmap: h, Position: Vector3{X: 10, Y: -4, Z: 20}, Width: 128, Depth: 64, HeightScale: 40}
	for _, p := range [][2]float64{{-54, -12}, {0, 0}, {30.3, 7.7}, {73.99, 51.9}, {-53.5, 51.25}} {
		x, z := p[0], p[1]
		u := (x - (10 - 64)) / 128
		v := (z - (20 - 32)) / 64
		want := -4 + 40*(0.25*u+0.5*v)
		if got := terrain.HeightAt(x, z); math.Abs(got-want) > 40.0/65535*1.01 {
			t.Errorf("HeightAt(%v, %v) = %v, want %v", x, z, got, want)
		}
	}
	// Outside the terrain the query clamps to the nearest edge.
	if got, edge := terrain.HeightAt(-500, 0), terrain.HeightAt(-54, 0); got != edge {
		t.Errorf("outside query = %v, want edge height %v", got, edge)
	}
}

func TestTerrainHeightAtUsesSharedDiagonal(t *testing.T) {
	// One cell, corners 0,0,0,1 (only (1,1) raised). The diagonal from (0,0)
	// to (1,1) means the cell center sits at half the raised corner.
	h := &Heightmap{Columns: 2, Rows: 2, Data: []uint16{0, 0, 0, 65535}}
	terrain := &Terrain{Heightmap: h, Width: 2, Depth: 2, HeightScale: 10}
	if got := terrain.HeightAt(0, 0); math.Abs(got-5) > 1e-9 {
		t.Fatalf("center height = %v, want 5 (diagonal split)", got)
	}
	// Below the diagonal (fx > fz) the height follows corners 00,10,11.
	if got := terrain.HeightAt(0.5, -0.5); math.Abs(got-2.5) > 1e-9 {
		t.Fatalf("(fx=0.75,fz=0.25) height = %v, want 2.5", got)
	}
}

func TestTerrainNormalAndSlope(t *testing.T) {
	// Height rises 1 m per meter of X: a 45 degree slope facing -X.
	h := planeHeightmap(t, 33, 33, func(u, v float64) float64 { return u })
	terrain := &Terrain{Heightmap: h, Width: 32, HeightScale: 32}
	s := terrain.Sample(3, -2)
	want := Vector3{X: -math.Sqrt2 / 2, Y: math.Sqrt2 / 2}
	if math.Abs(s.Normal.X-want.X) > 1e-3 || math.Abs(s.Normal.Y-want.Y) > 1e-3 || math.Abs(s.Normal.Z) > 1e-3 {
		t.Fatalf("normal = %+v, want %+v", s.Normal, want)
	}
	if math.Abs(s.Slope-45) > 0.1 {
		t.Fatalf("slope = %v, want 45", s.Slope)
	}
	if !s.Inside || terrain.Sample(100, 0).Inside {
		t.Fatal("Inside does not follow the terrain rectangle")
	}
	if n := terrain.NormalAt(0, 0); math.Abs(n.X*n.X+n.Y*n.Y+n.Z*n.Z-1) > 1e-9 {
		t.Fatalf("NormalAt is not unit length: %+v", n)
	}
}

func TestGenerateHeightmapIsDeterministic(t *testing.T) {
	noise := TerrainNoise{Seed: 42, Columns: 129, Rows: 97, Octaves: 5, Ridged: 0.4, Island: 0.6}
	a := GenerateHeightmap(noise)
	b := GenerateHeightmap(noise)
	if !reflect.DeepEqual(a.Data, b.Data) {
		t.Fatal("same seed produced different samples")
	}
	other := GenerateHeightmap(TerrainNoise{Seed: 43, Columns: 129, Rows: 97, Octaves: 5, Ridged: 0.4, Island: 0.6})
	if reflect.DeepEqual(a.Data, other.Data) {
		t.Fatal("different seeds produced identical samples")
	}
	low, high := uint16(65535), uint16(0)
	for _, value := range a.Data {
		low, high = min(low, value), max(high, value)
	}
	if low != 0 || high != 65535 {
		t.Fatalf("generated range = %d..%d, want the full 16-bit range", low, high)
	}
	// The golden digest pins the generator across platforms (amd64, arm64,
	// js/wasm). Update it only on an intended generator change.
	sum := fnv.New64a()
	_ = binary.Write(sum, binary.LittleEndian, a.Data)
	if got, want := sum.Sum64(), uint64(terrainNoiseGolden); got != want {
		t.Fatalf("generator digest = %#x, want %#x", got, want)
	}
}

func TestTerrainLODTable(t *testing.T) {
	flat := planeHeightmap(t, 257, 257, func(u, v float64) float64 { return 0.3*u + 0.1*v })
	lod := computeTerrainLOD(flat, 256, 256)
	// 256 m over 256 texels: root 256 m = 32 quads * 1 m * 2^3.
	if lod.levels != 4 || lod.spacing != 1 {
		t.Fatalf("levels=%d spacing=%v, want 4 and 1", lod.levels, lod.spacing)
	}
	for level, value := range lod.errors {
		if value > 1.0/65535 {
			t.Fatalf("a plane has error %v at level %d", value, level)
		}
	}
	rough := GenerateHeightmap(TerrainNoise{Seed: 7, Columns: 257, Rows: 257})
	errs := computeTerrainLOD(rough, 512, 512).errors
	if errs[0] > 1e-9 {
		t.Fatalf("finest level error %v, want 0 where the grid matches the samples", errs[0])
	}
	for level := 1; level < len(errs); level++ {
		if errs[level] < errs[level-1] {
			t.Fatalf("errors not monotonic: %v", errs)
		}
	}
	if errs[len(errs)-1] <= 0 {
		t.Fatalf("coarsest level of a noisy map has no error: %v", errs)
	}
}

func TestTerrainSceneIRDefaultsAndHeights(t *testing.T) {
	h := GenerateHeightmap(TerrainNoise{Seed: 1, Columns: 65, Rows: 65})
	terrain := &Terrain{Heightmap: h, Position: Vector3{Y: -3}}
	ir := terrain.SceneIR()
	if ir == nil {
		t.Fatal("valid terrain lowered to nil")
	}
	if ir.ID != "terrain" || ir.SizeX != 256 || ir.SizeZ != 256 || ir.HeightScale != 32 || ir.PixelError != 1 || ir.TriplanarSlope != 35 {
		t.Fatalf("defaults not applied: %+v", ir)
	}
	if ir.MinX != -128 || ir.MinZ != -128 || ir.BaseY != -3 || ir.MinHeight != -3 || ir.MaxHeight != 29 {
		t.Fatalf("frame wrong: minX=%v minZ=%v baseY=%v range %v..%v", ir.MinX, ir.MinZ, ir.BaseY, ir.MinHeight, ir.MaxHeight)
	}
	if !ir.CastShadow || !ir.ReceiveShadow {
		t.Fatal("shadows default off")
	}
	if len(ir.Layers) != 1 || ir.Layers[0].Color != defaultTerrainLayerColor || ir.Layers[0].MinHeight != -terrainOpenHeight {
		t.Fatalf("default layer = %+v", ir.Layers)
	}
	if len(ir.LODErrors) != ir.Levels {
		t.Fatalf("lodErrors has %d entries for %d levels", len(ir.LODErrors), ir.Levels)
	}
	raw, err := base64.StdEncoding.DecodeString(ir.Heights)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := HeightmapFromRaw(raw, ir.Columns, ir.Rows, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Data, h.Data) {
		t.Fatal("wire heights do not round-trip")
	}

	withSrc := &Terrain{Heightmap: h, Src: " /terrain.r16 ", CastShadow: Bool(false)}
	srcIR := withSrc.SceneIR()
	if srcIR.Src != "/terrain.r16" || srcIR.Heights != "" || srcIR.CastShadow {
		t.Fatalf("Src mode: src=%q heights=%d castShadow=%v", srcIR.Src, len(srcIR.Heights), srcIR.CastShadow)
	}
	if (&Terrain{}).SceneIR() != nil || (*Terrain)(nil).SceneIR() != nil {
		t.Fatal("terrain without a heightmap must lower to nil")
	}
}

func TestTerrainLayerLowering(t *testing.T) {
	layers := make([]TerrainLayer, 6)
	layers[1] = TerrainLayer{Albedo: " rock.png ", MinSlope: 30, MaxSlope: 120, MinHeight: Float(4), Roughness: 3}
	got := lowerTerrainLayers(layers)
	if len(got) != TerrainMaxLayers {
		t.Fatalf("lowered %d layers, want %d", len(got), TerrainMaxLayers)
	}
	rock := got[1]
	if rock.Albedo != "rock.png" || rock.Color != "#ffffff" || rock.MinSlope != 30 || rock.MaxSlope != 90 || rock.MinHeight != 4 || rock.MaxHeight != terrainOpenHeight || rock.Roughness != 1 {
		t.Fatalf("rock layer = %+v", rock)
	}
	if got[0].Color != "#808080" || got[0].Scale != 4 || got[0].HeightBlend != 2 || got[0].SlopeBlend != 6 {
		t.Fatalf("empty layer defaults = %+v", got[0])
	}
}

func TestTerrainEnvironmentWire(t *testing.T) {
	h := GenerateHeightmap(TerrainNoise{Seed: 3, Columns: 33, Rows: 33})
	props := Props{Environment: Environment{
		FogDensity: 0.01,
		Terrain:    &Terrain{Heightmap: h, Splat: &SplatMap{Columns: 1, Rows: 1, Data: []byte{255, 0, 0, 0}}},
	}}
	sceneIR := props.SceneIR()
	if sceneIR.Environment.Terrain == nil || sceneIR.Environment.IsZero() {
		t.Fatal("terrain did not lower into the environment")
	}
	direct, err := json.Marshal(sceneIR)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(sceneIR.legacyProps())
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(direct, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(legacy, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("direct and legacy terrain wire differ\n%s\n%s", direct, legacy)
	}
	if !strings.Contains(string(direct), `"terrain":{"id":"terrain"`) || !strings.Contains(string(direct), `"splat":{"columns":1,"rows":1,"data":"/wAAAA=="}`) {
		t.Fatalf("terrain wire missing: %s", direct)
	}
	// Lowering twice reuses the cached heights string.
	first := h.derived(256, 256)
	second := h.derived(256, 256)
	if first != second {
		t.Fatal("derived heightmap data is recomputed on every lowering")
	}
}

func TestTerrainValidateRejectsBadInput(t *testing.T) {
	h := GenerateHeightmap(TerrainNoise{Seed: 1, Columns: 9, Rows: 9})
	bad := []*Terrain{
		{Heightmap: h, Width: -1},
		{Heightmap: h, Position: Vector3{X: math.NaN()}},
		{Heightmap: h, Splat: &SplatMap{Columns: 2, Rows: 2, Data: make([]byte, 4)}},
	}
	for i, terrain := range bad {
		if terrain.Validate() == nil || terrain.SceneIR() != nil {
			t.Errorf("case %d: invalid terrain accepted", i)
		}
		if terrain.HeightAt(0, 0) != 0 {
			t.Errorf("case %d: invalid terrain answered a height query", i)
		}
	}
}

// TestTerrainBrowserFixture pins the browser parity fixture: a TerrainIR and
// Go height queries that client/js/scene3d-terrain.test.mjs replays against the
// browser's decoder and height function. Set GOSX_UPDATE_TERRAIN_FIXTURE=1 to
// rewrite it after an intended change.
func TestTerrainBrowserFixture(t *testing.T) {
	h := GenerateHeightmap(TerrainNoise{Seed: 11, Columns: 33, Rows: 25, Ridged: 0.5})
	terrain := &Terrain{Heightmap: h, Position: Vector3{X: 4, Y: -6, Z: -3}, Width: 96, Depth: 72, HeightScale: 30,
		Layers: []TerrainLayer{{Color: "#556b2f"}, {Color: "#8a8a8a", MinSlope: 30}}}
	type query struct {
		X, Z, Height float64
		Normal       Vector3
	}
	fixture := struct {
		Terrain *TerrainIR `json:"terrain"`
		Queries []query    `json:"queries"`
	}{Terrain: terrain.SceneIR()}
	for i := 0; i < 40; i++ {
		x := -44 + float64(i%8)*12.9 + float64(i)*0.37
		z := -39 + float64(i/8)*17.3 + float64(i%3)*0.71
		s := terrain.Sample(x, z)
		fixture.Queries = append(fixture.Queries, query{X: x, Z: z, Height: s.Height, Normal: s.Normal})
	}
	got, err := json.MarshalIndent(fixture, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := "../client/js/testdata/scene3d-terrain-fixture.json"
	if os.Getenv("GOSX_UPDATE_TERRAIN_FIXTURE") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture (set GOSX_UPDATE_TERRAIN_FIXTURE=1 to create it): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("browser terrain fixture is stale; rerun with GOSX_UPDATE_TERRAIN_FIXTURE=1")
	}
}

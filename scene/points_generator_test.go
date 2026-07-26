package scene

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"testing"
)

var updatePointsGolden = flag.Bool("update-points-golden", false,
	"rewrite scene/testdata/points_generator_golden.json")

// pointsGoldenCase pins one descriptor to the exact bits it must expand to.
//
// The arrays are summarised by a SHA-256 over their little-endian IEEE-754
// bytes rather than listed in full: the m31labs star layer alone is 16200
// floats, and a digest asserts exact equality over every one of them at a
// fraction of the fixture size. Head holds the first few values verbatim so a
// failure is debuggable without regenerating anything.
type pointsGoldenCase struct {
	Name          string          `json:"name"`
	Count         int             `json:"count"`
	Generator     PointsGenerator `json:"generator"`
	PositionsSHA  string          `json:"positionsSha256"`
	SizesSHA      string          `json:"sizesSha256"`
	PositionsHead []string        `json:"positionsHead"`
	SizesHead     []string        `json:"sizesHead"`
}

type pointsGoldenFile struct {
	Comment string             `json:"_comment"`
	Cases   []pointsGoldenCase `json:"cases"`
}

func pointsGoldenPath() string {
	return filepath.Join("testdata", "points_generator_golden.json")
}

// float64BitsDigest hashes a float slice by its exact bit patterns, so the
// digest is sensitive to a single ulp anywhere in the array.
func float64BitsDigest(values []float64) string {
	h := sha256.New()
	var buf [8]byte
	for _, v := range values {
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v))
		h.Write(buf[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func float64BitsHead(values []float64, n int) []string {
	if len(values) < n {
		n = len(values)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(values[i]))
		out[i] = hex.EncodeToString(buf[:])
	}
	return out
}

func flattenVector3(points []Vector3) []float64 {
	out := make([]float64, 0, len(points)*3)
	for _, p := range points {
		out = append(out, p.X, p.Y, p.Z)
	}
	return out
}

// pointsGoldenCases are the descriptors the fixture pins. The first two are
// the real m31labs.dev sky layers, so a change that would move a star on the
// live site fails here first.
func pointsGoldenCases() []pointsGoldenCase {
	return []pointsGoldenCase{
		{
			// m31labs.dev galaxy/starfield shared star layer.
			Name:  "m31labs-galaxy-stars",
			Count: 5400,
			Generator: PointsGenerator{
				Kind: PointsGenBoxScatter, Seed: 0, Stride: 3,
				OffsetX: 0, OffsetY: 1, OffsetZ: 2, OffsetSize: 7,
				Extent:  Vec3(2000, 2000, 2000),
				SizeMin: 0.92, SizeMax: 4.17, SizeExponent: 1,
			},
		},
		{
			// m31labs.dev near depth-accent layer; exercises the power-biased
			// size curve (canonicalPow) and a non-zero centre.
			Name:  "m31labs-starfield-near",
			Count: 380,
			Generator: PointsGenerator{
				Kind: PointsGenBoxScatter, Seed: 31, Stride: 7,
				OffsetX: 0, OffsetY: 1, OffsetZ: 2, OffsetSize: 3,
				Center:  Vec3(0, 0, -150),
				Extent:  Vec3(1440, 1008, 220),
				SizeMin: 1.7, SizeMax: 5.2, SizeExponent: 2.4,
			},
		},
		{
			// Defaults and negative seeds: Kind/Stride/SizeExponent empty.
			Name:  "defaults-and-negative-seed",
			Count: 64,
			Generator: PointsGenerator{
				Seed: -119, OffsetX: 0, OffsetY: 1, OffsetZ: 2, OffsetSize: 3,
				Center:  Vec3(-3.5, 12.25, 0.125),
				Extent:  Vec3(7, 0.5, 1024),
				SizeMin: 0.1, SizeMax: 9.75,
			},
		},
		{
			// Fractional exponent below 1, which drives canonicalPow through a
			// different branch of the log/exp reduction.
			Name:  "sublinear-size-curve",
			Count: 128,
			Generator: PointsGenerator{
				Kind: PointsGenBoxScatter, Seed: 9001, Stride: 5,
				OffsetX: 4, OffsetY: 0, OffsetZ: 3, OffsetSize: 1,
				Extent:  Vec3(64, 64, 64),
				SizeMin: 0.25, SizeMax: 3, SizeExponent: 0.37,
			},
		},
	}
}

func buildPointsGolden() []pointsGoldenCase {
	cases := pointsGoldenCases()
	for i := range cases {
		positions, sizes := cases[i].Generator.Generate(cases[i].Count)
		flat := flattenVector3(positions)
		cases[i].PositionsSHA = float64BitsDigest(flat)
		cases[i].SizesSHA = float64BitsDigest(sizes)
		cases[i].PositionsHead = float64BitsHead(flat, 9)
		cases[i].SizesHead = float64BitsHead(sizes, 3)
	}
	return cases
}

// TestPointsGeneratorGoldenBits pins the generator output to exact bits. The
// JavaScript runtime asserts against the same fixture in
// client/js/11b-scene-points-generate.test.mjs; together they are the
// cross-language determinism gate.
func TestPointsGeneratorGoldenBits(t *testing.T) {
	if *updatePointsGolden {
		file := pointsGoldenFile{
			Comment: "Bit-exact expansion of scene.PointsGenerator descriptors. " +
				"Digests are SHA-256 over little-endian IEEE-754 bytes. The " +
				"JavaScript runtime must reproduce these exactly; see " +
				"client/js/11b-scene-points-generate.test.mjs.",
			Cases: buildPointsGolden(),
		}
		data, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			t.Fatalf("marshal golden: %v", err)
		}
		data = append(data, '\n')
		if err := os.WriteFile(pointsGoldenPath(), data, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", pointsGoldenPath())
		return
	}

	raw, err := os.ReadFile(pointsGoldenPath())
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var file pointsGoldenFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	got := buildPointsGolden()
	if len(got) != len(file.Cases) {
		t.Fatalf("case count: golden has %d, code builds %d", len(file.Cases), len(got))
	}
	for i, want := range file.Cases {
		if got[i].Name != want.Name {
			t.Fatalf("case %d: name %q != golden %q", i, got[i].Name, want.Name)
		}
		if got[i].PositionsSHA != want.PositionsSHA {
			t.Errorf("%s: positions digest %s != golden %s (head %v vs %v)",
				want.Name, got[i].PositionsSHA, want.PositionsSHA,
				got[i].PositionsHead, want.PositionsHead)
		}
		if got[i].SizesSHA != want.SizesSHA {
			t.Errorf("%s: sizes digest %s != golden %s (head %v vs %v)",
				want.Name, got[i].SizesSHA, want.SizesSHA,
				got[i].SizesHead, want.SizesHead)
		}
	}
}

// TestCanonicalSinMatchesStdlib documents the empirical basis for the whole
// design: the ported sine reproduces Go's own math.Sin exactly across the
// argument range a point generator reaches, so adopting the canonical kernel
// does not move any existing point. (Go's math.Sin and V8's Math.sin, by
// contrast, disagree on 19.78% of these same seeds.)
func TestCanonicalSinMatchesStdlib(t *testing.T) {
	mismatches := 0
	for seed := -20000; seed <= 40000; seed++ {
		arg := float64(seed)*12.9898 + 78.233
		if canonicalSin(arg) != math.Sin(arg) {
			if mismatches < 5 {
				t.Errorf("seed %d: canonicalSin=%v math.Sin=%v", seed, canonicalSin(arg), math.Sin(arg))
			}
			mismatches++
		}
	}
	if mismatches != 0 {
		t.Fatalf("canonicalSin diverged from math.Sin on %d seeds", mismatches)
	}
}

// TestPointsHash01MatchesLegacyExpression proves the canonical hash is the
// same function the m31labs.dev sky was authored against, so converting a
// layer to a descriptor is a payload change and not a visual one.
func TestPointsHash01MatchesLegacyExpression(t *testing.T) {
	legacy := func(seed int) float64 {
		x := math.Sin(float64(seed)*12.9898+78.233) * 43758.5453
		return x - math.Floor(x)
	}
	for seed := 0; seed <= 40000; seed++ {
		if got, want := pointsHash01(seed), legacy(seed); got != want {
			t.Fatalf("seed %d: pointsHash01=%v legacy=%v", seed, got, want)
		}
	}
}

// TestCanonicalPowTracksStdlib bounds the one place the canonical kernel does
// not reproduce Go's stdlib bit-for-bit. canonicalPow is exp(y*log(x)) while
// math.Pow uses repeated squaring, so results can differ in the last ulp.
func TestCanonicalPowTracksStdlib(t *testing.T) {
	const tolerance = 1e-15
	worst := 0.0
	for seed := 0; seed <= 20000; seed++ {
		x := pointsHash01(seed)
		for _, y := range []float64{2.4, 0.37, 3, 1.5} {
			got, want := canonicalPow(x, y), math.Pow(x, y)
			if d := math.Abs(got - want); d > worst {
				worst = d
			}
		}
	}
	if worst > tolerance {
		t.Fatalf("canonicalPow drifted from math.Pow by %g (tolerance %g)", worst, tolerance)
	}
	t.Logf("max |canonicalPow - math.Pow| = %g", worst)
}

// TestPointsGeneratorDefaults checks the sparse-descriptor normalisation both
// runtimes rely on.
func TestPointsGeneratorDefaults(t *testing.T) {
	n := PointsGenerator{}.normalized()
	if n.Kind != PointsGenBoxScatter {
		t.Errorf("default kind = %q, want %q", n.Kind, PointsGenBoxScatter)
	}
	if n.Stride != 3 {
		t.Errorf("default stride = %d, want 3", n.Stride)
	}
	if n.SizeExponent != 1 {
		t.Errorf("default size exponent = %v, want 1", n.SizeExponent)
	}
	if !(PointsGenerator{}).Supported() {
		t.Error("empty descriptor should normalise to a supported kind")
	}
	if (PointsGenerator{Kind: "spiral-arm"}).Supported() {
		t.Error("unknown kind should not report as supported")
	}
}

// TestPointsGeneratorGenerateGuards covers the degenerate inputs the client
// mirrors.
func TestPointsGeneratorGenerateGuards(t *testing.T) {
	g := PointsGenerator{Kind: PointsGenBoxScatter, Extent: Vec3(1, 1, 1)}
	if p, s := g.Generate(0); p != nil || s != nil {
		t.Error("zero count should generate nothing")
	}
	if p, s := g.Generate(-4); p != nil || s != nil {
		t.Error("negative count should generate nothing")
	}
	if p, s := (PointsGenerator{Kind: "spiral-arm"}).Generate(8); p != nil || s != nil {
		t.Error("unknown kind should generate nothing")
	}
	p, s := g.Generate(5)
	if len(p) != 5 || len(s) != 5 {
		t.Fatalf("expected 5 positions and sizes, got %d and %d", len(p), len(s))
	}
}

// lowerPointsForTest lowers a single Points node and returns its IR record.
func lowerPointsForTest(t *testing.T, pts Points) PointsIR {
	t.Helper()
	l := &graphLowerer{anchors: make(map[string]worldTransform)}
	l.lowerPoints(pts, identityTransform())
	if len(l.points) != 1 {
		t.Fatalf("expected 1 lowered points record, got %d", len(l.points))
	}
	return l.points[0]
}

// TestPointsGeneratorLoweringOmitsArrays is the payload contract: a
// procedural layer must ship the recipe and none of the expanded data.
func TestPointsGeneratorLoweringOmitsArrays(t *testing.T) {
	record := lowerPointsForTest(t, Points{
		ID:    "stars",
		Count: 5400,
		Generator: &PointsGenerator{
			Seed: 0, Stride: 3, OffsetX: 0, OffsetY: 1, OffsetZ: 2, OffsetSize: 7,
			Extent: Vec3(2000, 2000, 2000), SizeMin: 0.92, SizeMax: 4.17,
		},
	})
	if len(record.Positions) != 0 {
		t.Errorf("generator layer emitted %d position values, want 0", len(record.Positions))
	}
	if len(record.Sizes) != 0 {
		t.Errorf("generator layer emitted %d size values, want 0", len(record.Sizes))
	}
	if record.Generator == nil {
		t.Fatal("generator descriptor missing from lowered record")
	}
	if record.Generator.Kind != PointsGenBoxScatter {
		t.Errorf("descriptor kind = %q, want %q", record.Generator.Kind, PointsGenBoxScatter)
	}
	if record.Generator.Stride != 3 || record.Generator.SizeExp != 1 {
		t.Errorf("descriptor defaults unresolved: stride=%d sizeExp=%v",
			record.Generator.Stride, record.Generator.SizeExp)
	}

	// The serialised layer must be small enough to be worth the exercise. The
	// arrays it replaces are 852_163 bytes on m31labs.dev.
	encoded, err := json.Marshal(record.legacyProps())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(encoded) > 1024 {
		t.Errorf("procedural layer serialised to %d bytes, expected well under 1KB", len(encoded))
	}
	t.Logf("procedural points layer serialises to %d bytes", len(encoded))
}

// TestPointsExplicitArraysUnchanged pins the compatibility promise: a layer
// with no generator lowers exactly as it did before this feature existed, and
// explicit arrays override a descriptor rather than doubling the payload.
func TestPointsExplicitArraysUnchanged(t *testing.T) {
	explicit := Points{
		ID:        "explicit",
		Count:     2,
		Positions: []Vector3{Vec3(1, 2, 3), Vec3(4, 5, 6)},
		Sizes:     []float64{0.5, 1.5},
	}
	record := lowerPointsForTest(t, explicit)
	if record.Generator != nil {
		t.Error("layer without a generator must not gain a descriptor")
	}
	wantPositions := []float64{1, 2, 3, 4, 5, 6}
	if len(record.Positions) != len(wantPositions) {
		t.Fatalf("positions length %d, want %d", len(record.Positions), len(wantPositions))
	}
	for i, want := range wantPositions {
		if record.Positions[i] != want {
			t.Errorf("position %d = %v, want %v", i, record.Positions[i], want)
		}
	}
	if _, ok := record.legacyProps()["generator"]; ok {
		t.Error("explicit layer must not serialise a generator key")
	}

	// Both supplied: arrays win, descriptor is dropped so the wire never
	// carries redundant data.
	both := explicit
	both.Generator = &PointsGenerator{Extent: Vec3(1, 1, 1)}
	bothRecord := lowerPointsForTest(t, both)
	if bothRecord.Generator != nil {
		t.Error("explicit positions must suppress the generator descriptor")
	}
	if len(bothRecord.Positions) != len(wantPositions) {
		t.Errorf("explicit positions lost: got %d values", len(bothRecord.Positions))
	}
	if len(bothRecord.Sizes) != 2 {
		t.Errorf("explicit sizes lost: got %d values", len(bothRecord.Sizes))
	}
}

// TestPointsGeneratorRoundTripsThroughJSON checks the descriptor survives the
// wire in the shape the client expander reads.
func TestPointsGeneratorRoundTripsThroughJSON(t *testing.T) {
	record := lowerPointsForTest(t, Points{
		ID:    "near",
		Count: 380,
		Generator: &PointsGenerator{
			Seed: 31, Stride: 7, OffsetX: 0, OffsetY: 1, OffsetZ: 2, OffsetSize: 3,
			Center: Vec3(0, 0, -150), Extent: Vec3(1440, 1008, 220),
			SizeMin: 1.7, SizeMax: 5.2, SizeExponent: 2.4,
		},
	})
	encoded, err := json.Marshal(record.legacyProps())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	gen, ok := decoded["generator"].(map[string]any)
	if !ok {
		t.Fatalf("generator key missing or wrong type in %s", encoded)
	}
	for key, want := range map[string]float64{
		"seed": 31, "stride": 7, "offsetSize": 3,
		"centerZ": -150, "extentX": 1440, "extentY": 1008, "extentZ": 220,
		"sizeMin": 1.7, "sizeMax": 5.2, "sizeExp": 2.4,
	} {
		got, ok := gen[key].(float64)
		if !ok {
			t.Errorf("generator.%s missing", key)
			continue
		}
		if got != want {
			t.Errorf("generator.%s = %v, want %v", key, got, want)
		}
	}
	if gen["kind"] != PointsGenBoxScatter {
		t.Errorf("generator.kind = %v, want %q", gen["kind"], PointsGenBoxScatter)
	}
}

// TestPointsGeneratorSizeExponentOne confirms the exponent-1 fast path is not
// merely close to the power form but identical, since that path carries the
// 5400-point star layer.
func TestPointsGeneratorSizeExponentOne(t *testing.T) {
	g := PointsGenerator{
		Kind: PointsGenBoxScatter, Seed: 5, Stride: 3,
		OffsetSize: 7, SizeMin: 0.92, SizeMax: 4.17, SizeExponent: 1,
	}
	_, sizes := g.Generate(256)
	for i, got := range sizes {
		draw := pointsHash01(g.Seed + i*g.Stride + g.OffsetSize)
		want := g.SizeMin + draw*(g.SizeMax-g.SizeMin)
		if got != want {
			t.Fatalf("size %d: %v != %v", i, got, want)
		}
	}
}

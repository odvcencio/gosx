package quantize

import (
	"math"
	"math/rand"
	"testing"
)

// randomPositions builds a cloud inside a deliberately off-centre box, so a
// grid that forgets the offset fails visibly.
func randomPositions(count int, seed int64, low, high [3]float64) []float64 {
	random := rand.New(rand.NewSource(seed))
	out := make([]float64, 0, count*3)
	for i := 0; i < count; i++ {
		for axis := 0; axis < 3; axis++ {
			out = append(out, low[axis]+random.Float64()*(high[axis]-low[axis]))
		}
	}
	// Pin the corners so the bounding box is exactly the requested box.
	copy(out[0:3], low[:])
	copy(out[3:6], high[:])
	return out
}

// The tests in this file validate the uniform-scale grid of this package only.
// The per-axis grid at m31labs.dev/turboquant/mesh is a different encoding, and
// nothing here round trips through it.
func TestPositionGridKeepsErrorUnderTheLatticeBound(t *testing.T) {
	low := [3]float64{-3.25, 100.5, -0.125}
	high := [3]float64{4.75, 101.5, 0.875}
	positions := randomPositions(4000, 42, low, high)

	for _, bits := range []int{8, 16} {
		grid := FitPositionGrid(positions, bits)
		report := grid.MeasurePositionError(positions)
		t.Logf("bits=%d scale=%g max=%g rms=%g bound=%g", bits, grid.Scale, report.Max, report.RMS, report.Bound)
		if report.Max > report.Bound {
			t.Fatalf("bits=%d: max error %g exceeds the lattice bound %g", bits, report.Max, report.Bound)
		}
		if report.RMS > report.Max {
			t.Fatalf("bits=%d: rms %g above max %g", bits, report.RMS, report.Max)
		}
		if !report.Contains() {
			t.Fatalf("bits=%d: the decoded bounds lost the source: %v %v vs %v %v",
				bits, report.DecodedLow, report.DecodedHigh, report.SourceLow, report.SourceHigh)
		}
		// A wrong step count would show as a systematic shrink of the box.
		for axis, delta := range report.ExtentDelta() {
			if math.Abs(delta) > grid.Scale {
				t.Fatalf("bits=%d axis %d: the box changed by %g, more than one step %g", bits, axis, delta, grid.Scale)
			}
		}
	}
}

func TestPositionGridScalesWithTheExtent(t *testing.T) {
	// The relative error must not depend on where the mesh sits, only on how
	// large it is. A grid that forgets the offset would fail this test.
	positions := randomPositions(2000, 7, [3]float64{0, 0, 0}, [3]float64{1, 1, 1})
	shifted := make([]float64, len(positions))
	for i, value := range positions {
		shifted[i] = value + 1000
	}
	base := FitPositionGrid(positions, 16).MeasurePositionError(positions)
	moved := FitPositionGrid(shifted, 16).MeasurePositionError(shifted)
	t.Logf("origin max %g, shifted max %g", base.Max, moved.Max)
	if moved.Max > base.Max*4 {
		t.Fatalf("moving the mesh raised the error from %g to %g", base.Max, moved.Max)
	}
}

func TestPositionGridBoundsHoldForAThinMesh(t *testing.T) {
	// A mesh that is wide on one axis and flat on another still uses one scale.
	// The flat axis must keep its two distinct values.
	positions := []float64{
		-50, 0, 0,
		50, 0, 0,
		0, 0.001, 0,
		0, -0.001, 0,
	}
	grid := FitPositionGrid(positions, 16)
	report := grid.MeasurePositionError(positions)
	t.Logf("thin mesh scale %g max %g bound %g extentDelta %v", grid.Scale, report.Max, report.Bound, report.ExtentDelta())
	if report.Max > report.Bound {
		t.Fatalf("max error %g exceeds bound %g", report.Max, report.Bound)
	}
	if !report.Contains() {
		t.Fatalf("decoded bounds %v %v lost source %v %v", report.DecodedLow, report.DecodedHigh, report.SourceLow, report.SourceHigh)
	}
	// The wide axis must keep its length. The thin axis may collapse to a
	// single lattice step, which is the whole point of one shared scale.
	if delta := report.ExtentDelta()[0]; math.Abs(delta) > grid.Scale {
		t.Fatalf("the wide axis changed by %g, more than one step %g", delta, grid.Scale)
	}
}

func TestPositionGridCatchesAShrunkenScale(t *testing.T) {
	// A grid whose scale is one step too small shrinks the whole mesh. The
	// containment check must reject it, so the check is not vacuous.
	positions := randomPositions(500, 5, [3]float64{0, 0, 0}, [3]float64{10, 10, 10})
	good := FitPositionGrid(positions, 16)
	if !good.MeasurePositionError(positions).Contains() {
		t.Fatal("the fitted grid must contain its own source")
	}
	shrunk := good
	shrunk.Scale = good.Scale * 0.9
	report := shrunk.MeasurePositionError(positions)
	if report.Contains() {
		t.Fatalf("a ten percent smaller scale must fail containment: decoded %v %v, source %v %v",
			report.DecodedLow, report.DecodedHigh, report.SourceLow, report.SourceHigh)
	}
}

func TestPositionGridHandlesADegenerateMesh(t *testing.T) {
	positions := []float64{2, 2, 2, 2, 2, 2}
	grid := FitPositionGrid(positions, 16)
	report := grid.MeasurePositionError(positions)
	if report.Max > 1e-6 {
		t.Fatalf("a single point must round trip exactly, got %g", report.Max)
	}
	if !report.Contains() {
		t.Fatal("a single point must stay inside its own bounds")
	}
}

func TestPositionGridEncodeStaysInRange(t *testing.T) {
	positions := randomPositions(500, 3, [3]float64{-1, -1, -1}, [3]float64{1, 1, 1})
	for _, bits := range []int{8, 16} {
		grid := FitPositionGrid(positions, bits)
		stored := grid.EncodeStream(positions)
		limit := int32(1)<<bits - 1
		for _, value := range stored {
			if value < 0 || value > limit {
				t.Fatalf("bits=%d: stored value %d leaves the range 0 to %d", bits, value, limit)
			}
		}
		if len(stored) != len(positions) {
			t.Fatalf("stored %d components, want %d", len(stored), len(positions))
		}
	}
}

func TestUnitCodecMatchesTheSpecificationDequantization(t *testing.T) {
	// The glTF specification defines the signed dequantization as
	// max(c / (2^(b-1) - 1), -1).
	for _, bits := range []int{8, 16} {
		codec := UnitCodec{Bits: bits, Signed: true}
		steps := float64(int(1)<<(bits-1) - 1)
		for _, stored := range []int32{int32(-steps) - 1, int32(-steps), -1, 0, 1, int32(steps)} {
			want := math.Max(float64(stored)/steps, -1)
			if got := codec.Decode(stored); got != want {
				t.Fatalf("bits=%d stored=%d: decode %g, want %g", bits, stored, got, want)
			}
		}
	}
	for _, bits := range []int{8, 16} {
		codec := UnitCodec{Bits: bits, Signed: false}
		steps := float64(int(1)<<bits - 1)
		for _, stored := range []int32{0, 1, int32(steps)} {
			want := float64(stored) / steps
			if got := codec.Decode(stored); got != want {
				t.Fatalf("bits=%d stored=%d: decode %g, want %g", bits, stored, got, want)
			}
		}
	}
}

func TestEncodeUnitVectorsKeepsTheAngleUnderTheBound(t *testing.T) {
	random := rand.New(rand.NewSource(19))
	normals := make([]float64, 0, 3000)
	for i := 0; i < 1000; i++ {
		// Sample the sphere uniformly, so no direction is favoured.
		z := random.Float64()*2 - 1
		angle := random.Float64() * 2 * math.Pi
		radius := math.Sqrt(1 - z*z)
		normals = append(normals, radius*math.Cos(angle), radius*math.Sin(angle), z)
	}
	for _, bits := range []int{8, 16} {
		codec := UnitCodec{Bits: bits, Signed: true}
		stored, report := EncodeUnitVectors(normals, 3, codec)
		t.Logf("bits=%d normal max %.4f deg, rms %.4f deg, bound %.4f deg", bits, report.MaxDegrees, report.RMSDegrees, report.Bound)
		if len(stored) != len(normals) {
			t.Fatalf("bits=%d: stored %d components, want %d", bits, len(stored), len(normals))
		}
		if report.MaxDegrees > report.Bound {
			t.Fatalf("bits=%d: max angle %g exceeds the lattice bound %g", bits, report.MaxDegrees, report.Bound)
		}
		if report.ZeroLength != 0 {
			t.Fatalf("bits=%d: %d unit vectors reported as zero length", bits, report.ZeroLength)
		}
	}
}

func TestEncodeUnitVectorsKeepsTangentHandedness(t *testing.T) {
	tangents := []float64{
		1, 0, 0, 1,
		0, 1, 0, -1,
		0, 0, 1, 1,
	}
	codec := UnitCodec{Bits: 8, Signed: true}
	stored, _ := EncodeUnitVectors(tangents, 4, codec)
	if len(stored) != 12 {
		t.Fatalf("stored %d components, want 12", len(stored))
	}
	for i, want := range []float64{1, -1, 1} {
		got := codec.Decode(stored[i*4+3])
		if got != want {
			t.Fatalf("tangent %d handedness %g, want %g", i, got, want)
		}
	}
}

func TestEncodeUnitVectorsRenormalizesAShortVector(t *testing.T) {
	// A source vector of length one half must still decode to a unit direction,
	// because the encoder normalizes before it rounds.
	values := []float64{0.5, 0, 0}
	codec := UnitCodec{Bits: 8, Signed: true}
	stored, report := EncodeUnitVectors(values, 3, codec)
	if codec.Decode(stored[0]) != 1 {
		t.Fatalf("decoded x %g, want 1", codec.Decode(stored[0]))
	}
	if report.MaxDegrees > 1e-9 {
		t.Fatalf("an axis aligned vector must round trip exactly, got %g degrees", report.MaxDegrees)
	}
}

func TestEncodeUnitRangeReportsClamping(t *testing.T) {
	codec := UnitCodec{Bits: 16, Signed: false}
	inside := []float64{0, 0.25, 0.5, 1}
	stored, report := EncodeUnitRange(inside, codec, 0)
	if report.Clamped != 0 {
		t.Fatalf("values inside the range must not clamp: %d", report.Clamped)
	}
	if report.Max > report.Bound {
		t.Fatalf("max error %g exceeds the bound %g", report.Max, report.Bound)
	}
	if stored[0] != 0 || stored[3] != 65535 {
		t.Fatalf("the range ends must map to the range ends: %v", stored)
	}

	outside := []float64{-0.5, 1.5}
	_, outsideReport := EncodeUnitRange(outside, codec, 0)
	if outsideReport.Clamped != 2 {
		t.Fatalf("both values sit outside the range, got %d clamped", outsideReport.Clamped)
	}
}

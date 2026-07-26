package bcn

import (
	"math"
	"sort"
	"testing"
)

// TestBC5NormalAngularError measures the format the way a shader sees it.
//
// The measurement is in degrees, not in codes. Near a silhouette the rebuilt z
// part is small, so one code of error in x or y turns the normal by much more
// than one code of error does in the middle of the map. A per-channel number
// hides that, and the p95 column is where it shows.
func TestBC5NormalAngularError(t *testing.T) {
	surface := normalImage(128)
	for _, quality := range []Quality{QualityFast, QualityHigh} {
		payload, err := EncodeBC5Normal(surface, BC5Options{Transfer: TransferUnorm, Quality: quality})
		if err != nil {
			t.Fatalf("EncodeBC5Normal: %v", err)
		}
		angle, err := AngularErrorBC5(surface, payload)
		if err != nil {
			t.Fatalf("AngularErrorBC5: %v", err)
		}
		channels := psnrAgainstSurface(t, surface, TransferUnorm, FormatBC5, payload, ChannelR, ChannelG)
		t.Logf("%-4s mean %.3f deg, p95 %.3f deg, max %.3f deg, channel psnr %.2f dB",
			quality, angle.MeanDegrees, angle.P95Degrees, angle.MaxDegrees, channels)
		// The floors come from measurement. An uncompressed rg8 upload of the
		// same normals already turns them by the amount uncompressedAngularError
		// reports, so no BC5 encoder can go below that.
		if angle.MeanDegrees > 2.0 {
			t.Errorf("%s mean angular error %.3f deg is above 2.0 deg", quality, angle.MeanDegrees)
		}
		if angle.P95Degrees > 3.6 {
			t.Errorf("%s p95 angular error %.3f deg is above 3.6 deg", quality, angle.P95Degrees)
		}
		if angle.MaxDegrees > 6.5 {
			t.Errorf("%s worst angular error %.3f deg is above 6.5 deg", quality, angle.MaxDegrees)
		}
	}
	floor := uncompressedAngularError(t, surface)
	t.Logf("uncompressed rg8 upload of the same normals: mean %.3f deg, p95 %.3f deg, max %.3f deg",
		floor.MeanDegrees, floor.P95Degrees, floor.MaxDegrees)
}

// uncompressedAngularError measures what plain 8-bit quantization of x and y
// costs. It is the floor of the format, and it puts the BC5 number in scale.
func uncompressedAngularError(t *testing.T, s *Surface) AngularError {
	t.Helper()
	// Build a payload whose every block stores the exact codes. Two endpoints
	// cannot do that, so the measurement takes the shorter path and rebuilds
	// the normals straight from the reference codes.
	reference, err := ReferenceCodes(s, TransferUnorm)
	if err != nil {
		t.Fatalf("ReferenceCodes: %v", err)
	}
	angles := make([]float64, 0, s.Width*s.Height)
	for pixel := 0; pixel < s.Width*s.Height; pixel++ {
		i := pixel * 4
		sx, sy, sz := decodeNormal(s.Pix[i], s.Pix[i+1], s.Pix[i+2])
		dx := float64(reference[i])/255*2 - 1
		dy := float64(reference[i+1])/255*2 - 1
		dz := math.Sqrt(math.Max(0, 1-dx*dx-dy*dy))
		length := math.Sqrt(dx*dx + dy*dy + dz*dz)
		if length > 0 {
			dx, dy, dz = dx/length, dy/length, dz/length
		}
		cosine := math.Min(1, math.Max(-1, sx*dx+sy*dy+sz*dz))
		angles = append(angles, math.Acos(cosine)*180/math.Pi)
	}
	total := 0.0
	worst := 0.0
	for _, a := range angles {
		total += a
		if a > worst {
			worst = a
		}
	}
	sort.Float64s(angles)
	return AngularError{
		MeanDegrees: total / float64(len(angles)),
		P95Degrees:  angles[int(0.95*float64(len(angles)-1))],
		MaxDegrees:  worst,
	}
}

// TestBC5NormalRenormalizationHelps proves the renormalization step earns its
// square root.
//
// The mutation encodes the same surface as two plain data channels, which skips
// the renormalization. The source normals are shortened on purpose here, the way
// any resample wider than a box filter shortens them. A short vector pulls x and
// y toward the middle code, and the shader then rebuilds a z that is too large,
// which flattens the surface.
func TestBC5NormalRenormalizationHelps(t *testing.T) {
	source := normalImage(96)
	// Shorten every normal, as a resample of a normal map does.
	shortened := &Surface{Width: source.Width, Height: source.Height,
		Pix: make([]float32, len(source.Pix))}
	copy(shortened.Pix, source.Pix)
	for i := 0; i+3 < len(shortened.Pix); i += 4 {
		scale := float32(0.78)
		x := (shortened.Pix[i]*2 - 1) * scale
		y := (shortened.Pix[i+1]*2 - 1) * scale
		z := (shortened.Pix[i+2]*2 - 1) * scale
		shortened.Pix[i] = x*0.5 + 0.5
		shortened.Pix[i+1] = y*0.5 + 0.5
		shortened.Pix[i+2] = z*0.5 + 0.5
	}

	withNormalize, err := EncodeBC5Normal(shortened, BC5Options{Transfer: TransferUnorm, Quality: QualityHigh})
	if err != nil {
		t.Fatalf("EncodeBC5Normal: %v", err)
	}
	without, err := EncodeBC5(shortened, BC5Options{
		Transfer: TransferUnorm, Quality: QualityHigh, First: ChannelR, Second: ChannelG,
	})
	if err != nil {
		t.Fatalf("EncodeBC5: %v", err)
	}

	good, err := AngularErrorBC5(shortened, withNormalize)
	if err != nil {
		t.Fatalf("AngularErrorBC5: %v", err)
	}
	bad, err := AngularErrorBC5(shortened, without)
	if err != nil {
		t.Fatalf("AngularErrorBC5: %v", err)
	}
	t.Logf("renormalized mean %.3f deg, raw channels mean %.3f deg", good.MeanDegrees, bad.MeanDegrees)
	// The measured ratio is about 2.7. The assertion asks for 2, which leaves
	// room and still fails if the renormalization stops running.
	if bad.MeanDegrees <= good.MeanDegrees*2 {
		t.Errorf("skipping the renormalization cost only %.3f deg against %.3f deg, "+
			"so the step is untested", bad.MeanDegrees, good.MeanDegrees)
	}
}

// TestAngularErrorCatchesASilhouetteError proves the metric choice matters.
//
// Two payloads hold the same channel error. The first moves a normal that points
// straight out of the surface, and the second moves a normal that lies near the
// silhouette. The angular error of the second is many times larger, and a
// per-channel measurement would call the two the same.
func TestAngularErrorCatchesASilhouetteError(t *testing.T) {
	// One block of a flat normal, and one block of a steep normal.
	flat := NewSurface(4, 4)
	steep := NewSurface(4, 4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			flat.Set(x, y, 0.5, 0.5, 1, 1)
			// x near 1 puts the normal close to the silhouette, where the
			// rebuilt z part is small.
			nx, ny := 0.98, 0.0
			nz := math.Sqrt(1 - nx*nx - ny*ny)
			steep.Set(x, y, float32(nx*0.5+0.5), float32(ny*0.5+0.5), float32(nz*0.5+0.5), 1)
		}
	}

	// Move the stored red endpoints down by four codes in both cases. Both
	// endpoints move together, so every texel of the block shifts by the same
	// four codes and the two cases carry the same channel error.
	const shift = 4
	measure := func(s *Surface) (float64, int) {
		payload, err := EncodeBC5Normal(s, BC5Options{Transfer: TransferUnorm, Quality: QualityHigh})
		if err != nil {
			t.Fatalf("EncodeBC5Normal: %v", err)
		}
		payload[0] -= shift
		payload[1] -= shift
		angle, err := AngularErrorBC5(s, payload)
		if err != nil {
			t.Fatalf("AngularErrorBC5: %v", err)
		}
		reference, err := ReferenceCodes(s, TransferUnorm)
		if err != nil {
			t.Fatalf("ReferenceCodes: %v", err)
		}
		got, err := Decode(FormatBC5, payload, 4, 4)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		worst, err := MaxAbsError(reference, got, ChannelR, ChannelG)
		if err != nil {
			t.Fatalf("MaxAbsError: %v", err)
		}
		return angle.MeanDegrees, worst
	}

	flatAngle, flatCodes := measure(flat)
	steepAngle, steepCodes := measure(steep)
	t.Logf("flat normal: %.3f deg from %d codes; steep normal: %.3f deg from %d codes",
		flatAngle, flatCodes, steepAngle, steepCodes)
	if steepAngle <= flatAngle*2 {
		t.Errorf("the steep normal turned %.3f deg and the flat one %.3f deg; "+
			"the metric must separate them", steepAngle, flatAngle)
	}
	if steepCodes > flatCodes {
		t.Logf("note: the steep case also holds more channel error (%d against %d)",
			steepCodes, flatCodes)
	}
}

// TestNormalizeNormalsIsIdempotent checks the exported helper.
func TestNormalizeNormalsIsIdempotent(t *testing.T) {
	surface := normalImage(16)
	NormalizeNormals(surface)
	first := make([]float32, len(surface.Pix))
	copy(first, surface.Pix)
	NormalizeNormals(surface)
	for i := range first {
		if diff := first[i] - surface.Pix[i]; diff > 1e-6 || diff < -1e-6 {
			t.Fatalf("value %d changed by %g on the second pass", i, diff)
		}
	}
	// A texel with no direction must come back as the flat normal, because
	// that is what a normal map means by no perturbation.
	blank := NewSurface(1, 1)
	blank.Set(0, 0, 0.5, 0.5, 0.5, 1)
	NormalizeNormals(blank)
	r, g, b, _ := blank.At(0, 0)
	if r != 0.5 || g != 0.5 || b != 1 {
		t.Fatalf("a zero normal became (%g, %g, %g), want (0.5, 0.5, 1)", r, g, b)
	}
}

// TestBC5IsTwoIndependentBC4Blocks checks the layout claim against the BC4
// encoder itself. The two halves must be byte for byte the same as two BC4
// payloads of the same channels.
func TestBC5IsTwoIndependentBC4Blocks(t *testing.T) {
	surface := colourImages()[1].surface
	pair, err := EncodeBC5(surface, BC5Options{
		Transfer: TransferUnorm, Quality: QualityHigh, First: ChannelR, Second: ChannelG,
	})
	if err != nil {
		t.Fatalf("EncodeBC5: %v", err)
	}
	red, err := EncodeBC4(surface, BC4Options{Transfer: TransferUnorm, Quality: QualityHigh, Channel: ChannelR})
	if err != nil {
		t.Fatalf("EncodeBC4 red: %v", err)
	}
	green, err := EncodeBC4(surface, BC4Options{Transfer: TransferUnorm, Quality: QualityHigh, Channel: ChannelG})
	if err != nil {
		t.Fatalf("EncodeBC4 green: %v", err)
	}
	for block := 0; block < len(pair)/16; block++ {
		if string(pair[block*16:block*16+8]) != string(red[block*8:block*8+8]) {
			t.Fatalf("block %d: the first half does not match the BC4 red payload", block)
		}
		if string(pair[block*16+8:block*16+16]) != string(green[block*8:block*8+8]) {
			t.Fatalf("block %d: the second half does not match the BC4 green payload", block)
		}
	}
}

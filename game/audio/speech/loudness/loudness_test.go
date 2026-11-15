package loudness

import (
	"math"
	"testing"
)

func sine(rate int, hz, amp, seconds float64) Audio {
	n := int(seconds * float64(rate))
	a := Audio{Rate: rate, Samples: make([]float64, n)}
	for i := range a.Samples {
		a.Samples[i] = amp * math.Sin(2*math.Pi*hz*float64(i)/float64(rate))
	}
	return a
}

// BS.1770 calibration: a mono 997 Hz sine of peak amplitude 1.0 reads
// -3.01 LUFS (the standard's reference point), so peak -20 dBFS reads -23.01.
func TestIntegratedCalibration(t *testing.T) {
	a := sine(48000, 997, FromDB(-20), 3)
	got := Integrated(a)
	if math.Abs(got-(-23.01)) > 0.05 {
		t.Errorf("-20 dBFS-peak 997 Hz sine = %.2f LUFS, want -23.01", got)
	}
	// Doubling amplitude adds 6.02 dB.
	b := sine(48000, 997, FromDB(-14), 3)
	if d := Integrated(b) - got; math.Abs(d-6.02) > 0.05 {
		t.Errorf("6 dB step measured as %.2f dB", d)
	}
}

func TestIntegratedSilenceAndShortClip(t *testing.T) {
	if Integrated(Audio{Rate: 48000, Samples: make([]float64, 48000)}) != -200 {
		t.Error("silence is not -200")
	}
	short := sine(48000, 997, FromDB(-20), 0.15)
	long := sine(48000, 997, FromDB(-20), 3)
	if d := math.Abs(Integrated(short) - Integrated(long)); d > 0.5 {
		t.Errorf("a short clip reads %.2f LU away from the same tone held long", d)
	}
}

func TestTruePeakSeesIntersamplePeaks(t *testing.T) {
	// A sine at fs/4 sampled 45 degrees off its crest: every sample is 0.707 of
	// the true crest, so sample peak understates by 3 dB.
	a := Audio{Rate: 48000, Samples: make([]float64, 4800)}
	for i := range a.Samples {
		a.Samples[i] = math.Sin(2*math.Pi*float64(i)/4 + math.Pi/4)
	}
	if p := Peak(a); math.Abs(p-0.7071) > 0.01 {
		t.Fatalf("sample peak = %v", p)
	}
	if tp := TruePeak(a); tp < 0.95 {
		t.Errorf("true peak = %v, want near 1", tp)
	}
}

func TestLimitHoldsCeiling(t *testing.T) {
	a := sine(48000, 220, 1.8, 0.5)
	out := Limit(a, 0.5, 72)
	if p := Peak(out); p > 0.5+1e-9 {
		t.Errorf("peak after limiting = %v", p)
	}
	quiet := sine(48000, 220, 0.1, 0.5)
	out = Limit(quiet, 0.5, 72)
	for i := range quiet.Samples {
		if math.Abs(out.Samples[i]-quiet.Samples[i]) > 1e-12 {
			t.Fatal("limiter changed a signal under the ceiling")
		}
	}
}

func TestNormalizeHitsTargetAndCeiling(t *testing.T) {
	a := sine(48000, 500, FromDB(-30), 1)
	out, rep := Normalize(a, -23, -1)
	if math.Abs(rep.LUFS-(-23)) > 0.2 {
		t.Errorf("loudness = %.2f, want -23", rep.LUFS)
	}
	if rep.TruePeakDB > -1+0.05 {
		t.Errorf("true peak = %.2f dBTP", rep.TruePeakDB)
	}
	_ = out
	// A peaky clip: loud spike on quiet body. The limiter holds the peak and
	// the report shows the shortfall instead of pretending.
	b := sine(48000, 500, FromDB(-30), 1)
	b.Samples[24000] = 1
	_, rep = Normalize(b, -14, -1)
	if rep.TruePeakDB > -1+0.05 {
		t.Errorf("peaky clip true peak = %.2f dBTP", rep.TruePeakDB)
	}
	if rep.LUFS > -14+0.05 {
		t.Errorf("peaky clip loudness %.2f exceeded the target", rep.LUFS)
	}
}

func TestTrimAndFade(t *testing.T) {
	a := Audio{Rate: 48000, Samples: make([]float64, 48000)}
	for i := 20000; i < 28000; i++ {
		a.Samples[i] = 0.5
	}
	tr := Trim(a, -50, 5)
	pad := 240
	if want := 8000 + 2*pad; len(tr.Samples) != want {
		t.Errorf("trimmed length = %d, want %d", len(tr.Samples), want)
	}
	if len(Trim(Audio{Rate: 48000, Samples: make([]float64, 100)}, -50, 5).Samples) != 0 {
		t.Error("silence not trimmed to nothing")
	}
	f := Fade(tr, 2)
	if f.Samples[0] != 0 || f.Samples[len(f.Samples)-1] != 0 {
		t.Error("fade edges are not zero")
	}
}

func TestResamplePreservesTone(t *testing.T) {
	a := sine(24000, 1000, 0.5, 0.5)
	b := Resample(a, 48000)
	if len(b.Samples) != 24000 || b.Rate != 48000 {
		t.Fatalf("length %d rate %d", len(b.Samples), b.Rate)
	}
	want := sine(48000, 1000, 0.5, 0.5)
	var worst float64
	for i := 2000; i < 22000; i++ {
		worst = math.Max(worst, math.Abs(b.Samples[i]-want.Samples[i]))
	}
	if worst > 0.01 {
		t.Errorf("resampled tone differs by %v", worst)
	}
}

func TestWAVRoundTrip(t *testing.T) {
	a := sine(48000, 440, 0.5, 0.1)
	b, err := ParseWAV(WAV16(a))
	if err != nil {
		t.Fatal(err)
	}
	if b.Rate != 48000 || len(b.Samples) != len(a.Samples) {
		t.Fatalf("round trip: rate %d len %d", b.Rate, len(b.Samples))
	}
	for i := range a.Samples {
		if math.Abs(a.Samples[i]-b.Samples[i]) > 1.0/32767 {
			t.Fatalf("sample %d differs", i)
		}
	}
	if _, err := ParseWAV([]byte("nope")); err == nil {
		t.Error("garbage accepted")
	}
}

func burst(rate int, hz, amp, seconds float64) []float64 { return sine(rate, hz, amp, seconds).Samples }

func TestRemoveDCAndDeClick(t *testing.T) {
	a := sine(48000, 300, 0.3, 0.5)
	for i := range a.Samples {
		a.Samples[i] += 0.1
	}
	b := RemoveDC(a)
	var sum float64
	for _, v := range b.Samples {
		sum += v
	}
	if math.Abs(sum/float64(len(b.Samples))) > 1e-9 {
		t.Error("DC remains")
	}
	c := sine(48000, 300, 0.3, 0.5)
	clean := append([]float64(nil), c.Samples...)
	c.Samples[10000] += 0.6
	c.Samples[20000] -= 0.5
	fixed, n := DeClick(c)
	if n != 2 {
		t.Fatalf("repaired %d samples, want 2", n)
	}
	for _, i := range []int{10000, 20000} {
		if math.Abs(fixed.Samples[i]-clean[i]) > 0.01 {
			t.Errorf("sample %d = %v, want about %v", i, fixed.Samples[i], clean[i])
		}
	}
	// A clean tone is left alone.
	if _, n := DeClick(sine(48000, 300, 0.3, 0.5)); n != 0 {
		t.Errorf("repaired %d samples of a clean tone", n)
	}
}

func TestSliceBySilence(t *testing.T) {
	rate := 48000
	var s []float64
	gap := make([]float64, rate/2)
	for i := 0; i < 3; i++ {
		s = append(s, gap...)
		s = append(s, burst(rate, 400, 0.3, 0.3+0.1*float64(i))...)
	}
	s = append(s, gap...)
	spans := SliceBySilence(Audio{Rate: rate, Samples: s}, -45, 200, 80)
	if len(spans) != 3 {
		t.Fatalf("spans = %v, want 3", spans)
	}
	for i, sp := range spans {
		want := int((0.3 + 0.1*float64(i)) * float64(rate))
		if got := sp.To - sp.From; got < want || got > want+rate/5+rate/100 {
			t.Errorf("span %d length %d, want about %d", i, got, want)
		}
	}
	if len(SliceBySilence(Audio{Rate: rate, Samples: make([]float64, rate)}, -45, 200, 80)) != 0 {
		t.Error("silence produced spans")
	}
}

func TestSliceByClaps(t *testing.T) {
	rate := 48000
	clap := make([]float64, 200)
	for i := range clap {
		clap[i] = 0.95 * math.Pow(-1, float64(i)) * math.Exp(-float64(i)/60)
	}
	var s []float64
	gap := make([]float64, rate/4)
	for i := 0; i < 3; i++ {
		s = append(s, clap...)
		s = append(s, gap...)
		s = append(s, burst(rate, 400, 0.2, 0.4)...)
		s = append(s, gap...)
	}
	spans := SliceByClaps(Audio{Rate: rate, Samples: s}, -3, 100)
	if len(spans) != 3 {
		t.Fatalf("spans = %v, want 3", spans)
	}
	for i, sp := range spans {
		take := Cut(Audio{Rate: rate, Samples: s}, sp)
		if Peak(take) > 0.25 || Peak(take) < 0.15 {
			t.Errorf("take %d peak %v: a clap leaked in or the speech was cut", i, Peak(take))
		}
	}
}

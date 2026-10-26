package parity

import (
	_ "embed"
	"os"
	"testing"
)

//go:embed testdata/corpus.json
var corpusGolden []byte

// TestCorpusMatchesGolden runs the built-in corpus and demands the recorded
// hashes. The golden file is embedded, so the compiled test binary can be
// copied to another machine (for example an Apple Silicon Mac) and run alone.
//
// Set DET_PARITY_REGEN=path to write the golden file to path from this run.
func TestCorpusMatchesGolden(t *testing.T) {
	got := RunCorpus()
	if path := os.Getenv("DET_PARITY_REGEN"); path != "" {
		b, err := got.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}
	want, err := Unmarshal(corpusGolden)
	if err != nil {
		t.Fatal(err)
	}
	if d := Compare(want, got); d != nil {
		t.Fatalf("corpus differs from the golden trace: %v\nThis platform does not reproduce the recorded bits.", d)
	}
	t.Logf("corpus digest %s over %d steps", got.Digest(), len(got.Steps))
}

func TestRecorderAndCompare(t *testing.T) {
	v := 1.0
	mk := func(vals ...float64) Trace {
		r := NewRecorder("t", func(dst []byte) []byte { return AppendFloat64(dst, v) })
		for _, x := range vals {
			v = x
			r.Step()
		}
		return r.Trace()
	}
	a := mk(1, 2, 3)
	if d := Compare(a, mk(1, 2, 3)); d != nil {
		t.Fatalf("identical runs diverge: %v", d)
	}
	d := Compare(a, mk(1, 2.0000000000000004, 3))
	if d == nil || d.Step != 1 {
		t.Fatalf("want divergence at step 1, got %v", d)
	}
	if d := Compare(a, mk(1, 2)); d == nil || d.Step != 2 || d.WantLen != 3 || d.GotLen != 2 {
		t.Fatalf("want length divergence at step 2, got %+v", d)
	}
	if mk(0).Steps[0] == mk(negZero()).Steps[0] {
		t.Fatal("the sign of zero must change the hash")
	}
}

func negZero() float64 {
	z := 0.0
	return -z
}

func TestTraceJSONRoundTrip(t *testing.T) {
	tr := RunCorpus()
	b, err := tr.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Unmarshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if d := Compare(tr, back); d != nil || tr.Digest() != back.Digest() {
		t.Fatalf("round trip changed the trace: %v", d)
	}
}

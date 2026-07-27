package model

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"
)

// defaultModelDim is the embedding width of MiniLM-L6-v2, the model
// weights/minilm-l6-v2.gsxt is named for.
const defaultModelDim = 384

// TestDefaultWeightsPlaceholderIsRecordedNotHidden replaces a skip that could
// never be passed over.
//
// The file weights/minilm-l6-v2.gsxt is committed at 0 bytes, so HasWeights()
// returns false on every machine and in continuous integration, forever. The old
// test opened with:
//
//	if !HasWeights() { t.Skip("no embedded weights — run convert tool first") }
//
// which hid the empty placeholder AND left New, LoadFromBytes, buildModel,
// loadLayer and defaultTokenize at 0% coverage. A skip that is always taken is a
// deleted test with extra steps.
//
// The placeholder stays empty on purpose: real MiniLM-L6-v2 weights are about
// 90 MB, which does not belong in a library repository. But nothing in this
// repository fills it either — there is no converter command, no build step and
// no fetch script, and the skip message named a tool that does not exist. So this
// test RECORDS the state instead of hiding it, and it fails the moment the state
// changes without the coverage below being switched over.
//
// TestNewMatchesTheEmbeddedWeights asserts the behaviour that follows from the
// state, and TestLoadFromBytesBuildsAWorkingModel covers the load path through a
// synthetic tensor file, which is the same code path New takes.
func TestDefaultWeightsPlaceholderIsRecordedNotHidden(t *testing.T) {
	if len(embeddedWeights) != 0 {
		t.Fatalf("weights/minilm-l6-v2.gsxt now holds %d bytes. Real weights arrived, so the "+
			"default-model path can be tested directly: give TestNewMatchesTheEmbeddedWeights the "+
			"dimension and vector checks, and say in this test where the weights come from.",
			len(embeddedWeights))
	}
	if HasWeights() {
		t.Fatal("HasWeights() is true over a 0-byte embed; the length test in load.go broke")
	}
}

// TestNewMatchesTheEmbeddedWeights asserts what New does, in whichever state the
// embedded file is in. Both branches assert; neither skips. So the test keeps
// working when real weights land, and it fails if New stops matching the file.
func TestNewMatchesTheEmbeddedWeights(t *testing.T) {
	m, err := New()

	if !HasWeights() {
		if err == nil {
			t.Fatal("New() succeeded with no embedded weights; it must refuse rather than " +
				"return a model with an empty vocabulary")
		}
		if m != nil {
			t.Fatalf("New() returned both an error and a model (%v)", m)
		}
		// The message is the only thing a caller has to work from, so pin the part
		// that tells them what to do.
		if !strings.Contains(err.Error(), "LoadFromBytes") {
			t.Errorf("New() error must point the caller at LoadFromBytes, which accepts a "+
				"tensor file the caller supplies; got: %v", err)
		}
		return
	}

	if err != nil {
		t.Fatalf("New() failed with %d bytes of embedded weights: %v", len(embeddedWeights), err)
	}
	if m.Dim() != defaultModelDim {
		t.Errorf("New().Dim() = %d, want %d", m.Dim(), defaultModelDim)
	}
	vec, err := m.Encode("hello world")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(vec) != defaultModelDim {
		t.Fatalf("Encode returned %d values, want %d", len(vec), defaultModelDim)
	}
}

// syntheticWeights writes a tensor file shaped like the real one, with the tensor
// names buildModel demands and small random values.
//
// This is the injected fake model the default path needs. New() calls
// LoadFromBytes over the embedded bytes and nothing else, so exercising
// LoadFromBytes over a synthetic file covers every line New would reach.
func syntheticWeights(t *testing.T, dim, layers, ffDim, vocab, maxSeq int) []byte {
	t.Helper()
	rng := rand.New(rand.NewSource(7))
	tensor := func(name string, shape ...int) Tensor {
		n := 1
		for _, d := range shape {
			n *= d
		}
		data := make([]float32, n)
		for i := range data {
			data[i] = float32(rng.NormFloat64()) * 0.05
		}
		return Tensor{Name: name, Shape: shape, Data: data}
	}
	// A layer norm with gamma 1 and beta 0 keeps the forward pass numerically
	// tame, so a norm assertion below tests the math and not the random draw.
	norm := func(prefix string) []Tensor {
		gamma := make([]float32, dim)
		for i := range gamma {
			gamma[i] = 1
		}
		return []Tensor{
			{Name: prefix + ".weight", Shape: []int{dim}, Data: gamma},
			{Name: prefix + ".bias", Shape: []int{dim}, Data: make([]float32, dim)},
		}
	}

	tensors := []Tensor{
		tensor("embeddings.word_embeddings.weight", vocab, dim),
		tensor("embeddings.position_embeddings.weight", maxSeq, dim),
	}
	tensors = append(tensors, norm("embeddings.LayerNorm")...)

	for i := 0; i < layers; i++ {
		p := fmt.Sprintf("encoder.layer.%d.", i)
		for _, which := range []string{"query", "key", "value"} {
			tensors = append(tensors,
				tensor(p+"attention.self."+which+".weight", dim, dim),
				tensor(p+"attention.self."+which+".bias", dim),
			)
		}
		tensors = append(tensors,
			tensor(p+"attention.output.dense.weight", dim, dim),
			tensor(p+"attention.output.dense.bias", dim),
		)
		tensors = append(tensors, norm(p+"attention.output.LayerNorm")...)
		tensors = append(tensors,
			tensor(p+"intermediate.dense.weight", ffDim, dim),
			tensor(p+"intermediate.dense.bias", ffDim),
			tensor(p+"output.dense.weight", dim, ffDim),
			tensor(p+"output.dense.bias", dim),
		)
		tensors = append(tensors, norm(p+"output.LayerNorm")...)
	}

	var buf bytes.Buffer
	if err := WriteTensorFile(&buf, tensors); err != nil {
		t.Fatalf("write synthetic tensor file: %v", err)
	}
	return buf.Bytes()
}

// TestLoadFromBytesBuildsAWorkingModel covers the load path the empty
// placeholder left untested: LoadFromBytes, buildModel, loadLayer and
// defaultTokenize.
//
// The synthetic file carries the same tensor names and the same shape relations
// as the real one, so buildModel derives dim, vocab, maxSeq, the layer count and
// ffDim from the file exactly as it would from real weights.
func TestLoadFromBytesBuildsAWorkingModel(t *testing.T) {
	const (
		dim    = 32
		layers = 2
		ffDim  = 64
		vocab  = 128
		maxSeq = 16
	)
	m, err := LoadFromBytes(syntheticWeights(t, dim, layers, ffDim, vocab, maxSeq))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}

	// Every shape buildModel derives from the file, not from an argument.
	if m.Dim() != dim {
		t.Errorf("Dim() = %d, want %d (from the word-embedding shape)", m.Dim(), dim)
	}
	if m.vocabSize != vocab {
		t.Errorf("vocabSize = %d, want %d", m.vocabSize, vocab)
	}
	if m.maxSeq != maxSeq {
		t.Errorf("maxSeq = %d, want %d (from the position-embedding shape)", m.maxSeq, maxSeq)
	}
	if len(m.layers) != layers {
		t.Fatalf("built %d layers, want %d; buildModel counts them by scanning tensor names",
			len(m.layers), layers)
	}
	if got := m.layers[0].FFDim; got != ffDim {
		t.Errorf("FFDim = %d, want %d (from the intermediate-dense shape)", got, ffDim)
	}
	// dim is 32, so the head rule in buildModel gives dim/16.
	if got := m.layers[0].Heads; got != dim/16 {
		t.Errorf("Heads = %d, want %d", got, dim/16)
	}

	// The fused QKV block must hold the three projections back to back, or
	// attention reads key weights as query weights.
	if got, want := len(m.layers[0].QKV.Weight), 3*dim*dim; got != want {
		t.Errorf("QKV weight length = %d, want %d", got, want)
	}
	if got, want := len(m.layers[0].QKV.Bias), 3*dim; got != want {
		t.Errorf("QKV bias length = %d, want %d", got, want)
	}

	vec, err := m.Encode("hello world from a synthetic model")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(vec) != dim {
		t.Fatalf("Encode returned %d values, want %d", len(vec), dim)
	}
	var normSq float64
	for _, v := range vec {
		normSq += float64(v) * float64(v)
	}
	if math.Abs(normSq-1.0) > 1e-3 {
		t.Errorf("Encode output is not L2-normalized: norm^2 = %f, want 1.0", normSq)
	}

	batch, err := m.EncodeBatch([]string{"one", "two", "three"})
	if err != nil {
		t.Fatalf("EncodeBatch: %v", err)
	}
	if len(batch) != 3 {
		t.Fatalf("EncodeBatch returned %d vectors, want 3", len(batch))
	}
	for i, got := range batch {
		if len(got) != dim {
			t.Errorf("EncodeBatch[%d] has %d values, want %d", i, len(got), dim)
		}
	}
}

// TestLoadFromBytesRejectsBrokenInput proves the loader reports a bad file rather
// than building a model with missing tensors. A silent partial load would produce
// embeddings that look valid and mean nothing.
func TestLoadFromBytesRejectsBrokenInput(t *testing.T) {
	good := syntheticWeights(t, 32, 1, 64, 64, 16)

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{name: "empty", data: nil, want: "read tensor file"},
		{name: "wrong magic", data: append([]byte("NOPE"), good[4:]...), want: "bad magic"},
		{name: "truncated body", data: good[:len(good)/2], want: "read tensor file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := LoadFromBytes(tc.data)
			if err == nil {
				t.Fatalf("LoadFromBytes accepted %s input and returned a model", tc.name)
			}
			if m != nil {
				t.Errorf("LoadFromBytes returned a model alongside the error: %v", m)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error must name the failure; want it to contain %q, got: %v", tc.want, err)
			}
		})
	}
}

// TestLoadFromBytesNamesTheMissingTensor pins the diagnostic a partial weight
// file must produce. Dropping the position embeddings is the realistic version of
// this failure, because a converter that renames one key produces exactly it.
func TestLoadFromBytesRejectsAMissingTensor(t *testing.T) {
	const dropped = "embeddings.position_embeddings.weight"

	var buf bytes.Buffer
	full, err := ReadTensorFile(bytes.NewReader(syntheticWeights(t, 32, 1, 64, 64, 16)))
	if err != nil {
		t.Fatalf("read synthetic file back: %v", err)
	}
	kept := make([]Tensor, 0, len(full))
	for _, tensor := range full {
		if tensor.Name == dropped {
			continue
		}
		kept = append(kept, tensor)
	}
	if len(kept) == len(full) {
		t.Fatalf("%q was not in the synthetic file, so this test drops nothing", dropped)
	}
	if err := WriteTensorFile(&buf, kept); err != nil {
		t.Fatalf("write reduced file: %v", err)
	}

	if _, err := LoadFromBytes(buf.Bytes()); err == nil {
		t.Fatalf("LoadFromBytes accepted a file with no %q", dropped)
	} else if !strings.Contains(err.Error(), dropped) {
		t.Errorf("the error must name the missing tensor %q; got: %v", dropped, err)
	}
}

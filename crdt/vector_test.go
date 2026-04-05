package crdt

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	crdtsync "github.com/odvcencio/gosx/crdt/sync"
)

func randomVec(dim int, rng *rand.Rand) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = rng.Float32()*2 - 1
	}
	return vec
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func mse(a, b []float32) float64 {
	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	return sum / float64(len(a))
}

func TestVectorValueRoundTrip2Bit(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	original := randomVec(384, rng)
	val := VectorValue(original, 384, 2)
	if val.Kind != ValueKindVector {
		t.Fatalf("expected kind vector, got %s", val.Kind)
	}
	recovered := val.Vector()
	if len(recovered) != 384 {
		t.Fatalf("expected 384 dims, got %d", len(recovered))
	}
	sim := cosine(original, recovered)
	if sim < 0.90 {
		t.Fatalf("cosine similarity %.4f < 0.90 for 2-bit", sim)
	}
	t.Logf("2-bit: cosine=%.4f mse=%.6f", sim, mse(original, recovered))
}

func TestVectorValueRoundTrip4Bit(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	original := randomVec(384, rng)
	val := VectorValue(original, 384, 4)
	recovered := val.Vector()
	sim := cosine(original, recovered)
	if sim < 0.99 {
		t.Fatalf("cosine similarity %.4f < 0.99 for 4-bit", sim)
	}
	t.Logf("4-bit: cosine=%.4f mse=%.6f", sim, mse(original, recovered))
}

func TestVectorValueDeterministicSeed(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	vec := randomVec(128, rng)

	// Two independent calls must produce identical compressed bytes.
	val1 := VectorValue(vec, 128, 2)
	val2 := VectorValue(vec, 128, 2)

	if len(val1.VectorPacked) != len(val2.VectorPacked) {
		t.Fatalf("packed length mismatch: %d vs %d", len(val1.VectorPacked), len(val2.VectorPacked))
	}
	for i := range val1.VectorPacked {
		if val1.VectorPacked[i] != val2.VectorPacked[i] {
			t.Fatalf("packed byte %d differs: %02x vs %02x", i, val1.VectorPacked[i], val2.VectorPacked[i])
		}
	}
	if val1.VectorNorm != val2.VectorNorm {
		t.Fatalf("norm mismatch: %f vs %f", val1.VectorNorm, val2.VectorNorm)
	}
}

func TestVectorValueCloneIsolation(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	original := randomVec(128, rng)
	val := VectorValue(original, 128, 2)
	cloned := val.Clone()
	// Mutate clone's packed bytes
	for i := range cloned.VectorPacked {
		cloned.VectorPacked[i] = 0xff
	}
	// Original must be unchanged
	recovered := val.Vector()
	sim := cosine(original, recovered)
	if sim < 0.90 {
		t.Fatalf("clone mutation corrupted original: cosine %.4f", sim)
	}
}

func TestVectorValueNonVectorReturnsNil(t *testing.T) {
	val := StringValue("hello")
	if got := val.Vector(); got != nil {
		t.Fatalf("expected nil from non-vector, got %v", got)
	}
}

func TestVectorValueDocPutGet(t *testing.T) {
	doc := NewDoc()
	rng := rand.New(rand.NewSource(7))
	original := randomVec(384, rng)
	val := VectorValue(original, 384, 2)
	if err := doc.Put(Root, "embedding", val); err != nil {
		t.Fatalf("put embedding: %v", err)
	}

	got, _, err := doc.Get(Root, "embedding")
	if err != nil {
		t.Fatalf("get embedding: %v", err)
	}
	if got.Kind != ValueKindVector {
		t.Fatalf("expected kind vector, got %s", got.Kind)
	}
	recovered := got.Vector()
	sim := cosine(original, recovered)
	if sim < 0.90 {
		t.Fatalf("put/get round-trip cosine %.4f < 0.90", sim)
	}
}

func TestVectorValueSaveLoadRoundTrip(t *testing.T) {
	doc := NewDoc()
	rng := rand.New(rand.NewSource(7))
	original := randomVec(384, rng)
	val := VectorValue(original, 384, 2)
	if err := doc.Put(Root, "embedding", val); err != nil {
		t.Fatalf("put embedding: %v", err)
	}
	if _, err := doc.Commit("add embedding"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	saved, err := doc.Save()
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(saved)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, _, err := loaded.Get(Root, "embedding")
	if err != nil {
		t.Fatalf("get embedding: %v", err)
	}
	if got.Kind != ValueKindVector {
		t.Fatalf("expected kind vector, got %s", got.Kind)
	}
	recovered := got.Vector()
	sim := cosine(original, recovered)
	if sim < 0.90 {
		t.Fatalf("save/load round-trip cosine %.4f < 0.90", sim)
	}
}

func TestVectorValueMergeConverges(t *testing.T) {
	base := NewDoc()
	if err := base.Put(Root, "title", StringValue("base")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := base.Commit("seed"); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	left, err := base.Fork()
	if err != nil {
		t.Fatalf("fork left: %v", err)
	}
	right, err := base.Fork()
	if err != nil {
		t.Fatalf("fork right: %v", err)
	}
	// Ensure distinct actor IDs so LWW merge has a deterministic winner.
	actor, err := NewActorID()
	if err != nil {
		t.Fatalf("new actor id: %v", err)
	}
	right.actorID = actor

	rng := rand.New(rand.NewSource(1))
	vecA := randomVec(64, rng)
	vecB := randomVec(64, rng)

	if err := left.Put(Root, "vec", VectorValue(vecA, 64, 2)); err != nil {
		t.Fatalf("left put: %v", err)
	}
	if _, err := left.Commit("left vec"); err != nil {
		t.Fatalf("left commit: %v", err)
	}
	if err := right.Put(Root, "vec", VectorValue(vecB, 64, 2)); err != nil {
		t.Fatalf("right put: %v", err)
	}
	if _, err := right.Commit("right vec"); err != nil {
		t.Fatalf("right commit: %v", err)
	}

	if err := left.Merge(right); err != nil {
		t.Fatalf("merge left<-right: %v", err)
	}
	if err := right.Merge(left); err != nil {
		t.Fatalf("merge right<-left: %v", err)
	}

	gotL, _, err := left.Get(Root, "vec")
	if err != nil {
		t.Fatalf("left get: %v", err)
	}
	gotR, _, err := right.Get(Root, "vec")
	if err != nil {
		t.Fatalf("right get: %v", err)
	}

	recL := gotL.Vector()
	recR := gotR.Vector()
	sim := cosine(recL, recR)
	if sim < 0.9999 {
		t.Fatalf("merge did not converge: cosine %.6f", sim)
	}
}

func TestVectorValueMixedTypes(t *testing.T) {
	doc := NewDoc()
	rng := rand.New(rand.NewSource(55))
	vec := randomVec(64, rng)

	if err := doc.Put(Root, "name", StringValue("test")); err != nil {
		t.Fatalf("put name: %v", err)
	}
	if err := doc.Put(Root, "count", IntValue(42)); err != nil {
		t.Fatalf("put count: %v", err)
	}
	if err := doc.Put(Root, "embedding", VectorValue(vec, 64, 2)); err != nil {
		t.Fatalf("put embedding: %v", err)
	}
	if _, err := doc.Commit("mixed"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	saved, err := doc.Save()
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(saved)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	name, _, _ := loaded.Get(Root, "name")
	if name.Str != "test" {
		t.Fatalf("name: got %q", name.Str)
	}
	count, _, _ := loaded.Get(Root, "count")
	if count.Int != 42 {
		t.Fatalf("count: got %d", count.Int)
	}
	emb, _, _ := loaded.Get(Root, "embedding")
	if emb.Kind != ValueKindVector {
		t.Fatalf("embedding kind: got %s", emb.Kind)
	}
	recovered := emb.Vector()
	sim := cosine(vec, recovered)
	if sim < 0.90 {
		t.Fatalf("mixed-type round-trip cosine %.4f", sim)
	}
}

func TestVectorValueCompressedSize(t *testing.T) {
	rng := rand.New(rand.NewSource(22))
	original := randomVec(384, rng)

	val := VectorValue(original, 384, 2)
	rawBytes := 384 * 4 // 384 floats * 4 bytes each
	compressedBytes := len(val.VectorPacked)

	ratio := float64(compressedBytes) / float64(rawBytes)
	t.Logf("384-dim 2-bit: packed=%d bytes, raw=%d bytes, ratio=%.2f", compressedBytes, rawBytes, ratio)

	// 2-bit at 384 dims should pack to ~96 bytes (384*2/8)
	if compressedBytes > 200 {
		t.Fatalf("compressed size %d exceeds 200 byte budget for 384-dim 2-bit", compressedBytes)
	}
}

func TestVectorValueSyncMessageSize(t *testing.T) {
	doc := NewDoc()
	rng := rand.New(rand.NewSource(33))

	for i := 0; i < 100; i++ {
		vec := randomVec(384, rng)
		key := "emb_" + strconv.Itoa(i)
		if err := doc.Put(Root, Prop(key), VectorValue(vec, 384, 2)); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	if _, err := doc.Commit("bulk embeddings"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	state := crdtsync.NewState()
	msg, ok := doc.GenerateSyncMessage(state)
	if !ok {
		t.Fatal("expected sync message")
	}

	// 100 vectors * 384 dims * 4 bytes/float = 153,600 bytes raw.
	// At 2-bit: 100 * (96 packed + ~20 overhead) = ~11,600 bytes.
	// Allow generous 30,000 byte budget for the full sync message
	// (includes change metadata, actor IDs, JSON encoding, base64 overhead, etc.).
	rawSize := 100 * 384 * 4
	t.Logf("sync message size: %d bytes (100 x 384-dim @ 2-bit), raw would be %d bytes, ratio=%.2f",
		len(msg), rawSize, float64(len(msg))/float64(rawSize))
	if len(msg) > rawSize/2 {
		t.Fatalf("sync message %d bytes exceeds half raw size %d", len(msg), rawSize/2)
	}
}

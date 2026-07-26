package semantic

// Candidate sizing for the two-stage search that every consumer in this package
// runs: a fast scan over TurboQuant-quantized vectors, then an exact cosine
// re-rank over a small candidate set.
//
// The exact re-rank is not redundant. Measured over 1000 random 128-dimension
// unit vectors at 3-bit quantization (see TestQuantizedRecallAgainstExactRanking):
//
//	quantized only          recall@5 = 0.527   top-1 = 0.425
//	over-fetch x2  re-rank  recall@5 = 0.728   top-1 = 0.910
//	over-fetch x4  re-rank  recall@5 = 0.880   top-1 = 0.975
//	over-fetch x8  re-rank  recall@5 = 0.972   top-1 = 0.995
//	over-fetch x16 re-rank  recall@5 = 0.998   top-1 = 1.000
//	whole store    re-rank  recall@5 = 1.000   top-1 = 1.000
//
// Dropping the re-rank would cut top-1 accuracy to 42.5 percent, so the
// unquantized copies stay. Re-ranking the whole store instead costs a second
// full pass and defeats the index, so the candidate set is bounded.
//
// The package fetches overFetchFactor times k candidates, with a floor for small
// k. That reaches the accuracy of a whole-store re-rank while scoring a small
// fraction of the store exactly.
const (
	// overFetchFactor multiplies the caller's k to size the candidate set.
	overFetchFactor = 16
	// candidateFloor is the smallest candidate set the package requests. It keeps
	// the accuracy of threshold matching, where the caller asks for one result
	// but a near tie decides the answer.
	candidateFloor = 64
)

// candidateCount returns the number of quantized candidates to fetch for a
// caller that wants k exact results out of a store holding size entries.
func candidateCount(k, size int) int {
	if k <= 0 || size <= 0 {
		return 0
	}
	count := k * overFetchFactor
	if count < candidateFloor {
		count = candidateFloor
	}
	if count > size {
		count = size
	}
	return count
}

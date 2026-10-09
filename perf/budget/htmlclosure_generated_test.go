package budget

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"m31labs.dev/gosx/internal/assetmeasure"
	"m31labs.dev/gosx/internal/pagecaps"
)

// This encoder isolates document traversal from compressor performance. The
// separate srcdoc regressions verify actual whole-document compression.
func executionCorpusEncoder(body []byte) (assetmeasure.Sizes, error) {
	n := int64(len(body))
	return assetmeasure.Sizes{Raw: n, Gzip: n, Brotli: n, SHA256: testMeasureHash(body)}, nil
}

func TestInlineActiveDocumentClosureCorpus(t *testing.T) {
	rng := rand.New(rand.NewSource(55002))
	seeds := documentExecutableSeeds()
	dir := t.TempDir()
	info := publicTestReport(t).Info
	placements, rejected, sums := 0, 0, 0
	for i, seed := range seeds {
		for variant := 0; variant < 8; variant++ {
			markup := documentSeedVariant(seed, variant, rng)
			t.Run(fmt.Sprintf("%02d-%s/%d", i, seed.name, variant), func(t *testing.T) {
				root, err := measureHTML([]byte(markup), HTMLMeasureOptions{}, executionCorpusEncoder)
				if err != nil {
					t.Fatal(err)
				}
				assertExecution := func(body string) (HTMLMeasurement, bool) {
					caps, detectErr := pagecaps.FromHTML([]byte(body))
					got, measureErr := measureHTML([]byte(body), HTMLMeasureOptions{}, executionCorpusEncoder)
					placements++
					if detectErr != nil || measureErr != nil {
						var input *InputError
						// Some seeds already contain srcdoc. Adding an outer
						// placement can exceed the total crossing bound. Both
						// paths must reject, including executable prefixes.
						if detectErr == nil || !errors.As(measureErr, &input) || input.Code != "capability" || input.Pointer != "/html" {
							t.Fatalf("nesting rejection differs: detect=%v measure=%v", detectErr, measureErr)
						}
						rejected++
						return HTMLMeasurement{}, false
					}
					if documentZeroJS(caps) != seed.zeroJS || got.HTMLExecution != root.HTMLExecution {
						t.Fatalf("placement differs: zero-js=%t want=%t execution=%+v want=%+v", documentZeroJS(caps), seed.zeroJS, got.HTMLExecution, root.HTMLExecution)
					}
					if !seed.zeroJS && got.ExecutableSources == 0 {
						t.Fatal("detected execution has no measured sources")
					}
					return got, true
				}
				if _, ok := assertExecution(markup); !ok {
					t.Fatal("root seed rejected")
				}
				child := measureCorpusReport(t, dir, info, markup, true, executionCorpusEncoder)
				placements++
				if child.execution["/counter/"] != root.HTMLExecution {
					t.Fatalf("reachable child differs: %+v want %+v", child.execution["/counter/"], root.HTMLExecution)
				}
				for _, row := range child.Rows {
					for _, policy := range row.Policies {
						if policy.Name == "zero-js" && policy.Passed != seed.zeroJS {
							t.Fatal("child execution verdict differs", policy)
						}
					}
				}
				for depth := 1; depth <= pagecaps.MaxSrcdocDepth; depth++ {
					assertExecution(srcdocWrap(markup, depth, ""))
				}
				// Four copies at different positions: root, fetched child,
				// srcdoc depth one and srcdoc depth two. Max stays per script;
				// every additive observation must be exactly four times root.
				outer := markup + `<iframe src="/child/"></iframe>` + srcdocWrap(markup, 1, "") + srcdocWrap(markup, 2, "")
				all := measureCorpusDocuments(t, dir, info, outer, markup, executionCorpusEncoder)
				want := HTMLExecution{
					ExecutableSources: 4 * root.ExecutableSources, ExecutableScripts: 4 * root.ExecutableScripts,
					SyncExecutableScripts: 4 * root.SyncExecutableScripts, InlineAppScriptBytes: 4 * root.InlineAppScriptBytes,
					InlineAppScriptMax: root.InlineAppScriptMax,
				}
				if all.execution["/counter/"] != want {
					t.Fatalf("closure sum differs: %+v want %+v", all.execution["/counter/"], want)
				}
				sums++
			})
		}
	}
	t.Logf("seed=55002 snippets=%d placements=%d over-depth rejections=%d four-placement sums=%d", len(seeds)*8, placements, rejected, sums)
}

# E4 — Cross-repo byte-equality test for the WGSL goldens (repo: elio)

Depends on: E2 (goldens in Elio) and gosx G05 (copies in gosx testdata).

## Goal

GoSX embeds the cull and Hi-Z WGSL as string arrays and checks them against
`client/js/testdata/elio/*.wgsl` (G05 test part A). This test checks those
gosx copies against Elio's goldens, so an Elio kernel change that is not
carried into gosx fails in Elio's own suite. It skips cleanly when the gosx
sibling is absent or predates G05.

## Step — create `stdlib/gpudriven_gosx_sync_test.go`

Exact content:

```go
package stdlib

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestGPUDrivenWGSLMatchesGoSX keeps the WGSL that GoSX embeds for its
// GPU-driven instancing host byte-identical to the goldens here. GoSX keeps
// copies in client/js/testdata/elio/ and checks its embedded strings against
// them, so this test closes the loop across the two repositories. It skips
// when the gosx sibling checkout (the go.mod replace target) is absent.
func TestGPUDrivenWGSLMatchesGoSX(t *testing.T) {
	pairs := []struct{ golden, gosx string }{
		{"gpudriven/cull.wgsl", "gpudriven_cull.wgsl"},
		{"gpudriven/hiz_downsample.wgsl", "gpudriven_hiz_downsample.wgsl"},
	}
	for _, pair := range pairs {
		want, err := os.ReadFile(pair.golden)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join("..", "..", "gosx", "client", "js", "testdata", "elio", pair.gosx)
		got, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			t.Skipf("gosx sibling checkout has no %s; run gosx task G05 first", path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from %s: regenerate the golden here, copy it to gosx, and re-embed it (spec task G05)", path, pair.golden)
		}
	}
}
```

The test runs with the package directory as its working directory, so
`../../gosx` is `<root>/gosx`, the same sibling Elio's `go.mod` replace uses.

## Verify

```sh
cd <root>/elio
gofmt -l stdlib/gpudriven_gosx_sync_test.go          # prints nothing
go test ./stdlib -run TestGPUDrivenWGSLMatchesGoSX -count=1 -v
go test ./... -count=1
```

- With gosx at G05 or later: `--- PASS: TestGPUDrivenWGSLMatchesGoSX`.
- With gosx before G05: `--- SKIP` naming the missing file.
- Negative check (validation run): appending one line to the gosx copy of
  `gpudriven_cull.wgsl` makes the test FAIL; restore the file afterwards with
  `git checkout`.

## Commit (elio)

`test(stdlib): check gpu-driven WGSL goldens against the gosx copies`

- fail when gosx embeds a stale copy of the cull or Hi-Z kernel
- skip when the gosx sibling checkout does not carry the copies yet

# E1 — Kernel sources and Go wrappers (repo: elio)

Depends on: E0.

## Goal

Add the two Scene3D GPU-driven kernels to Elio's stdlib as `.elio` source
files, with Go accessors that parse them. GoSX never imports Elio. It embeds
the WGSL these sources emit (E2 → G05).

## Files to create

### 1. `stdlib/gpudriven/cull.elio`

Copy appendix A §A1 exactly. Then check:
`sha256sum stdlib/gpudriven/cull.elio` →
`01fdea902048ae858190988531209a87d468d475c94e38a7f101375cae54a5f8`.

### 2. `stdlib/gpudriven/hiz_downsample.elio`

Copy appendix A §A2 exactly. Then check:
`sha256sum stdlib/gpudriven/hiz_downsample.elio` →
`57626c7a65fc2f7e1cc456a173879105158ebfdf8616658a83ec3f7511094649`.

### 3. `stdlib/gpudriven.go`

```go
package stdlib

import (
	_ "embed"
	"fmt"

	"m31labs.dev/elio/ir"
	"m31labs.dev/elio/parse"
)

// GPUDrivenCullSource is the .elio source of the Scene3D GPU-driven instance
// cull: frustum, optional distance and contribution culls, two-phase Hi-Z
// occlusion, and shadow-light views. GoSX embeds the WGSL that
// emit/wgsl produces from it in client/runtime/scene3d/indirect-instancing.ts.
//
//go:embed gpudriven/cull.elio
var GPUDrivenCullSource string

// GPUDrivenHiZDownsampleSource is the .elio source of one clamped Hi-Z pyramid
// level over a single packed buffer.
//
//go:embed gpudriven/hiz_downsample.elio
var GPUDrivenHiZDownsampleSource string

// GPUDrivenCull parses GPUDrivenCullSource. Entry kernel: "cull".
func GPUDrivenCull() (*ir.Module, error) {
	mod, err := parse.Parse(GPUDrivenCullSource)
	if err != nil {
		return nil, fmt.Errorf("stdlib: gpudriven/cull.elio: %w", err)
	}
	return mod, nil
}

// GPUDrivenHiZDownsample parses GPUDrivenHiZDownsampleSource. Entry kernel:
// "downsample".
func GPUDrivenHiZDownsample() (*ir.Module, error) {
	mod, err := parse.Parse(GPUDrivenHiZDownsampleSource)
	if err != nil {
		return nil, fmt.Errorf("stdlib: gpudriven/hiz_downsample.elio: %w", err)
	}
	return mod, nil
}
```

`stdlib` importing `parse` creates no import cycle: `parse` imports only `ir`
and gotreesitter. This was checked with `go vet ./stdlib`.

### 4. `stdlib/gpudriven_valid_test.go`

```go
package stdlib

import (
	"testing"

	"m31labs.dev/elio/sema"
)

// TestGPUDrivenKernelsAreValid pins that both embedded GoSX kernels parse,
// pass semantic analysis, and expose the entry names GoSX dispatches.
func TestGPUDrivenKernelsAreValid(t *testing.T) {
	cull, err := GPUDrivenCull()
	if err != nil {
		t.Fatal(err)
	}
	if errs := sema.Check(cull); len(errs) != 0 {
		t.Fatalf("cull.elio failed sema:\n%v", sema.Errors(errs))
	}
	down, err := GPUDrivenHiZDownsample()
	if err != nil {
		t.Fatal(err)
	}
	if errs := sema.Check(down); len(errs) != 0 {
		t.Fatalf("hiz_downsample.elio failed sema:\n%v", sema.Errors(errs))
	}
	if got := cull.Kernels[0].Name; got != "cull" {
		t.Fatalf("cull entry = %q, want cull", got)
	}
	if got := down.Kernels[0].Name; got != "downsample" {
		t.Fatalf("downsample entry = %q, want downsample", got)
	}
}
```

## Verify

```sh
cd <root>/elio
gofmt -l stdlib                     # prints nothing for the new files
go vet ./stdlib
go test ./stdlib -run TestGPUDrivenKernelsAreValid -count=1 -v   # PASS
/tmp/elio check stdlib/gpudriven/cull.elio                        # ok
/tmp/elio check stdlib/gpudriven/hiz_downsample.elio              # ok
go test ./...                                                     # all ok
```

If the test fails with `syntax error near "// ..."`, E0 was skipped. Do E0
first.

## Commit (elio)

`add(stdlib): add scene3d gpu-driven cull and hi-z kernels`

- `gpudriven/cull.elio`: frustum, distance, contribution and two-phase Hi-Z
  occlusion culling plus shadow-light views, one invocation per instance.
- `gpudriven/hiz_downsample.elio`: one clamped max-reduction pyramid level over
  a single packed buffer.
- `GPUDrivenCull` / `GPUDrivenHiZDownsample` parse the embedded sources.

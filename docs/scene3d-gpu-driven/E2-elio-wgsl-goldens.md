# E2 — WGSL goldens and all-backend emission (repo: elio)

Depends on: E1.

## Goal

Pin the exact WGSL that GoSX embeds, and prove that every text backend emits
both kernels.

## Files to create

### 1. `stdlib/gpudriven_emit_test.go`

```go
package stdlib

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/elio/emit/wgsl"
	"m31labs.dev/elio/ir"
)

// TestGPUDrivenWGSLGoldens pins the exact WGSL GoSX embeds. GoSX copies these
// two files byte for byte into client/js/testdata/elio/ and inlines the same
// text in client/runtime/scene3d/indirect-instancing.ts, so a change here is a change
// to the browser runtime. Regenerate with:
//
//	UPDATE_GOLDEN=1 go test ./stdlib -run TestGPUDrivenWGSLGoldens
func TestGPUDrivenWGSLGoldens(t *testing.T) {
	for _, tc := range []struct {
		golden string
		build  func() (*ir.Module, error)
	}{
		{"cull.wgsl", GPUDrivenCull},
		{"hiz_downsample.wgsl", GPUDrivenHiZDownsample},
	} {
		mod, err := tc.build()
		if err != nil {
			t.Fatal(err)
		}
		src, err := wgsl.Emit(mod)
		if err != nil {
			t.Fatalf("%s: emit: %v", tc.golden, err)
		}
		path := filepath.Join("gpudriven", tc.golden)
		if os.Getenv("UPDATE_GOLDEN") != "" {
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s (run UPDATE_GOLDEN=1 to create): %v", path, err)
		}
		if src != string(want) {
			t.Errorf("%s: emitted WGSL differs from the golden; if the change is intended, regenerate it and re-sync GoSX", tc.golden)
		}
	}
}
```

### 2. `conformance/gpudriven_conformance_test.go`

```go
package conformance

import (
	"strings"
	"testing"

	prismvalidate "m31labs.dev/prism/validate"

	"m31labs.dev/elio/emit/glsl"
	"m31labs.dev/elio/emit/metal"
	"m31labs.dev/elio/emit/wgsl"
	"m31labs.dev/elio/ir"
	"m31labs.dev/elio/stdlib"
)

// TestGPUDrivenKernelsAllBackends emits the Scene3D GPU-driven kernels for
// every text backend and validates each with its external validator when the
// tool is on PATH. CPU execution lives in stdlib/gpudriven_test.go.
func TestGPUDrivenKernelsAllBackends(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kernel string
		build  func() (*ir.Module, error)
	}{
		{"cull", "cull", stdlib.GPUDrivenCull},
		{"hiz_downsample", "downsample", stdlib.GPUDrivenHiZDownsample},
	} {
		mod, err := tc.build()
		if err != nil {
			t.Fatal(err)
		}
		wsrc, err := wgsl.Emit(mod)
		if err != nil {
			t.Fatalf("%s wgsl: %v", tc.name, err)
		}
		if !strings.Contains(wsrc, "fn "+tc.kernel+"(") {
			t.Errorf("%s wgsl lacks entry %q", tc.name, tc.kernel)
		}
		gsrc, err := glsl.Emit(mod)
		if err != nil {
			t.Fatalf("%s glsl: %v", tc.name, err)
		}
		msrc, err := metal.Emit(mod)
		if err != nil {
			t.Fatalf("%s metal: %v", tc.name, err)
		}
		if !strings.Contains(msrc, "kernel void "+tc.kernel+"(") {
			t.Errorf("%s metal lacks kernel %q\n%s", tc.name, tc.kernel, msrc)
		}
		// External validators skip their own subtest when the tool is absent,
		// so the emission checks above always run.
		t.Run(tc.name+"/naga", func(t *testing.T) {
			prismvalidate.Shader(t, "naga", wsrc, ".wgsl", func(f string) []string { return []string{f} })
		})
		t.Run(tc.name+"/glslang", func(t *testing.T) {
			prismvalidate.Shader(t, "glslangValidator", gsrc, ".comp", func(f string) []string { return []string{"-V", f, "-S", "comp"} })
		})
	}
}
```

The validator calls sit in subtests on purpose. `prismvalidate.Shader` skips
its test when the tool is missing, and a skip at the top level would also skip
the GLSL and Metal emission checks.

## Steps

1. Create both files.
2. Generate the goldens:
   `UPDATE_GOLDEN=1 go test ./stdlib -run TestGPUDrivenWGSLGoldens -count=1`.
   This writes `stdlib/gpudriven/cull.wgsl` and
   `stdlib/gpudriven/hiz_downsample.wgsl`.
3. Check the goldens byte-for-byte:

   ```sh
   sha256sum stdlib/gpudriven/cull.wgsl stdlib/gpudriven/hiz_downsample.wgsl
   # 86de419e4baf2067b5c3e26067e34abf1e4657a27d8565fdab379c2aa57ad485  stdlib/gpudriven/cull.wgsl
   # d35c9e084df9c15897058429ffdfea3a85fd74a6bf3c48243accd99391cb8428  stdlib/gpudriven/hiz_downsample.wgsl
   ```

   Different hashes mean the `.elio` sources differ from appendix A. Fix the
   sources; never edit a golden by hand.

## Verify

```sh
go test ./stdlib ./conformance -run 'GPUDriven' -count=1 -v
# PASS TestGPUDrivenWGSLGoldens, PASS TestGPUDrivenKernelsAllBackends
# (its naga/glslang subtests SKIP when the tools are absent)
go test ./...
```

## Commit (elio)

`test(stdlib): pin gpu-driven kernel wgsl goldens and backend emission`

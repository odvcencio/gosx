# G02 — Go authoring surface: `scene.GPUDriven` and `SceneIR.GPUDriven` (repo: gosx)

Depends on: 01. Touches only Go files under `scene/`.

## Goal

Authors opt in with `scene.Props{GPUDriven: &scene.GPUDriven{...}}`. Lowering
emits `"gpuDriven": {"occlusion": ..., "shadowCulling": ...}` on the SceneIR
wire. The diff protocol reports a change of the field in
`Diff.RemountFields`, because no command kind carries it.

Design decisions (do not change them):

- `Props.GPUDriven` is a pointer. `nil` means "not opted in" and the wire
  carries nothing, so every existing scene marshals byte-for-byte as before
  (the 400-case golden in `scene/sceneir_marshal_equal_test.go` must not move).
- `ShadowCulling *bool` with `nil` meaning true. On the wire `shadowCulling` is
  always present (no `omitempty`), so the client never guesses the default.
- The mode is performance-only: it adds no capability feature and must not
  change `BackendCaps` (tested).
- `SceneIR.isZero()` does NOT look at `GPUDriven`, the same as
  `ShadowMaxPixels`: a scene with no records is still empty.

## Step 1 — create `scene/gpu_driven.go`

Exact content:

```go
package scene

// GPUDriven opts a scene into GPU-driven instancing on the WebGPU backend.
//
// When set, the WebGPU renderer culls every eligible InstancedMesh on the GPU
// with one compute dispatch per view: the camera, plus each shadow light when
// ShadowCulling is on. Survivors are drawn with indirect draws that read their
// instance records from a storage buffer, so per-instance colors survive the
// cull.
//
// The mode changes no pixels. Every cull is conservative: an instance that
// covers a pixel is always drawn. WebGL, Canvas and headless rendering ignore
// the field, and so does a WebGPU device that cannot run it.
//
// An InstancedMesh is eligible when it draws in the opaque pass, has at least
// one instance, and has no authored CullKernelWGSL. Every other mesh keeps the
// renderer's existing path.
type GPUDriven struct {
	// Occlusion turns on two-phase hierarchical-Z occlusion culling for the
	// camera. The main pass first draws what was visible last frame, builds a
	// depth pyramid from that depth, then culls again and draws only the
	// instances that just became visible. It costs one compute pass and one
	// extra render pass per frame. It pays off when large occluders hide many
	// instances.
	Occlusion bool
	// ShadowCulling culls shadow casters against each shadow light's frustum
	// before the shadow pass draws them. Nil means true.
	ShadowCulling *bool
}

// GPUDrivenIR is the wire form of GPUDriven. ShadowCulling is always resolved,
// so the client never has to know the default.
type GPUDrivenIR struct {
	Occlusion     bool `json:"occlusion,omitempty"`
	ShadowCulling bool `json:"shadowCulling"`
}

// sceneIR lowers the authored mode. A nil receiver means the scene did not opt
// in, and lowers to nil so the wire carries nothing.
func (g *GPUDriven) sceneIR() *GPUDrivenIR {
	if g == nil {
		return nil
	}
	shadowCulling := true
	if g.ShadowCulling != nil {
		shadowCulling = *g.ShadowCulling
	}
	return &GPUDrivenIR{Occlusion: g.Occlusion, ShadowCulling: shadowCulling}
}

// legacyProps mirrors the reflection marshal of GPUDrivenIR for the map-tree
// path. TestSceneIRDirectMarshalMatchesLegacy pins the two against each other.
func (g *GPUDrivenIR) legacyProps() map[string]any {
	out := map[string]any{"shadowCulling": g.ShadowCulling}
	if g.Occlusion {
		out["occlusion"] = true
	}
	return out
}
```

## Step 2 — `scene/scene.go`: add the field at the END of `Props`

Anchor (exactly once):

```go
	// window.__gosx.audio.registerManifest — see the Audio type
	// (scene/audio.go) for the two-engine (gosxAudio/arcadeAudio) model.
	Audio *Audio
}
```

Replace with:

```go
	// window.__gosx.audio.registerManifest — see the Audio type
	// (scene/audio.go) for the two-engine (gosxAudio/arcadeAudio) model.
	Audio *Audio
	// GPUDriven opts the scene into GPU-driven instancing on the WebGPU
	// backend. Nil (the default) keeps the classic instanced path. See the
	// GPUDriven type (scene/gpu_driven.go).
	GPUDriven *GPUDriven
}
```

Put it at the end on purpose. Inserting it inside the aligned block near
`Shadows` makes gofmt realign `Physics` and `Graph`, which is noise in review.

## Step 3 — `scene/scene_ir.go`: three edits

3a. The struct field. Anchor (exactly once):

```go
	ShadowMaxPixels    int                  `json:"shadowMaxPixels,omitempty"`
```

Insert directly after it:

```go
	// GPUDriven: see Props.GPUDriven (scene/gpu_driven.go). Nil when the scene
	// does not opt in, so the wire carries nothing.
	GPUDriven *GPUDrivenIR `json:"gpuDriven,omitempty"`
```

3b. Lowering. Anchor (exactly once, inside `func (p Props) SceneIR() SceneIR`):

```go
	ir.ShadowMaxPixels = p.Shadows.resolveMaxPixels()
```

Insert directly after it:

```go
	ir.GPUDriven = p.GPUDriven.sceneIR()
```

3c. The map-tree path. Anchor (exactly once, inside `legacyProps`):

```go
	if ir.ShadowMaxPixels != 0 {
		out["shadowMaxPixels"] = ir.ShadowMaxPixels
	}
```

Insert directly after it:

```go
	if ir.GPUDriven != nil {
		out["gpuDriven"] = ir.GPUDriven.legacyProps()
	}
```

## Step 4 — `scene/diff.go`: the diff policy

`TestSceneIRFieldPoliciesCoverEveryField` fails for any `SceneIR` field
without a policy. Anchor (exactly once, inside `sceneIRFieldPolicies`):

```go
	"QualityLadder": {
		policy: sceneIRRemount,
```

Insert directly BEFORE it:

```go
	"GPUDriven": {
		policy: sceneIRRemount,
		reason: "A WebGPU renderer mode the client reads from the scene at mount. No command kind " +
			"carries it.",
		changed: func(previous, next *SceneIR) bool {
			return !sceneRecordJSONEqual(previous.GPUDriven, next.GPUDriven)
		},
	},
```

## Step 5 — `scene/diff_coverage_test.go`: the mutator

Anchor (exactly once):

```go
	"ShadowMaxPixels":    func(ir *SceneIR) { ir.ShadowMaxPixels = 4096 },
```

Insert directly after it:

```go
	"GPUDriven":          func(ir *SceneIR) { ir.GPUDriven = &GPUDrivenIR{Occlusion: true, ShadowCulling: true} },
```

## Step 6 — create `scene/gpu_driven_test.go`

Exact content:

```go
package scene

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// gpuDrivenTestProps is a small scene with one instanced batch, the thing the
// GPU-driven mode acts on.
func gpuDrivenTestProps(mode *GPUDriven) Props {
	return Props{
		GPUDriven: mode,
		Graph: NewGraph(InstancedMesh{
			ID:        "crates",
			Count:     3,
			Geometry:  BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Positions: []Vector3{{X: -2}, {X: 0}, {X: 2}},
		}),
	}
}

func TestGPUDrivenUnsetLowersToNothing(t *testing.T) {
	ir := gpuDrivenTestProps(nil).SceneIR()
	if ir.GPUDriven != nil {
		t.Fatalf("GPUDriven = %#v, want nil when the scene does not opt in", ir.GPUDriven)
	}
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "gpuDriven") {
		t.Fatalf("wire carries gpuDriven for a scene that did not opt in: %s", data)
	}
}

func TestGPUDrivenLowersDefaults(t *testing.T) {
	ir := gpuDrivenTestProps(&GPUDriven{}).SceneIR()
	want := &GPUDrivenIR{Occlusion: false, ShadowCulling: true}
	if !reflect.DeepEqual(ir.GPUDriven, want) {
		t.Fatalf("GPUDriven = %#v, want %#v", ir.GPUDriven, want)
	}
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"gpuDriven":{"shadowCulling":true}`) {
		t.Fatalf("wire = %s, want it to contain \"gpuDriven\":{\"shadowCulling\":true}", data)
	}
}

func TestGPUDrivenLowersExplicitFlags(t *testing.T) {
	off := false
	ir := gpuDrivenTestProps(&GPUDriven{Occlusion: true, ShadowCulling: &off}).SceneIR()
	want := &GPUDrivenIR{Occlusion: true, ShadowCulling: false}
	if !reflect.DeepEqual(ir.GPUDriven, want) {
		t.Fatalf("GPUDriven = %#v, want %#v", ir.GPUDriven, want)
	}
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"gpuDriven":{"occlusion":true,"shadowCulling":false}`) {
		t.Fatalf("wire = %s, want it to contain \"gpuDriven\":{\"occlusion\":true,\"shadowCulling\":false}", data)
	}
}

// The map-tree path (LegacyProps) and the reflection path (MarshalJSON) must
// agree, as TestSceneIRDirectMarshalMatchesLegacy requires for every field.
func TestGPUDrivenDirectMarshalMatchesLegacy(t *testing.T) {
	for _, mode := range []*GPUDriven{{}, {Occlusion: true}} {
		ir := gpuDrivenTestProps(mode).SceneIR()
		directBytes, err := json.Marshal(ir)
		if err != nil {
			t.Fatal(err)
		}
		legacyBytes, err := json.Marshal(ir.legacyProps())
		if err != nil {
			t.Fatal(err)
		}
		var direct, legacy any
		if err := json.Unmarshal(directBytes, &direct); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(legacyBytes, &legacy); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(direct, legacy) {
			t.Fatalf("direct vs legacy mismatch\n direct: %s\n legacy: %s", directBytes, legacyBytes)
		}
	}
}

func TestGPUDrivenRoundTripsThroughUnmarshal(t *testing.T) {
	ir := gpuDrivenTestProps(&GPUDriven{Occlusion: true}).SceneIR()
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SceneIR
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.GPUDriven, ir.GPUDriven) {
		t.Fatalf("decoded GPUDriven = %#v, want %#v", decoded.GPUDriven, ir.GPUDriven)
	}
}

// The mode is performance-only: it must not move the honesty-gate verdict.
func TestGPUDrivenDoesNotChangeBackendCaps(t *testing.T) {
	plain := gpuDrivenTestProps(nil).SceneIR()
	driven := gpuDrivenTestProps(&GPUDriven{Occlusion: true}).SceneIR()
	if !reflect.DeepEqual(plain.BackendCaps, driven.BackendCaps) {
		t.Fatalf("BackendCaps changed with GPUDriven:\n plain:  %#v\n driven: %#v", plain.BackendCaps, driven.BackendCaps)
	}
}

func TestGPUDrivenChangeIsReportedForRemount(t *testing.T) {
	previous := gpuDrivenTestProps(nil).SceneIR()
	next := gpuDrivenTestProps(&GPUDriven{}).SceneIR()
	diff := DiffScene(previous, next, DiffOptions{})
	if !reflect.DeepEqual(diff.RemountFields, []string{"GPUDriven"}) {
		t.Fatalf("RemountFields = %v, want [GPUDriven]", diff.RemountFields)
	}
	if len(diff.Commands) != 0 {
		t.Fatalf("commands = %#v, want none", diff.Commands)
	}
}
```

## Verify

```sh
gofmt -l scene/                 # prints nothing
go vet ./scene/
go test ./scene/ -run 'GPUDriven|SceneIRFieldPolic|DiffScene|DirectMarshal|MarshalMatchesGolden|PropsMarshalJSONGolden' -count=1
go test ./scene/... ./render/bundle ./scene/capability
git diff --check
```

All pass. `TestSceneIRMarshalMatchesGoldenBytes` and
`TestPropsMarshalJSONGolden` must pass WITHOUT regenerating goldens; if either
asks for a regeneration, the field leaked into scenes that did not opt in —
recheck `omitempty` and the nil receiver.

Expected diff size: `scene/diff.go` +8, `scene/diff_coverage_test.go` +1,
`scene/scene.go` +4, `scene/scene_ir.go` +7, two new files (59 and 129 lines).

## Commit

`add(scene): add gpu-driven instancing authoring surface`

- add scene.GPUDriven and Props.GPUDriven, lowered to SceneIR.GPUDriven
- report a changed mode in Diff.RemountFields; no command kind carries it
- pin wire shape, legacy-map parity, round trip and unchanged BackendCaps

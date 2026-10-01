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

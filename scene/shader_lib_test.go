package scene

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// kernelSource returns a synthetic WGSL compute kernel source of at least
// shaderLibThreshold+n bytes so it qualifies for shader-lib hoisting.
func kernelSource(n int) string {
	base := `@group(0) @binding(0) var<storage,read_write> particles : array<Particle>;
struct Particle { pos : vec3<f32>, vel : vec3<f32>, life : f32 }
@compute @workgroup_size(64)
fn simulate(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if (i >= arrayLength(&particles)) { return; }
  particles[i].pos += particles[i].vel;
  particles[i].life -= 0.016;
}
`
	// Pad to exceed threshold with unique marker.
	pad := strings.Repeat("// padding ", (shaderLibThreshold/11)+1)
	return base + pad + strings.Repeat("x", n)
}

// TestShaderLibRoundTrip: 8 compute systems sharing one 30KB kernel.
// Marshal → assert single lib entry + 8 refs + size reduction.
// Unmarshal/inflate → identical to input.
func TestShaderLibRoundTrip(t *testing.T) {
	const nSystems = 8
	kernel := kernelSource(30000)

	systems := make([]ComputeParticlesIR, nSystems)
	for i := range systems {
		systems[i] = ComputeParticlesIR{
			ID:          "sys-" + string(rune('a'+i)),
			Count:       1000,
			Emitter:     ParticleEmitterIR{Kind: "point"},
			Material:    ParticleMaterialIR{Color: "#ffffff"},
			ComputeWGSL: kernel,
		}
	}

	ir := SceneIR{
		ComputeParticles: systems,
	}

	// Marshal via the MarshalJSON path (which applies applyShaderLib).
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// 1. Exactly one shaderLib entry.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	libRaw, ok := raw["shaderLib"].(map[string]any)
	if !ok {
		t.Fatalf("shaderLib missing or wrong type in marshaled output; keys: %v", mapKeys(raw))
	}
	if len(libRaw) != 1 {
		t.Errorf("shaderLib length = %d, want 1; keys: %v", len(libRaw), mapKeys(libRaw))
	}

	// 2. shaderLib key starts with "sl:".
	for k := range libRaw {
		if !strings.HasPrefix(k, "sl:") {
			t.Errorf("shaderLib key %q must start with sl:", k)
		}
	}

	// 3. All 8 systems have computeWGSLRef, no computeWGSL.
	cps, ok := raw["computeParticles"].([]any)
	if !ok {
		t.Fatalf("computeParticles missing or wrong type")
	}
	if len(cps) != nSystems {
		t.Fatalf("computeParticles length = %d, want %d", len(cps), nSystems)
	}
	for i, cpRaw := range cps {
		cp := cpRaw.(map[string]any)
		if _, hasInline := cp["computeWGSL"]; hasInline {
			t.Errorf("system[%d] still has inline computeWGSL after dedup", i)
		}
		ref, hasRef := cp["computeWGSLRef"].(string)
		if !hasRef {
			t.Errorf("system[%d] missing computeWGSLRef", i)
			continue
		}
		if !strings.HasPrefix(ref, "sl:") {
			t.Errorf("system[%d] computeWGSLRef = %q, want sl:... prefix", i, ref)
		}
	}

	// 4. Size reduction: marshaled output must be smaller than 8 inline copies.
	inlineSize := len(kernel) * nSystems
	actualSize := len(data)
	if actualSize >= inlineSize {
		t.Errorf("no size reduction: data=%d bytes, 8x inline=%d bytes", actualSize, inlineSize)
	}
	t.Logf("payload size: marshaled=%d bytes, 8x inline would be=%d bytes (%.1f%% savings)",
		actualSize, inlineSize, 100.0*(1.0-float64(actualSize)/float64(inlineSize)))

	// 5. Gzip comparison.
	gzInline := gzipSize(t, kernelInlineJSON(t, ir, kernel, nSystems))
	gzDedup := gzipSize(t, data)
	t.Logf("gzip: dedup=%d bytes, inline=%d bytes (%.1f%% gzip savings)",
		gzDedup, gzInline, 100.0*(1.0-float64(gzDedup)/float64(gzInline)))

	// 6. Unmarshal + inflate: round-trip identical to original.
	var ir2 SceneIR
	if err := json.Unmarshal(data, &ir2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(ir2.ComputeParticles) != nSystems {
		t.Fatalf("after unmarshal ComputeParticles length = %d, want %d", len(ir2.ComputeParticles), nSystems)
	}
	for i, cp := range ir2.ComputeParticles {
		if cp.ComputeWGSL != kernel {
			t.Errorf("system[%d] ComputeWGSL after inflate: got %d bytes, want %d bytes",
				i, len(cp.ComputeWGSL), len(kernel))
		}
		if cp.ComputeWGSLRef != "" {
			t.Errorf("system[%d] ComputeWGSLRef not cleared after inflate: %q", i, cp.ComputeWGSLRef)
		}
	}
	// ShaderLib should be cleared after inflation (no longer needed).
	if len(ir2.ShaderLib) != 0 {
		t.Errorf("ShaderLib should be empty after inflate, got %v", ir2.ShaderLib)
	}
}

func TestShaderLibWaterSystemWGSLRoundTrip(t *testing.T) {
	kernel := kernelSource(4096)
	ir := SceneIR{
		WaterSystems: []WaterSystemIR{
			{ID: "water-a", Resolution: 256, SeedWGSL: kernel, DropWGSL: kernel, SimulationWGSL: kernel, PoolVertexWGSL: kernel, PoolFragmentWGSL: kernel, SurfaceFragmentWGSL: kernel, ObjectShadowWGSL: kernel, ObjectMeshShadowVertexWGSL: kernel, ObjectMeshShadowFragmentWGSL: kernel},
			{ID: "water-b", Resolution: 256, SeedWGSL: kernel, DropWGSL: kernel, SimulationWGSL: kernel, PoolVertexWGSL: kernel, PoolFragmentWGSL: kernel, SurfaceFragmentWGSL: kernel, ObjectShadowWGSL: kernel, ObjectMeshShadowVertexWGSL: kernel, ObjectMeshShadowFragmentWGSL: kernel},
		},
	}

	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["shaderLib"].(map[string]any); !ok {
		t.Fatalf("shaderLib missing from water system payload: %s", data)
	}
	systems := raw["waterSystems"].([]any)
	for i, systemRaw := range systems {
		system := systemRaw.(map[string]any)
		if _, hasInline := system["simulationWGSL"]; hasInline {
			t.Fatalf("waterSystems[%d] still has inline simulationWGSL after dedup", i)
		}
		if ref, ok := system["simulationWGSLRef"].(string); !ok || !strings.HasPrefix(ref, "sl:") {
			t.Fatalf("waterSystems[%d] simulationWGSLRef = %#v", i, system["simulationWGSLRef"])
		}
		if _, hasInline := system["seedWGSL"]; hasInline {
			t.Fatalf("waterSystems[%d] still has inline seedWGSL after dedup", i)
		}
		if ref, ok := system["seedWGSLRef"].(string); !ok || !strings.HasPrefix(ref, "sl:") {
			t.Fatalf("waterSystems[%d] seedWGSLRef = %#v", i, system["seedWGSLRef"])
		}
		if _, hasInline := system["dropWGSL"]; hasInline {
			t.Fatalf("waterSystems[%d] still has inline dropWGSL after dedup", i)
		}
		if ref, ok := system["dropWGSLRef"].(string); !ok || !strings.HasPrefix(ref, "sl:") {
			t.Fatalf("waterSystems[%d] dropWGSLRef = %#v", i, system["dropWGSLRef"])
		}
		if _, hasInline := system["surfaceFragmentWGSL"]; hasInline {
			t.Fatalf("waterSystems[%d] still has inline surfaceFragmentWGSL after dedup", i)
		}
		if ref, ok := system["surfaceFragmentWGSLRef"].(string); !ok || !strings.HasPrefix(ref, "sl:") {
			t.Fatalf("waterSystems[%d] surfaceFragmentWGSLRef = %#v", i, system["surfaceFragmentWGSLRef"])
		}
		if _, hasInline := system["poolVertexWGSL"]; hasInline {
			t.Fatalf("waterSystems[%d] still has inline poolVertexWGSL after dedup", i)
		}
		if ref, ok := system["poolVertexWGSLRef"].(string); !ok || !strings.HasPrefix(ref, "sl:") {
			t.Fatalf("waterSystems[%d] poolVertexWGSLRef = %#v", i, system["poolVertexWGSLRef"])
		}
		if _, hasInline := system["poolFragmentWGSL"]; hasInline {
			t.Fatalf("waterSystems[%d] still has inline poolFragmentWGSL after dedup", i)
		}
		if ref, ok := system["poolFragmentWGSLRef"].(string); !ok || !strings.HasPrefix(ref, "sl:") {
			t.Fatalf("waterSystems[%d] poolFragmentWGSLRef = %#v", i, system["poolFragmentWGSLRef"])
		}
		if _, hasInline := system["objectShadowWGSL"]; hasInline {
			t.Fatalf("waterSystems[%d] still has inline objectShadowWGSL after dedup", i)
		}
		if ref, ok := system["objectShadowWGSLRef"].(string); !ok || !strings.HasPrefix(ref, "sl:") {
			t.Fatalf("waterSystems[%d] objectShadowWGSLRef = %#v", i, system["objectShadowWGSLRef"])
		}
		if _, hasInline := system["objectMeshShadowVertexWGSL"]; hasInline {
			t.Fatalf("waterSystems[%d] still has inline objectMeshShadowVertexWGSL after dedup", i)
		}
		if ref, ok := system["objectMeshShadowVertexWGSLRef"].(string); !ok || !strings.HasPrefix(ref, "sl:") {
			t.Fatalf("waterSystems[%d] objectMeshShadowVertexWGSLRef = %#v", i, system["objectMeshShadowVertexWGSLRef"])
		}
		if _, hasInline := system["objectMeshShadowFragmentWGSL"]; hasInline {
			t.Fatalf("waterSystems[%d] still has inline objectMeshShadowFragmentWGSL after dedup", i)
		}
		if ref, ok := system["objectMeshShadowFragmentWGSLRef"].(string); !ok || !strings.HasPrefix(ref, "sl:") {
			t.Fatalf("waterSystems[%d] objectMeshShadowFragmentWGSLRef = %#v", i, system["objectMeshShadowFragmentWGSLRef"])
		}
	}

	var roundTrip SceneIR
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(roundTrip.WaterSystems) != 2 {
		t.Fatalf("WaterSystems length = %d, want 2", len(roundTrip.WaterSystems))
	}
	for i, system := range roundTrip.WaterSystems {
		if system.SimulationWGSL != kernel {
			t.Fatalf("WaterSystems[%d].SimulationWGSL length = %d, want %d", i, len(system.SimulationWGSL), len(kernel))
		}
		if system.SimulationWGSLRef != "" {
			t.Fatalf("WaterSystems[%d].SimulationWGSLRef not cleared after inflate: %q", i, system.SimulationWGSLRef)
		}
		if system.SeedWGSL != kernel || system.DropWGSL != kernel {
			t.Fatalf("WaterSystems[%d] seed/drop WGSL fields did not inflate", i)
		}
		if system.ObjectShadowWGSL != kernel || system.ObjectMeshShadowVertexWGSL != kernel || system.ObjectMeshShadowFragmentWGSL != kernel {
			t.Fatalf("WaterSystems[%d] object shadow WGSL fields did not inflate", i)
		}
		if system.PoolVertexWGSL != kernel || system.PoolFragmentWGSL != kernel {
			t.Fatalf("WaterSystems[%d] pool WGSL fields did not inflate", i)
		}
	}
}

func TestShaderLibPreservesExistingRenderRefs(t *testing.T) {
	source := kernelSource(2048)
	id := shaderLibID(source)
	ir := SceneIR{
		ComputeParticles: []ComputeParticlesIR{
			{
				ID:                    "render-ref-a",
				Count:                 16,
				RenderVertexWGSLRef:   id,
				RenderFragmentWGSLRef: id,
				RenderShaderBackend:   "selena",
				RenderShaderLayout:    map[string]any{"entryPoints": map[string]any{"vertexStorage": "vertexStorageMain"}},
			},
		},
		ShaderLib: map[string]string{id: source},
	}

	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	libRaw, ok := raw["shaderLib"].(map[string]any)
	if !ok {
		t.Fatalf("shaderLib missing after marshaling existing refs; keys: %v", mapKeys(raw))
	}
	if got := libRaw[id]; got != source {
		t.Fatalf("shaderLib[%q] length = %d, want %d", id, len(fmt.Sprint(got)), len(source))
	}
	cps, ok := raw["computeParticles"].([]any)
	if !ok || len(cps) != 1 {
		t.Fatalf("computeParticles length = %d, want 1", len(cps))
	}
	cp := cps[0].(map[string]any)
	if cp["renderVertexWGSLRef"] != id || cp["renderFragmentWGSLRef"] != id {
		t.Fatalf("render WGSL refs not preserved: %#v", cp)
	}
}

// TestShaderLibSingleSystemNoHoist: a single compute system — no dedup should occur.
func TestShaderLibSingleSystemNoHoist(t *testing.T) {
	kernel := kernelSource(5000)
	ir := SceneIR{
		ComputeParticles: []ComputeParticlesIR{
			{
				ID:          "solo",
				Count:       100,
				Emitter:     ParticleEmitterIR{Kind: "point"},
				Material:    ParticleMaterialIR{Color: "#ff0000"},
				ComputeWGSL: kernel,
			},
		},
	}
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	json.Unmarshal(data, &raw)

	if _, hasLib := raw["shaderLib"]; hasLib {
		t.Error("single system: shaderLib must NOT be present (no hoisting for single occurrence)")
	}
	cps := raw["computeParticles"].([]any)
	cp := cps[0].(map[string]any)
	if _, hasRef := cp["computeWGSLRef"]; hasRef {
		t.Error("single system: computeWGSLRef must not appear")
	}
	if inline, ok := cp["computeWGSL"].(string); !ok || inline != kernel {
		t.Errorf("single system: computeWGSL inline must be preserved")
	}
}

// TestShaderLibBelowThreshold: strings shorter than threshold are never hoisted.
func TestShaderLibBelowThreshold(t *testing.T) {
	shortKernel := "fn short() {}" // well under 1KB
	ir := SceneIR{
		ComputeParticles: []ComputeParticlesIR{
			{ID: "a", Count: 1, Emitter: ParticleEmitterIR{Kind: "point"}, Material: ParticleMaterialIR{}, ComputeWGSL: shortKernel},
			{ID: "b", Count: 1, Emitter: ParticleEmitterIR{Kind: "point"}, Material: ParticleMaterialIR{}, ComputeWGSL: shortKernel},
		},
	}
	data, _ := json.Marshal(ir)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if _, hasLib := raw["shaderLib"]; hasLib {
		t.Error("short kernel: shaderLib must NOT appear (below threshold)")
	}
}

// TestShaderLibDeterminism: same input produces same output across two marshal calls.
func TestShaderLibDeterminism(t *testing.T) {
	kernel := kernelSource(2000)
	ir := SceneIR{
		ComputeParticles: []ComputeParticlesIR{
			{ID: "x", Count: 1, Emitter: ParticleEmitterIR{Kind: "point"}, Material: ParticleMaterialIR{}, ComputeWGSL: kernel},
			{ID: "y", Count: 1, Emitter: ParticleEmitterIR{Kind: "point"}, Material: ParticleMaterialIR{}, ComputeWGSL: kernel},
		},
	}
	data1, _ := json.Marshal(ir)
	data2, _ := json.Marshal(ir)
	if string(data1) != string(data2) {
		t.Errorf("non-deterministic marshal:\n  data1: %s\n  data2: %s", data1[:200], data2[:200])
	}
	// Key must start with sl:.
	var raw map[string]any
	json.Unmarshal(data1, &raw)
	lib := raw["shaderLib"].(map[string]any)
	for k := range lib {
		if !strings.HasPrefix(k, "sl:") {
			t.Errorf("lib key %q must start with sl:", k)
		}
	}
}

// TestShaderLibMissingRefTolerance: inflateShaderLib is tolerant when a ref
// points to a missing lib entry — the ref field is removed but no crash.
func TestShaderLibMissingRefTolerance(t *testing.T) {
	scene := map[string]any{
		"shaderLib": map[string]any{
			"sl:aabbcc": "real kernel",
		},
		"computeParticles": []any{
			map[string]any{
				"id":             "ok",
				"computeWGSLRef": "sl:aabbcc",
			},
			map[string]any{
				"id":             "bad",
				"computeWGSLRef": "sl:doesnotexist",
			},
		},
	}
	inflateShaderLib(scene)

	cps := scene["computeParticles"].([]any)
	ok_ := cps[0].(map[string]any)
	bad := cps[1].(map[string]any)

	if ok_["computeWGSL"] != "real kernel" {
		t.Errorf("valid ref not inflated: got %v", ok_["computeWGSL"])
	}
	if _, hasRef := ok_["computeWGSLRef"]; hasRef {
		t.Error("ref key not deleted after inflation")
	}
	if _, has := bad["computeWGSL"]; has {
		t.Error("missing-ref should not produce computeWGSL field")
	}
	if _, has := bad["computeWGSLRef"]; has {
		t.Error("ref key should be deleted even when lib entry missing")
	}
	if _, hasLib := scene["shaderLib"]; hasLib {
		t.Error("shaderLib key should be removed after inflation")
	}
}

// TestShaderLibObjectFields: customVertexWGSL / customFragmentWGSL dedup works
// for objects.
func TestShaderLibObjectFields(t *testing.T) {
	longFrag := kernelSource(2000) // long enough to qualify
	ir := SceneIR{
		Objects: []ObjectIR{
			{ID: "o1", Kind: "mesh", CustomFragmentWGSL: longFrag},
			{ID: "o2", Kind: "mesh", CustomFragmentWGSL: longFrag},
		},
	}
	data, _ := json.Marshal(ir)
	var raw map[string]any
	json.Unmarshal(data, &raw)

	if _, hasLib := raw["shaderLib"]; !hasLib {
		t.Fatal("shaderLib must be present for duplicated object shader")
	}
	objs := raw["objects"].([]any)
	for i, objRaw := range objs {
		obj := objRaw.(map[string]any)
		if _, has := obj["customFragmentWGSL"]; has {
			t.Errorf("objects[%d] still has inline customFragmentWGSL after dedup", i)
		}
		if _, has := obj["customFragmentWGSLRef"]; !has {
			t.Errorf("objects[%d] missing customFragmentWGSLRef", i)
		}
	}

	// Round-trip inflate.
	var ir2 SceneIR
	json.Unmarshal(data, &ir2)
	if ir2.Objects[0].CustomFragmentWGSL != longFrag {
		t.Error("Objects[0] CustomFragmentWGSL not restored after inflate")
	}
	if ir2.Objects[1].CustomFragmentWGSL != longFrag {
		t.Error("Objects[1] CustomFragmentWGSL not restored after inflate")
	}
}

// TestShaderLibNoHoistWhenDifferentKernels: two systems with different kernels
// → no hoisting.
func TestShaderLibNoHoistWhenDifferentKernels(t *testing.T) {
	kernelA := kernelSource(0) + "A"
	kernelB := kernelSource(0) + "B"
	ir := SceneIR{
		ComputeParticles: []ComputeParticlesIR{
			{ID: "a", Count: 1, Emitter: ParticleEmitterIR{Kind: "point"}, Material: ParticleMaterialIR{}, ComputeWGSL: kernelA},
			{ID: "b", Count: 1, Emitter: ParticleEmitterIR{Kind: "point"}, Material: ParticleMaterialIR{}, ComputeWGSL: kernelB},
		},
	}
	data, _ := json.Marshal(ir)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if _, hasLib := raw["shaderLib"]; hasLib {
		t.Error("different kernels: shaderLib must not appear")
	}
}

// ------- InstancedMesh cull kernel shaderLib tests (Task 1 + 3) -------

// cullKernelSource returns a synthetic WGSL cull kernel source ≥ shaderLibThreshold bytes.
func cullKernelSource(n int) string {
	base := `@group(0) @binding(0) var<storage,read_write> transforms : array<mat4x4<f32>>;
@group(0) @binding(1) var<storage,read_write> visible : array<u32>;
struct CullUniforms { frustum : array<vec4<f32>, 6>, count : u32 }
@group(0) @binding(2) var<uniform> cull : CullUniforms;
@compute @workgroup_size(64)
fn cullInstances(@builtin(global_invocation_id) gid : vec3<u32>) {
  let i = gid.x;
  if (i >= cull.count) { return; }
  let pos = transforms[i][3].xyz;
  var inside : bool = true;
  for (var p = 0u; p < 6u; p++) {
    if (dot(cull.frustum[p].xyz, pos) + cull.frustum[p].w < 0.0) {
      inside = false; break;
    }
  }
  visible[i] = select(0u, 1u, inside);
}
`
	pad := strings.Repeat("// pad ", (shaderLibThreshold/7)+1)
	return base + pad + strings.Repeat("x", n)
}

// TestInstancedMeshCullFieldsRoundTrip verifies that cull fields on InstancedMeshIR
// survive a full SceneIR.MarshalJSON → SceneIR.UnmarshalJSON round-trip, and that
// an InstancedMesh without cull fields produces a payload with no cull keys at all
// (the "absent ⇒ byte-identical" additive contract).
func TestInstancedMeshCullFieldsRoundTrip(t *testing.T) {
	transforms := make([]float64, 16)
	transforms[0], transforms[5], transforms[10], transforms[15] = 1, 1, 1, 1 // identity

	ir := SceneIR{
		InstancedMeshes: []InstancedMeshIR{
			{
				ID:              "with-cull",
				Count:           1,
				Kind:            "box",
				Transforms:      transforms,
				CullKernelWGSL:  "@compute @workgroup_size(64) fn cull() {}",
				CullKernelEntry: "cullInstances",
				CullRadius:      50.0,
				CullBackend:     "elio",
			},
			{
				ID:         "no-cull",
				Count:      1,
				Kind:       "box",
				Transforms: transforms,
			},
		},
	}

	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	meshes, ok := raw["instancedMeshes"].([]any)
	if !ok || len(meshes) != 2 {
		t.Fatalf("instancedMeshes length = %d, want 2", len(meshes))
	}

	// First mesh: cull fields must be present.
	m0 := meshes[0].(map[string]any)
	if m0["cullKernelWGSL"] != "@compute @workgroup_size(64) fn cull() {}" {
		t.Errorf("cullKernelWGSL missing or wrong: %v", m0["cullKernelWGSL"])
	}
	if m0["cullKernelEntry"] != "cullInstances" {
		t.Errorf("cullKernelEntry missing or wrong: %v", m0["cullKernelEntry"])
	}
	if m0["cullRadius"] != 50.0 {
		t.Errorf("cullRadius missing or wrong: %v", m0["cullRadius"])
	}
	if m0["cullBackend"] != "elio" {
		t.Errorf("cullBackend missing or wrong: %v", m0["cullBackend"])
	}

	// Second mesh: NO cull keys at all (omitempty contract).
	m1 := meshes[1].(map[string]any)
	for _, key := range []string{"cullKernelWGSL", "cullKernelWGSLRef", "cullKernelEntry", "cullRadius", "cullBackend"} {
		if _, has := m1[key]; has {
			t.Errorf("no-cull mesh must not have key %q", key)
		}
	}

	// Round-trip unmarshal: cull fields must be restored.
	var ir2 SceneIR
	if err := json.Unmarshal(data, &ir2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(ir2.InstancedMeshes) != 2 {
		t.Fatalf("after unmarshal InstancedMeshes length = %d, want 2", len(ir2.InstancedMeshes))
	}
	got := ir2.InstancedMeshes[0]
	if got.CullKernelWGSL != "@compute @workgroup_size(64) fn cull() {}" {
		t.Errorf("CullKernelWGSL not restored: %q", got.CullKernelWGSL)
	}
	if got.CullKernelEntry != "cullInstances" {
		t.Errorf("CullKernelEntry not restored: %q", got.CullKernelEntry)
	}
	if got.CullRadius != 50.0 {
		t.Errorf("CullRadius not restored: %v", got.CullRadius)
	}
	if got.CullBackend != "elio" {
		t.Errorf("CullBackend not restored: %q", got.CullBackend)
	}
}

// TestInstancedMeshCullShaderLibRoundTrip: 2 instanced meshes sharing the same
// cull kernel trigger hoisting; after inflate the kernel is restored inline.
func TestInstancedMeshCullShaderLibRoundTrip(t *testing.T) {
	const nMeshes = 2
	kernel := cullKernelSource(5000)
	transforms := make([]float64, 16)
	transforms[0], transforms[5], transforms[10], transforms[15] = 1, 1, 1, 1

	meshes := make([]InstancedMeshIR, nMeshes)
	for i := range meshes {
		meshes[i] = InstancedMeshIR{
			ID:             "mesh-" + string(rune('a'+i)),
			Count:          1,
			Kind:           "box",
			Transforms:     transforms,
			CullKernelWGSL: kernel,
		}
	}

	ir := SceneIR{InstancedMeshes: meshes}
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	// shaderLib must exist with exactly one entry.
	libRaw, ok := raw["shaderLib"].(map[string]any)
	if !ok {
		t.Fatalf("shaderLib missing or wrong type; keys: %v", mapKeys(raw))
	}
	if len(libRaw) != 1 {
		t.Errorf("shaderLib length = %d, want 1; keys: %v", len(libRaw), mapKeys(libRaw))
	}

	// Each mesh must have cullKernelWGSLRef, no inline cullKernelWGSL.
	imsRaw, ok := raw["instancedMeshes"].([]any)
	if !ok || len(imsRaw) != nMeshes {
		t.Fatalf("instancedMeshes length = %d, want %d", len(imsRaw), nMeshes)
	}
	for i, imRaw := range imsRaw {
		im := imRaw.(map[string]any)
		if _, hasInline := im["cullKernelWGSL"]; hasInline {
			t.Errorf("mesh[%d] still has inline cullKernelWGSL after dedup", i)
		}
		ref, hasRef := im["cullKernelWGSLRef"].(string)
		if !hasRef {
			t.Errorf("mesh[%d] missing cullKernelWGSLRef", i)
			continue
		}
		if !strings.HasPrefix(ref, "sl:") {
			t.Errorf("mesh[%d] cullKernelWGSLRef = %q, want sl:... prefix", i, ref)
		}
	}

	// Inflate round-trip: kernel must be restored inline.
	var ir2 SceneIR
	if err := json.Unmarshal(data, &ir2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(ir2.InstancedMeshes) != nMeshes {
		t.Fatalf("after inflate InstancedMeshes length = %d, want %d", len(ir2.InstancedMeshes), nMeshes)
	}
	for i, im := range ir2.InstancedMeshes {
		if im.CullKernelWGSL != kernel {
			t.Errorf("mesh[%d] CullKernelWGSL after inflate: got %d bytes, want %d bytes",
				i, len(im.CullKernelWGSL), len(kernel))
		}
		if im.CullKernelWGSLRef != "" {
			t.Errorf("mesh[%d] CullKernelWGSLRef not cleared after inflate: %q", i, im.CullKernelWGSLRef)
		}
	}
	if len(ir2.ShaderLib) != 0 {
		t.Errorf("ShaderLib should be empty after inflate, got %v", ir2.ShaderLib)
	}
}

// ---- helpers ----

func mapKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func gzipSize(t *testing.T, data []byte) int {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	w.Close()
	return buf.Len()
}

// kernelInlineJSON builds the JSON equivalent of ir with all kernels inline
// (no dedup), for size comparison.
func kernelInlineJSON(t *testing.T, ir SceneIR, kernel string, n int) []byte {
	t.Helper()
	// Build a raw map with all kernels inline.
	cps := make([]map[string]any, n)
	for i := range cps {
		cps[i] = map[string]any{
			"id":          "sys-" + string(rune('a'+i)),
			"count":       1000,
			"emitter":     map[string]any{"kind": "point"},
			"material":    map[string]any{"color": "#ffffff"},
			"computeWGSL": kernel,
		}
	}
	out := map[string]any{"computeParticles": cps}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("kernelInlineJSON: %v", err)
	}
	return data
}

package main

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"testing"

	"m31labs.dev/gosx/scene"
)

var wantSceneKeys = []string{
	"both", "left", "right", "moved-opaque", "moved", "one", "empty",
	"reference-left", "reference-right",
}

func casterOf(t *testing.T, ir scene.SceneIR) scene.InstancedMeshIR {
	t.Helper()
	for _, im := range ir.InstancedMeshes {
		if im.ID == casterID {
			return im
		}
	}
	t.Fatalf("scene has no instanced caster %q", casterID)
	return scene.InstancedMeshIR{}
}

func receiverOf(t *testing.T, ir scene.SceneIR) scene.ObjectIR {
	t.Helper()
	for _, o := range ir.Objects {
		if o.ID == receiverID {
			return o
		}
	}
	t.Fatalf("scene has no receiver %q", receiverID)
	return scene.ObjectIR{}
}

func keyLightOf(t *testing.T, ir scene.SceneIR) scene.LightIR {
	t.Helper()
	for _, l := range ir.Lights {
		if l.ID == lightID {
			return l
		}
	}
	t.Fatalf("scene has no key light %q", lightID)
	return scene.LightIR{}
}

// translation extracts the translation column of a column-major mat4.
func translation(m []float64) (float64, float64, float64) {
	return m[12], m[13], m[14]
}

// columnNorm is the length of one rotation/scale basis column. A mat4 is
// column-major with a stride of 4, so column col starts at col*4.
func columnNorm(m []float64, col int) float64 {
	return math.Hypot(math.Hypot(m[col*4], m[col*4+1]), m[col*4+2])
}

func assertTransform(t *testing.T, m []float64, wantPos [3]float64, wantScale [3]float64, wantRotated bool) {
	t.Helper()
	if len(m) != 16 {
		t.Fatalf("transform has %d elements, want 16", len(m))
	}
	x, y, z := translation(m)
	if x != wantPos[0] || y != wantPos[1] || z != wantPos[2] {
		t.Fatalf("translation = (%v,%v,%v), want %v", x, y, z, wantPos)
	}
	for col := 0; col < 3; col++ {
		norm := columnNorm(m, col)
		if math.Abs(norm-wantScale[col]) > 1e-9 {
			t.Fatalf("basis column %d norm = %v, want scale %v", col, norm, wantScale[col])
		}
	}
	rotated := m[0] != wantScale[0] || m[5] != wantScale[1] || m[10] != wantScale[2]
	if rotated != wantRotated {
		t.Fatalf("rotation presence = %v, want %v", rotated, wantRotated)
	}
}

func assertSharedStatics(t *testing.T, ir scene.SceneIR) {
	t.Helper()
	receiver := receiverOf(t, ir)
	if receiver.MaterialKind != "standard" {
		t.Fatalf("receiver materialKind = %q, want standard", receiver.MaterialKind)
	}
	if receiver.Color != "#ffffff" {
		t.Fatalf("receiver color = %q, want #ffffff", receiver.Color)
	}
	if receiver.Width != 3 || receiver.Height != 2.2 || receiver.Depth != 0.1 {
		t.Fatalf("receiver box = %v/%v/%v", receiver.Width, receiver.Height, receiver.Depth)
	}
	if receiver.X != 0 || receiver.Y != 0 || receiver.Z != -0.5 {
		t.Fatalf("receiver position = %v/%v/%v", receiver.X, receiver.Y, receiver.Z)
	}
	if receiver.CastShadow || !receiver.ReceiveShadow {
		t.Fatalf("receiver shadow flags wrong: cast=%v receive=%v", receiver.CastShadow, receiver.ReceiveShadow)
	}
	if receiver.Wireframe == nil || *receiver.Wireframe {
		t.Fatal("receiver must be non-wireframe")
	}
	if receiver.Roughness != 1 || receiver.Metalness != 0 || receiver.IOR == nil || *receiver.IOR != 1.5 {
		t.Fatal("receiver standard material values wrong")
	}
	if len(ir.Lights) != 1 {
		t.Fatalf("scene light count = %d, want exactly 1", len(ir.Lights))
	}
	light := keyLightOf(t, ir)
	if light.Kind != "directional" {
		t.Fatalf("light kind = %q, want directional", light.Kind)
	}
	if light.Intensity != 1.2 {
		t.Fatalf("light intensity = %v", light.Intensity)
	}
	if light.DirectionX != 0.7 || light.DirectionY != -0.7 || light.DirectionZ != -1 {
		t.Fatalf("light direction = %v/%v/%v", light.DirectionX, light.DirectionY, light.DirectionZ)
	}
	if !light.CastShadow {
		t.Fatal("light must cast shadows")
	}
	if light.ShadowSize != 512 {
		t.Fatalf("light shadow size = %v, want 512", light.ShadowSize)
	}
	if light.ShadowCascades != 1 {
		t.Fatalf("light shadow cascades = %d, want 1", light.ShadowCascades)
	}
}

func assertCasterMaterial(t *testing.T, im scene.InstancedMeshIR) {
	t.Helper()
	if im.Width != 0.55 || im.Height != 0.55 || im.Depth != 0.15 {
		t.Fatalf("caster box = %v/%v/%v", im.Width, im.Height, im.Depth)
	}
	if im.Color != "#2040c0" {
		t.Fatalf("caster color = %q", im.Color)
	}
	if im.Wireframe == nil || *im.Wireframe {
		t.Fatal("caster must be non-wireframe")
	}
	if im.Roughness != 1 || im.Metalness != 0 || im.IOR == nil || *im.IOR != 1.5 {
		t.Fatal("caster standard material values wrong")
	}
	if im.Opacity == nil || *im.Opacity != 1 {
		t.Fatal("caster opacity must be 1")
	}
	if !im.CastShadow || im.ReceiveShadow {
		t.Fatal("caster shadow flags wrong")
	}
}

var wantTranslations = map[string][2][3]float64{
	"both":         {{-0.55, 0.35, 0.5}, {0.35, 0.35, 0.5}},
	"left":         {{-0.55, 0.35, 0.5}, {0.35, 0.35, 0.5}},
	"right":        {{-0.55, 0.35, 0.5}, {0.35, 0.35, 0.5}},
	"moved-opaque": {{-0.9, 0.6, 0.55}, {0.7, 0.7, 0.4}},
	"one":          {{-0.55, 0.35, 0.5}},
	"moved":        {{-0.9, 0.6, 0.55}, {0.7, 0.7, 0.4}},
}

func TestFixtureShape(t *testing.T) {
	f := buildFixture()
	if f.Schema != schema {
		t.Fatalf("schema = %q", f.Schema)
	}
	gotKeys := make([]string, 0, len(f.Scenes))
	for k := range f.Scenes {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(gotKeys)
	wantKeys := append([]string(nil), wantSceneKeys...)
	sort.Strings(wantKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("scene keys = %v, want %v", gotKeys, wantKeys)
	}
	for name, ir := range f.Scenes {
		assertSharedStatics(t, ir)
		switch name {
		case "empty":
			if len(ir.InstancedMeshes) != 0 {
				t.Fatalf("empty scene has %d instanced batches, want 0", len(ir.InstancedMeshes))
			}
			if len(ir.Objects) != 1 || ir.Objects[0].ID != receiverID {
				t.Fatalf("empty scene objects = %+v, want only the receiver %q", ir.Objects, receiverID)
			}
		case "reference-left", "reference-right":
			if len(ir.InstancedMeshes) != 0 {
				t.Fatalf("%s must not carry an instanced batch", name)
			}
			want := scene.Vec3(-0.55, 0.35, 0.5)
			if name == "reference-right" {
				want = scene.Vec3(0.35, 0.35, 0.5)
			}
			if len(ir.Objects) != 2 {
				t.Fatalf("%s object count = %d, want receiver + one reference mesh", name, len(ir.Objects))
			}
			found := false
			for _, o := range ir.Objects {
				if o.ID == receiverID || o.ID == lightID {
					continue
				}
				found = true
				if o.X != want.X || o.Y != want.Y || o.Z != want.Z {
					t.Fatalf("%s reference position = %v/%v/%v, want %v/%v/%v", name, o.X, o.Y, o.Z, want.X, want.Y, want.Z)
				}
				if !o.CastShadow {
					t.Fatalf("%s reference mesh must cast shadows", name)
				}
				if !o.AlphaCutoff.IsZero() {
					t.Fatalf("%s reference mesh must have no cutoff", name)
				}
			}
			if !found {
				t.Fatalf("%s has no reference mesh", name)
			}
		default:
			im := casterOf(t, ir)
			assertCasterMaterial(t, im)
			wantCount := 2
			if name == "one" {
				wantCount = 1
			}
			if im.Count != wantCount || len(im.Transforms) != wantCount*16 {
				t.Fatalf("%s count/transforms = %d/%d, want %d", name, im.Count, len(im.Transforms), wantCount)
			}
			if len(im.Colors) != wantCount {
				t.Fatalf("%s colors length = %d, want %d", name, len(im.Colors), wantCount)
			}
			for i, c := range im.Colors {
				rgb := "rgba(32,64,192,0.5)"
				if name == "moved-opaque" {
					rgb = "rgba(32,64,192,1)"
				} else if name == "left" && i == 1 || name == "right" && i == 0 {
					rgb = "rgba(32,64,192,0.25)"
				}
				if c != rgb {
					t.Fatalf("%s color[%d] = %q, want exactly %q", name, i, c, rgb)
				}
			}
			wantPos := wantTranslations[name]
			for i := 0; i < wantCount; i++ {
				m := im.Transforms[i*16 : (i+1)*16]
				scale := [3]float64{1, 1, 1}
				rotated := false
				if name == "moved" || name == "moved-opaque" {
					if i == 0 {
						scale = [3]float64{1.2, 0.8, 1.5}
					} else {
						scale = [3]float64{0.9, 1.3, 1.1}
					}
					rotated = true
				}
				assertTransform(t, m, wantPos[i], scale, rotated)
			}
			// moved-opaque must omit the cutoff; every other caster keeps
			// the explicit 0.5 threshold.
			if name == "moved-opaque" {
				if !im.AlphaCutoff.IsZero() {
					t.Fatal("moved-opaque caster must omit alpha cutoff")
				}
			} else if v, ok := im.AlphaCutoff.Value(); !ok || v != 0.5 {
				t.Fatal("caster alpha cutoff must be 0.5")
			}
		}
	}
}

// TestMovedOpaqueControl proves the opaque control shares moved's typed
// transforms exactly; it is a control for the existing moved case, not an
// oracle for new runtime output.
func TestMovedOpaqueControl(t *testing.T) {
	f := buildFixture()
	moved := casterOf(t, f.Scenes["moved"])
	opaque := casterOf(t, f.Scenes["moved-opaque"])
	if moved.Count != 2 || opaque.Count != 2 {
		t.Fatalf("moved/moved-opaque counts = %d/%d, want 2/2", moved.Count, opaque.Count)
	}
	if !reflect.DeepEqual(moved.Transforms, opaque.Transforms) {
		t.Fatal("moved-opaque transforms must exactly equal moved transforms")
	}
	for i, c := range opaque.Colors {
		if c != "rgba(32,64,192,1)" {
			t.Fatalf("moved-opaque color[%d] = %q, want rgba(32,64,192,1)", i, c)
		}
	}
	if !opaque.AlphaCutoff.IsZero() {
		t.Fatal("moved-opaque must omit alpha cutoff")
	}
}

func TestTransitions(t *testing.T) {
	f := buildFixture()
	wantNames := []string{
		"both-to-left", "left-to-right", "right-to-moved",
		"moved-to-one", "one-to-empty", "empty-to-both",
	}
	wantCounts := []int{2, 2, 2, 1, 0, 2}
	if len(f.Transitions) != len(wantNames) {
		t.Fatalf("transitions = %d, want %d", len(f.Transitions), len(wantNames))
	}
	for i, tr := range f.Transitions {
		if tr.Name != wantNames[i] {
			t.Fatalf("transition %d name = %q, want %q", i, tr.Name, wantNames[i])
		}
		wantFrom, wantTo := wantNames[i], wantNames[i]
		switch i {
		case 0:
			wantFrom, wantTo = "both", "left"
		case 1:
			wantFrom, wantTo = "left", "right"
		case 2:
			wantFrom, wantTo = "right", "moved"
		case 3:
			wantFrom, wantTo = "moved", "one"
		case 4:
			wantFrom, wantTo = "one", "empty"
		case 5:
			wantFrom, wantTo = "empty", "both"
		}
		if tr.From != wantFrom || tr.To != wantTo {
			t.Fatalf("transition %d link = %q->%q, want %q->%q", i, tr.From, tr.To, wantFrom, wantTo)
		}
		if len(tr.Commands) != 1 {
			t.Fatalf("transition %d has %d commands, want exactly 1", i, len(tr.Commands))
		}
		cmd := tr.Commands[0]
		if cmd.Kind != scene.CommandSetInstancedMeshes {
			t.Fatalf("transition %d command kind = %d, want CommandSetInstancedMeshes", i, cmd.Kind)
		}
		data, ok := cmd.Data.(map[string]any)
		if !ok {
			t.Fatalf("transition %d command data type %T, want map[string]any", i, cmd.Data)
		}
		raw, ok := data["instancedMeshes"]
		if !ok {
			t.Fatalf("transition %d command payload missing %q key", i, "instancedMeshes")
		}
		meshes, ok := raw.([]scene.InstancedMeshIR)
		if !ok {
			t.Fatalf("transition %d instanced command payload type %T", i, raw)
		}
		wantMeshes := f.Scenes[tr.To].InstancedMeshes
		if !reflect.DeepEqual(meshes, wantMeshes) {
			t.Fatalf("transition %d meshes = %+v, want %+v", i, meshes, wantMeshes)
		}
		if tr.To == "empty" && meshes != nil {
			t.Fatalf("transition %d removal must carry a nil payload, got %d meshes", i, len(meshes))
		}
		if len(meshes) == 1 && meshes[0].ID != casterID {
			t.Fatalf("transition %d target id = %q, want %q", i, meshes[0].ID, casterID)
		}
		if len(meshes) == 1 && meshes[0].Count != wantCounts[i] {
			t.Fatalf("transition %d target count = %d, want %d", i, meshes[0].Count, wantCounts[i])
		}
	}
}

func TestDeterministicJSON(t *testing.T) {
	first, err := json.Marshal(buildFixture())
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(buildFixture())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("fixture JSON is not deterministic across builds")
	}
}

package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestPatchModelsCommandWire(t *testing.T) {
	cmd := PatchModelsCommand([]ModelPatch{
		{ID: "home-00", MorphWeights: map[string]float64{"mass": .8, "bad": math.Inf(1)}, Scale: &Vector3{X: 1.02, Y: 1.04, Z: 1.02}},
		{ID: "home-01", MorphWeights: map[string]float64{}, Visible: Bool(false)},
		{ID: "home-02"},
		{MorphWeights: map[string]float64{"mass": 1}},
	})
	if cmd.Kind != 16 {
		t.Fatalf("kind = %d, want 16", cmd.Kind)
	}
	raw, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":16,"data":{"models":[` +
		`{"id":"home-00","morphWeights":{"mass":0.8},"scaleX":1.02,"scaleY":1.04,"scaleZ":1.02},` +
		`{"id":"home-01","morphWeights":{},"visible":false},` +
		`{"id":"home-02"}]}}`
	if string(raw) != want {
		t.Fatalf("wire =\n%s\nwant\n%s", raw, want)
	}
}

func TestAttachmentsLowerAndPatch(t *testing.T) {
	ir := NewGraph(Model{ID: "p", Src: "/a.glb", Attachments: []Attachment{
		{Name: "mask", Src: " /gear.glb ", Mesh: "mask-cage", Joint: "head"},
		{Name: "visor", Src: "/gear.glb", Mesh: "visor", Joint: "head", Hidden: true, Material: &StandardMaterial{Color: "#223344", Opacity: Float(.4)}},
		{Name: "no joint", Src: "/gear.glb"},
	}}).SceneIR()
	got := ir.Models[0].Attachments
	if len(got) != 2 || got[0].Src != "/gear.glb" || got[0].Material != nil || !got[1].Hidden || got[1].Material == nil || got[1].Material.Color != "#223344" {
		t.Fatalf("attachments %+v", got)
	}
	raw, err := json.Marshal(ir.Models[0])
	if err != nil || !strings.Contains(string(raw), `"attachments":[{"name":"mask","src":"/gear.glb","mesh":"mask-cage","joint":"head"}`) {
		t.Fatalf("model json %s %v", raw, err)
	}
	cmd, _ := json.Marshal(PatchModelsCommand([]ModelPatch{{ID: "p", Attachments: map[string]bool{"visor": true, "mask": false}, Uniforms: map[string]any{"skin": []float64{.5, .3, .2}}}}))
	if !strings.Contains(string(cmd), `"attachments":{"mask":false,"visor":true}`) || !strings.Contains(string(cmd), `"uniforms":{"skin":[0.5,0.3,0.2]}`) {
		t.Fatalf("patch %s", cmd)
	}
}

func TestJointScalesLowerAndPatch(t *testing.T) {
	ir := NewGraph(Model{ID: "p", Src: "/a.glb", JointScales: map[string]float64{"head": 1.08, "": 2, "neck": -1, "foot": math.NaN()}}).SceneIR()
	if got := ir.Models[0].JointScales; len(got) != 1 || got["head"] != 1.08 {
		t.Fatalf("joint scales %v", got)
	}
	raw, err := json.Marshal(ir.Models[0])
	if err != nil || !strings.Contains(string(raw), `"jointScales":{"head":1.08}`) {
		t.Fatalf("model json %s %v", raw, err)
	}
	cmd, _ := json.Marshal(PatchModelsCommand([]ModelPatch{
		{ID: "p", JointScales: map[string]float64{"head": 1.1, "neck": 0}},
		{ID: "q", JointScales: map[string]float64{}},
		{ID: "r"},
	}))
	want := `{"kind":16,"data":{"models":[{"id":"p","jointScales":{"head":1.1}},{"id":"q","jointScales":{}},{"id":"r"}]}}`
	if string(cmd) != want {
		t.Fatalf("patch\n%s\nwant\n%s", cmd, want)
	}
}

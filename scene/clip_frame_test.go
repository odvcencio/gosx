package scene

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestModelClipSampledLowersToWire(t *testing.T) {
	props := Props{Graph: NewGraph(
		Model{ID: "p1", Src: "/m/fighter.glb", Scale: Vec3(1, 1, 1), ClipSampled: true},
		Model{ID: "p2", Src: "/m/fighter.glb", Scale: Vec3(1, 1, 1), ClipSampled: true, RootMotion: true},
		Model{ID: "prop", Src: "/m/crate.glb", Scale: Vec3(1, 1, 1)},
	)}
	ir := props.SceneIR()
	if !ir.Models[0].ClipSampled || ir.Models[0].RootMotion {
		t.Fatalf("p1 flags = %+v", ir.Models[0])
	}
	if !ir.Models[1].ClipSampled || !ir.Models[1].RootMotion {
		t.Fatalf("p2 flags = %+v", ir.Models[1])
	}
	raw, err := json.Marshal(ir.Models[0])
	if err != nil || !strings.Contains(string(raw), `"clipSampled":true`) || strings.Contains(string(raw), "rootMotion") {
		t.Fatalf("model json = %s, err %v", raw, err)
	}
	raw, _ = json.Marshal(ir.Models[2])
	if strings.Contains(string(raw), "clipSampled") {
		t.Fatalf("default model must omit clipSampled: %s", raw)
	}
	models := props.LegacyProps()["scene"].(map[string]any)["models"].([]map[string]any)
	if models[0]["clipSampled"] != true || models[1]["rootMotion"] != true {
		t.Fatalf("legacy models = %#v", models)
	}
	if _, ok := models[2]["clipSampled"]; ok {
		t.Fatalf("default legacy model must omit clipSampled: %#v", models[2])
	}
}

func TestEncodeClipFrameLayout(t *testing.T) {
	frame := ClipFrame{Models: []ClipModel{
		{ID: "p1", Layers: []ClipLayer{
			{Name: "idle", Time: 1.5, Weight: 0.25, Loop: true},
			{Index: 3, Time: 0.5, Weight: 0.75},
		}},
		{ID: "p2", Layers: []ClipLayer{{Name: "idle", Time: 2, Weight: 1}}},
	}}
	got, err := EncodeClipFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	if string(got[:4]) != "GSP4" {
		t.Fatalf("magic = %q", got[:4])
	}
	if le.Uint16(got[4:]) != 1 { // one dictionary name
		t.Fatalf("name count = %d", le.Uint16(got[4:]))
	}
	off := 6
	if n := int(le.Uint16(got[off:])); string(got[off+2:off+2+n]) != "idle" {
		t.Fatalf("name = %q", got[off+2:off+2+n])
	} else {
		off += 2 + n
	}
	if le.Uint16(got[off:]) != 2 {
		t.Fatalf("model count = %d", le.Uint16(got[off:]))
	}
	off += 2
	if n := int(le.Uint16(got[off:])); string(got[off+2:off+2+n]) != "p1" {
		t.Fatalf("model id")
	} else {
		off += 2 + n
	}
	if got[off] != 2 {
		t.Fatalf("layer count = %d", got[off])
	}
	off++
	if le.Uint16(got[off:]) != 0 { // dictionary index 0
		t.Fatalf("ref0 = %#x", le.Uint16(got[off:]))
	}
	if math.Float32frombits(le.Uint32(got[off+2:])) != 1.5 || math.Float32frombits(le.Uint32(got[off+6:])) != 0.25 || got[off+10] != 1 {
		t.Fatalf("layer0 bytes % x", got[off:off+11])
	}
	off += 11
	if le.Uint16(got[off:]) != 0x8000|3 {
		t.Fatalf("ref1 = %#x", le.Uint16(got[off:]))
	}
	if got[off+10] != 0 {
		t.Fatalf("layer1 loop flag = %d", got[off+10])
	}
}

func TestClipFrameEncoderReuseIsStable(t *testing.T) {
	frame := ClipFrame{Models: []ClipModel{{ID: "p1", Layers: []ClipLayer{{Name: "a", Time: 1, Weight: 1}}}}}
	var enc ClipFrameEncoder
	first, err := enc.Encode(frame)
	if err != nil {
		t.Fatal(err)
	}
	want := string(first)
	other, _ := enc.Encode(ClipFrame{Models: []ClipModel{{ID: "zz", Layers: []ClipLayer{{Name: "q", Time: 9, Weight: 1}}}}})
	_ = other
	again, err := enc.Encode(frame)
	if err != nil || string(again) != want {
		t.Fatalf("re-encode differs: %v", err)
	}
}

func TestEncodeClipFrameRejectsInvalid(t *testing.T) {
	layer := ClipLayer{Name: "a", Time: 1, Weight: 1}
	cases := map[string]ClipFrame{
		"empty id":         {Models: []ClipModel{{Layers: []ClipLayer{layer}}}},
		"duplicate id":     {Models: []ClipModel{{ID: "x"}, {ID: "x"}}},
		"five layers":      {Models: []ClipModel{{ID: "x", Layers: []ClipLayer{layer, layer, layer, layer, layer}}}},
		"nan time":         {Models: []ClipModel{{ID: "x", Layers: []ClipLayer{{Name: "a", Time: math.NaN(), Weight: 1}}}}},
		"inf time":         {Models: []ClipModel{{ID: "x", Layers: []ClipLayer{{Name: "a", Time: math.Inf(1), Weight: 1}}}}},
		"negative time":    {Models: []ClipModel{{ID: "x", Layers: []ClipLayer{{Name: "a", Time: -0.1, Weight: 1}}}}},
		"negative weight":  {Models: []ClipModel{{ID: "x", Layers: []ClipLayer{{Name: "a", Time: 0, Weight: -1}}}}},
		"nan weight":       {Models: []ClipModel{{ID: "x", Layers: []ClipLayer{{Name: "a", Time: 0, Weight: math.NaN()}}}}},
		"negative index":   {Models: []ClipModel{{ID: "x", Layers: []ClipLayer{{Index: -1, Time: 0, Weight: 1}}}}},
		"huge index":       {Models: []ClipModel{{ID: "x", Layers: []ClipLayer{{Index: 0x8000, Time: 0, Weight: 1}}}}},
		"float32 overflow": {Models: []ClipModel{{ID: "x", Layers: []ClipLayer{{Name: "a", Time: 1e300, Weight: 1}}}}},
	}
	for name, frame := range cases {
		if _, err := EncodeClipFrame(frame); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if _, err := EncodeClipFrame(ClipFrame{}); err != nil {
		t.Errorf("empty frame: %v", err)
	}
}

func TestEncodeClipFrameZeroAlloc(t *testing.T) {
	frame := ClipFrame{Models: []ClipModel{
		{ID: "p1", Layers: []ClipLayer{{Name: "idle", Time: 1, Weight: 1}, {Name: "walk", Time: 2, Weight: 0.5}, {Name: "hit", Time: 3, Weight: 0.1}, {Name: "block", Time: 4, Weight: 0.2}}},
		{ID: "p2", Layers: []ClipLayer{{Name: "idle", Time: 1, Weight: 1}, {Name: "walk", Time: 2, Weight: 0.5}, {Name: "hit", Time: 3, Weight: 0.1}, {Name: "block", Time: 4, Weight: 0.2}}},
	}}
	var enc ClipFrameEncoder
	if _, err := enc.Encode(frame); err != nil {
		t.Fatal(err)
	}
	if allocs := testing.AllocsPerRun(100, func() { _, _ = enc.Encode(frame) }); allocs != 0 {
		t.Fatalf("steady-state Encode allocated %v times", allocs)
	}
}

// clipFrameGoldenHex is shared with client/js/scene3d-clip-frame.test.mjs, which
// decodes the same bytes. Change both together.
const clipFrameGoldenHex = "475350340100040077616c6b0200020070310200000000c03f0000803e0103800000003f0000403f000200703200"

func TestEncodeClipFrameGolden(t *testing.T) {
	got, err := EncodeClipFrame(ClipFrame{Models: []ClipModel{
		{ID: "p1", Layers: []ClipLayer{{Name: "walk", Time: 1.5, Weight: 0.25, Loop: true}, {Index: 3, Time: 0.5, Weight: 0.75}}},
		{ID: "p2"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != clipFrameGoldenHex {
		t.Fatalf("wire bytes changed:\n got %x\nwant %s", got, clipFrameGoldenHex)
	}
}

func BenchmarkEncodeClipFrameTwoModelsFourLayers(b *testing.B) {
	layers := []ClipLayer{{Name: "idle", Time: 1, Weight: 1}, {Name: "walk", Time: 2, Weight: 0.5}, {Name: "hit", Time: 3, Weight: 0.1}, {Name: "block", Time: 4, Weight: 0.2}}
	frame := ClipFrame{Models: []ClipModel{{ID: "p1", Layers: layers}, {ID: "p2", Layers: layers}}}
	var enc ClipFrameEncoder
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := enc.Encode(frame); err != nil {
			b.Fatal(err)
		}
	}
}

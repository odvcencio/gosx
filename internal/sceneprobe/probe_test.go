package sceneprobe

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProbeSkipsGeometryAndRecordsPresence(t *testing.T) {
	p := Decode(json.RawMessage(`{"preferCanvas":true,"scene":{"points":[{"positions":[` + strings.Repeat("1,", 1000) + `1],"compressedPositions":null}],"instancedMeshes":[{}]}}`))
	if p.Err != nil {
		t.Fatal(p.Err)
	}
	if !p.ComputeChunk || !p.DecompressChunk || p.WebGPU || p.WebGL {
		t.Fatalf("wrong decisions: %+v", p)
	}
	if p.Scene.Points[0].CompressedPositions.Truthy {
		t.Fatal("null must be false for startup gates")
	}
}

func TestJSONTruthiness(t *testing.T) {
	for _, raw := range []string{"null", "false", "0", "-0", "0.0", `""`} {
		if TruthyJSON(json.RawMessage(raw)) {
			t.Errorf("%s is false", raw)
		}
	}
	for _, raw := range []string{"true", "1", "-1", "[]", "{}", `"0"`} {
		if !TruthyJSON(json.RawMessage(raw)) {
			t.Errorf("%s is true", raw)
		}
	}
}

func TestMalformedOptionalFieldKeepsIndependentReaders(t *testing.T) {
	p := Decode(json.RawMessage(`{"timelines":true,"controlZoom":"bad","poster":"/scene.png","scene":{"models":[{"src":"/model.glb","textureDescriptors":{"BaseColor":{"URI":"/texture.ktx2"}}}]}}`))
	if p.Err == nil || p.PreloadErr != nil || p.ChunkErr != nil || p.PosterErr != nil {
		t.Fatal("malformed flag affected another reader")
	}
	if !p.Timelines || p.ControlZoom || p.Poster != "/scene.png" || p.ComputeChunk || p.DecompressChunk || p.Scene.Models[0].Src != "/model.glb" || p.Scene.Models[0].Textures()[0] != "/texture.ktx2" {
		t.Fatalf("fallback changed decisions: %+v", p)
	}
}

func TestProbePreservesLegacyNormalization(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		gpu, gl bool
	}{
		{`{}`, true, true},
		{`{"preferWebGL":false}`, true, false},
		{`{"preferCanvas":true}`, false, false},
		{`{"forceWebGL":true,"preferCanvas":true}`, false, true},
		{`{"requireWebGL":true,"preferWebGL":false}`, false, true},
		{`{"backendCaps":{"capable":["WeBgPu","WEBGL2"]}}`, true, true},
		{`{"backendCaps":{"capable":[]}}`, false, false},
		{`{"backendCaps":{"capable":["webgl2"]},"scene":{"backendCaps":{"capable":["WEBGPU"]}}}`, true, false},
	} {
		p := Decode(json.RawMessage(tc.raw))
		if p.Err != nil || p.WebGPU != tc.gpu || p.WebGL != tc.gl {
			t.Errorf("%s: backends = (%v, %v), error = %v", tc.raw, p.WebGPU, p.WebGL, p.Err)
		}
	}
	p := Decode(json.RawMessage(`{"poster":" /poster.png ","posterAlt":" padded alt ","scene":{"objects":[{"texture":"/fallback.png","normalMap":"/normal.png","textureDescriptors":{"BASECOLOR":{"URI":" /base.ktx2 "},"NoRmAl":{"uri":" \t "}}}]}}`))
	if p.Err != nil || p.Poster != "/poster.png" || p.PosterAlt != " padded alt " {
		t.Fatalf("poster normalization changed: %+v", p)
	}
	textures := p.Scene.Objects[0].Textures()
	if textures[0] != " /base.ktx2 " || textures[1] != "/normal.png" {
		t.Fatalf("texture descriptor case/empty fallback changed: %q", textures)
	}
}

package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/internal/sceneprobe"
)

func TestScene3DProbeReusedForHeadEmission(t *testing.T) {
	raw := json.RawMessage(`{"timelines":true,"particleBursts":true,"controlZoom":true,"modelPreparation":true,"walk":{},"vessel":{"nodeId":"boat"},"poster":"/scene.png","scene":{"instancedMeshes":[{}],"points":[{"compressedPositions":[1,2,3]}]}}`)
	probe := sceneprobe.Decode(raw)
	r := NewRenderer("probe")
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: raw, Scene3DProbe: probe}, gosx.Text(""))
	entry := &r.Manifest().Engines[0]
	if entry.Scene3DProbe != probe {
		t.Fatal("prepared probe was decoded again")
	}
	if string(entry.Props) != string(raw) {
		t.Fatal("props changed during registration")
	}
	for i := 0; i < 3; i++ {
		compute, decompress := r.scene3DChunkNeeds()
		if !compute || !decompress || !r.scene3DNeedsTimelineChunk() || !r.scene3DNeedsParticleBurstChunk() || !r.scene3DNeedsZoomChunk() || !r.scene3DNeedsModelPreparationChunk() || !r.scene3DNeedsWalkChunk() || !r.scene3DNeedsVesselChunk() {
			t.Fatal("probe lost a declared feature")
		}
		r.PreloadHints()
		r.BootstrapScript()
		if entry.Scene3DProbe != probe {
			t.Fatal("head emission replaced probe")
		}
	}
	other := NewRenderer("other")
	other.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: raw}, gosx.Text(""))
	if other.Manifest().Engines[0].Scene3DProbe == probe {
		t.Fatal("probe shared across requests")
	}
}

func TestEngineRawPropsPreserveNumbersAndEscapeHTML(t *testing.T) {
	r := NewRenderer("raw")
	raw := json.RawMessage(`{"integer":9007199254740993,"html":"<script>&"}`)
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: raw}, gosx.Text(""))
	props := string(r.Manifest().Engines[0].Props)
	if !strings.Contains(props, "9007199254740993") || strings.Contains(props, "<script>") {
		t.Fatalf("unsafe or rounded raw props: %s", props)
	}
	if _, err := engineProps(json.RawMessage(`{"bad":`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

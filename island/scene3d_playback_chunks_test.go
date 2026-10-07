package island

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

func TestScene3DPlaybackChunksRequireOptIn(t *testing.T) {
	for _, tc := range []struct {
		name                string
		props               []string
		timelines, bursts   bool
		compute, decompress bool
	}{
		{name: "absent", props: []string{`{}`}},
		{name: "explicit false", props: []string{`{"timelines":false,"particleBursts":false}`}},
		{name: "nested false", props: []string{`{"scene":{"timelines":false,"particleBursts":false}}`}},
		{name: "flat timeline", props: []string{`{"timelines":true}`}, timelines: true},
		{name: "nested timeline", props: []string{`{"scene":{"timelines":true}}`}, timelines: true},
		{name: "flat burst", props: []string{`{"particleBursts":true}`}, bursts: true, compute: true},
		{name: "nested burst", props: []string{`{"scene":{"particleBursts":true}}`}, bursts: true, compute: true},
		{name: "nested opt-ins", props: []string{`{"timelines":false,"particleBursts":false,"scene":{"timelines":true,"particleBursts":true}}`}, timelines: true, bursts: true, compute: true},
		{name: "outer opt-ins", props: []string{`{"timelines":true,"particleBursts":true,"scene":{"timelines":false,"particleBursts":false}}`}, timelines: true, bursts: true, compute: true},
		{name: "compute alone", props: []string{`{"scene":{"computeParticles":[{"id":"smoke"}]}}`}, compute: true},
		{name: "multiple scenes", props: []string{`{}`, `{"timelines":true}`, `{"scene":{"particleBursts":true}}`, `{"timelines":false,"particleBursts":false}`}, timelines: true, bursts: true, compute: true},
		// Compute/decompress must not short-circuit discovery of later opt-ins.
		{name: "later opt-ins", props: []string{`{"computeParticles":[{}],"compression":{"lod":true}}`, `{"timelines":true,"particleBursts":true}`}, timelines: true, bursts: true, compute: true, decompress: true},
		{name: "later content", props: []string{`{"timelines":true,"particleBursts":true}`, `{"compression":{"lod":true}}`}, timelines: true, bursts: true, compute: true, decompress: true},
		{name: "undecodable preserves permissive fallback", props: []string{`{"scene":42}`}, timelines: true, bursts: true, compute: true, decompress: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRenderer("main")
			// Unrelated engines cannot enable Scene3D features.
			r.RenderEngine(engine.Config{Name: "Other", Kind: engine.KindSurface, Props: json.RawMessage(`{"timelines":true,"particleBursts":true}`)}, gosx.Text(""))
			for _, props := range tc.props {
				r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(props)}, gosx.Text(""))
			}
			markup := gosx.RenderHTML(r.BootstrapScript())
			for _, chunk := range []struct {
				name string
				want bool
			}{
				{"presentation", tc.timelines || tc.bursts},
				{"timeline", tc.timelines}, {"particle-burst", tc.bursts},
				{"compute", tc.compute}, {"decompress", tc.decompress},
			} {
				attr := "data-gosx-scene3d-" + chunk.name + "-url="
				if got := strings.Contains(markup, attr); got != chunk.want {
					t.Errorf("%s advertised = %t, want %t", chunk.name, got, chunk.want)
				}
			}
		})
	}
}

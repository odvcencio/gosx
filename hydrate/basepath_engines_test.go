package hydrate

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestBuiltInEngineURLsUseBasePath(t *testing.T) {
	for _, tc := range []struct {
		name, component, kind, props, want string
	}{
		{"video", "NamedVideo", "video", `{
 "src":"/media/movie.webm?quality=hd#start", "poster":"/media/poster.png", "sync":"/video-sync",
 "sources":[{"src":"/media/alternate.webm","type":"video/webm"},{"url":"https://cdn.example/movie.webm"}],
 "subtitleBase":"/subtitles", "subtitleTracks":[{"src":"/subtitles/en.vtt","id":"/identity"}],
 "subtitles":{"refreshEndpoint":"/refresh"}, "telemetry":{"endpoint":"/video-events"},
 "metadata":{"src":"/unchanged"}, "identity":9007199254740993
}`, `{
 "src":"/game/media/movie.webm?quality=hd#start", "poster":"/game/media/poster.png", "sync":"/game/video-sync",
 "sources":[{"src":"/game/media/alternate.webm","type":"video/webm"},{"url":"https://cdn.example/movie.webm"}],
 "subtitleBase":"/game/subtitles", "subtitleTracks":[{"src":"/game/subtitles/en.vtt","id":"/identity"}],
 "subtitles":{"refreshEndpoint":"/game/refresh"}, "telemetry":{"endpoint":"/game/video-events"},
 "metadata":{"src":"/unchanged"}, "identity":9007199254740993
}`},
		{"scene", "GoSXScene3D", "surface", `{
 "scene":{
  "models":[{"src":"/models/city.gltf","previewSrc":"/models/preview.glb","fullSrc":"/models/full.glb", "inState":{"src":"/models/other.gltf"}}],
  "objects":[{"texture":"/assets/wood.png","normalMap":"/assets/normal.png", "detail":{"ground":{"albedo":"/assets/detail.png"}}, "textureDescriptors":{"baseColor":{"uri":"/assets/wood.ktx2"},"data":{"mask":{"uri":"/assets/mask.ktx2"}}},"customUniforms":{"src":"/unchanged"}}],
  "environment":{"envMap":"/assets/env.hdr","inState":{"envMap":"/assets/night.hdr"},"ibl":{"source":"/assets/env.hdr","radiance":{"uri":"/assets/radiance.ktx2"}}},
  "sprites":[{"src":"/assets/sprite.png"}],
  "instancedGLBMeshes":[{"src":"/models/crowd.glb"}],
  "nodes":[{"mesh":{"src":"/models/node.glb"},"material":{"emissiveMap":"/assets/emissive.png"},"metadata":{"src":"/unchanged"}}],
  "metadata":{"src":"/unchanged","texture":"/unchanged"}
 }, "src":"/unrelated", "identity":9007199254740993
}`, `{
 "scene":{
  "models":[{"src":"/game/models/city.gltf","previewSrc":"/game/models/preview.glb","fullSrc":"/game/models/full.glb", "inState":{"src":"/game/models/other.gltf"}}],
  "objects":[{"texture":"/game/assets/wood.png","normalMap":"/game/assets/normal.png", "detail":{"ground":{"albedo":"/game/assets/detail.png"}}, "textureDescriptors":{"baseColor":{"uri":"/game/assets/wood.ktx2"},"data":{"mask":{"uri":"/game/assets/mask.ktx2"}}},"customUniforms":{"src":"/unchanged"}}],
  "environment":{"envMap":"/game/assets/env.hdr","inState":{"envMap":"/game/assets/night.hdr"},"ibl":{"source":"/game/assets/env.hdr","radiance":{"uri":"/game/assets/radiance.ktx2"}}},
  "sprites":[{"src":"/game/assets/sprite.png"}],
  "instancedGLBMeshes":[{"src":"/game/models/crowd.glb"}],
  "nodes":[{"mesh":{"src":"/game/models/node.glb"},"material":{"emissiveMap":"/game/assets/emissive.png"},"metadata":{"src":"/unchanged"}}],
  "metadata":{"src":"/unchanged","texture":"/unchanged"}
 }, "src":"/unrelated", "identity":9007199254740993
}`},
		{"scene legacy props", "GoSXScene3D", "surface", `{"models":[{"src":"/models/city.gltf"}],"environment":{"envMap":"/assets/env.hdr"}}`, `{"models":[{"src":"/game/models/city.gltf"}],"environment":{"envMap":"/game/assets/env.hdr"}}`},
		{"custom engine", "Custom", "surface", `{"src":"/media/movie.webm","scene":{"models":[{"src":"/models/city.gltf"}]}}`, `{"src":"/media/movie.webm","scene":{"models":[{"src":"/models/city.gltf"}]}}`},
		{"external and relative", "GoSXVideo", "video", `{"src":"https://cdn.example/movie.webm","poster":"//cdn.example/poster.png","sync":"wss://realtime.example/ws","subtitleBase":"relative","sources":[{"src":"/game/media/movie.webm"}]}`, `{"src":"https://cdn.example/movie.webm","poster":"//cdn.example/poster.png","sync":"wss://realtime.example/ws","subtitleBase":"relative","sources":[{"src":"/game/media/movie.webm"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManifest()
			m.Engines = []EngineEntry{{Component: tc.component, Kind: tc.kind, Props: json.RawMessage(tc.props)}}
			out := m.WithBasePath("/game")
			decode := func(raw string) any {
				d := json.NewDecoder(strings.NewReader(raw))
				d.UseNumber()
				var value any
				if err := d.Decode(&value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			if !reflect.DeepEqual(decode(string(out.Engines[0].Props)), decode(tc.want)) {
				t.Fatalf("props = %s, want %s", out.Engines[0].Props, tc.want)
			}
			if string(m.Engines[0].Props) != tc.props {
				t.Fatal("shared props mutated")
			}
			if again := out.WithBasePath("/game"); string(again.Engines[0].Props) != string(out.Engines[0].Props) {
				t.Fatal("props prefixed twice")
			}
			if m.WithBasePath("") != m {
				t.Fatal("default manifest changed")
			}
		})
	}
}

func TestEngineBasePathKeepsUnrecognizedProps(t *testing.T) {
	for _, props := range []string{"null", `"opaque"`, `[]`, `{invalid`, `{"src":3,"sources":null,"metadata":{"src":"/untouched"}}`} {
		entry := EngineEntry{Component: "GoSXVideo", Kind: "video", Props: json.RawMessage(props)}
		if got := enginePropsWithBasePath("/game", entry); string(got) != props {
			t.Fatalf("props changed: %s", got)
		}
	}
}

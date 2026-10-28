package gametypes

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The client audio runtime reads these types as JSON, so the field names and
// the rules for leaving a field out are a wire format. Each case pins the JSON
// of one value and checks that it decodes back to the same value. The strings
// are the output of the definitions that package game declared before they
// moved here.
func TestJSONWireFormat(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		// zero is a new value to decode into.
		zero any
		want string
	}{
		{
			name: "manifest with every field",
			value: AudioManifest{
				MasterVolume: 0.5,
				Buses:        []AudioBus{{ID: "sfx", Parent: "master", Volume: 0.8, Muted: true}},
				Clips: []AudioClip{{
					ID: "hit", URI: "/audio/hit.ogg", ContentType: "audio/ogg", Bus: "sfx",
					Preload: true, Loop: true, Volume: 0.75, Rate: 1.5,
					Metadata: map[string]string{"quality": "high"},
				}},
			},
			zero: &AudioManifest{},
			want: `{"masterVolume":0.5,"buses":[{"id":"sfx","parent":"master","volume":0.8,"muted":true}],` +
				`"clips":[{"id":"hit","uri":"/audio/hit.ogg","contentType":"audio/ogg","bus":"sfx",` +
				`"preload":true,"loop":true,"volume":0.75,"rate":1.5,"metadata":{"quality":"high"}}]}`,
		},
		{
			name:  "empty manifest",
			value: AudioManifest{},
			zero:  &AudioManifest{},
			want:  `{}`,
		},
		{
			name:  "bus and clip keep id and uri when empty",
			value: []any{AudioBus{}, AudioClip{}},
			zero:  &[]any{},
			want:  `[{"id":""},{"id":"","uri":""}]`,
		},
		{
			name: "playback with every field",
			value: AudioPlayback{
				Clip: "hit", Bus: "sfx", Handle: "h1", Volume: 0.9, Rate: 1.1, Pan: -0.25, Loop: true,
				Position: &Vec3{X: 2, Y: 1.5, Z: -4}, Velocity: &Vec3{X: 1},
				RefDistance: 2, MaxDistance: 64, RolloffFactor: 0.75,
			},
			zero: &AudioPlayback{},
			want: `{"clip":"hit","bus":"sfx","handle":"h1","volume":0.9,"rate":1.1,"pan":-0.25,"loop":true,` +
				`"position":{"x":2,"y":1.5,"z":-4},"velocity":{"x":1},` +
				`"refDistance":2,"maxDistance":64,"rolloffFactor":0.75}`,
		},
		{
			name:  "empty playback keeps clip",
			value: AudioPlayback{},
			zero:  &AudioPlayback{},
			want:  `{"clip":""}`,
		},
		{
			name:  "vec3 leaves out zero axes",
			value: Vec3{Y: 3},
			zero:  &Vec3{},
			want:  `{"y":3}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(raw) != tc.want {
				t.Fatalf("Marshal = %s\nwant      %s", raw, tc.want)
			}
			if err := json.Unmarshal(raw, tc.zero); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			// Typed values decode back to themselves. The []any case decodes
			// to generic maps, so only compare the typed ones.
			if _, generic := tc.zero.(*[]any); generic {
				return
			}
			got := reflect.ValueOf(tc.zero).Elem().Interface()
			if !reflect.DeepEqual(got, tc.value) {
				t.Fatalf("decoded %#v, want %#v", got, tc.value)
			}
		})
	}
}

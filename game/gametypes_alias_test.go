package game

import (
	"reflect"
	"testing"

	"m31labs.dev/gosx/game/gametypes"
)

// Package game declares AssetID, Vec3 and the audio manifest types as aliases
// of the gametypes ones, so a game.AudioManifest passes to game/audio, which
// takes a gametypes.AudioManifest, with no conversion. A defined type
// ("type AudioManifest gametypes.AudioManifest") would look the same at most
// call sites and then fail to compile at the first call that crosses the two
// packages. A component store keys its columns by reflect.Type, so identity
// also keeps a Vec3 component in the same column as before. This test pins the
// identity.
func TestSharedTypesAreGametypesAliases(t *testing.T) {
	for _, tc := range []struct {
		name         string
		game, shared reflect.Type
	}{
		{"AssetID", reflect.TypeFor[AssetID](), reflect.TypeFor[gametypes.AssetID]()},
		{"Vec3", reflect.TypeFor[Vec3](), reflect.TypeFor[gametypes.Vec3]()},
		{"AudioBus", reflect.TypeFor[AudioBus](), reflect.TypeFor[gametypes.AudioBus]()},
		{"AudioClip", reflect.TypeFor[AudioClip](), reflect.TypeFor[gametypes.AudioClip]()},
		{"AudioManifest", reflect.TypeFor[AudioManifest](), reflect.TypeFor[gametypes.AudioManifest]()},
		{"AudioPlayback", reflect.TypeFor[AudioPlayback](), reflect.TypeFor[gametypes.AudioPlayback]()},
	} {
		if tc.game != tc.shared {
			t.Errorf("game.%s is the type %v and gametypes.%s is the type %v; game must declare an alias (type %s = gametypes.%s)",
				tc.name, tc.game, tc.name, tc.shared, tc.name, tc.name)
		}
	}
}

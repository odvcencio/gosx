// Package gametypes declares the data types that package game and the browser
// audio package game/audio both use: AssetID, Vec3, and the audio manifest
// types AudioBus, AudioClip, AudioManifest and AudioPlayback.
//
// This package has no imports. A browser module can name these types without
// linking package game, which imports hub, sim and the root gosx package, and
// through them net/http, crypto/tls and crypto/x509.
//
// Package game declares each type as an alias, for example
// "type AudioManifest = gametypes.AudioManifest". A game.AudioManifest and a
// gametypes.AudioManifest are the same type, so values pass between the two
// packages without conversion and code that uses the game names needs no
// change. The JSON field names below are the wire format of the client audio
// manifest.
package gametypes

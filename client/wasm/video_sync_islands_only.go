//go:build js && wasm && gosx_tiny_islands_only

// Video-sync JS bridge — islands-only stub.
//
// The tiny build omits the videosync dependency entirely. The bridge-side
// methods are guarded by the same !gosx_tiny_islands_only build tag in
// bridge_video_sync.go, so no engine registry exists in this build. This
// file ensures the registerVideoSyncRuntime symbol exists and is a no-op,
// keeping the call in registerRuntime unconditional across both builds.

package main

import "m31labs.dev/gosx/client/bridge"

func registerVideoSyncRuntime(b *bridge.Bridge) {}

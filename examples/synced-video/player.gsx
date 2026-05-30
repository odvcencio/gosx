// Package syncedvideo is a minimal example of a follow-mode synced video
// engine using GoSX's built-in video engine and drift-correction bridge.
//
// Live convergence behavior requires:
//   - a browser (the sync socket and drift engine run in the runtime WASM)
//   - a pkg/theatre-compatible sync server reachable at the URL passed via Sync
//
// This file is not unit-testable in isolation — it demonstrates the authoring
// shape only. The actual drift correction runs client-side via the
// __gosx_video_sync_* WASM exports (with a parity-locked JS fallback).
package syncedvideo

//gosx:engine video
func SyncedPlayer(props struct {
	Src      string
	Sync     string
	SyncMode string
}) Node {
	return <video
		src={props.Src}
		sync={props.Sync}
		syncMode={props.SyncMode}
		playsinline
		muted
	/>
}

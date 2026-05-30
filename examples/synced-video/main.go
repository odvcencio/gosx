// Example synced-video demonstrates a follow-mode synced video engine.
//
// Live drift-correction requires a browser (DriftCorrector + SyncEngine run in
// the runtime WASM via __gosx_video_sync_* exports, with a parity-locked JS
// fallback) and a pkg/theatre-compatible sync server at SYNC_URL.
// Without those, the page renders the baseline <video> fallback and plays locally.
//
// Run: go run ./examples/synced-video
// Visit: http://localhost:8080
//
// Set SYNC_URL to point at a running theatre sync server, e.g.:
//
//	SYNC_URL=ws://localhost:9090/sync go run ./examples/synced-video
package main

import (
	"fmt"
	"log"
	"os"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

func main() {
	syncURL := getenv("SYNC_URL", "ws://localhost:9090/sync")
	streamURL := getenv("STREAM_URL", "https://example.com/placeholder/stream.m3u8")

	app := server.New()
	app.Page("GET /", func(ctx *server.Context) gosx.Node {
		return ctx.Video(server.VideoProps{
			Src:         streamURL,
			Sync:        syncURL,
			SyncMode:    "follow",
			Muted:       true,
			PlaysInline: true,
		})
	})

	port := getenv("PORT", "8080")
	fmt.Printf("synced-video example at http://localhost:%s\n", port)
	log.Fatal(app.ListenAndServe(":" + port))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

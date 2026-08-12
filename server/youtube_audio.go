package server

import (
	_ "embed"
	"net/http"
)

// YouTubeAudioBridgePath is the runtime asset path that serves the YouTube
// audio bridge. Any element with data-gosx-youtube-audio="<youtube url>"
// becomes a play/pause toggle for hidden background audio; the active
// element carries data-gosx-youtube-audio-state="playing" for CSS styling.
// The bridge shares one hidden player per page and lazy-loads the YouTube
// IFrame API on first activation.
const YouTubeAudioBridgePath = "/gosx/youtube-audio.js"

//go:embed youtube_audio.js
var youtubeAudioBridgeJS []byte

// YouTubeAudioBridgeScriptTag returns the HTML script tag that loads the
// YouTube audio bridge.
func YouTubeAudioBridgeScriptTag() string {
	return `<script defer src="` + YouTubeAudioBridgePath + `"></script>`
}

func serveYouTubeAudioBridge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	MarkObservedRequest(r, "runtime", YouTubeAudioBridgePath)
	w.Write(youtubeAudioBridgeJS)
}

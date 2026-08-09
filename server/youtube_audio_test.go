package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAppServesYouTubeAudioBridge(t *testing.T) {
	app := New()
	handler := app.Build()

	req := httptest.NewRequest(http.MethodGet, YouTubeAudioBridgePath, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("expected JS content type, got %q", got)
	}

	body := w.Body.String()
	// Pin the declarative contract: activation attribute, CSS state
	// attribute, lazy API load, and the shared hidden player host.
	for _, want := range []string{
		"data-gosx-youtube-audio",
		"data-gosx-youtube-audio-state",
		"gosx-youtube-api-script",
		"gosx-youtube-audio-host",
		"onYouTubeIframeAPIReady",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("youtube audio bridge missing %q", want)
		}
	}
}

func TestYouTubeAudioBridgeScriptTag(t *testing.T) {
	tag := YouTubeAudioBridgeScriptTag()
	if !strings.Contains(tag, YouTubeAudioBridgePath) || !strings.Contains(tag, "defer") {
		t.Fatalf("unexpected script tag %q", tag)
	}
}

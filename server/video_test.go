package server

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

func TestVideoRendersServerBaselineWithSingleSourceAttrs(t *testing.T) {
	html := gosx.RenderHTML(Video(VideoProps{
		Src:          "media/promo.mp4",
		Poster:       "media/poster.jpg",
		Controls:     true,
		Muted:        true,
		PlaysInline:  true,
		Width:        960,
		Height:       540,
		SubtitleBase: "subs",
		SubtitleTracks: []VideoTrack{
			{ID: "en", Language: "en", Title: "English", Default: true},
		},
	}, gosx.Text("Download video")))

	for _, snippet := range []string{
		`<video`,
		`data-gosx-video-fallback="true"`,
		`src="/media/promo.mp4"`,
		`poster="/media/poster.jpg"`,
		`controls`,
		`muted`,
		`playsinline`,
		`width="960"`,
		`height="540"`,
		`<track src="/subs/en.vtt" kind="subtitles" srclang="en" label="English" default`,
		`Download video`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestVideoRendersMultiSourceBaselineWithoutRootSrcAttr(t *testing.T) {
	html := gosx.RenderHTML(Video(VideoProps{
		Sources: []VideoSource{
			{Src: "media/promo.webm", Type: "video/webm"},
			{Src: "media/promo.mp4", Type: "video/mp4"},
		},
		SubtitleTracks: []VideoTrack{
			{ID: "en", Language: "en", Title: "English", Kind: "captions", Src: "subs/en-custom.vtt"},
		},
	}))

	openingTag := html
	if idx := strings.Index(openingTag, ">"); idx >= 0 {
		openingTag = openingTag[:idx]
	}
	if strings.Contains(openingTag, ` src="`) {
		t.Fatalf("did not expect root video src attr when source children are present, got %q", openingTag)
	}
	for _, snippet := range []string{
		`<source src="/media/promo.webm" type="video/webm"`,
		`<source src="/media/promo.mp4" type="video/mp4"`,
		`<track src="/subs/en-custom.vtt" kind="captions" srclang="en" label="English"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestVideoEngineConfigUsesManagedVideoDefaults(t *testing.T) {
	cfg := VideoEngineConfig(VideoProps{
		Src:        "media/promo.mp4",
		AudioTrack: "3",
		AudioTracks: []VideoAudioTrack{
			{ID: "3", Language: "jpn", Label: "Japanese", Default: true},
		},
	})

	if cfg.Name != defaultVideoEngineName {
		t.Fatalf("expected default engine name %q, got %q", defaultVideoEngineName, cfg.Name)
	}
	if cfg.Kind != engine.KindVideo {
		t.Fatalf("expected video engine kind, got %q", cfg.Kind)
	}
	if !strings.Contains(string(cfg.Props), `"src":"/media/promo.mp4"`) {
		t.Fatalf("expected normalized video src in props, got %s", string(cfg.Props))
	}
	if !strings.Contains(string(cfg.Props), `"audioTrack":"3"`) || !strings.Contains(string(cfg.Props), `"audioTracks":[`) {
		t.Fatalf("expected audio track contract in props, got %s", string(cfg.Props))
	}
	if got := len(cfg.Capabilities); got != 3 {
		t.Fatalf("expected three default capabilities, got %d", got)
	}
}

func TestSyncTuningMarshalOmitsZeroFields(t *testing.T) {
	props := VideoProps{
		Src: "media/test.mp4",
		SyncTuning: &SyncTuning{
			ToleranceThreshold: 1.0,
			MaxSeeksPerMinute:  6,
		},
	}
	data, err := json.Marshal(normalizeVideoProps(props))
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"syncTuning":`) {
		t.Fatalf("expected syncTuning key in JSON, got %s", got)
	}
	if !strings.Contains(got, `"toleranceThreshold":1`) {
		t.Fatalf("expected toleranceThreshold:1 in syncTuning, got %s", got)
	}
	if !strings.Contains(got, `"maxSeeksPerMinute":6`) {
		t.Fatalf("expected maxSeeksPerMinute:6 in syncTuning, got %s", got)
	}
	// Zero fields must be omitted (omitempty).
	if strings.Contains(got, `"rateThreshold"`) {
		t.Fatalf("expected zero rateThreshold to be omitted, got %s", got)
	}
	if strings.Contains(got, `"seekThreshold"`) {
		t.Fatalf("expected zero seekThreshold to be omitted, got %s", got)
	}
}

func TestSyncTuningNilOmitsKey(t *testing.T) {
	props := VideoProps{
		Src:        "media/test.mp4",
		SyncTuning: nil,
	}
	data, err := json.Marshal(normalizeVideoProps(props))
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	got := string(data)
	if strings.Contains(got, `"syncTuning"`) {
		t.Fatalf("expected syncTuning key to be absent when nil, got %s", got)
	}
}

func TestSyncStrategyRoundTrips(t *testing.T) {
	for _, strategy := range []string{"", "nudge", "snap", "nudge-legacy"} {
		props := VideoProps{
			Src:          "media/test.mp4",
			SyncStrategy: strategy,
		}
		normalized := normalizeVideoProps(props)
		if normalized.SyncStrategy != strings.TrimSpace(strategy) {
			t.Fatalf("SyncStrategy %q did not survive normalize, got %q", strategy, normalized.SyncStrategy)
		}
		data, err := json.Marshal(normalized)
		if err != nil {
			t.Fatalf("unexpected marshal error for strategy %q: %v", strategy, err)
		}
		got := string(data)
		if strategy != "" && !strings.Contains(got, `"syncStrategy":"`+strategy+`"`) {
			t.Fatalf("SyncStrategy %q not found in JSON: %s", strategy, got)
		}
	}
}

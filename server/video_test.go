package server

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/engine"
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
	cfg := VideoEngineConfig(VideoProps{Src: "media/promo.mp4"})

	if cfg.Name != defaultVideoEngineName {
		t.Fatalf("expected default engine name %q, got %q", defaultVideoEngineName, cfg.Name)
	}
	if cfg.Kind != engine.KindVideo {
		t.Fatalf("expected video engine kind, got %q", cfg.Kind)
	}
	if !strings.Contains(string(cfg.Props), `"src":"/media/promo.mp4"`) {
		t.Fatalf("expected normalized video src in props, got %s", string(cfg.Props))
	}
	if got := len(cfg.Capabilities); got != 3 {
		t.Fatalf("expected three default capabilities, got %d", got)
	}
}

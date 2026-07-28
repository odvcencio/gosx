package server

import (
	"net/http"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func posterConfig() ScenePosterConfig {
	return ScenePosterConfig{URL: "/posters/hero.a1b2c3.png", Alt: "A lit sphere over a dark floor", Width: 640, Height: 360}
}

// TestScenePosterMountStyleReservesTheBox pins the Cumulative Layout Shift
// answer. Without an aspect ratio the mount has no height until the canvas
// arrives, and the page moves when it does.
func TestScenePosterMountStyleReservesTheBox(t *testing.T) {
	style := ScenePosterMountStyle(posterConfig())
	for _, want := range []string{
		"position:relative",
		"aspect-ratio:640 / 360",
		`background-image:url("/posters/hero.a1b2c3.png")`,
		"background-size:cover",
	} {
		if !strings.Contains(style, want) {
			t.Fatalf("mount style is missing %q: %s", want, style)
		}
	}
}

// TestScenePosterMountStyleSkipsAGuessedRatio proves an unknown size produces no
// reservation instead of a wrong one. A wrong reservation shifts layout exactly
// as much as no reservation, and it also hides the mistake.
func TestScenePosterMountStyleSkipsAGuessedRatio(t *testing.T) {
	cfg := posterConfig()
	cfg.Width, cfg.Height = 0, 0
	style := ScenePosterMountStyle(cfg)
	if strings.Contains(style, "aspect-ratio") {
		t.Fatalf("a poster with no dimensions declared an aspect ratio: %s", style)
	}
	if !strings.Contains(style, "background-image") {
		t.Fatalf("the background survived nothing: %s", style)
	}
}

// TestScenePosterEmitsNothingWithoutAURL covers the no-poster page. Every helper
// must produce an empty result, because markup that points at a missing image
// costs a request and paints a broken-image glyph.
func TestScenePosterEmitsNothingWithoutAURL(t *testing.T) {
	cfg := ScenePosterConfig{Alt: "unused", Width: 640, Height: 360}
	if style := ScenePosterMountStyle(cfg); style != "" {
		t.Fatalf("mount style = %q, want empty", style)
	}
	if html := gosx.RenderHTML(ScenePosterImage(cfg)); html != "" {
		t.Fatalf("poster image = %q, want empty", html)
	}
	if html := gosx.RenderHTML(ScenePosterPreload(cfg)); html != "" {
		t.Fatalf("poster preload = %q, want empty", html)
	}
}

// TestScenePosterImageIsCrawlableAndUrgent pins the four attributes that make
// the poster useful: a real source, an alt string, eager loading, and a high
// fetch priority.
func TestScenePosterImageIsCrawlableAndUrgent(t *testing.T) {
	html := gosx.RenderHTML(ScenePosterImage(posterConfig()))
	for _, want := range []string{
		`src="/posters/hero.a1b2c3.png"`,
		`alt="A lit sphere over a dark floor"`,
		`loading="eager"`,
		`fetchpriority="high"`,
		`data-gosx-scene3d-poster="true"`,
		`width="640"`,
		`height="360"`,
		"position:absolute",
		"object-fit:cover",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("poster image is missing %q:\n%s", want, html)
		}
	}
	// An absolutely positioned poster must not add height of its own; the
	// mount's aspect ratio is the only thing that reserves the box.
	if !strings.Contains(html, "inset:0") {
		t.Fatalf("the poster does not fill the mount:\n%s", html)
	}
}

// TestScenePosterPreloadStartsTheFetchEarly pins the one mechanism behind the
// paint claim. The browser must learn the poster URL from the document head, not
// from the mount element halfway down the body.
func TestScenePosterPreloadStartsTheFetchEarly(t *testing.T) {
	html := gosx.RenderHTML(ScenePosterPreload(posterConfig()))
	for _, want := range []string{
		`rel="preload"`, `as="image"`, `href="/posters/hero.a1b2c3.png"`,
		`type="image/png"`, `fetchpriority="high"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("poster preload is missing %q:\n%s", want, html)
		}
	}
}

func TestScenePosterCacheHeaders(t *testing.T) {
	immutable := http.Header{}
	WriteScenePosterHeaders(immutable, "abc123", true)
	if got := immutable.Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q", got)
	}
	if got := immutable.Get("Cache-Control"); !strings.Contains(got, "immutable") || !strings.Contains(got, "max-age=31536000") {
		t.Fatalf("immutable poster cache control = %q", got)
	}
	if got := immutable.Get("ETag"); got != `"abc123"` {
		t.Fatalf("etag = %q", got)
	}

	// A poster served from a stable name is not immutable. Saying it is would
	// pin a stale picture in every shared cache for a year.
	mutable := http.Header{}
	WriteScenePosterHeaders(mutable, "abc123", false)
	if got := mutable.Get("Cache-Control"); strings.Contains(got, "immutable") {
		t.Fatalf("a non-hashed poster claimed immutability: %q", got)
	}
	if got := mutable.Get("Cache-Control"); !strings.Contains(got, "must-revalidate") {
		t.Fatalf("a non-hashed poster does not revalidate: %q", got)
	}
}

// TestScenePosterURLIsNormalized proves a web-root path and a bare path reach
// the same URL, so an author cannot produce a poster that only loads from one
// route depth.
func TestScenePosterURLIsNormalized(t *testing.T) {
	cfg := posterConfig()
	cfg.URL = "posters/hero.png"
	html := gosx.RenderHTML(ScenePosterImage(cfg))
	if !strings.Contains(html, `src="/posters/hero.png"`) {
		t.Fatalf("relative poster path was not normalized:\n%s", html)
	}
}

// TestScenePosterStyleEscapesTheURL keeps a crafted asset name from closing the
// CSS url() token and injecting a declaration.
func TestScenePosterStyleEscapesTheURL(t *testing.T) {
	cfg := posterConfig()
	cfg.URL = `/posters/he"ro.png`
	style := ScenePosterMountStyle(cfg)
	if strings.Contains(style, `url("/posters/he"ro.png")`) {
		t.Fatalf("the poster URL closed its own quote: %s", style)
	}
	if !strings.Contains(style, `\"`) {
		t.Fatalf("the poster URL was not escaped: %s", style)
	}
}

// TestScenePosterStyleKeepsNonASCIIPaths pins the reason this file does not use
// strconv.Quote. Go writes a non-ASCII rune as \uXXXX, and CSS reads a backslash
// followed by a non-hexadecimal character as that literal character, so a quoted
// path would ask the browser for a file that does not exist.
func TestScenePosterStyleKeepsNonASCIIPaths(t *testing.T) {
	cfg := posterConfig()
	cfg.URL = "/posters/café.png"
	style := ScenePosterMountStyle(cfg)
	if !strings.Contains(style, `url("/posters/café.png")`) {
		t.Fatalf("a non-ASCII poster path was mangled: %s", style)
	}
}

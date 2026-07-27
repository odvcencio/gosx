package server

// This file carries the serving half of the Scene3D poster.
//
// A poster is a still image of a scene that a page paints while WebAssembly and
// the interactive renderer boot. Without it a 3D page paints an empty rectangle
// until the whole runtime is ready, which fails Largest Contentful Paint by
// construction and shows a crawler nothing.
//
// Three rules shape the markup below. Break any one and the poster trades one
// Core Web Vital failure for another.
//
//  1. The mount element reserves its box before any image or canvas arrives.
//     A poster that pops into the flow moves the page and costs Cumulative
//     Layout Shift. The aspect-ratio declaration does that reservation.
//  2. The same poster URL appears twice: once as an <img> child and once as the
//     mount element's background image. The <img> gives a crawler real content,
//     an alt string, and an element that Largest Contentful Paint can measure.
//     The background keeps the picture on screen after the Scene3D runtime
//     clears the mount's children, which it does before it creates the canvas.
//     Both draw the same decoded bytes, so removing the child reveals an
//     identical picture and nothing flashes.
//  3. The canvas needs no z-index. CSS paints a parent's background before any
//     descendant, so the canvas always covers the poster once it draws its first
//     frame.
//
// The Scene3D runtime in client/js already sets position:relative on the mount
// and already removes every child before it appends the canvas. This markup
// depends on both, and changes nothing in the runtime.
//
// Drop the child clear and the poster <img> stays under the canvas forever.
// Drop the relative position and the background poster escapes the mount. Both
// faults look like a server bug, so the claims below point at the browser file
// that really owns the behaviour.
//
//	gosx:claim has client/js/bootstrap-src/20-scene-mount.js `clearChildren\(ctx.mount\);`
//	gosx:claim has client/js/bootstrap-src/20-scene-mount.js `ctx.mount.style.position = "relative";`

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"m31labs.dev/gosx"
)

// ScenePosterMaxAge is how long a poster stays fresh in a shared cache. A build
// writes a poster whose name carries its content hash, so a changed scene
// produces a changed URL and this age never serves a stale picture.
const ScenePosterMaxAge = 365 * 24 * time.Hour

// ScenePosterConfig describes one poster on one Scene3D mount.
type ScenePosterConfig struct {
	// URL is the poster image path. An empty URL means the page has no poster,
	// and every function here returns an empty result rather than markup that
	// points at nothing.
	URL string
	// Alt describes the scene for a reader who cannot see it and for a crawler.
	// An empty Alt produces alt="", which marks the poster as decoration.
	Alt string
	// Width and Height are the poster's own pixel dimensions. They set the
	// aspect ratio that reserves the mount box, so a poster cannot shift layout.
	Width  int
	Height int
}

// Valid reports whether this configuration can produce markup.
func (c ScenePosterConfig) Valid() bool {
	return strings.TrimSpace(c.URL) != ""
}

// aspectRatio returns the CSS aspect-ratio value, or an empty string when the
// configuration carries no usable dimensions. An unknown ratio must produce no
// declaration rather than a guessed one, because a wrong reservation shifts
// layout exactly as much as no reservation.
func (c ScenePosterConfig) aspectRatio() string {
	if c.Width <= 0 || c.Height <= 0 {
		return ""
	}
	return strconv.Itoa(c.Width) + " / " + strconv.Itoa(c.Height)
}

// ScenePosterMountStyle returns the inline style for the Scene3D mount element.
//
// It does two jobs and no more. It reserves the box with an aspect ratio, and
// it repeats the poster as a background so the picture survives the moment the
// runtime clears the mount's children.
//
// It returns an empty string when the poster URL is empty, so a page without a
// poster emits no style attribute at all.
func ScenePosterMountStyle(cfg ScenePosterConfig) string {
	if !cfg.Valid() {
		return ""
	}
	url := AssetURL(cfg.URL)
	declarations := []string{
		"position:relative",
		"background-image:url(" + cssURLString(url) + ")",
		"background-size:cover",
		"background-position:center",
		"background-repeat:no-repeat",
	}
	if ratio := cfg.aspectRatio(); ratio != "" {
		declarations = append(declarations, "aspect-ratio:"+ratio)
	}
	return strings.Join(declarations, ";")
}

// ScenePosterImage returns the <img> child that a Scene3D mount carries before
// hydration.
//
// The element is absolutely positioned across the mount so it never adds height
// of its own; the mount's aspect ratio already reserves the box. fetchpriority
// high and loading eager tell the browser this is the picture the reader came
// for, because a poster that arrives late is a poster that did not help.
//
// It returns an empty node when the poster URL is empty.
func ScenePosterImage(cfg ScenePosterConfig) gosx.Node {
	if !cfg.Valid() {
		return gosx.Text("")
	}
	attrs := []any{
		gosx.Attr("src", AssetURL(cfg.URL)),
		gosx.Attr("alt", cfg.Alt),
		gosx.Attr("data-gosx-scene3d-poster", "true"),
		gosx.Attr("decoding", "async"),
		gosx.Attr("loading", "eager"),
		gosx.Attr("fetchpriority", "high"),
		gosx.Attr("style", "position:absolute;inset:0;width:100%;height:100%;object-fit:cover;border-radius:inherit"),
	}
	if cfg.Width > 0 {
		attrs = append(attrs, gosx.Attr("width", cfg.Width))
	}
	if cfg.Height > 0 {
		attrs = append(attrs, gosx.Attr("height", cfg.Height))
	}
	return gosx.El("img", gosx.Attrs(attrs...))
}

// ScenePosterPreload returns the head <link> that starts the poster fetch in the
// first round trip.
//
// This is the whole mechanism behind the paint claim: the poster is a static
// file already on disk, so the browser can request and decode it while the
// WebAssembly module and the renderer chunk are still downloading. Without the
// preload the browser does not learn the poster URL until it parses the mount
// element, which costs the head of the document.
//
// It returns an empty node when the poster URL is empty.
func ScenePosterPreload(cfg ScenePosterConfig) gosx.Node {
	if !cfg.Valid() {
		return gosx.Text("")
	}
	return gosx.El("link", gosx.Attrs(
		gosx.Attr("rel", "preload"),
		gosx.Attr("as", "image"),
		gosx.Attr("href", AssetURL(cfg.URL)),
		gosx.Attr("type", "image/png"),
		gosx.Attr("fetchpriority", "high"),
	))
}

// ScenePosterCachePolicy returns the caching rules for a poster response.
//
// A poster is immutable only when its URL carries its content hash. A build that
// writes hero.png and overwrites it next release must not be told to cache for a
// year, so contentAddressed selects between the two answers instead of assuming
// the safe-looking one.
func ScenePosterCachePolicy(contentAddressed bool) CachePolicy {
	if contentAddressed {
		return CachePolicy{Public: true, MaxAge: ScenePosterMaxAge, Immutable: true}
	}
	// An hour is short enough that a rebuild reaches readers the same day, and
	// long enough that a repeat visit within one session pays nothing.
	return CachePolicy{Public: true, MaxAge: time.Hour, MustRevalidate: true}
}

// WriteScenePosterHeaders sets the content type, cache policy and validator for
// a poster response. The hash is the SHA-256 that gosx scene poster reported for
// the same bytes, so an ETag describes the picture a reader received.
func WriteScenePosterHeaders(header http.Header, sha256Hex string, contentAddressed bool) {
	if header == nil {
		return
	}
	header.Set("Content-Type", "image/png")
	if value := ScenePosterCachePolicy(contentAddressed).headerValue(); value != "" {
		header.Set("Cache-Control", value)
	}
	if hash := strings.TrimSpace(sha256Hex); hash != "" {
		header.Set("ETag", fmt.Sprintf("%q", hash))
	}
}

// cssURLString wraps a URL in a CSS string and escapes only what a CSS string
// must escape: the backslash, the closing quote, and a line break.
//
// It does not use strconv.Quote. Go escapes a non-ASCII rune as \uXXXX, and CSS
// reads a backslash followed by a non-hexadecimal character as that literal
// character, so strconv.Quote turns "café.png" into "cafu00e9.png" and the
// browser requests a file that does not exist.
//
// The </ replacement keeps a crafted asset name from closing a surrounding
// <style> element when a caller writes this value into a stylesheet.
func cssURLString(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\A `,
		"\r", `\D `,
		"</", `<\/`,
	)
	return `"` + replacer.Replace(value) + `"`
}

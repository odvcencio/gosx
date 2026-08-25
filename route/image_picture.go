package route

import (
	"fmt"
	"sort"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/server"
)

// buildManifestImagePicture builds a <picture> element from the build-time
// image variants gosx build's imagepipe stage recorded in
// buildmanifest.Manifest.Images (issue #200), for the file-router's
// <Image> BUILTIN TAG specifically (gosx#201). server.Image, the Go helper
// function a page.server.go file calls directly, keeps its existing
// single-<img> contract unchanged — only the JSX tag gains this markup, so
// one Go function does not silently start returning a different element
// shape out from under an existing direct caller.
//
// It returns ok=false — and renderImage falls straight through to the
// unmodified #199-fixed server.Image call — in every one of these cases,
// so <Image> never fails a render just because a build-time variant is
// unavailable:
//
//   - No App has registered a manifest lookup yet: an ordinary `gosx dev`
//     run with no prior `gosx build`, or any process that never called
//     App.Build() at all (for example RenderProgramComponent called
//     directly, as cmd/gosx render and every route package test do).
//   - src is external, a data: URI, or otherwise not a source gosx build's
//     imagepipe stage ever records — see buildmanifest.ImageAsset.Source.
//   - props.Format or props.Quality names an explicit encode parameter. A
//     pre-built variant was encoded once, at build time, with neither; it
//     cannot honor a caller's per-request override the way the runtime
//     optimizer endpoint can, so a request naming either falls back to
//     that endpoint instead of silently ignoring the override.
func buildManifestImagePicture(props server.ImageProps, extra []any) (gosx.Node, bool) {
	if strings.TrimSpace(props.Format) != "" || props.Quality != 0 {
		return gosx.Node{}, false
	}

	asset, ok := server.LookupImageManifestAsset(server.AssetURL(props.Src))
	if !ok || asset.Width <= 0 || asset.Height <= 0 || len(asset.Variants) == 0 {
		return gosx.Node{}, false
	}

	webp := imageVariantsByFormat(asset.Variants, "webp")
	native, _ := nativeImageVariants(asset.Variants)

	width, height := manifestImageDisplaySize(props, asset)
	base := manifestImageBaseAttrs(props, width, height)
	sizes := manifestImageSizes(props.Sizes)

	switch {
	case len(webp) > 0 && len(native) > 0:
		// An opaque source gets both tqwebp and same-source native-format
		// ladders by default.
		// A WebP <source> (smaller, modern) plus an <img> fallback in the
		// source's own original format — every browser without WebP
		// <picture> support still gets a real, correctly sized image via
		// the <img> alone.
		source := gosx.El("source", gosx.Attrs(
			gosx.Attr("type", "image/webp"),
			gosx.Attr("srcset", imageSrcsetValue(webp)),
			gosx.Attr("sizes", sizes),
		))
		return gosx.El("picture", source, manifestImageOnly(base, sizes, native, extra)), true
	case len(native) > 0:
		// An alpha-bearing source retains only its native fallback because
		// tqwebp deliberately refuses alpha for now. A plain <img>, not a
		// <picture> — real hashed files, no per-request resize, and no
		// empty <source> to render around.
		return manifestImageOnly(base, sizes, native, extra), true
	case len(webp) > 0:
		// A WebP-native source with no same-source native-format rung
		// alongside it (cmd/gosx never generates a redundant same-format
		// fallback for one — see imagePipeNativeFormat). Its own WebP
		// variants already ARE the "original format", so they render on a
		// plain <img> — the same top-level element shape a non-manifest
		// render already produces — rather than a <picture> wrapping one
		// <source type="image/webp"> and an <img> naming the identical
		// bytes twice.
		return manifestImageOnly(base, sizes, webp, extra), true
	default:
		return gosx.Node{}, false
	}
}

// manifestImageOnly renders one <img> whose src/srcset/sizes come from one
// format's variant ladder — used directly for a single-format asset, and as
// the fallback element inside buildManifestImagePicture's <picture>.
func manifestImageOnly(base []any, sizes string, variants []buildmanifest.ImageVariantAsset, extra []any) gosx.Node {
	attrs := append(append([]any{}, base...),
		gosx.Attr("src", server.ImageManifestVariantURL(variants[len(variants)-1])),
		gosx.Attr("srcset", imageSrcsetValue(variants)),
		gosx.Attr("sizes", sizes),
	)
	args := append([]any{gosx.Attrs(attrs...)}, extra...)
	return gosx.El("img", args...)
}

// manifestImageBaseAttrs returns the attributes shared by every manifest-
// backed <img> regardless of format: alt, injected width/height, and the
// loading/decoding/fetchpriority triad gosx#199 already defined for
// server.Image. src/srcset/sizes are added by the caller once it knows
// which format's variant ladder it is rendering.
func manifestImageBaseAttrs(props server.ImageProps, width, height int) []any {
	loading, decoding, fetchPriority := manifestImageLoadingAttrs(props)
	attrs := []any{
		gosx.Attr("alt", props.Alt),
		gosx.Attr("width", width),
		gosx.Attr("height", height),
		gosx.Attr("loading", loading),
		gosx.Attr("decoding", decoding),
	}
	if fetchPriority != "" {
		attrs = append(attrs, gosx.Attr("fetchpriority", fetchPriority))
	}
	return attrs
}

// manifestImageLoadingAttrs mirrors server.Image's own loading/decoding/
// fetchpriority defaulting (server/image.go) exactly, so a manifest-backed
// <picture>/<img> and the #199 fallback path never disagree about priority
// semantics: an explicit prop always wins; Priority flips the default to
// eager+high; fetchPriority is empty (omitted) unless FetchPriority or
// Priority asks for one.
func manifestImageLoadingAttrs(props server.ImageProps) (loading, decoding, fetchPriority string) {
	loading = strings.TrimSpace(props.Loading)
	if loading == "" {
		if props.Priority {
			loading = "eager"
		} else {
			loading = "lazy"
		}
	}
	decoding = strings.TrimSpace(props.Decoding)
	if decoding == "" {
		decoding = "async"
	}
	fetchPriority = strings.TrimSpace(props.FetchPriority)
	if fetchPriority == "" && props.Priority {
		fetchPriority = "high"
	}
	return loading, decoding, fetchPriority
}

// manifestImageDisplaySize resolves the <img> width/height HTML attributes
// for a manifest-backed <Image>. An author-supplied width and height both
// win verbatim (server.Image's existing "exact rectangle, can change aspect
// ratio" contract); a single explicit dimension derives the other
// proportionally from the source's own intrinsic ratio — capped
// presentation via attrs. With neither set, the source's own intrinsic
// Width/Height is injected automatically: gosx#201's whole point, the one
// step next/image still asks an author to perform, deleted for a local,
// build-time-known source.
func manifestImageDisplaySize(props server.ImageProps, asset buildmanifest.ImageAsset) (int, int) {
	switch {
	case props.Width > 0 && props.Height > 0:
		return props.Width, props.Height
	case props.Width > 0:
		height := int(float64(asset.Height) * (float64(props.Width) / float64(asset.Width)))
		if height < 1 {
			height = 1
		}
		return props.Width, height
	case props.Height > 0:
		width := int(float64(asset.Width) * (float64(props.Height) / float64(asset.Height)))
		if width < 1 {
			width = 1
		}
		return width, props.Height
	default:
		return asset.Width, asset.Height
	}
}

// manifestImageSizes defaults sizes to "100vw" whenever the author leaves it
// unset. Unlike server.Image's legacy responsive path (which defaults sizes
// only when Responsive is explicitly set), a manifest-backed render always
// carries a real, build-time-generated width ladder, so it is always worth
// a sizes hint — there is no narrower "fixed, non-responsive" mode here to
// distinguish it from.
func manifestImageSizes(sizes string) string {
	sizes = strings.TrimSpace(sizes)
	if sizes == "" {
		return "100vw"
	}
	return sizes
}

// imageVariantsByFormat returns every variant at format, sorted ascending
// by width. The manifest itself already orders variants by the ladder gosx
// build walked (ascending), but this does not assume that ordering.
func imageVariantsByFormat(variants []buildmanifest.ImageVariantAsset, format string) []buildmanifest.ImageVariantAsset {
	out := make([]buildmanifest.ImageVariantAsset, 0, len(variants))
	for _, variant := range variants {
		if variant.Format == format {
			out = append(out, variant)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Width < out[j].Width })
	return out
}

// nativeImageVariants returns the one non-WebP format rung asset.Variants
// carries, if any, sorted ascending by width, plus that format's name.
// cmd/gosx's stageImageVariants generates at most one non-WebP format per
// source — the format matching the source's own extension (see
// imagePipeNativeFormat) — so the first non-webp entry found names the
// only such format present.
func nativeImageVariants(variants []buildmanifest.ImageVariantAsset) ([]buildmanifest.ImageVariantAsset, string) {
	format := ""
	for _, variant := range variants {
		if variant.Format != "webp" {
			format = variant.Format
			break
		}
	}
	if format == "" {
		return nil, ""
	}
	return imageVariantsByFormat(variants, format), format
}

// imageSrcsetValue joins one format's variant ladder into an HTML srcset
// value ("<url> <width>w, <url> <width>w, ...").
func imageSrcsetValue(variants []buildmanifest.ImageVariantAsset) string {
	entries := make([]string, 0, len(variants))
	for _, variant := range variants {
		entries = append(entries, fmt.Sprintf("%s %dw", server.ImageManifestVariantURL(variant), variant.Width))
	}
	return strings.Join(entries, ", ")
}

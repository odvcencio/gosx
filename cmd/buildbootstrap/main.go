// Command buildbootstrap builds the client bootstrap bundles from
// client/js/bootstrap-src. It is the pure-Go replacement for the retired
// Node build script (client/js/build-bootstrap.mjs): same source
// concatenation order, same bundle set, same .gz/.br sidecars, same --check
// mode, same output paths — with no npm, no node_modules, and no JS toolchain.
//
// Minification defaults to esbuild's native Go library (esbuild is written in
// Go; the npm package was only a wrapper around it), which keeps the minified
// bundles byte-identical to what the retired Node pipeline produced and keeps
// composed source maps. A pure tdewolff/minify backend is available via
// -minifier=tdewolff for A/B comparison; as of the migration it produces
// smaller raw/brotli output on the large bundles but breaches three committed
// size gates (see docs in the repo history), so it is not the default.
//
// Usage:
//
//	go run ./cmd/buildbootstrap            # rebuild all bundles
//	go run ./cmd/buildbootstrap --check    # exit 1 if committed bundles are stale
//
// Set GOSX_BUNDLE_DEBUG=1 to append sourceMappingURL trailers to the bundles.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	esbuild "github.com/evanw/esbuild/pkg/api"
	"github.com/klauspost/compress/gzip"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/js"
)

const (
	tailFile               = "bootstrap-src/30-tail.js"
	runtimeSceneCoreFile   = "bootstrap-src/10-runtime-scene-core.js"
	runtimePrimitivesFile  = "bootstrap-src/10-runtime-primitives.js"
	runtimeUtilsStart      = "  // Pending manifest reference, set during init, consumed when runtime is ready.\n"
	runtimeUtilsEnd        = "  function sceneCSSVarReference(value) {\n"
	sectionEngineMounting  = "  // --------------------------------------------------------------------------\n  // Engine mounting\n  // --------------------------------------------------------------------------\n"
	sectionHubConnections  = "  // --------------------------------------------------------------------------\n  // Hub connections\n  // --------------------------------------------------------------------------\n"
	sectionIslandDisposal  = "  // --------------------------------------------------------------------------\n  // Island disposal\n  // --------------------------------------------------------------------------\n"
	sectionHydration       = "  // --------------------------------------------------------------------------\n  // Hydration\n  // --------------------------------------------------------------------------\n"
	sectionCapabilityProbe = "  function entryRequiresAsyncWebGPUProbe(entry) {\n"
	sectionRuntimeReady    = "  // --------------------------------------------------------------------------\n  // Runtime ready callback\n  // --------------------------------------------------------------------------\n"
	sectionEventDelegation = "  // --------------------------------------------------------------------------\n  // Event delegation\n  // --------------------------------------------------------------------------\n"
)

type source struct {
	kind  string // "file" or "extract"
	rel   string // path relative to client/js, forward slashes
	label string // sourcemap label ("rel" or "rel#id")
	start string // extract start marker (inclusive)
	end   string // extract end marker (exclusive)
}

func sourceFile(rel string) source {
	return source{kind: "file", rel: rel, label: rel}
}

func sourceExtract(rel, id, start, end string) source {
	return source{kind: "extract", rel: rel, label: rel + "#" + id, start: start, end: end}
}

type output struct {
	name    string // bundle file name relative to client/js
	sources []source
}

var outputs = []output{
	{
		name: "bootstrap.js",
		sources: []source{
			sourceFile("bootstrap-src/00-textlayout.js"),
			sourceFile("bootstrap-src/04-telemetry.js"),
			sourceFile("bootstrap-src/05-document-env.js"),
			sourceFile("bootstrap-src/06-declarative-actions.js"),
			sourceFile("bootstrap-src/07-declarative-regions.js"),
			sourceFile(runtimePrimitivesFile),
			sourceFile(runtimeSceneCoreFile),
			sourceFile("bootstrap-src/11-scene-math.js"),
			sourceFile("bootstrap-src/11a-scene-decompress.js"),
			sourceFile("bootstrap-src/12-scene-geometry.js"),
			sourceFile("bootstrap-src/13-scene-material.js"),
			sourceFile("bootstrap-src/14-scene-lighting.js"),
			sourceFile("bootstrap-src/15-scene-ir-schema.js"),
			sourceFile("bootstrap-src/15-scene-ir-schema-strict.js"),
			sourceFile("bootstrap-src/15-scene-draw-plan.js"),
			sourceFile("bootstrap-src/15b-scene-planner.js"),
			sourceFile("bootstrap-src/15c-scene-backend-registry.js"),
			sourceFile("bootstrap-src/15a-scene-postfx-shared.js"),
			sourceFile("bootstrap-src/16b-scene-hdr.js"),
			sourceFile("bootstrap-src/16-scene-webgl.js"),
			// 16z provides _externalProbe and window.__gosx_scene3d_webgpu_probe,
			// which 16a-scene-webgpu.js references at runtime. Without it the
			// legacy monolithic bootstrap.js throws ReferenceError the first
			// time the scene3d mount path touches the webgpu probe, which in
			// turn aborts GoSXScene3D engine registration and kills 38 tests
			// in runtime.test.js that rely on scene3d mount.
			sourceFile("bootstrap-src/16z-scene-webgpu-probe.js"),
			sourceFile("bootstrap-src/16a-scene-webgpu.js"),
			sourceFile("bootstrap-src/16b-scene-compute.js"),
			sourceFile("bootstrap-src/17-scene-input.js"),
			sourceFile("bootstrap-src/18-scene-canvas.js"),
			sourceFile("bootstrap-src/19-scene-gltf.js"),
			sourceFile("bootstrap-src/19a-scene-animation.js"),
			sourceFile("bootstrap-src/19b-scene-control-forms.js"),
			sourceFile("bootstrap-src/20-scene-mount.js"),
			// 28 installs window.__gosx_video_sync_js_create — the pure-JS drift
			// engine the video factory (in 30-tail.js) uses on the brain-absent
			// path. It must load before the tail.
			sourceFile("bootstrap-src/28-video-sync-fallback.js"),
			sourceFile(tailFile),
		},
	},
	{
		name: "bootstrap-lite.js",
		sources: []source{
			sourceFile("bootstrap-src/00-textlayout.js"),
			sourceFile("bootstrap-src/04-telemetry.js"),
			sourceFile("bootstrap-src/05-document-env.js"),
			sourceFile("bootstrap-src/06-declarative-actions.js"),
			sourceFile("bootstrap-src/07-declarative-regions.js"),
			sourceFile("bootstrap-src/25-lite-tail.js"),
		},
	},
	{
		name: "bootstrap-runtime.js",
		sources: []source{
			sourceFile("bootstrap-src/00-textlayout.js"),
			sourceFile("bootstrap-src/04-telemetry.js"),
			sourceFile("bootstrap-src/05-document-env.js"),
			sourceFile("bootstrap-src/06-declarative-actions.js"),
			sourceFile("bootstrap-src/07-declarative-regions.js"),
			sourceExtract(runtimeSceneCoreFile, "runtime-utils", runtimeUtilsStart, runtimeUtilsEnd),
			sourceFile(runtimePrimitivesFile),
			sourceFile("bootstrap-src/26-runtime-tail.js"),
		},
	},
	{
		name: "bootstrap-feature-islands.js",
		sources: []source{
			sourceFile("bootstrap-src/26a-feature-islands-prefix.js"),
			sourceExtract(tailFile, "islands-event-delegation", sectionEventDelegation, sectionEngineMounting),
			sourceExtract(tailFile, "islands-dispose",
				"  window.__gosx_dispose_island = function(islandID) {\n",
				"\n  window.__gosx_dispose_engine = function(engineID) {\n"),
			sourceExtract(tailFile, "islands-hydration", sectionHydration, sectionRuntimeReady),
			sourceFile("bootstrap-src/26a-feature-islands-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-engines.js",
		sources: []source{
			sourceFile("bootstrap-src/26b-feature-engines-prefix.js"),
			// 26b1 installs window.__gosx_paint_canvas_bundle — the standalone 2D
			// painter the canvas2d surface-kind render loop (in 26b-prefix's
			// _startCanvasSurfaceRAF) calls each frame. Self-contained IIFE; load
			// order is immaterial since the loop resolves the global at rAF time.
			sourceFile("bootstrap-src/26b1-canvas2d-painter.js"),
			// 26b2 installs window.__gosx_canvas_board_labels_sync — the DOM label
			// overlay that positions real HTML <span> elements over the WebGPU/canvas
			// board so text stays in the DOM (subpixel rendering, future editability).
			// Self-contained IIFE; the slice-4 RAF loop calls sync each frame.
			sourceFile("bootstrap-src/26b2-canvas-board-labels.js"),
			// 28 installs window.__gosx_video_sync_js_create, the pure-JS drift
			// engine the video factory uses when the WASM brain is absent. The
			// engines feature carries the video factory, so it must carry the
			// fallback engine too.
			sourceFile("bootstrap-src/28-video-sync-fallback.js"),
			sourceExtract(tailFile, "runtime-capability-probe", sectionCapabilityProbe,
				"  async function hydrateIsland(entry) {\n"),
			sourceExtract(tailFile, "engines-mounting", sectionEngineMounting, sectionHubConnections),
			sourceExtract(tailFile, "engines-dispose",
				"  window.__gosx_dispose_engine = function(engineID) {\n",
				"\n  window.__gosx_disconnect_hub = function(hubID) {\n"),
			sourceFile("bootstrap-src/26b-feature-engines-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-hubs.js",
		sources: []source{
			sourceFile("bootstrap-src/26c-feature-hubs-prefix.js"),
			sourceExtract(tailFile, "hubs-connections", sectionHubConnections, sectionIslandDisposal),
			sourceExtract(tailFile, "hubs-disconnect",
				"  window.__gosx_disconnect_hub = function(hubID) {\n",
				"\n  async function disposePage() {\n"),
			sourceFile("bootstrap-src/26c-feature-hubs-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-scene3d.js",
		sources: []source{
			sourceFile("bootstrap-src/26d-feature-scene3d-prefix.js"),
			sourceFile(runtimePrimitivesFile),
			sourceFile(runtimeSceneCoreFile),
			sourceFile("bootstrap-src/11-scene-math.js"),
			sourceFile("bootstrap-src/11a-scene-decompress.js"),
			sourceFile("bootstrap-src/12-scene-geometry.js"),
			sourceFile("bootstrap-src/13-scene-material.js"),
			sourceFile("bootstrap-src/14-scene-lighting.js"),
			sourceFile("bootstrap-src/15-scene-ir-schema.js"),
			sourceFile("bootstrap-src/15-scene-ir-schema-strict.js"),
			sourceFile("bootstrap-src/15-scene-draw-plan.js"),
			sourceFile("bootstrap-src/15b-scene-planner.js"),
			sourceFile("bootstrap-src/15c-scene-backend-registry.js"),
			sourceFile("bootstrap-src/15a-scene-postfx-shared.js"),
			sourceFile("bootstrap-src/16b-scene-hdr.js"),
			sourceFile("bootstrap-src/16b-scene-compute.js"),
			sourceFile("bootstrap-src/16-scene-webgl.js"),
			// 16a-scene-webgpu.js is NOT here — it moved to
			// bootstrap-feature-scene3d-webgpu.js so WebGL-only pages (Safari,
			// Firefox on most platforms, ForceWebGL) don't parse WebGPU code
			// they'll never run. 16b-scene-compute.js stays in this chunk
			// because WebGL uses its CPU particle-system path. 16z holds the
			// tiny stub + adapter probe so the WebGL mount path stays sync.
			sourceFile("bootstrap-src/16z-scene-webgpu-probe.js"),
			sourceFile("bootstrap-src/17-scene-input.js"),
			sourceFile("bootstrap-src/18-scene-canvas.js"),
			// 19-scene-gltf.js is NOT here — it moved to
			// bootstrap-feature-scene3d-gltf.js so pages that don't load .glb/
			// .gltf model assets (galaxies, particle systems, CSS-driven 3D
			// scenes — the majority of Scene3D consumers) don't pay the ~30KB
			// parse cost. 20-scene-mount.js lazy-fetches the chunk on first
			// model request via ensureGLTFFeatureLoaded().
			//
			// 19a-scene-animation.js is NOT here either — it moved to
			// bootstrap-feature-scene3d-animation.js. Pages that don't use
			// keyframe animations or skeletal clips skip ~16KB of bone math
			// and quaternion slerp. Consumers that DO need the mixer can
			// lazy-load it via window.__gosx_scene3d_animation_api.
			sourceFile("bootstrap-src/19b-scene-control-forms.js"),
			sourceFile("bootstrap-src/20-scene-mount.js"),
			sourceFile("bootstrap-src/26d-feature-scene3d-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-scene3d-webgpu.js",
		sources: []source{
			sourceFile("bootstrap-src/26e-feature-scene3d-webgpu-prefix.js"),
			sourceFile("bootstrap-src/16a-scene-webgpu.js"),
			sourceFile("bootstrap-src/16b-scene-compute.js"),
			sourceFile("bootstrap-src/26e-feature-scene3d-webgpu-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-scene3d-gltf.js",
		sources: []source{
			sourceFile("bootstrap-src/26f-feature-scene3d-gltf-prefix.js"),
			sourceFile("bootstrap-src/19-scene-gltf.js"),
			sourceFile("bootstrap-src/26f-feature-scene3d-gltf-suffix.js"),
		},
	},
	{
		name: "bootstrap-feature-scene3d-animation.js",
		sources: []source{
			sourceFile("bootstrap-src/26g-feature-scene3d-animation-prefix.js"),
			sourceFile("bootstrap-src/19a-scene-animation.js"),
			sourceFile("bootstrap-src/26g-feature-scene3d-animation-suffix.js"),
		},
	},
}

const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// normalizeNewlines converts \r\n and bare \r to \n, matching the mjs
// implementation's `String(source).replace(/\r\n?/g, "\n")`.
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func extractSource(body string, src source) (string, error) {
	startIndex := strings.Index(body, src.start)
	if startIndex < 0 {
		return "", fmt.Errorf("missing start marker for %s", src.label)
	}
	searchFrom := startIndex + len(src.start)
	endOffset := strings.Index(body[searchFrom:], src.end)
	if endOffset < 0 {
		return "", fmt.Errorf("missing end marker for %s", src.label)
	}
	return body[startIndex : searchFrom+endOffset], nil
}

type compacted struct {
	code    string
	lineMap []int
}

func compactSource(body string) compacted {
	lines := strings.Split(normalizeNewlines(body), "\n")
	var out []string
	var lineMap []int
	lastBlank := false

	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		normalized := strings.TrimRight(line, " \t")
		if trimmed == "" {
			if lastBlank {
				continue
			}
			lastBlank = true
			out = append(out, "")
			lineMap = append(lineMap, index)
			continue
		}

		lastBlank = false
		out = append(out, normalized)
		lineMap = append(lineMap, index)
	}

	for len(out) > 0 && out[0] == "" {
		out = out[1:]
		lineMap = lineMap[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
		lineMap = lineMap[:len(lineMap)-1]
	}
	code := ""
	if len(out) > 0 {
		code = strings.Join(out, "\n") + "\n"
	}
	return compacted{code: code, lineMap: lineMap}
}

func base64VLQEncode(value int) string {
	current := value << 1
	if value < 0 {
		current = ((-value) << 1) | 1
	}
	var encoded strings.Builder
	for {
		digit := current & 31
		current >>= 5
		if current > 0 {
			digit |= 32
		}
		encoded.WriteByte(base64Chars[digit])
		if current <= 0 {
			break
		}
	}
	return encoded.String()
}

type mappingLine struct {
	source       int
	originalLine int
	column       int
}

func encodeMappings(lines []*mappingLine) string {
	segments := make([]string, 0, len(lines))
	previousSource := 0
	previousOriginalLine := 0
	previousOriginalColumn := 0

	for _, line := range lines {
		if line == nil {
			segments = append(segments, "")
			continue
		}
		segments = append(segments,
			base64VLQEncode(0)+
				base64VLQEncode(line.source-previousSource)+
				base64VLQEncode(line.originalLine-previousOriginalLine)+
				base64VLQEncode(line.column-previousOriginalColumn))
		previousSource = line.source
		previousOriginalLine = line.originalLine
		previousOriginalColumn = line.column
	}

	return strings.Join(segments, ";")
}

type sidecar struct {
	path  string
	bytes []byte // nil when compression does not beat the raw payload
}

func gzipCompress(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(raw); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func brotliCompress(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := brotli.NewWriterOptions(&buf, brotli.WriterOptions{Quality: brotli.BestCompression})
	if _, err := w.Write(raw); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compressedSidecars(filePath, code string) ([]sidecar, error) {
	raw := []byte(code)
	if len(raw) == 0 {
		return nil, nil
	}
	gz, err := gzipCompress(raw)
	if err != nil {
		return nil, fmt.Errorf("gzip %s: %w", filePath, err)
	}
	br, err := brotliCompress(raw)
	if err != nil {
		return nil, fmt.Errorf("brotli %s: %w", filePath, err)
	}
	sidecars := []sidecar{
		{path: filePath + ".gz", bytes: gz},
		{path: filePath + ".br", bytes: br},
	}
	for i := range sidecars {
		if len(sidecars[i].bytes) >= len(raw) {
			sidecars[i].bytes = nil
		}
	}
	return sidecars, nil
}

func writeCompressedSidecars(filePath, code string) error {
	sidecars, err := compressedSidecars(filePath, code)
	if err != nil {
		return err
	}
	for _, sc := range sidecars {
		if sc.bytes != nil {
			if err := os.WriteFile(sc.path, sc.bytes, 0o644); err != nil {
				return err
			}
			continue
		}
		if _, statErr := os.Stat(sc.path); statErr == nil {
			if err := os.Remove(sc.path); err != nil {
				return err
			}
		}
	}
	return nil
}

func sidecarsMatch(filePath, code string) (bool, error) {
	sidecars, err := compressedSidecars(filePath, code)
	if err != nil {
		return false, err
	}
	for _, sc := range sidecars {
		current, readErr := os.ReadFile(sc.path)
		exists := readErr == nil
		if sc.bytes == nil {
			if exists {
				return false, nil
			}
			continue
		}
		if !exists || !bytes.Equal(current, sc.bytes) {
			return false, nil
		}
	}
	return true, nil
}

type builtBundle struct {
	code string
	m    string
}

// jsQuote encodes a string exactly like JavaScript's JSON.stringify: it
// escapes `"`, `\`, and control characters (with the \b \t \n \f \r
// shorthands), and leaves everything else — including U+2028/U+2029 and
// non-ASCII — as raw UTF-8. encoding/json cannot be used here because it
// unconditionally escapes U+2028/U+2029 and encodes \b and \f as \u escapes,
// which would break byte-for-byte parity with the retired Node pipeline.
func jsQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func jsQuoteArray(values []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsQuote(v))
	}
	b.WriteByte(']')
	return b.String()
}

// compactedMapJSON serializes the pre-minification source map with the same
// key order and formatting the mjs pipeline used (JSON.stringify of
// {version, file, sources, sourcesContent, names, mappings}).
func compactedMapJSON(file string, sources, sourcesContent []string, mappings string) string {
	var b strings.Builder
	b.WriteString(`{"version":3,"file":`)
	b.WriteString(jsQuote(file))
	b.WriteString(`,"sources":`)
	b.WriteString(jsQuoteArray(sources))
	b.WriteString(`,"sourcesContent":`)
	b.WriteString(jsQuoteArray(sourcesContent))
	b.WriteString(`,"names":[],"mappings":`)
	b.WriteString(jsQuote(mappings))
	b.WriteByte('}')
	return b.String()
}

func buildCompactedBundle(dir string, entry output) (builtBundle, error) {
	type section struct {
		label     string
		raw       string
		compacted compacted
	}
	sections := make([]section, 0, len(entry.sources))
	for _, src := range entry.sources {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(src.rel)))
		if err != nil {
			return builtBundle{}, err
		}
		body := string(data)
		resolved := body
		if src.kind == "extract" {
			resolved, err = extractSource(body, src)
			if err != nil {
				return builtBundle{}, err
			}
		}
		sections = append(sections, section{
			label:     src.label,
			raw:       normalizeNewlines(resolved),
			compacted: compactSource(resolved),
		})
	}

	var code strings.Builder
	var lines []*mappingLine
	for index, sec := range sections {
		if index > 0 && code.Len() != 0 {
			code.WriteByte('\n')
			lines = append(lines, nil)
		}
		code.WriteString(sec.compacted.code)
		for _, originalLine := range sec.compacted.lineMap {
			lines = append(lines, &mappingLine{source: index, originalLine: originalLine})
		}
	}

	joined := code.String()
	if !strings.HasSuffix(joined, "\n") {
		joined += "\n"
	}

	sources := make([]string, 0, len(sections))
	sourcesContent := make([]string, 0, len(sections))
	for _, sec := range sections {
		sources = append(sources, sec.label)
		sourcesContent = append(sourcesContent, sec.raw)
	}
	m := compactedMapJSON(entry.name, sources, sourcesContent, encodeMappings(lines))
	return builtBundle{code: joined, m: m}, nil
}

// minifyESBuild minifies with esbuild's Go library, composing the compacted
// source map into the final map exactly like the retired mjs pipeline: the
// compacted map rides in as an inline sourceMappingURL data URL, and esbuild
// emits the composed external map.
func minifyESBuild(entry output, built builtBundle) (builtBundle, error) {
	dataURL := "data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte(built.m))
	input := built.code + "\n//# sourceMappingURL=" + dataURL
	result := esbuild.Transform(input, esbuild.TransformOptions{
		Charset:           esbuild.CharsetUTF8,
		LegalComments:     esbuild.LegalCommentsNone,
		Loader:            esbuild.LoaderJS,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Sourcefile:        entry.name,
		Sourcemap:         esbuild.SourceMapExternal,
		Target:            esbuild.ES2020,
	})
	if len(result.Errors) > 0 {
		return builtBundle{}, fmt.Errorf("esbuild %s: %s", entry.name, result.Errors[0].Text)
	}
	m, err := normalizeESBuildMap(result.Map, entry.name)
	if err != nil {
		return builtBundle{}, fmt.Errorf("normalize map for %s: %w", entry.name, err)
	}
	return builtBundle{code: string(result.Code), m: m}, nil
}

// esbuildMap mirrors the JSON shape of esbuild's external source map output.
type esbuildMap struct {
	Version        int      `json:"version"`
	Sources        []string `json:"sources"`
	SourcesContent []string `json:"sourcesContent"`
	Mappings       string   `json:"mappings"`
	Names          []string `json:"names"`
}

// normalizeESBuildMap reproduces the mjs normalizeGeneratedMap: parse
// esbuild's (pretty-printed) map, set .file, and re-serialize compactly with
// JSON.stringify semantics and esbuild's key order (version, sources,
// sourcesContent, mappings, names) plus file appended last.
func normalizeESBuildMap(raw []byte, fileName string) (string, error) {
	var parsed esbuildMap
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`{"version":`)
	b.WriteString(strconv.Itoa(parsed.Version))
	b.WriteString(`,"sources":`)
	b.WriteString(jsQuoteArray(parsed.Sources))
	b.WriteString(`,"sourcesContent":`)
	b.WriteString(jsQuoteArray(parsed.SourcesContent))
	b.WriteString(`,"mappings":`)
	b.WriteString(jsQuote(parsed.Mappings))
	b.WriteString(`,"names":`)
	b.WriteString(jsQuoteArray(parsed.Names))
	b.WriteString(`,"file":`)
	b.WriteString(jsQuote(fileName))
	b.WriteByte('}')
	return b.String(), nil
}

// minifyTdewolff minifies with the pure-Go tdewolff/minify JS minifier. It
// cannot compose source maps, so callers keep the compacted (pre-minify) map.
func minifyTdewolff(code string) (string, error) {
	minifier := &js.Minifier{Version: 2020}
	var out bytes.Buffer
	if err := minifier.Minify(minify.New(), &out, strings.NewReader(code), nil); err != nil {
		return "", err
	}
	return out.String(), nil
}

func normalizeGeneratedCode(code, mapName string, debugSourcemaps bool) string {
	next := normalizeNewlines(code)
	if !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	if debugSourcemaps {
		next += "//# sourceMappingURL=" + mapName + "\n"
	}
	return next
}

func buildBundle(dir string, entry output, minifier string, debugSourcemaps bool) (builtBundle, error) {
	built, err := buildCompactedBundle(dir, entry)
	if err != nil {
		return builtBundle{}, err
	}
	switch minifier {
	case "esbuild":
		minified, err := minifyESBuild(entry, built)
		if err != nil {
			return builtBundle{}, err
		}
		return builtBundle{
			code: normalizeGeneratedCode(minified.code, entry.name+".map", debugSourcemaps),
			m:    minified.m,
		}, nil
	case "tdewolff":
		minified, err := minifyTdewolff(built.code)
		if err != nil {
			return builtBundle{}, fmt.Errorf("minify %s: %w", entry.name, err)
		}
		return builtBundle{
			code: normalizeGeneratedCode(minified, entry.name+".map", debugSourcemaps),
			// tdewolff cannot compose source maps; ship the compacted
			// (pre-minify) map, which still carries accurate sources and
			// sourcesContent.
			m: built.m,
		}, nil
	default:
		return builtBundle{}, fmt.Errorf("unknown minifier %q (want esbuild or tdewolff)", minifier)
	}
}

// findClientJS resolves the client/js directory: an explicit -dir wins,
// otherwise walk up from the working directory looking for
// client/js/bootstrap-src (so the tool works from the repo root or any
// subdirectory).
func findClientJS(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(filepath.Join(explicit, "bootstrap-src")); err != nil {
			return "", fmt.Errorf("-dir %q does not contain bootstrap-src: %w", explicit, err)
		}
		return explicit, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for probe := cwd; ; probe = filepath.Dir(probe) {
		candidate := filepath.Join(probe, "client", "js")
		if _, err := os.Stat(filepath.Join(candidate, "bootstrap-src")); err == nil {
			return candidate, nil
		}
		if filepath.Dir(probe) == probe {
			return "", errors.New("could not locate client/js/bootstrap-src; run from the gosx repo or pass -dir")
		}
	}
}

func run() error {
	dirFlag := flag.String("dir", "", "path to client/js (default: auto-detect from working directory)")
	check := flag.Bool("check", false, "verify committed bundles are up to date; exit 1 when stale")
	minifier := flag.String("minifier", "esbuild", "JS minifier backend: esbuild (default, byte-stable) or tdewolff (A/B comparison)")
	flag.Parse()

	dir, err := findClientJS(*dirFlag)
	if err != nil {
		return err
	}
	debugSourcemaps := os.Getenv("GOSX_BUNDLE_DEBUG") == "1"

	if *check {
		var stale []string
		for _, entry := range outputs {
			next, err := buildBundle(dir, entry, *minifier, debugSourcemaps)
			if err != nil {
				return err
			}
			bundlePath := filepath.Join(dir, entry.name)
			currentCode, _ := os.ReadFile(bundlePath)
			currentMap, _ := os.ReadFile(bundlePath + ".map")
			match, err := sidecarsMatch(bundlePath, next.code)
			if err != nil {
				return err
			}
			if string(currentCode) != next.code || string(currentMap) != next.m || !match {
				stale = append(stale, entry.name)
			}
		}
		if len(stale) > 0 {
			return fmt.Errorf("bootstrap runtime assets are out of date (%s). Run `go run ./cmd/buildbootstrap`", strings.Join(stale, ", "))
		}
		return nil
	}

	for _, entry := range outputs {
		built, err := buildBundle(dir, entry, *minifier, debugSourcemaps)
		if err != nil {
			return err
		}
		bundlePath := filepath.Join(dir, entry.name)
		if err := os.WriteFile(bundlePath, []byte(built.code), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(bundlePath+".map", []byte(built.m), 0o644); err != nil {
			return err
		}
		if err := writeCompressedSidecars(bundlePath, built.code); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

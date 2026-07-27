// Package variantsel is the capability vocabulary that asset variants and the
// server's fidelity verdict share.
//
// # Why the package exists
//
// The build pipeline emits several variants of one asset and tags each with the
// capabilities a consumer must have. The server already computes a per-scene,
// per-backend fidelity verdict before the browser asks for anything, in
// scene/capability. Those two facts only compose if both sides spell a
// capability the same way. This package owns the spelling.
//
// Every backend token and every feature token derives from the constants in
// scene/capability, not from a copied string. A renamed Backend or Feature
// therefore breaks the build here instead of silently producing a token nothing
// matches.
//
// # The safety rule
//
// FromBackendCaps intersects the format support of every capable backend. The
// verdict says which backends could render the page faithfully; it does not say
// which one the browser will choose. A variant that only WebGPU can upload must
// not be selected for a page that may fall back to WebGL2, because the fallback
// happens in the browser, after the response left the server.
//
// Canvas2D uploads no GPU texture at all. A page whose only capable backend is
// Canvas2D therefore gets no texture format tokens, and a selector falls back
// to the source image. That is the honest answer.
//
// # Block formats need device evidence, not a verdict
//
// The verdict proves backend fidelity. It does not prove device format support.
// Every block-compressed format sits behind an optional WebGPU feature and an
// optional WebGL2 extension, so no static backend table may promise one.
// backendFormats therefore holds no block token, and FromBackendCaps can never
// select a block variant. Block tokens enter a set only through
// FromDeviceEvidence, which reads what the live device reported. The server emits
// the whole variant set; the runtime picks one after it commits to a backend and
// reads adapter.features.
package variantsel

import (
	"sort"
	"strings"

	"m31labs.dev/gosx/scene/capability"
)

// Token is one capability name. Tokens are lowercase and namespaced with a
// colon, so a reader can tell a backend from a texture format at a glance.
type Token string

// Container tokens.
const (
	// ContainerKTX2 means the consumer parses a KTX2 container.
	ContainerKTX2 Token = "container:ktx2"
	// ContainerZlib means the consumer inflates KTX2 supercompression
	// scheme 3. A browser has DecompressionStream or pako; a native
	// consumer has compress/zlib.
	ContainerZlib Token = "container:ktx2-zlib"
)

// Texture format tokens. The spelling follows the WebGPU GPUTextureFormat
// enumeration, because that is the narrower and more precisely specified of the
// two backend vocabularies. A WebGL2 internal format maps onto it exactly.
const (
	FormatRGBA8Unorm     Token = "texture-format:rgba8unorm"
	FormatRGBA8UnormSRGB Token = "texture-format:rgba8unorm-srgb"
	FormatRGB8Unorm      Token = "texture-format:rgb8unorm"
	FormatRGB8UnormSRGB  Token = "texture-format:rgb8unorm-srgb"
	FormatRG8Unorm       Token = "texture-format:rg8unorm"
	FormatR8Unorm        Token = "texture-format:r8unorm"
	FormatRGBA16Float    Token = "texture-format:rgba16float"
	FormatRG16Float      Token = "texture-format:rg16float"
)

// Texture view tokens.
const (
	// ViewCube means the consumer binds a cube texture view.
	ViewCube Token = "texture-view:cube"
)

// Delivery budget tokens. These are the one axis the scene verdict cannot
// supply, because they describe the request and not the scene. The server
// derives exactly one of them per request. See BudgetFromHints.
const (
	BudgetHigh     Token = "budget:high"
	BudgetStandard Token = "budget:standard"
	BudgetLow      Token = "budget:low"
)

// Block-compressed format tokens.
//
// The BC family is built. assetpipe/texture/codec_bcn.go and codec_bc7.go fill
// BC1, BC3, BC4, BC5 and BC7 payloads, so a variant may carry one of those
// tokens and name a file that exists.
//
// The ASTC and ETC2 tokens still name targets no GoSX build step produces. They
// exist so a plan can state the gap. A selector that sees one and finds no built
// file must fall through, which the built-state check in the selector
// guarantees.
//
// Every spelling follows the WebGPU GPUTextureFormat enumeration. BC4 and BC5
// have no sRGB form, because they store data and not colour.
const (
	FormatBC1           Token = "texture-format:bc1-rgba-unorm"
	FormatBC1SRGB       Token = "texture-format:bc1-rgba-unorm-srgb"
	FormatBC3           Token = "texture-format:bc3-rgba-unorm"
	FormatBC3SRGB       Token = "texture-format:bc3-rgba-unorm-srgb"
	FormatBC4           Token = "texture-format:bc4-r-unorm"
	FormatBC5           Token = "texture-format:bc5-rg-unorm"
	FormatBC7           Token = "texture-format:bc7-rgba-unorm-srgb"
	FormatBC7Linear     Token = "texture-format:bc7-rgba-unorm"
	FormatASTC4x4       Token = "texture-format:astc-4x4-unorm-srgb"
	FormatASTC4x4Linear Token = "texture-format:astc-4x4-unorm"
	FormatASTC8x8       Token = "texture-format:astc-8x8-unorm-srgb"
	FormatETC2          Token = "texture-format:etc2-rgba8unorm-srgb"
	FormatETC2RGB       Token = "texture-format:etc2-rgb8unorm-srgb"
	FormatEACR11        Token = "texture-format:eac-r11unorm"
	FormatEACRG11       Token = "texture-format:eac-rg11unorm"
)

// Device feature tokens name the optional capability a block format needs.
//
// These are not backend guarantees. A WebGPU adapter reports each one in
// adapter.features, and a WebGL2 context reports the matching extension. The
// server cannot know any of them from a fidelity verdict, so they enter a
// request set only through FromDeviceEvidence.
const (
	FeatureTextureCompressionBC   Token = "device-feature:texture-compression-bc"
	FeatureTextureCompressionETC2 Token = "device-feature:texture-compression-etc2"
	FeatureTextureCompressionASTC Token = "device-feature:texture-compression-astc"
)

// blockFeatureFormats maps one device feature onto the format tokens it unlocks.
//
// The mapping is one-way on purpose. A format token never implies a feature
// token, because a variant states what it needs and evidence states what the
// device has.
var blockFeatureFormats = map[Token][]Token{
	FeatureTextureCompressionBC: {
		FormatBC1, FormatBC1SRGB,
		FormatBC3, FormatBC3SRGB,
		FormatBC4, FormatBC5,
		FormatBC7, FormatBC7Linear,
	},
	FeatureTextureCompressionETC2: {
		FormatETC2, FormatETC2RGB, FormatEACR11, FormatEACRG11,
	},
	FeatureTextureCompressionASTC: {
		FormatASTC4x4, FormatASTC4x4Linear, FormatASTC8x8,
	},
}

// BlockFeatureForFormat returns the device feature one block format needs, and
// false for a format that needs none.
//
// A variant carries both tokens: the format it holds and the feature that format
// needs. A selector then rejects the variant unless device evidence supplied
// both, which is what stops a block file reaching a device that cannot upload it.
func BlockFeatureForFormat(format Token) (Token, bool) {
	normalized := Token(strings.ToLower(strings.TrimSpace(string(format))))
	for feature, formats := range blockFeatureFormats {
		for _, candidate := range formats {
			if candidate == normalized {
				return feature, true
			}
		}
	}
	return "", false
}

// FormatsForFeature lists the format tokens one device feature unlocks.
func FormatsForFeature(feature Token) []Token {
	normalized := Token(strings.ToLower(strings.TrimSpace(string(feature))))
	formats := blockFeatureFormats[normalized]
	out := make([]Token, len(formats))
	copy(out, formats)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// webgpuFeatureNames maps a WebGPU adapter feature name onto its token. The
// names are the GPUFeatureName strings the runtime probe already collects.
var webgpuFeatureNames = map[string]Token{
	"texture-compression-bc":   FeatureTextureCompressionBC,
	"texture-compression-etc2": FeatureTextureCompressionETC2,
	"texture-compression-astc": FeatureTextureCompressionASTC,
}

// webglExtensionFeatures maps a WebGL2 extension name onto the format tokens it
// unlocks.
//
// WebGL2 splits the BC family across three extensions, so a context can have
// S3TC and not RGTC. The mapping is therefore per extension and not per family,
// and a WebGL2 set never carries a whole-family feature token.
var webglExtensionFeatures = map[string][]Token{
	"WEBGL_compressed_texture_s3tc":      {FormatBC1, FormatBC3},
	"WEBGL_compressed_texture_s3tc_srgb": {FormatBC1SRGB, FormatBC3SRGB},
	"EXT_texture_compression_rgtc":       {FormatBC4, FormatBC5},
	"EXT_texture_compression_bptc":       {FormatBC7, FormatBC7Linear},
	"WEBGL_compressed_texture_etc":       {FormatETC2, FormatETC2RGB, FormatEACR11, FormatEACRG11},
	"WEBGL_compressed_texture_astc":      {FormatASTC4x4, FormatASTC4x4Linear, FormatASTC8x8},
}

// FromDeviceEvidence turns a live device's own report into a capability set.
//
// # Why this function exists and FromBackendCaps cannot do the work
//
// The server's fidelity verdict proves which backends could draw the page. It
// proves nothing about texture formats: every block format sits behind an
// optional WebGPU feature and an optional WebGL2 extension, and no static table
// may promise them. The evidence lives on the device, in adapter.features or in
// gl.getExtension. That evidence arrives after the response left the server, so
// the runtime is the only place that can supply it.
//
// names holds whatever the device reported. Unknown names are ignored, which
// keeps a new browser feature from breaking selection.
//
// The returned set carries the container tokens too, because a consumer that
// reached this function parses KTX2 and inflates scheme 3.
func FromDeviceEvidence(backend capability.Backend, names []string) Set {
	set := NewSet(BackendToken(backend), ContainerKTX2, ContainerZlib)
	switch backend {
	case capability.BackendWebGPU:
		for _, name := range names {
			feature, ok := webgpuFeatureNames[strings.TrimSpace(name)]
			if !ok {
				continue
			}
			set.Add(feature)
			for _, format := range blockFeatureFormats[feature] {
				set.Add(format)
			}
		}
	case capability.BackendWebGL:
		for _, name := range names {
			formats, ok := webglExtensionFeatures[strings.TrimSpace(name)]
			if !ok {
				continue
			}
			// A WebGL2 extension unlocks part of a family, so the set gains
			// the whole-family feature token only when every format of that
			// family is present. The variant gate checks both tokens, so a
			// partial family stays unselectable rather than half selected.
			for _, format := range formats {
				set.Add(format)
			}
		}
		for feature, formats := range blockFeatureFormats {
			complete := true
			for _, format := range formats {
				if !set.Has(format) {
					complete = false
					break
				}
			}
			if complete {
				set.Add(feature)
			}
		}
	}
	// Every GPU backend uploads the uncompressed formats, so the device set
	// keeps the backend's own guarantees as well.
	for _, token := range backendFormats[backend] {
		set.Add(token)
	}
	return set
}

// BackendToken renders one capability.Backend as a token.
func BackendToken(b capability.Backend) Token {
	return Token("backend:" + strings.ToLower(strings.TrimSpace(string(b))))
}

// FeatureToken renders one capability.Feature as a token.
func FeatureToken(f capability.Feature) Token {
	return Token("feature:" + strings.ToLower(strings.TrimSpace(string(f))))
}

// Set is an unordered capability set.
type Set map[Token]struct{}

// NewSet builds a set from tokens.
func NewSet(tokens ...Token) Set {
	set := make(Set, len(tokens))
	for _, token := range tokens {
		set.Add(token)
	}
	return set
}

// Add inserts one token. It normalizes case and trims space.
func (s Set) Add(token Token) {
	normalized := Token(strings.ToLower(strings.TrimSpace(string(token))))
	if normalized == "" {
		return
	}
	s[normalized] = struct{}{}
}

// Has reports whether the set contains a token.
func (s Set) Has(token Token) bool {
	_, ok := s[Token(strings.ToLower(strings.TrimSpace(string(token))))]
	return ok
}

// HasAll reports whether the set contains every named capability. An empty
// requirement list is satisfied by any set.
func (s Set) HasAll(required []string) bool {
	for _, name := range required {
		if !s.Has(Token(name)) {
			return false
		}
	}
	return true
}

// Sorted returns the tokens in a stable order, for a manifest or a log line.
func (s Set) Sorted() []string {
	out := make([]string, 0, len(s))
	for token := range s {
		out = append(out, string(token))
	}
	sort.Strings(out)
	return out
}

// backendFormats lists the texture capabilities each rendering backend has.
//
// WebGPU: rgba8unorm, rgba8unorm-srgb, rg8unorm, r8unorm, rgba16float, and
// rg16float are all core GPUTextureFormat values, and all six are filterable.
// WebGPU has no three-channel 8-bit format at all, so rgb8unorm is absent.
//
// WebGL2: RGBA8, SRGB8_ALPHA8, RGB8, SRGB8, RG8, and R8 are core sized internal
// formats. RGBA16F and RG16F are core, and half-float textures are filterable in
// WebGL2 without an extension. TEXTURE_CUBE_MAP is core.
//
// Canvas2D: no GPU texture upload exists, so the row is empty.
var backendFormats = map[capability.Backend][]Token{
	capability.BackendWebGPU: {
		ContainerKTX2, ContainerZlib,
		FormatRGBA8Unorm, FormatRGBA8UnormSRGB,
		FormatRG8Unorm, FormatR8Unorm,
		FormatRGBA16Float, FormatRG16Float,
		ViewCube,
	},
	capability.BackendWebGL: {
		ContainerKTX2, ContainerZlib,
		FormatRGBA8Unorm, FormatRGBA8UnormSRGB,
		FormatRGB8Unorm, FormatRGB8UnormSRGB,
		FormatRG8Unorm, FormatR8Unorm,
		FormatRGBA16Float, FormatRG16Float,
		ViewCube,
	},
	capability.BackendCanvas2D: {},
}

// BackendTokens returns the texture capabilities of one backend.
func BackendTokens(b capability.Backend) Set {
	set := NewSet(BackendToken(b))
	for _, token := range backendFormats[b] {
		set.Add(token)
	}
	return set
}

// FromBackendCaps turns a fidelity verdict into a capability set.
//
// The result carries:
//
//   - one backend token per capable backend,
//   - one feature token per degraded feature, so a variant can be gated on a
//     gap the verdict already found,
//   - the intersection of the texture capabilities of the capable GPU
//     backends.
//
// The intersection is the load-bearing part. The verdict lists every backend
// that could draw the page. The browser picks one of them at mount time, and the
// server cannot know which. Handing a page a WebGPU-only variant while WebGL2
// stays a live fallback would produce a texture the fallback cannot upload.
//
// Canvas2D never contributes a texture capability, and its presence in Capable
// never removes one. A Canvas2D-only verdict yields no texture capability at
// all, which is correct: that page uploads no GPU texture.
func FromBackendCaps(caps capability.BackendCaps) Set {
	set := Set{}
	var gpu []capability.Backend
	for _, backend := range caps.Capable {
		set.Add(BackendToken(backend))
		if backend == capability.BackendCanvas2D {
			continue
		}
		gpu = append(gpu, backend)
	}
	for backend, features := range caps.Degraded {
		_ = backend
		for _, feature := range features {
			set.Add(FeatureToken(feature))
		}
	}
	if len(gpu) == 0 {
		return set
	}
	shared := BackendTokens(gpu[0])
	for _, backend := range gpu[1:] {
		other := BackendTokens(backend)
		for token := range shared {
			if strings.HasPrefix(string(token), "backend:") {
				continue
			}
			if !other.Has(token) {
				delete(shared, token)
			}
		}
	}
	for token := range shared {
		if strings.HasPrefix(string(token), "backend:") {
			continue
		}
		set.Add(token)
	}
	return set
}

// BudgetFromHints picks the delivery budget token from request signals.
//
// saveData is the Save-Data request header, which is a user instruction and
// therefore wins outright. deviceMemoryGiB is the Device-Memory client hint in
// gibibytes, which Chromium rounds to a power of two between 0.25 and 8. A
// value of zero means the hint was absent.
//
// The thresholds are deliberate: 4 GiB and above is a desktop or a recent
// flagship phone, 1 GiB and below is a low memory device that a 2048 pixel
// texture set would evict.
func BudgetFromHints(saveData bool, deviceMemoryGiB float64) Token {
	switch {
	case saveData:
		return BudgetLow
	case deviceMemoryGiB > 0 && deviceMemoryGiB <= 1:
		return BudgetLow
	case deviceMemoryGiB >= 4:
		return BudgetHigh
	default:
		return BudgetStandard
	}
}

// TierTokens returns the capability tokens a delivery tier requires.
//
// The ladder is inclusive downward. A high budget can take a standard or a low
// variant, and a low budget can take only a low one. The selector therefore
// gates the high tier on budget:high, the standard tier on nothing, and the low
// tier on nothing, and relies on preference order for the rest.
func TierTokens(tier string) []Token {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "high":
		return []Token{BudgetHigh}
	default:
		return nil
	}
}

// Strings renders tokens as the plain strings a Variant stores.
func Strings(tokens ...Token) []string {
	out := make([]string, 0, len(tokens))
	seen := map[Token]struct{}{}
	for _, token := range tokens {
		normalized := Token(strings.ToLower(strings.TrimSpace(string(token))))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, string(normalized))
	}
	sort.Strings(out)
	return out
}

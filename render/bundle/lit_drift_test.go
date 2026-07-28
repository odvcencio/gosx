package bundle

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx/engine"
)

// Drift guards for the lit shader.
//
// GoSX shades the same authored material twice. engine.RenderMaterial carries
// one set of fields (Roughness, Metalness, Clearcoat, Sheen, Transmission,
// Iridescence, Anisotropy, RoughnessMap, MetalnessMap, EmissiveMap). The native
// Go renderer reads them in render/bundle/lit.go litWGSL. The browser WebGPU
// renderer reads them in client/js/bootstrap-src/16a-scene-webgpu.js
// WGSL_PBR_FRAGMENT. Nothing regenerates one copy from the other.
//
// WHY A STRING COMPARISON IS THE WRONG GUARD HERE
//
// The two copies are not, and must not be, byte identical. They differ in bind
// group layout, in uniform packing (Go packs flags into vec4 lanes, the browser
// uses u32 fields), in entry point names (fs_main against fragmentMain), in the
// light record size (Go packs five vec4, the browser packs seven), and in render
// targets (Go writes a second pick target). A byte comparison would fail on the
// first day and would teach the reader to delete the test.
//
// WHAT THESE GUARDS PIN INSTEAD
//
// The shared material response: the numeric terms that turn one authored
// material into pixels. A viewer who loads the same scene under server side
// rendering and under the browser must see the same surface. Each pinned row
// names the term, the value, and the visible effect of a change.

// litJSFragmentName is the browser copy of the lit fragment shader.
const litJSFragmentName = "WGSL_PBR_FRAGMENT"

// goLitWhere and jsLitWhere label the two copies in failure output.
const (
	goLitWhere = "render/bundle/lit.go litWGSL"
	jsLitWhere = "client/js/bootstrap-src/16a-scene-webgpu.js WGSL_PBR_FRAGMENT"
)

// normalizeWGSLSyntax removes spelling differences that carry no math, so one
// pinned expression can be checked against both copies.
//
// It rewrites the long WGSL type spellings to the short ones (vec3<f32> becomes
// vec3f), collapses runs of blanks, and trims each line. It changes no operator,
// no constant, and no identifier, so a math edit on either side still shows up.
func normalizeWGSLSyntax(src string) string {
	replacer := strings.NewReplacer(
		"vec2<f32>", "vec2f",
		"vec3<f32>", "vec3f",
		"vec4<f32>", "vec4f",
		"mat3x3<f32>", "mat3x3f",
		"mat4x4<f32>", "mat4x4f",
	)
	lines := strings.Split(replacer.Replace(src), "\n")
	blanks := regexp.MustCompile(`[ \t]+`)
	for i, line := range lines {
		lines[i] = strings.TrimSpace(blanks.ReplaceAllString(line, " "))
	}
	return strings.Join(lines, "\n")
}

// sharedTerm is one shading term that both copies must compute the same way.
//
// goPat and jsPat are regular expressions over the normalized source of their
// own copy. want is the value every capture group must produce, with several
// groups joined by "|". An empty want makes the row a presence check.
type sharedTerm struct {
	id     string
	effect string // what a viewer sees when the two copies disagree
	goPat  string
	jsPat  string
	want   string
}

// litSharedTerms is the pinned agreement between litWGSL and WGSL_PBR_FRAGMENT.
//
// Add a row when you find a term both copies already share. Do not add a row
// for a term they compute differently; record that in litDivergentTerms so the
// difference stays visible instead of hiding inside a passing test.
var litSharedTerms = []sharedTerm{
	{
		id:     "dielectric-f0",
		effect: "Every non-metal highlight changes brightness.",
		goPat:  `mix\(vec3f\(([0-9.]+)\), baseColor, metalness\)`,
		jsPat:  `mix\(vec3f\(([0-9.]+)\), albedo, metalness\)`,
		want:   "0.04",
	},
	{
		id:     "roughness-floor",
		effect: "A polished material turns into a pinpoint mirror on one backend only.",
		goPat:  `clamp\(roughness \* \(1\.0 - abs\([A-Za-z.]+\) \* [0-9.]+\), ([0-9.]+), 1\.0\)`,
		jsPat:  `clamp\(roughness \* \(1\.0 - abs\([A-Za-z.]+\) \* [0-9.]+\), ([0-9.]+), 1\.0\)`,
		want:   "0.04",
	},
	{
		id:     "anisotropy-roughness-gain",
		effect: "An anisotropic material reads sharper on one backend than the other.",
		goPat:  `roughness \* \(1\.0 - abs\([A-Za-z.]+\) \* ([0-9.]+)\)`,
		jsPat:  `roughness \* \(1\.0 - abs\([A-Za-z.]+\) \* ([0-9.]+)\)`,
		want:   "0.28",
	},
	{
		id:     "ggx-alpha-is-roughness-squared",
		effect: "The whole specular lobe widens or narrows on one backend.",
		goPat:  `let a = roughness \* roughness;`,
		jsPat:  `let a = roughness \* roughness;`,
	},
	{
		id:     "ggx-alpha-squared",
		effect: "The specular lobe shape changes on one backend.",
		goPat:  `let a2 = a \* a;`,
		jsPat:  `let a2 = a \* a;`,
	},
	{
		id:     "ggx-denominator-shape",
		effect: "The specular peak moves off the mirror direction on one backend.",
		goPat:  `NdotH \* NdotH \* \(a2 - 1\.0\) \+ 1\.0`,
		jsPat:  `NdotH2 \* \(a2 - 1\.0\) \+ 1\.0`,
	},
	{
		id:     "fresnel-schlick-exponent",
		effect: "The rim brightening at grazing angles changes strength.",
		goPat:  `pow\(clamp\(1\.0 - [A-Za-z]+, 0\.0, 1\.0\), ([0-9.]+)\)`,
		jsPat:  `pow\(clamp\(1\.0 - [A-Za-z]+, 0\.0, 1\.0\), ([0-9.]+)\)`,
		want:   "5.0",
	},
	{
		id:     "energy-conserving-diffuse-weight",
		effect: "A metal picks up a diffuse term, or a dielectric loses one.",
		goPat:  `\(vec3f\(1\.0\) - kS\) \* \(1\.0 - metalness\)`,
		jsPat:  `\(vec3f\(1\.0\) - F\) \* \(1\.0 - metalness\)`,
	},
	{
		id:     "lambert-diffuse-normalization",
		effect: "Diffuse brightness shifts by a factor of pi.",
		goPat:  `kD \* baseColor / 3\.141592653589793`,
		jsPat:  `kD \* albedo / PI`,
	},
	{
		id:     "clearcoat-power-range",
		effect: "The clear coat sheen tightens or spreads on one backend.",
		goPat:  `mix\((12\.0), (96\.0), 1\.0 - roughness\)`,
		jsPat:  `mix\((12\.0), (96\.0), 1\.0 - roughness\)`,
		want:   "12.0|96.0",
	},
	{
		// The Go copy used to inline this as baseColor * pow(1.0 - NdotV, 3.0).
		// The sheen-composition fix gave it the same velvet local the browser
		// uses, so the pinned pattern now names that local on both sides. The
		// pattern got narrower, not looser: it still captures the exponent, and
		// it now also pins that sheen scales the falloff rather than the sum.
		id:     "sheen-falloff-exponent",
		effect: "The velvet edge on a fabric widens or narrows.",
		goPat:  `let velvet = pow\(1\.0 - NdotV, ([0-9.]+)\) \* sheen;`,
		jsPat:  `let velvet = pow\(1\.0 - NoV, ([0-9.]+)\) \* sheen;`,
		want:   "3.0",
	},
	{
		id:     "iridescence-tint-base-and-gain",
		effect: "An iridescent surface shifts hue range between backends.",
		goPat:  `color \* \(vec3f\(([0-9.]+)\) \+ iri \* ([0-9.]+)\)`,
		jsPat:  `color \* \(vec3f\(([0-9.]+)\) \+ iri \* ([0-9.]+)\)`,
		want:   "0.65|0.7",
	},
	{
		id:     "iridescence-facing-exponent",
		effect: "The iridescent band moves toward or away from the silhouette.",
		goPat:  `iridescence \* pow\(1\.0 - N(?:dot|o)V, ([0-9.]+)\)`,
		jsPat:  `iridescence \* pow\(1\.0 - N(?:dot|o)V, ([0-9.]+)\)`,
		want:   "2.0",
	},
	{
		id:     "transmission-metal-gate",
		effect: "A metal becomes translucent on one backend.",
		goPat:  `clamp\(material\.physicalParams\.z, 0\.0, 1\.0\) \* \(1\.0 - metalness\)`,
		jsPat:  `clamp\(material\.transmission, 0\.0, 1\.0\) \* \(1\.0 - metalness\)`,
	},
	{
		id:     "transmission-mix-weights",
		effect: "Glass reads more or less opaque between backends.",
		goPat:  `mix\(color, ambient \+ baseColor \* ([0-9.]+), transmission \* ([0-9.]+)\)`,
		jsPat:  `mix\(color, ambient \+ albedo \* ([0-9.]+), transmission \* ([0-9.]+)\)`,
		want:   "0.1|0.55",
	},
	{
		id:     "hemisphere-normal-blend",
		effect: "Sky and ground ambient swap or skew across the normal range.",
		goPat:  `N\.y \* 0\.5 \+ 0\.5`,
		jsPat:  `N\.y \* 0\.5 \+ 0\.5`,
	},
	{
		id:     "shadow-uses-hardware-compare",
		effect: "Shadow edges change from a hardware compare to a manual depth test.",
		goPat:  `textureSampleCompareLevel\(`,
		jsPat:  `textureSampleCompareLevel\(`,
	},
	{
		id:     "direct-light-composition",
		effect: "Direct light stops scaling by the cosine term or by the shadow factor.",
		goPat:  `\(diffuse \+ specular\) \* radiance \* NdotL \* shadow`,
		jsPat:  `\+ specular\) \* radiance \* NdotL \* shadowAtten`,
	},

	// The rows below arrived from litDivergentTerms. Each one records a
	// difference the two copies had until the native copy adopted the browser
	// form. The ledger row went away in the same change, so the difference
	// cannot come back unnoticed on either side.
	{
		// glTF 2.0 packs occlusion in red, roughness in green, and metalness in
		// blue. assetpipe/texture PackMetallicRoughness writes that layout, and
		// both browser renderers read it. The native copy read red, so a packed
		// texture drove roughness from the occlusion channel.
		id:     "roughness-map-channel",
		effect: "A packed glTF texture drives roughness from the occlusion channel on one backend.",
		goPat:  `let roughSample = textureSample\(roughnessMapTex, baseColorSampler, in\.uv\)\.([rgba]);`,
		jsPat:  `roughness = roughness \* textureSample\(roughnessTex, roughnessSamp, in\.uv\)\.([rgba]);`,
		want:   "g",
	},
	{
		id:     "metalness-map-channel",
		effect: "A packed glTF texture drives metalness from the occlusion channel on one backend.",
		goPat:  `let metalSample = textureSample\(metalnessMapTex, baseColorSampler, in\.uv\)\.([rgba]);`,
		jsPat:  `metalness = metalness \* textureSample\(metalnessTex, metalnessSamp, in\.uv\)\.([rgba]);`,
		want:   "b",
	},
	{
		// The native copy used a gain of 1.0, which is 1/0.28, about 3.6 times
		// the browser gain.
		id:     "clearcoat-gain",
		effect: "A coated highlight reads about 3.6 times too bright on the backend that drops the gain.",
		goPat:  `pow\(NdotV, clearcoatPower\) \* clearcoat \* ([0-9.]+)\)`,
		jsPat:  `color = color \+ vec3f\(cc \* ([0-9.]+)\);`,
		want:   "0.28",
	},
	{
		// Both copies now add the velvet term. The native copy used to blend
		// toward it, which dimmed the lit colour as sheen rose.
		id:     "sheen-composition",
		effect: "A fabric loses brightness and saturation on the backend that blends instead of adding.",
		goPat:  `color = color \+ baseColor \* velvet \* ([0-9.]+);`,
		jsPat:  `color = color \+ albedo \* velvet \* ([0-9.]+);`,
		want:   "0.55",
	},
	{
		// Both constants are arbitrary. Only agreement decides the colour a
		// viewer sees at one angle, so both copies carry the same three numbers.
		id:     "iridescence-phase-and-frequency",
		effect: "An iridescent surface shows a different hue at the same viewing angle.",
		goPat:  `cos\(vec3f\(0\.0, ([0-9.]+), ([0-9.]+)\) \+ NdotV \* ([0-9.]+)\)`,
		jsPat:  `cos\(vec3f\(0\.0, ([0-9.]+), ([0-9.]+)\) \+ vec3f\(NoV \* ([0-9.]+)\)\)`,
		want:   "2.1|4.2|8.0",
	},
	{
		// The emissive map replaces the emissive colour. The native copy used to
		// multiply the map by material.emissive.rgb. materialFromRender fills
		// those lanes from the base colour, so the native copy applied the base
		// colour twice.
		//
		// The two copies spell the choice differently, and they have to. The
		// native flag arrives as a float lane, so the native copy stays
		// branchless. The browser flag is a u32, so the browser branches. This
		// row is therefore a presence check on each side, not a value check.
		id:     "emissive-map-replaces-colour",
		effect: "With an emissive map one backend emits the map colour and the other emits the map times the base colour.",
		goPat:  `let emissiveColor = mix\(baseColor, emissiveSample, hasEmissiveMap\);`,
		jsPat:  `emissiveColor = textureSample\(emissiveTex, emissiveSamp, in\.uv\)\.rgb;`,
	},
	{
		id:     "emissive-strength-scales-colour",
		effect: "Emissive strength stops scaling the emissive colour, so a glow saturates or vanishes.",
		goPat:  `let emissive = emissiveColor \* material\.pbrParams\.z;`,
		jsPat:  `let emission = emissiveColor \* emissiveStrength;`,
	},
	{
		// The three ambient terms are independent, so each intensity gates only
		// its own colour. The native copy used to multiply the sky and ground
		// blend by the ambient intensity as well. That cut the whole dome to the
		// ambient intensity, which is 0.3 by default.
		//
		// The native sky and ground intensities arrive premultiplied into .rgb
		// from resolveHemisphereAmbient. The native pattern therefore carries no
		// explicit sky or ground factor, and the browser pattern does. The
		// ambient intensity travels in .a on the native side and as its own
		// uniform in the browser. TestLitAmbientIntensityReachesTheShaderOnce
		// pins that each intensity still arrives exactly once.
		id:     "ambient-three-term-sum",
		effect: "One intensity gates a colour it does not own, so the whole scene reads darker on one backend.",
		goPat:  `let envDiffuse = scene\.ambientColor\.rgb \* scene\.ambientColor\.a\n\+ scene\.skyColor\.rgb \* hemi\n\+ scene\.groundColor\.rgb \* \(1\.0 - hemi\);`,
		jsPat:  `let envDiffuse = env\.ambientColor \* env\.ambientIntensity\n\+ env\.skyColor \* env\.skyIntensity \* hemi\n\+ env\.groundColor \* env\.groundIntensity \* \(1\.0 - hemi\);`,
	},
	{
		id:     "ambient-scaled-by-base-colour",
		effect: "Ambient light stops taking the surface colour, so every shadowed face turns grey.",
		goPat:  `let ambient = envDiffuse \* baseColor;`,
		jsPat:  `ambient = envDiffuse \* albedo;`,
	},

	// The rows below arrived with the scene light array. The native copy read
	// one directional light until 2026-07-26, so a scene lit by a point light,
	// a spot light or a hemisphere light took a canned key light under server
	// side rendering and on the desktop, and its authored lights on the web.
	// Each row pins one falloff both copies now compute the same way.
	{
		id:     "light-kind-codes",
		effect: "A scene reads a different light kind on one backend, so a spot light shades as a point light.",
		goPat:  `let kind = u32\(light\.position\.w\);`,
		jsPat:  `let lightType = u32\(light\.position\.w\);`,
	},
	{
		id:     "light-loop-is-runtime-bounded",
		effect: "A backend gains a compile-time light cap, so a scene silently loses its later lights.",
		goPat:  `arrayLength\(&lights\)`,
		jsPat:  `arrayLength\(&lights\)`,
	},
	{
		id:     "ambient-light-is-flat",
		effect: "An ambient light picks up a cosine term or a falloff on one backend.",
		goPat:  `directSum = directSum \+ baseColor \* lightColor \* intensity;`,
		jsPat:  `Lo = Lo \+ albedo \* lightColor \* intensity;`,
	},
	{
		id:     "hemisphere-light-blend",
		effect: "A hemisphere light swaps its sky and ground colours, or skews the blend across the normal range.",
		goPat:  `let hemiBlend = N\.y \* 0\.5 \+ 0\.5;`,
		jsPat:  `let hBlend = N\.y \* 0\.5 \+ 0\.5;`,
	},
	{
		id:     "hemisphere-light-colour-order",
		effect: "A hemisphere light lights the top of a sphere with the ground colour.",
		goPat:  `mix\(light\.groundPenumbra\.rgb, lightColor, hemiBlend\)`,
		jsPat:  `mix\(light\.groundPenumbra\.rgb, lightColor, hBlend\)`,
	},
	{
		// three.js windows the inverse square law by the range, so a point
		// light reaches exactly zero at its range instead of trailing off.
		id:     "point-light-windowed-falloff",
		effect: "A ranged point light reaches further on one backend, or stops short on the other.",
		goPat:  `let ratio = clamp\(1\.0 - pow\(dist / range, ([0-9.]+)\), 0\.0, 1\.0\);`,
		jsPat:  `let ratio = clamp\(1\.0 - pow\(distance / range, ([0-9.]+)\), 0\.0, 1\.0\);`,
		want:   "4.0",
	},
	{
		id:     "point-light-inverse-square",
		effect: "A ranged point light falls off linearly instead of by the inverse square.",
		goPat:  `return ratio \* ratio / max\(dist \* dist, ([0-9.]+)\);`,
		jsPat:  `return ratio \* ratio / max\(distance \* distance, ([0-9.]+)\);`,
		want:   "0.0001",
	},
	{
		id:     "point-light-decay-falloff",
		effect: "A point light with no range uses a different decay exponent, so it reads brighter far away.",
		goPat:  `return 1\.0 / max\(pow\(dist, decay\), ([0-9.]+)\);`,
		jsPat:  `return 1\.0 / max\(pow\(distance, decay\), ([0-9.]+)\);`,
		want:   "0.0001",
	},
	{
		id:     "spot-cone-angles",
		effect: "A spot light cone changes width, or its penumbra fades from the wrong edge.",
		goPat:  `let outerCos = cos\(angle\);\nlet innerCos = cos\(angle \* \(1\.0 - penumbra\)\);`,
		jsPat:  `let outerCos = cos\(angle\);\nlet innerCos = cos\(angle \* \(1\.0 - penumbra\)\);`,
	},
	{
		id:     "spot-cone-clamp",
		effect: "A spot light edge hardens or softens, or leaks light behind the cone.",
		goPat:  `return clamp\(\(cosAngle - outerCos\) / max\(innerCos - outerCos, ([0-9.]+)\), 0\.0, 1\.0\);`,
		jsPat:  `return clamp\(\(cosAngle - outerCos\) / max\(innerCos - outerCos, ([0-9.]+)\), 0\.0, 1\.0\);`,
		want:   "0.001",
	},
	{
		id:     "spot-attenuation-is-cone-times-distance",
		effect: "A spot light loses its distance falloff, so it stays as bright far away as near.",
		goPat:  `attenuation = pointLightAttenuation\(dist, range, decay\) \* cone;`,
		jsPat:  `attenuation = pointLightAttenuation\(dist, range, decay\) \* cone;`,
	},
	{
		id:     "radiance-includes-attenuation",
		effect: "A point or spot light stops attenuating with distance on one backend.",
		goPat:  `let radiance = lightColor \* intensity \* attenuation;`,
		jsPat:  `let radiance = lightColor \* intensity \* attenuation;`,
	},
	{
		id:     "shadow-follows-one-directional-light",
		effect: "The shadow map lands on a light it was never fitted to, or on every light at once.",
		goPat:  `if \(kind == 1u && i32\(i\) == shadowLightIndex\)`,
		jsPat:  `if \(shadow\.hasShadow0 != 0u && i32\(i\) == shadow\.shadowLightIndex0\)`,
	},
}

// TestLitWGSLSharedMaterialTermsMatchJSWebGPU pins the shading terms that
// render/bundle/lit.go litWGSL and 16a-scene-webgpu.js WGSL_PBR_FRAGMENT
// already compute the same way.
//
// A PASS PROVES: every pinned term still appears in both copies and still
// carries the same numeric value there. An edit to one copy that changes one of
// those values, renames the expression, or deletes it fails here.
//
// A PASS DOES NOT PROVE: that the two shaders produce the same pixel. They do
// not. See TestLitWGSLKnownDivergenceFromJSWebGPU for the recorded differences.
// A pass also covers only the rows in litSharedTerms; a term absent from that
// table is unguarded. It proves nothing about bind group layout, uniform
// offsets, texture binding order, or anything either shader computes outside
// these expressions.
//
// TestLitDriftGuardsDetectMutation proves this test can fail.
func TestLitWGSLSharedMaterialTermsMatchJSWebGPU(t *testing.T) {
	goSrc, jsSrc := litShaderCopies(t)
	for _, problem := range checkSharedTerms(litSharedTerms, goLitWhere, goSrc, jsLitWhere, jsSrc) {
		t.Error(problem)
	}
}

// TestLitAmbientIntensityReachesTheShaderOnce pins where each backend applies
// each environment intensity.
//
// The two backends split the work differently. The native path multiplies the
// sky and ground intensity into the colour before upload, in
// resolveHemisphereAmbient, and carries the ambient intensity in
// ambientColor.a. The browser uploads three raw colours and three raw
// intensities and multiplies in the shader. Both must apply one factor per term.
// Apply one twice and that term darkens by its own intensity squared.
//
// A PASS PROVES four things:
//
//   - resolveHemisphereAmbient applies the sky and ground intensity once.
//   - litWGSL names scene.skyColor and scene.groundColor once each, and
//     multiplies neither one by an intensity.
//   - The browser uniform packer uploads each intensity unmultiplied.
//   - WGSL_PBR_FRAGMENT names each intensity once.
//
// A PASS DOES NOT PROVE: that an unauthored environment lights the two backends
// the same. It does not. TestLitEnvironmentDefaultFallbacksDiverge records that.
func TestLitAmbientIntensityReachesTheShaderOnce(t *testing.T) {
	// An authored sky of pure red at half intensity must arrive as 0.5 in red.
	// A second application would arrive as 0.25.
	sky, ground := resolveHemisphereAmbient(engine.RenderBundle{
		Environment: engine.RenderEnvironment{
			SkyColor:        "#ff0000",
			SkyIntensity:    0.5,
			GroundColor:     "#00ff00",
			GroundIntensity: 0.25,
		},
	})
	if sky[0] != 0.5 {
		t.Errorf("resolveHemisphereAmbient returned sky red %g for colour #ff0000 at intensity 0.5, want 0.5. A value of 0.25 means the intensity is applied twice.", sky[0])
	}
	if ground[1] != 0.25 {
		t.Errorf("resolveHemisphereAmbient returned ground green %g for colour #00ff00 at intensity 0.25, want 0.25.", ground[1])
	}

	goSrc, jsSrc := litShaderCopies(t)
	for _, once := range []struct {
		where  string
		src    string
		needle string
		why    string
	}{
		{goLitWhere, goSrc, "scene.skyColor", "the sky intensity is already inside .rgb, so a second mention risks a second factor"},
		{goLitWhere, goSrc, "scene.groundColor", "the ground intensity is already inside .rgb, so a second mention risks a second factor"},
		{goLitWhere, goSrc, "scene.ambientColor.a", "the ambient intensity belongs in the ambient term only"},
		{jsLitWhere, jsSrc, "env.skyIntensity", "the browser holds the raw colour, so the shader must scale it once"},
		{jsLitWhere, jsSrc, "env.groundIntensity", "the browser holds the raw colour, so the shader must scale it once"},
		{jsLitWhere, jsSrc, "env.ambientIntensity", "the browser holds the raw colour, so the shader must scale it once"},
	} {
		if got := strings.Count(once.src, once.needle); got != 1 {
			t.Errorf("%s names %s %d times, want once: %s", once.where, once.needle, got, once.why)
		}
	}

	// The browser packer must upload the raw intensity. A product here would
	// apply it twice, because the shader multiplies as well.
	js := readJSWebGPURenderer(t)
	for _, raw := range []string{
		"data[3] = sceneNumber(env.ambientIntensity, 0);",
		"data[7] = sceneNumber(env.skyIntensity, 0);",
		"data[11] = sceneNumber(env.groundIntensity, 0);",
	} {
		if !strings.Contains(js, raw) {
			t.Errorf("%s no longer uploads the raw intensity line:\n  %s\nWGSL_PBR_FRAGMENT multiplies the colour by the intensity, so premultiplying here would apply it twice.", jsWebGPURendererFile, raw)
		}
	}
}

// TestLitEnvironmentDefaultFallbacksDiverge records the one ambient difference
// the audit of 2026-07-26 could not close.
//
// litWGSL and WGSL_PBR_FRAGMENT now compose ambient light the same way. The
// values they compose do not match when a scene authors no environment. The
// native path substitutes a full sky and ground intensity of 1.0 in
// resolveHemisphereAmbient. The browser substitutes 0, so an unauthored scene
// gets no dome at all in the browser and a bright dome under server side
// rendering.
//
// The old litWGSL masked that gap: it multiplied the whole dome by the ambient
// intensity. The composition fix removed the mask, so this test records the gap
// instead of leaving it to memory.
//
// A PASS PROVES: the two default sets are still the ones written down. A change
// to either forces a fresh decision.
//
// A PASS DOES NOT PROVE: that the gap is acceptable. It is not. Closing it means
// editing resolveHemisphereAmbient in render/bundle/renderer.go, which sits
// outside the files this audit owns.
func TestLitEnvironmentDefaultFallbacksDiverge(t *testing.T) {
	sky, ground := resolveHemisphereAmbient(engine.RenderBundle{})
	// 0.80 sky red and 0.28 ground red are the authored defaults, so an
	// intensity of 1.0 leaves them untouched.
	if sky[0] != 0.80 || ground[0] != 0.28 {
		t.Errorf("resolveHemisphereAmbient defaults are now sky red %g and ground red %g, recorded as 0.80 and 0.28. If the fallback intensities moved toward the browser default of 0, say so and update this record.", sky[0], ground[0])
	}
	js := readJSWebGPURenderer(t)
	for _, zero := range []string{
		"data[7] = sceneNumber(env.skyIntensity, 0);",
		"data[11] = sceneNumber(env.groundIntensity, 0);",
	} {
		if !strings.Contains(js, zero) {
			t.Errorf("%s no longer defaults its sky or ground intensity to 0:\n  %s\nThe native path defaults both to 1.0. If the browser default moved, the gap changed size; re-measure it.", jsWebGPURendererFile, zero)
		}
	}
}

// TestLightKindCodesMatchTheBrowserPacker pins the kind code every backend
// writes for one authored light kind.
//
// The codes are the contract between the packer and the shader, and each side
// owns its own packer: lightKindCode in render/bundle/renderer.go, and
// sceneWebGPULightTypeCode in the browser renderer. A code that drifts on one
// side shades a spot light as a point light, with no error anywhere.
//
// A PASS PROVES: both packers map the same seven authored kinds to the same
// seven numbers, and both fall back to the point code for an unknown kind.
//
// A PASS DOES NOT PROVE: that the shader shades each code the same way. The
// rows in litSharedTerms cover the five kinds both copies shade, and the
// rect-area-light row in litDivergentTerms records the one they do not.
func TestLightKindCodesMatchTheBrowserPacker(t *testing.T) {
	js := readJSWebGPURenderer(t)
	for _, row := range []struct {
		kind string
		code float32
	}{
		{"ambient", 0},
		{"light-probe", 0},
		{"directional", 1},
		{"point", 2},
		{"spot", 3},
		{"hemisphere", 4},
		{"rect-area", 5},
	} {
		if got := lightKindCode(row.kind); got != row.code {
			t.Errorf("lightKindCode(%q) = %g, want %g. Both packers feed the same shader branch, so change both or neither.",
				row.kind, got, row.code)
		}
		want := fmt.Sprintf("case %q: return %d;", row.kind, int(row.code))
		if !strings.Contains(js, want) {
			t.Errorf("%s no longer maps %q to code %d:\n  %s\nA drifted code shades one light kind as another with no error.",
				jsWebGPURendererFile, row.kind, int(row.code), want)
		}
	}
	// An unknown kind takes the point code on both sides. Dropping it instead
	// would make one backend ignore a light the other shades.
	if got := lightKindCode("no-such-kind"); got != lightKindPoint {
		t.Errorf("lightKindCode of an unknown kind = %g, want the point code %d", got, lightKindPoint)
	}
	if !strings.Contains(js, "default: return 2;") {
		t.Errorf("%s no longer defaults an unknown light kind to the point code", jsWebGPURendererFile)
	}
}

// TestLightPackingDefaultsMatchTheBrowser pins the substituted value each
// backend uses for an unset light field.
//
// engine.RenderBundle marshals with omitempty, so a zero on the native side is
// an absent field on the browser side, and an absent field takes the browser
// default. The two therefore have to substitute the same numbers or the same
// authored light shades differently on the two backends.
//
// A PASS PROVES: resolveSceneLights substitutes intensity 1, decay 2, shadow
// bias 0.005 and a straight-down direction, and the browser packer names the
// same four defaults.
//
// A PASS DOES NOT PROVE: that a partially authored direction agrees. It does
// not. TestLightDirectionOmitEmptyGapIsRecorded holds that gap.
func TestLightPackingDefaultsMatchTheBrowser(t *testing.T) {
	packed, shadowIndex := resolveSceneLights(engine.RenderBundle{
		Lights: []engine.RenderLight{{Kind: "point"}},
	}, nil)
	if len(packed) != 1 {
		t.Fatalf("resolveSceneLights packed %d lights for one authored light", len(packed))
	}
	if shadowIndex != -1 {
		t.Errorf("shadow index = %d for a scene with no directional light, want -1", shadowIndex)
	}
	for _, row := range []struct {
		name  string
		index int
		want  float32
		js    string
	}{
		{"direction x", 4, 0, "out[base + 4] = sceneNumber(light.directionX, 0);"},
		{"direction y", 5, -1, "out[base + 5] = sceneNumber(light.directionY, -1);"},
		{"direction z", 6, 0, "out[base + 6] = sceneNumber(light.directionZ, 0);"},
		{"intensity", 7, 1, "out[base + 7] = sceneNumber(light.intensity, 1);"},
		{"decay", 12, 2, "out[base + 12] = sceneNumber(light.decay, 2);"},
		{"shadow bias", 13, 0.005, "out[base + 13] = sceneNumber(light.shadowBias, 0.005);"},
		{"cone angle", 15, 0, "out[base + 15] = sceneNumber(light.angle, 0);"},
		{"penumbra", 19, 0, "out[base + 19] = clamp01(sceneNumber(light.penumbra, 0));"},
	} {
		if got := packed[0][row.index]; got != row.want {
			t.Errorf("resolveSceneLights substitutes %s = %g for an unset field, want %g", row.name, got, row.want)
		}
		if js := readJSWebGPURenderer(t); !strings.Contains(js, row.js) {
			t.Errorf("%s no longer substitutes the same %s default:\n  %s\nengine.RenderBundle omits a zero field, so the two defaults have to match.",
				jsWebGPURendererFile, row.name, row.js)
		}
	}
	// An unset colour is white on both sides, and an unset ground colour is
	// black. A hemisphere light with no ground colour must darken downward.
	if packed[0][8] != 1 || packed[0][9] != 1 || packed[0][10] != 1 {
		t.Errorf("an unset light colour packed as %v, want white", packed[0][8:11])
	}
	if packed[0][16] != 0 || packed[0][17] != 0 || packed[0][18] != 0 {
		t.Errorf("an unset ground colour packed as %v, want black", packed[0][16:19])
	}
}

// TestLightDirectionOmitEmptyGapIsRecorded holds a wire-format gap the light
// array did not create and cannot close.
//
// engine.RenderLight tags DirectionY with omitempty. A light authored with
// direction (1, 0, 0) therefore marshals without directionY, and the browser
// packer substitutes -1 for the missing field. The native packer sees the Go
// struct, not the JSON, so it keeps 0. The same light points along +X natively
// and diagonally down in the browser.
//
// A PASS PROVES: the native packer keeps a partially zero direction, and the
// browser still substitutes -1 for an absent directionY.
//
// A PASS DOES NOT PROVE: that the gap is acceptable. It is not. Closing it
// means dropping omitempty from the three direction fields in
// engine/render_bundle.go, which changes the wire format for every backend.
func TestLightDirectionOmitEmptyGapIsRecorded(t *testing.T) {
	packed, _ := resolveSceneLights(engine.RenderBundle{
		Lights: []engine.RenderLight{{Kind: "directional", DirectionX: 1}},
	}, nil)
	if packed[0][4] != 1 || packed[0][5] != 0 || packed[0][6] != 0 {
		t.Errorf("resolveSceneLights packed direction %v for an authored (1, 0, 0); a change here changes the size of the recorded gap", packed[0][4:7])
	}
	if !strings.Contains(readJSWebGPURenderer(t), "out[base + 5] = sceneNumber(light.directionY, -1);") {
		t.Errorf("%s no longer substitutes -1 for an absent directionY; re-measure the omitempty gap", jsWebGPURendererFile)
	}
}

// TestNativeKeyLightFallbackDivergesFromTheBrowser records the one lighting
// difference the light array did not close.
//
// The native path substitutes a built-in key light when a bundle authors no
// light at all, so an unlit demo still shows a shaded body. The browser
// substitutes nothing and renders the environment dome alone. Every native
// golden frame in scene/harness/testdata depends on the fallback, and a preview
// that renders black teaches an author the renderer is broken.
//
// A PASS PROVES three things:
//
//   - The fallback fires only when the bundle authors no light at all.
//   - Its direction, colour and intensity are still the ones
//     resolveDirectionalLight substitutes, so the two agree on the same frame.
//   - The browser still bounds its loop by the authored count alone.
//
// A PASS DOES NOT PROVE: that the difference is acceptable. Closing it means
// giving the browser the same fallback.
func TestNativeKeyLightFallbackDivergesFromTheBrowser(t *testing.T) {
	packed, shadowIndex := resolveSceneLights(engine.RenderBundle{}, nil)
	if len(packed) != 1 {
		t.Fatalf("a bundle with no lights packed %d lights, want the one fallback key light", len(packed))
	}
	if shadowIndex != 0 {
		t.Errorf("the fallback key light has shadow index %d, want 0; the cascades are fitted to its direction", shadowIndex)
	}
	dir, color, _ := resolveDirectionalLight(engine.RenderBundle{})
	// litWGSL normalizes both scene.lightDir and every packed direction, so
	// compare the unit vectors the shader really uses. The cascade fit reads
	// scene.lightDir and the light loop reads the record; a mismatch would put
	// the shadow map on a light it was never fitted to.
	packedDir := unitVector([3]float32{packed[0][4], packed[0][5], packed[0][6]})
	fitDir := unitVector(dir)
	for i := range fitDir {
		if absDiff(packedDir[i], fitDir[i]) > 1e-6 {
			t.Errorf("fallback direction component %d is %g, and resolveDirectionalLight uses %g. The cascade fit reads one and the shader reads the other, so a mismatch puts the shadow in the wrong place.", i, packedDir[i], fitDir[i])
		}
	}
	for i := 0; i < 3; i++ {
		if got := packed[0][8+i]; got != color[i] {
			t.Errorf("fallback colour component %d is %g, want %g", i, got, color[i])
		}
	}
	if got := packed[0][7]; got != color[3] {
		t.Errorf("fallback intensity is %g, want %g", got, color[3])
	}
	// One authored light of any kind cancels the fallback, exactly as the
	// browser has no fallback at all.
	authored, _ := resolveSceneLights(engine.RenderBundle{
		Lights: []engine.RenderLight{{Kind: "ambient", Color: "#ffffff"}},
	}, nil)
	if len(authored) != 1 || authored[0][3] != lightKindAmbient {
		t.Fatalf("a scene with one ambient light packed %d records, first kind %v; the fallback must not join an authored scene",
			len(authored), authored[0][3])
	}
	if !strings.Contains(readJSWebGPURenderer(t), "let lightCount = min(frame.lightCount, arrayLength(&lights));") {
		t.Errorf("%s no longer bounds its light loop by the authored count; re-measure the fallback gap", jsWebGPURendererFile)
	}
}

func absDiff(a, b float32) float32 {
	if a > b {
		return a - b
	}
	return b - a
}

// unitVector returns v scaled to unit length, or v unchanged when it is zero.
func unitVector(v [3]float32) [3]float32 {
	length := float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])))
	if length == 0 {
		return v
	}
	return [3]float32{v[0] / length, v[1] / length, v[2] / length}
}

// divergentTerm is one shading term the two copies already compute differently.
//
// goLine and jsLine are exact substrings of the normalized source of their own
// copy. The check fails when either line disappears, so a change on either side
// forces an update to this ledger and a fresh decision about the difference.
type divergentTerm struct {
	id      string
	effect  string // what a viewer sees today
	verdict string // which copy this audit judges correct, and why
	goLine  string
	jsLine  string
}

// litDivergentTerms records where litWGSL and WGSL_PBR_FRAGMENT already shade
// the same authored material differently.
//
// This is a ledger of open defects, not a statement that the differences are
// acceptable. Each row lists the effect and a verdict.
//
// When a fix closes a difference, delete the row and add the term to
// litSharedTerms in the same change. Do not relax a row to let a one sided fix
// pass. A row that names a term neither copy carries any more is a guard that
// stopped guarding.
var litDivergentTerms = []divergentTerm{
	{
		// This row stayed open on purpose. The audit of 2026-07-26 moved the
		// native copy to the browser form on seven numeric terms. In each of
		// those the native copy broke a specification, or carried an arbitrary
		// constant. This one is different. Both formulations are accepted, and
		// the native one is the better of the two.
		//
		// Measured ratio of the native visibility term to the browser one, at
		// NdotV = NdotL, both terms including their own divide:
		//
		//	facing  roughness 0.1  roughness 0.5  roughness 1.0
		//	1.00    1.00           1.00           1.00
		//	0.70    1.13           1.13           1.03
		//	0.50    1.31           1.31           1.13
		//	0.30    1.79           1.73           1.41
		//	0.15    3.27           2.79           2.21
		//	0.05    12.74          7.07           5.57
		//
		// The two agree exactly head on. They part company toward the silhouette,
		// where the native copy is brighter, and the native copy is right there.
		// Smith masking must approach 1 as the surface approaches a mirror, and
		// the Hammon form does. The browser k = (roughness + 1)^2 / 8 never falls
		// below 0.125. It therefore keeps shadowing a mirror smooth surface and
		// reads too dark at grazing angles. It also costs three divides, not one.
		//
		// So the browser copies are the ones to move, in
		// client/js/bootstrap-src/16a-scene-webgpu.js and 16-scene-webgl.js. Both
		// files sit outside this audit's scope. The other direction reaches
		// agreement by making the server side renderer less accurate, and that
		// renderer is also the test oracle.
		id:      "specular-geometry-term",
		effect:  "Specular brightness differs at grazing angles, from 1.13 times at a facing ratio of 0.7 up to 12.7 times at 0.05.",
		verdict: "Keep the native form. Go uses the Hammon height correlated Smith visibility, which folds in the 1/(4 NdotL NdotV) factor and approaches no masking as roughness approaches zero. The browser uses Schlick GGX with k = (roughness + 1)^2 / 8, which keeps masking a mirror and reads too dark at grazing angles. Move both browser copies to the correlated form.",
		goLine:  "return 0.5 / max(ggxV + ggxL, 1e-5);",
		jsLine:  "let k = (r * r) / 8.0;",
	},
	{
		id:      "specular-denominator",
		effect:  "Follows from the geometry term above. Listed so both halves stay pinned.",
		verdict: "Go carries no explicit divide because its visibility term already contains it. The browser divides. Each copy is consistent with itself and inconsistent with the other. This row closes when the browser copies adopt the correlated visibility term and drop their divide.",
		goLine:  "let specular = D * G * F;",
		jsLine:  "let denominator = 4.0 * NoV * NdotL + 0.0001;",
	},
	{
		// Not a shader defect. The native lit vertex format has no per instance
		// colour to read. Locations 4 to 7 carry the model matrix, and location 8
		// carries pickData as a vec4u. Closing this row needs a new instance
		// attribute in the native vertex buffer layout, so it needs renderer.go
		// and object_mesh.go, not this shader.
		id:      "output-alpha-source",
		effect:  "Per instance alpha is ignored under server side rendering, so an instance faded through instanceColor.a stays opaque.",
		verdict: "The browser multiplies material opacity by the per instance alpha. Go writes only the material alpha. The native path carries no per instance colour at all, so this row waits on a vertex layout change rather than a shader edit.",
		goLine:  "out.color = vec4f(color, clamp(material.baseColor.a, 0.0, 1.0));",
		jsLine:  "let finalOpacity = material.opacity * clamp(in.instanceColor.a, 0.0, 1.0);",
	},
	{
		// Browser WebGPU now consumes the generated assetpipe split-sum
		// radiance/irradiance/LUT products. This row remains because the native
		// renderer consumes the separate raw EnvironmentMap field and its
		// underived level-zero approximation, while browser WebGPU does not.
		// Closing it requires one transport vocabulary (preferably the generated
		// products) across native and browser, not removal of either live path.
		id:      "environment-map",
		effect:  "The native renderer consumes the authored raw environment cube while browser WebGPU consumes only generated split-sum IBL products, so the two surfaces can light differently.",
		verdict: "Keep both honest paths and record the remaining carrier gap. Browser WebGPU now consumes assetpipe split-sum products, but it still does not consume the raw EnvironmentMap field that the native renderer samples.",
		goLine:  "let cubeIBL = (cubeDiffuse + envSpecular) * scene.envParams.x * scene.envParams.z;",
		jsLine:  "ambient = (diffuseIBL + specularIBL) * env.envIntensity;",
	},
	{
		// This row records the same underived factor from the other side. The
		// native copy carries it and the browser WebGPU copy carries nothing,
		// so a reader who deletes the row above must still meet the factor.
		id:      "environment-specular-roughness-factor",
		effect:  "Follows from the row above. A rough native surface reflects the environment through a linear falloff instead of a prefiltered mip level, so a poster reads glossier than a split-sum fit would.",
		verdict: "Record only. The factor is not defensible on its own, and neither is deleting it while the native path is the one backend that reads the image at all. It retires when a prefiltered cube and a BRDF lookup table land, which is the same work that closes the row above.",
		goLine:  "let envSpecular = textureSample(envCubeTexture, envCubeSampler, envReflect).rgb * F * (1.0 - roughness * 0.65);",
		jsLine:  "var color = ambient + Lo + emission;",
	},
	{
		id:      "normal-map-tangent-basis",
		effect:  "A normal mapped surface lights differently on the two backends, most visibly on low polygon geometry.",
		verdict: "Go derives the tangent frame per pixel from screen space derivatives because its vertex format carries no tangent. The browser uses the authored vertex tangent. Neither is wrong, but the results differ. Record the gap, or add a tangent attribute to the native path.",
		goLine:  "let tangentRaw = (q1 * st2.y - q2 * st1.y) / det;",
		jsLine:  "let TBN = mat3x3f(T, B, N);",
	},
	{
		// This row is not closable in the shader. The rectangle the polygon form
		// factor integrates over needs a width and a height, and
		// engine.RenderLight declares neither, so the two edge vectors the
		// browser builds in sceneWebGPURectAreaBasis cannot be built here. The
		// native Light record stops at five vec4 for the same reason.
		//
		// Closing this row means, in order: add Width and Height to
		// engine.RenderLight, carry them through scene/preview/preview.go, pack
		// two more vec4 into resolveSceneLights, port the three LTC functions
		// into litWGSL, and then change the rect-area-light cell in
		// scene/capability/capability.go from {WebGPU true, WebGL false} to name
		// the native backend as well. Do not change that cell before the shader
		// draws the light: a false cell that reads true is worse than a gap.
		id:      "rect-area-light",
		effect:  "A rect-area light lights the scene in the browser under WebGPU and contributes nothing under server side rendering, on the desktop, or in a poster.",
		verdict: "Keep the native copy silent. The browser evaluates the exact three.js diffuse form factor plus a representative-point specular lobe. The native path has no rectangle to integrate, because engine.RenderLight carries no width and no height, so any native answer would be invented. scene/preview/coverage.go reports the drop to the author.",
		goLine:  "if (kind == 5u) {",
		jsLine:  "Lo = Lo + rectAreaLightRadiance(light, in.worldPos, N, V, albedo, roughness, metalness, F0, NoV);",
	},
}

// TestLitWGSLKnownDivergenceFromJSWebGPU pins the recorded differences between
// render/bundle/lit.go litWGSL and 16a-scene-webgpu.js WGSL_PBR_FRAGMENT.
//
// A PASS PROVES: every recorded difference is still present, exactly as written
// down, on both sides. Nobody can change one of these terms, on either copy,
// without this test failing and forcing an update to the ledger. That is the
// point: these are open defects, and the test stops them from drifting further
// and stops them from being closed silently on one backend only.
//
// A PASS DOES NOT PROVE: that the differences are acceptable. Read the verdict
// on each row. A pass also does not prove the ledger is complete. It lists the
// differences this audit found by reading the two copies, and it covers no
// difference outside the fragment shading math.
//
// TestLitDriftGuardsDetectMutation proves this test can fail.
func TestLitWGSLKnownDivergenceFromJSWebGPU(t *testing.T) {
	goSrc, jsSrc := litShaderCopies(t)
	for _, problem := range checkDivergentTerms(litDivergentTerms, goLitWhere, goSrc, jsLitWhere, jsSrc) {
		t.Error(problem)
	}
}

// litShaderCopies returns the normalized source of both lit fragment copies.
func litShaderCopies(t *testing.T) (goSrc, jsSrc string) {
	t.Helper()
	return normalizeWGSLSyntax(litWGSL),
		normalizeWGSLSyntax(jsShaderSource(t, readJSWebGPURenderer(t), litJSFragmentName))
}

// checkSharedTerms returns one problem per violated row. It returns an empty
// slice when both copies agree on every row.
//
// The check lives apart from the test function so a self test can feed it a
// mutated copy and prove the guard fires. A guard nobody has seen fail is a
// guard nobody should trust.
func checkSharedTerms(terms []sharedTerm, goWhere, goSrc, jsWhere, jsSrc string) []string {
	var problems []string
	seen := map[string]bool{}
	for _, term := range terms {
		if seen[term.id] {
			problems = append(problems, fmt.Sprintf("the term table lists id %q twice", term.id))
			continue
		}
		seen[term.id] = true
		for _, side := range []struct {
			where   string
			src     string
			pattern string
		}{
			{goWhere, goSrc, term.goPat},
			{jsWhere, jsSrc, term.jsPat},
		} {
			values, err := findPinned(side.pattern, side.src)
			if err != nil {
				problems = append(problems, fmt.Sprintf("term %q: %s: %v\nEffect: %s\nThe expression was renamed, reshaped, or deleted. Update the pinned pattern together with the shader change.",
					term.id, side.where, err, term.effect))
				continue
			}
			for _, got := range values {
				if got == term.want {
					continue
				}
				problems = append(problems, fmt.Sprintf("term %q: %s now carries %q, pinned value is %q.\nEffect: %s\nBoth copies shade the same engine.RenderMaterial, so change both or neither.",
					term.id, side.where, got, term.want, term.effect))
			}
		}
	}
	return problems
}

// checkDivergentTerms returns one problem per ledger row whose recorded line no
// longer appears in its own copy.
func checkDivergentTerms(terms []divergentTerm, goWhere, goSrc, jsWhere, jsSrc string) []string {
	var problems []string
	seen := map[string]bool{}
	for _, term := range terms {
		if seen[term.id] {
			problems = append(problems, fmt.Sprintf("the divergence ledger lists id %q twice", term.id))
			continue
		}
		seen[term.id] = true
		if term.goLine == term.jsLine {
			problems = append(problems, fmt.Sprintf("ledger row %q records the same line on both sides, so it is not a divergence; move it to the shared table", term.id))
			continue
		}
		for _, side := range []struct {
			where string
			src   string
			line  string
		}{
			{goWhere, goSrc, term.goLine},
			{jsWhere, jsSrc, term.jsLine},
		} {
			if strings.Contains(side.src, side.line) {
				continue
			}
			problems = append(problems, fmt.Sprintf("ledger row %q: %s no longer contains the recorded line:\n  %s\nEffect on record: %s\nVerdict on record: %s\nUpdate the ledger and say whether the difference is now closed.",
				term.id, side.where, side.line, term.effect, term.verdict))
		}
	}
	return problems
}

// findPinned returns the capture text of every match of pattern in src, with
// several capture groups joined by "|". A pattern that matches nothing is an
// error, because a silent zero match is how a guard stops guarding.
func findPinned(pattern, src string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("pinned pattern %s does not compile: %w", pattern, err)
	}
	matches := re.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("pinned pattern %s matches nothing", pattern)
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, strings.Join(m[1:], "|"))
	}
	return out, nil
}

// litGuardMutation is one edit that the guards above must catch.
type litGuardMutation struct {
	name    string
	side    string // "go" or "js"
	from    string
	to      string
	wantRow string // the row id the failure must name
}

// litSharedGuardMutations are edits that break agreement between the copies.
var litSharedGuardMutations = []litGuardMutation{
	{
		name:    "browser lowers the anisotropy roughness gain",
		side:    "js",
		from:    "abs(material.anisotropy) * 0.28",
		to:      "abs(material.anisotropy) * 0.30",
		wantRow: "anisotropy-roughness-gain",
	},
	{
		name:    "native renderer raises the dielectric F0",
		side:    "go",
		from:    "mix(vec3f(0.04), baseColor, metalness)",
		to:      "mix(vec3f(0.05), baseColor, metalness)",
		wantRow: "dielectric-f0",
	},
	{
		name:    "browser widens the clear coat power range",
		side:    "js",
		from:    "mix(12.0, 96.0, 1.0 - roughness)",
		to:      "mix(12.0, 128.0, 1.0 - roughness)",
		wantRow: "clearcoat-power-range",
	},
	{
		name:    "native renderer drops the pi normalization from the diffuse lobe",
		side:    "go",
		from:    "kD * baseColor / 3.141592653589793",
		to:      "kD * baseColor",
		wantRow: "lambert-diffuse-normalization",
	},
	{
		name:    "browser drops the shadow factor from the direct light term",
		side:    "js",
		from:    "* radiance * NdotL * shadowAtten",
		to:      "* radiance * NdotL",
		wantRow: "direct-light-composition",
	},

	// The mutations below revert one of the seven terms the audit of 2026-07-26
	// moved into litSharedTerms. Each one restores the exact value the native
	// copy carried before that audit. A passing run here is therefore the
	// evidence that nobody can undo one of those fixes in silence.
	{
		name:    "native renderer reverts the roughness map to the red channel",
		side:    "go",
		from:    "let roughSample = textureSample(roughnessMapTex, baseColorSampler, in.uv).g;",
		to:      "let roughSample = textureSample(roughnessMapTex, baseColorSampler, in.uv).r;",
		wantRow: "roughness-map-channel",
	},
	{
		name:    "browser moves the roughness map to the red channel",
		side:    "js",
		from:    "roughness = roughness * textureSample(roughnessTex, roughnessSamp, in.uv).g;",
		to:      "roughness = roughness * textureSample(roughnessTex, roughnessSamp, in.uv).r;",
		wantRow: "roughness-map-channel",
	},
	{
		name:    "native renderer reverts the metalness map to the red channel",
		side:    "go",
		from:    "let metalSample = textureSample(metalnessMapTex, baseColorSampler, in.uv).b;",
		to:      "let metalSample = textureSample(metalnessMapTex, baseColorSampler, in.uv).r;",
		wantRow: "metalness-map-channel",
	},
	{
		name:    "native renderer reverts the clear coat gain to 1.0",
		side:    "go",
		from:    "color = color + vec3f(pow(NdotV, clearcoatPower) * clearcoat * 0.28);",
		to:      "color = color + vec3f(pow(NdotV, clearcoatPower) * clearcoat);",
		wantRow: "clearcoat-gain",
	},
	{
		name:    "native renderer reverts the sheen term to a blend",
		side:    "go",
		from:    "color = color + baseColor * velvet * 0.55;",
		to:      "color = mix(color, color + baseColor * velvet, sheen * 0.35);",
		wantRow: "sheen-composition",
	},
	{
		name:    "native renderer reverts the iridescence phases and frequency",
		side:    "go",
		from:    "cos(vec3f(0.0, 2.1, 4.2) + NdotV * 8.0)",
		to:      "cos(vec3f(0.0, 2.0943951, 4.1887902) + NdotV * 6.2831853)",
		wantRow: "iridescence-phase-and-frequency",
	},
	{
		name:    "browser moves the iridescence frequency back to one turn",
		side:    "js",
		from:    "cos(vec3f(0.0, 2.1, 4.2) + vec3f(NoV * 8.0))",
		to:      "cos(vec3f(0.0, 2.1, 4.2) + vec3f(NoV * 6.2831853))",
		wantRow: "iridescence-phase-and-frequency",
	},
	{
		name:    "native renderer reverts the emissive map to a tint",
		side:    "go",
		from:    "let emissiveColor = mix(baseColor, emissiveSample, hasEmissiveMap);",
		to:      "let emissiveColor = material.emissive.rgb * mix(vec3f(1.0), emissiveSample, hasEmissiveMap);",
		wantRow: "emissive-map-replaces-colour",
	},
	{
		name:    "native renderer lets the ambient intensity gate the sky term again",
		side:    "go",
		from:    "+ scene.skyColor.rgb * hemi",
		to:      "+ scene.skyColor.rgb * hemi * scene.ambientColor.a",
		wantRow: "ambient-three-term-sum",
	},
	{
		name:    "native renderer drops the base colour from the ambient term",
		side:    "go",
		from:    "let ambient = envDiffuse * baseColor;",
		to:      "let ambient = envDiffuse;",
		wantRow: "ambient-scaled-by-base-colour",
	},

	// The mutations below cover the rows the scene light array added. Each one
	// breaks a light term on one backend only, which is exactly how a scene
	// starts shading differently under server side rendering than in a browser.
	{
		name:    "native renderer reads the light kind from the wrong lane",
		side:    "go",
		from:    "let kind = u32(light.position.w);",
		to:      "let kind = u32(light.direction.w);",
		wantRow: "light-kind-codes",
	},
	{
		name:    "native renderer puts a compile-time cap on the light loop",
		side:    "go",
		from:    "let lightCount = min(u32(max(scene.lightParams.x, 0.0)), arrayLength(&lights));",
		to:      "let lightCount = min(u32(max(scene.lightParams.x, 0.0)), 4u);",
		wantRow: "light-loop-is-runtime-bounded",
	},
	{
		name:    "native renderer gives an ambient light a cosine term",
		side:    "go",
		from:    "directSum = directSum + baseColor * lightColor * intensity;",
		to:      "directSum = directSum + baseColor * lightColor * intensity * NdotV;",
		wantRow: "ambient-light-is-flat",
	},
	{
		name:    "browser skews the hemisphere blend across the normal range",
		side:    "js",
		from:    "let hBlend = N.y * 0.5 + 0.5;",
		to:      "let hBlend = N.y * 0.25 + 0.5;",
		wantRow: "hemisphere-light-blend",
	},
	{
		name:    "native renderer swaps the hemisphere sky and ground colours",
		side:    "go",
		from:    "mix(light.groundPenumbra.rgb, lightColor, hemiBlend)",
		to:      "mix(lightColor, light.groundPenumbra.rgb, hemiBlend)",
		wantRow: "hemisphere-light-colour-order",
	},
	{
		name:    "native renderer changes the point light window exponent",
		side:    "go",
		from:    "let ratio = clamp(1.0 - pow(dist / range, 4.0), 0.0, 1.0);",
		to:      "let ratio = clamp(1.0 - pow(dist / range, 2.0), 0.0, 1.0);",
		wantRow: "point-light-windowed-falloff",
	},
	{
		name:    "browser drops the inverse square from a ranged point light",
		side:    "js",
		from:    "return ratio * ratio / max(distance * distance, 0.0001);",
		to:      "return ratio * ratio / max(distance * distance, 0.001);",
		wantRow: "point-light-inverse-square",
	},
	{
		name:    "native renderer raises the unranged point light floor",
		side:    "go",
		from:    "return 1.0 / max(pow(dist, decay), 0.0001);",
		to:      "return 1.0 / max(pow(dist, decay), 0.01);",
		wantRow: "point-light-decay-falloff",
	},
	{
		name:    "native renderer widens the spot inner cone",
		side:    "go",
		from:    "let innerCos = cos(angle * (1.0 - penumbra));",
		to:      "let innerCos = cos(angle * (1.0 - penumbra * 0.5));",
		wantRow: "spot-cone-angles",
	},
	{
		name:    "browser hardens the spot cone edge",
		side:    "js",
		from:    "return clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.001), 0.0, 1.0);",
		to:      "return clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.1), 0.0, 1.0);",
		wantRow: "spot-cone-clamp",
	},
	{
		name:    "native renderer drops the distance falloff from a spot light",
		side:    "go",
		from:    "attenuation = pointLightAttenuation(dist, range, decay) * cone;",
		to:      "attenuation = cone;",
		wantRow: "spot-attenuation-is-cone-times-distance",
	},
	{
		name:    "native renderer drops the attenuation from the radiance",
		side:    "go",
		from:    "let radiance = lightColor * intensity * attenuation;",
		to:      "let radiance = lightColor * intensity;",
		wantRow: "radiance-includes-attenuation",
	},
	{
		name:    "native renderer shadows every light with the one shadow map",
		side:    "go",
		from:    "if (kind == 1u && i32(i) == shadowLightIndex)",
		to:      "if (kind == 1u)",
		wantRow: "shadow-follows-one-directional-light",
	},
}

// litDivergentGuardMutations are edits that silently close a recorded
// divergence on one backend only, or move it further apart.
var litDivergentGuardMutations = []litGuardMutation{
	{
		name:    "native renderer adopts the browser specular visibility without updating the ledger",
		side:    "go",
		from:    "return 0.5 / max(ggxV + ggxL, 1e-5);",
		to:      "return NdotV / (NdotV * (1.0 - 0.25) + 0.25);",
		wantRow: "specular-geometry-term",
	},
	{
		name:    "browser adopts the correlated visibility without updating the ledger",
		side:    "js",
		from:    "let k = (r * r) / 8.0;",
		to:      "let k = 0.0;",
		wantRow: "specular-geometry-term",
	},
	{
		name:    "native renderer divides the specular term without updating the ledger",
		side:    "go",
		from:    "let specular = D * G * F;",
		to:      "let specular = D * G * F / (4.0 * NdotV * NdotL + 0.0001);",
		wantRow: "specular-denominator",
	},
	{
		name:    "native renderer folds a per instance alpha into the output without updating the ledger",
		side:    "go",
		from:    "out.color = vec4f(color, clamp(material.baseColor.a, 0.0, 1.0));",
		to:      "out.color = vec4f(color, clamp(material.baseColor.a * in.instanceAlpha, 0.0, 1.0));",
		wantRow: "output-alpha-source",
	},
	{
		name:    "native renderer drops the environment-map tap to reach parity without updating the ledger",
		side:    "go",
		from:    "let cubeIBL = (cubeDiffuse + envSpecular) * scene.envParams.x * scene.envParams.z;",
		to:      "let cubeIBL = vec3f(0.0);",
		wantRow: "environment-map",
	},
	{
		name:    "browser drops generated split-sum IBL without updating the ledger",
		side:    "js",
		from:    "ambient = (diffuseIBL + specularIBL) * env.envIntensity;",
		to:      "ambient = diffuseIBL * env.envIntensity;",
		wantRow: "environment-map",
	},
	{
		name:    "native renderer replaces the underived roughness factor without updating the ledger",
		side:    "go",
		from:    "let envSpecular = textureSample(envCubeTexture, envCubeSampler, envReflect).rgb * F * (1.0 - roughness * 0.65);",
		to:      "let envSpecular = textureSampleLevel(envCubeTexture, envCubeSampler, envReflect, roughness * 6.0).rgb * F;",
		wantRow: "environment-specular-roughness-factor",
	},
	{
		name:    "native renderer adopts an authored tangent basis without updating the ledger",
		side:    "go",
		from:    "let tangentRaw = (q1 * st2.y - q2 * st1.y) / det;",
		to:      "let tangentRaw = in.tangent.xyz;",
		wantRow: "normal-map-tangent-basis",
	},
	{
		name:    "native renderer starts approximating a rect-area light without updating the ledger",
		side:    "go",
		from:    "if (kind == 5u) {",
		to:      "if (kind == 6u) {",
		wantRow: "rect-area-light",
	},
	{
		name:    "browser stops shading a rect-area light without updating the ledger",
		side:    "js",
		from:    "Lo = Lo + rectAreaLightRadiance(light, in.worldPos, N, V, albedo, roughness, metalness, F0, NoV);",
		to:      "Lo = Lo + vec3f(0.0);",
		wantRow: "rect-area-light",
	},
}

// TestLitDriftGuardsDetectMutation proves the two guards above can fail.
//
// It first confirms both guards pass on the shipped sources. It then applies one
// edit at a time to a copy held in memory and confirms the matching guard
// reports a problem that names the affected row. No file changes.
//
// An audit of this repository found eighteen tests that passed while proving
// nothing. This test is the evidence that the lit drift guards are not the
// nineteenth.
func TestLitDriftGuardsDetectMutation(t *testing.T) {
	goSrc, jsSrc := litShaderCopies(t)

	if problems := checkSharedTerms(litSharedTerms, goLitWhere, goSrc, jsLitWhere, jsSrc); len(problems) != 0 {
		t.Fatalf("the shared term table must pass on the shipped sources before the mutation check means anything; got %d problems:\n%s", len(problems), strings.Join(problems, "\n"))
	}
	if problems := checkDivergentTerms(litDivergentTerms, goLitWhere, goSrc, jsLitWhere, jsSrc); len(problems) != 0 {
		t.Fatalf("the divergence ledger must pass on the shipped sources before the mutation check means anything; got %d problems:\n%s", len(problems), strings.Join(problems, "\n"))
	}

	t.Run("shared", func(t *testing.T) {
		for _, mut := range litSharedGuardMutations {
			t.Run(mut.name, func(t *testing.T) {
				mutGo, mutJS := applyLitMutation(t, mut, goSrc, jsSrc)
				assertProblemNamesRow(t, checkSharedTerms(litSharedTerms, goLitWhere, mutGo, jsLitWhere, mutJS), mut)
			})
		}
	})

	t.Run("divergent", func(t *testing.T) {
		for _, mut := range litDivergentGuardMutations {
			t.Run(mut.name, func(t *testing.T) {
				mutGo, mutJS := applyLitMutation(t, mut, goSrc, jsSrc)
				assertProblemNamesRow(t, checkDivergentTerms(litDivergentTerms, goLitWhere, mutGo, jsLitWhere, mutJS), mut)
			})
		}
	})
}

// applyLitMutation returns the two copies with one edit applied to one of them.
// It fails when the text to edit is absent, because then the mutation would
// prove nothing.
func applyLitMutation(t *testing.T, mut litGuardMutation, goSrc, jsSrc string) (string, string) {
	t.Helper()
	switch mut.side {
	case "go":
		if !strings.Contains(goSrc, mut.from) {
			t.Fatalf("mutation %q cannot apply: %s does not contain %q", mut.name, goLitWhere, mut.from)
		}
		return strings.Replace(goSrc, mut.from, mut.to, 1), jsSrc
	case "js":
		if !strings.Contains(jsSrc, mut.from) {
			t.Fatalf("mutation %q cannot apply: %s does not contain %q", mut.name, jsLitWhere, mut.from)
		}
		return goSrc, strings.Replace(jsSrc, mut.from, mut.to, 1)
	default:
		t.Fatalf("mutation %q has side %q; use \"go\" or \"js\"", mut.name, mut.side)
		return goSrc, jsSrc
	}
}

// assertProblemNamesRow fails when the guard stayed silent, or when it
// complained about a row other than the mutated one.
func assertProblemNamesRow(t *testing.T, problems []string, mut litGuardMutation) {
	t.Helper()
	if len(problems) == 0 {
		t.Fatalf("mutation %q produced no problem; the guard does not cover row %q", mut.name, mut.wantRow)
	}
	for _, problem := range problems {
		if strings.Contains(problem, mut.wantRow) {
			return
		}
	}
	t.Fatalf("mutation %q produced problems, but none names row %q:\n%s", mut.name, mut.wantRow, strings.Join(problems, "\n"))
}

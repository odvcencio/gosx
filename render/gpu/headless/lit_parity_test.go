package headless

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// GoSX now shades the same authored material three times.
//
//   - render/bundle/lit.go litWGSL runs on a real WebGPU device.
//   - client/js/bootstrap-src/16a-scene-webgpu.js runs in the browser.
//   - litProgram.shade in this package runs on the CPU and is the oracle for
//     every deterministic test, every golden frame and the whole agent authoring
//     loop.
//
// render/bundle/lit_drift_test.go guards the first two against each other. This
// file guards the third against the first.
//
// A guard is needed, and the reason is recorded history. The ambient dome was
// corrected in litWGSL and left wrong in this copy. Nothing caught it, because
// this path re-implements the shading model and never reads the shader source. A
// golden frame could not catch it either: regenerating the golden pinned the
// wrong value.
//
// WHY A STRING COMPARISON IS THE WRONG GUARD, AND WHAT THIS ONE DOES INSTEAD
//
// The two copies are not, and cannot be, byte identical. One is WGSL and one is
// Go. So this file pins the numbers instead. Each row names a Go constant that
// litProgram.shade really uses and a regular expression that finds the same
// number in the shader. Change either side alone and the row fails.

// litShaderPath is the shader this copy follows.
const litShaderPath = "../../bundle/lit.go"

// readLitShader returns the shader source, with runs of blanks collapsed so one
// pattern matches whatever the formatter did to the indentation.
func readLitShader(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(litShaderPath))
	if err != nil {
		t.Fatalf("read %s: %v", litShaderPath, err)
	}
	blanks := regexp.MustCompile(`[ \t]+`)
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(blanks.ReplaceAllString(line, " "))
	}
	return strings.Join(lines, "\n")
}

// litConstantRow is one number this package shares with litWGSL.
//
// value is what the rasterizer really computes with. pattern must capture the
// same number from the shader source. effect names what a reader sees when the
// two part company.
type litConstantRow struct {
	id      string
	effect  string
	value   float32
	pattern string
}

// litSharedConstants is the pinned agreement between litProgram.shade and
// litWGSL.
//
// Add a row when this copy starts using a number the shader also uses. Do not add
// a row for a number only one copy has.
var litSharedConstants = []litConstantRow{
	{
		id:      "dielectric-f0",
		effect:  "Every non-metal highlight changes brightness.",
		value:   dielectricF0,
		pattern: `mix\(vec3<f32>\(([0-9.]+)\), baseColor, metalness\)`,
	},
	{
		id:      "roughness-floor",
		effect:  "A polished material turns into a pinpoint mirror on one backend only.",
		value:   roughnessFloor,
		pattern: `clamp\(roughness \* \(1\.0 - abs\(anisotropy\) \* [0-9.]+\), ([0-9.]+), 1\.0\)`,
	},
	{
		id:      "anisotropy-roughness-gain",
		effect:  "An anisotropic material reads sharper on one backend than the other.",
		value:   anisotropyRoughnessGain,
		pattern: `roughness \* \(1\.0 - abs\(anisotropy\) \* ([0-9.]+)\)`,
	},
	{
		id:      "clearcoat-power-low",
		effect:  "A rough coated surface spreads its highlight differently.",
		value:   clearcoatPowerLow,
		pattern: `mix\(([0-9.]+), 96\.0, 1\.0 - roughness\)`,
	},
	{
		id:      "clearcoat-power-high",
		effect:  "A polished coated surface tightens its highlight differently.",
		value:   clearcoatPowerHigh,
		pattern: `mix\(12\.0, ([0-9.]+), 1\.0 - roughness\)`,
	},
	{
		id:      "clearcoat-gain",
		effect:  "A coated highlight reads about 3.6 times too bright on one backend.",
		value:   clearcoatGain,
		pattern: `pow\(NdotV, clearcoatPower\) \* clearcoat \* ([0-9.]+)\)`,
	},
	{
		id:      "sheen-gain",
		effect:  "A fabric loses or gains brightness at its silhouette.",
		value:   sheenGain,
		pattern: `color = color \+ baseColor \* velvet \* ([0-9.]+);`,
	},
	{
		id:      "iridescence-phase-green",
		effect:  "An iridescent surface shows a different hue at the same angle.",
		value:   iridescencePhaseGreen,
		pattern: `cos\(vec3<f32>\(0\.0, ([0-9.]+), 4\.2\)`,
	},
	{
		id:      "iridescence-phase-blue",
		effect:  "An iridescent surface shows a different hue at the same angle.",
		value:   iridescencePhaseBlue,
		pattern: `cos\(vec3<f32>\(0\.0, 2\.1, ([0-9.]+)\)`,
	},
	{
		id:      "iridescence-frequency",
		effect:  "The iridescent bands sit closer together or further apart.",
		value:   iridescenceFrequency,
		pattern: `\+ NdotV \* ([0-9.]+)\)`,
	},
	{
		id:      "iridescence-tint-base",
		effect:  "An iridescent surface darkens or brightens overall.",
		value:   iridescenceTintBase,
		pattern: `color \* \(vec3<f32>\(([0-9.]+)\) \+ iri \* [0-9.]+\)`,
	},
	{
		id:      "iridescence-tint-gain",
		effect:  "The iridescent hue swing gets deeper or shallower.",
		value:   iridescenceTintGain,
		pattern: `color \* \(vec3<f32>\([0-9.]+\) \+ iri \* ([0-9.]+)\)`,
	},
	{
		id:      "transmission-base-gain",
		effect:  "Glass keeps more or less of its own colour.",
		value:   transmissionBaseGain,
		pattern: `mix\(color, ambient \+ baseColor \* ([0-9.]+), transmission \* [0-9.]+\)`,
	},
	{
		id:      "transmission-mix-gain",
		effect:  "Glass reads more or less opaque.",
		value:   transmissionMixGain,
		pattern: `mix\(color, ambient \+ baseColor \* [0-9.]+, transmission \* ([0-9.]+)\)`,
	},
	{
		id:      "env-specular-rough-fade",
		effect:  "A rough surface keeps too much or too little of its cubemap reflection.",
		value:   envSpecularRoughFade,
		pattern: `\* F \* \(1\.0 - roughness \* ([0-9.]+)\)`,
	},
	{
		id:      "range-window-exponent",
		effect:  "A ranged point or spot light fades over a different distance, so its pool of light changes size.",
		value:   rangeWindowExponent,
		pattern: `pow\(dist / range, ([0-9.]+)\)`,
	},
	{
		id:      "attenuation-floor",
		effect:  "A light at the surface divides by zero on one backend and clamps on the other.",
		value:   attenuationFloor,
		pattern: `max\(dist \* dist, ([0-9.]+)\)`,
	},
	{
		id:      "spot-cone-floor",
		effect:  "A spot light with no penumbra band divides by zero on one backend.",
		value:   spotConeFloor,
		pattern: `max\(innerCos - outerCos, ([0-9.]+)\)`,
	},
}

// TestLitProgramConstantsMatchTheShader pins every number litProgram.shade shares
// with litWGSL.
//
// A PASS PROVES: each pinned number still appears in the shader with the value
// this package computes with. An edit to one copy alone fails here.
//
// A PASS DOES NOT PROVE: that the two produce the same pixel. They cannot. The
// shader writes a high dynamic range target and a tone map pass follows it; this
// package writes bytes with no tone map. It also proves nothing about a term with
// no row, so add a row when you add a term.
//
// TestLitProgramConstantGuardsDetectMutation proves this test can fail.
func TestLitProgramConstantsMatchTheShader(t *testing.T) {
	shader := readLitShader(t)
	for _, problem := range checkLitConstants(litSharedConstants, shader) {
		t.Error(problem)
	}
}

// TestLitProgramStructureMatchesTheShader pins the shape of the terms, not their
// numbers. A number can agree while the expression around it moved.
func TestLitProgramStructureMatchesTheShader(t *testing.T) {
	shader := readLitShader(t)
	for _, row := range []struct {
		id     string
		effect string
		needle string
	}{
		{
			id:     "roughness-map-channel",
			effect: "A packed glTF texture drives roughness from the wrong channel.",
			needle: "let roughSample = textureSample(roughnessMapTex, baseColorSampler, in.uv).g;",
		},
		{
			id:     "metalness-map-channel",
			effect: "A packed glTF texture drives metalness from the wrong channel.",
			needle: "let metalSample = textureSample(metalnessMapTex, baseColorSampler, in.uv).b;",
		},
		{
			id:     "ambient-three-term-sum",
			effect: "One intensity gates a colour it does not own, so the scene reads darker.",
			needle: "let envDiffuse = scene.ambientColor.rgb * scene.ambientColor.a",
		},
		{
			id:     "ambient-scaled-by-base-colour",
			effect: "Every shadowed face turns grey instead of keeping its colour.",
			needle: "let ambient = envDiffuse * baseColor;",
		},
		{
			id:     "hemisphere-normal-blend",
			effect: "Sky and ground ambient swap or skew across the normal range.",
			needle: "let hemi = N.y * 0.5 + 0.5;",
		},
		{
			id:     "direct-light-composition",
			effect: "Direct light stops scaling by the cosine term or by the shadow factor.",
			needle: "let direct = (diffuse + specular) * radiance * NdotL * shadow;",
		},
		{
			id:     "energy-conserving-diffuse",
			effect: "A metal picks up a diffuse term, or a dielectric loses one.",
			needle: "let kD = (vec3<f32>(1.0) - kS) * (1.0 - metalness);",
		},
		{
			id:     "lambert-diffuse-normalization",
			effect: "Diffuse brightness shifts by a factor of pi.",
			needle: "let diffuse = kD * baseColor / 3.141592653589793;",
		},
		{
			id:     "specular-is-d-times-g-times-f",
			effect: "The specular lobe gains or loses the 1/(4 NdotL NdotV) factor.",
			needle: "let specular = D * G * F;",
		},
		{
			id:     "correlated-smith-visibility",
			effect: "Grazing highlights change brightness by up to a factor of twelve.",
			needle: "return 0.5 / max(ggxV + ggxL, 1e-5);",
		},
		{
			id:     "emissive-map-replaces-colour",
			effect: "An emissive map tints the base colour instead of replacing it.",
			needle: "let emissiveColor = mix(baseColor, emissiveSample, hasEmissiveMap);",
		},
		{
			id:     "occlusion-map-scales-indirect-only",
			effect: "AO stops darkening the indirect terms, or starts dimming direct light too.",
			needle: "var color = direct + (ambient + cubeIBL) * ao + emissive;",
		},
		// The rows below arrived with the runtime light array on 2026-07-27.
		// litProgram.directLight is the CPU copy of that loop.
		{
			id:     "light-count-bounded-by-both-the-count-and-the-array",
			effect: "The loop reads the zero records past the live count, so a black ambient light joins every frame.",
			needle: "let lightCount = min(u32(max(scene.lightParams.x, 0.0)), arrayLength(&lights));",
		},
		{
			id:     "ambient-light-is-a-flat-product",
			effect: "An ambient light gains a cosine term or a specular lobe, so a face turned away goes dark.",
			needle: "directSum = directSum + baseColor * lightColor * intensity;",
		},
		{
			id:     "hemisphere-light-blends-ground-toward-sky",
			effect: "A hemisphere light puts the ground colour on the top of a sphere.",
			needle: "let hemiColor = mix(light.groundPenumbra.rgb, lightColor, hemiBlend);",
		},
		{
			id:     "hemisphere-light-normal-blend",
			effect: "The sky and ground split moves off the horizon.",
			needle: "let hemiBlend = N.y * 0.5 + 0.5;",
		},
		{
			id:     "point-light-windowed-falloff",
			effect: "A ranged light stops fading to nothing at its range, so it lights the whole scene.",
			needle: "return ratio * ratio / max(dist * dist, 0.0001);",
		},
		{
			id:     "point-light-inverse-power-falloff",
			effect: "A light with no range stops obeying its decay, so distance no longer dims it.",
			needle: "return 1.0 / max(pow(dist, decay), 0.0001);",
		},
		{
			id:     "spot-cone-attenuation",
			effect: "A spot light lights the whole scene, or nothing at all.",
			needle: "return clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.001), 0.0, 1.0);",
		},
		{
			id:     "spot-inner-cone-from-penumbra",
			effect: "A spot edge turns hard when the author asked for a soft one.",
			needle: "let innerCos = cos(angle * (1.0 - penumbra));",
		},
		{
			id:     "falloff-rides-in-the-radiance",
			effect: "The distance falloff lands outside the direct term, so it also scales the shadow or the cosine.",
			needle: "let radiance = lightColor * intensity * attenuation;",
		},
		{
			id:     "one-shadow-map-on-the-primary-directional-light",
			effect: "A second light reads a shadow map fitted to a different light, so its shadow sits in the wrong place.",
			needle: "if (kind == 1u && i32(i) == shadowLightIndex) {",
		},
		{
			id:     "rect-area-light-contributes-nothing",
			effect: "A rect-area light gets approximated as a point light, which is a shape the author never asked for.",
			needle: "if (kind == 5u) {",
		},
		{
			id:     "direct-names-the-summed-light",
			effect: "The direct term stops being the sum over the light array.",
			needle: "let direct = directSum;",
		},
	} {
		if strings.Contains(shader, row.needle) {
			continue
		}
		t.Errorf("row %q: %s no longer contains:\n  %s\nEffect: %s\n"+
			"litProgram.shade in device.go implements this term. Change both copies or neither.",
			row.id, litShaderPath, row.needle, row.effect)
	}
}

// checkLitConstants returns one problem per row whose number no longer agrees.
//
// The check lives apart from the test function so the mutation self test can feed
// it a mutated shader and prove the guard fires.
func checkLitConstants(rows []litConstantRow, shader string) []string {
	var problems []string
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.id] {
			problems = append(problems, fmt.Sprintf("the constant table lists id %q twice", row.id))
			continue
		}
		seen[row.id] = true
		re, err := regexp.Compile(row.pattern)
		if err != nil {
			problems = append(problems, fmt.Sprintf("row %q: pattern %s does not compile: %v", row.id, row.pattern, err))
			continue
		}
		match := re.FindStringSubmatch(shader)
		if match == nil {
			problems = append(problems, fmt.Sprintf(
				"row %q: %s no longer holds an expression matching %s.\nEffect: %s\n"+
					"The shader term was renamed, reshaped or deleted. Update this row with the shader change.",
				row.id, litShaderPath, row.pattern, row.effect))
			continue
		}
		got, err := strconv.ParseFloat(match[1], 32)
		if err != nil {
			problems = append(problems, fmt.Sprintf("row %q captured %q, which is not a number", row.id, match[1]))
			continue
		}
		if float32(got) != row.value {
			problems = append(problems, fmt.Sprintf(
				"row %q: %s carries %g and this package computes with %g.\nEffect: %s\n"+
					"Both copies shade the same engine.RenderMaterial, so change both or neither.",
				row.id, litShaderPath, got, row.value, row.effect))
		}
	}
	return problems
}

// TestLitProgramConstantGuardsDetectMutation proves the constant guard can fail.
//
// It edits a copy of the shader held in memory, one number at a time, and
// requires the guard to report a problem that names the affected row. No file
// changes.
func TestLitProgramConstantGuardsDetectMutation(t *testing.T) {
	shader := readLitShader(t)
	if problems := checkLitConstants(litSharedConstants, shader); len(problems) != 0 {
		t.Fatalf("the constant table must pass on the shipped shader before the mutation check means anything; got:\n%s",
			strings.Join(problems, "\n"))
	}
	for _, mutation := range []struct {
		name    string
		from    string
		to      string
		wantRow string
	}{
		{
			name:    "the shader raises the dielectric F0",
			from:    "mix(vec3<f32>(0.04), baseColor, metalness)",
			to:      "mix(vec3<f32>(0.08), baseColor, metalness)",
			wantRow: "dielectric-f0",
		},
		{
			name:    "the shader widens the clear coat power range",
			from:    "mix(12.0, 96.0, 1.0 - roughness)",
			to:      "mix(12.0, 128.0, 1.0 - roughness)",
			wantRow: "clearcoat-power-high",
		},
		{
			name:    "the shader lowers the sheen gain",
			from:    "color = color + baseColor * velvet * 0.55;",
			to:      "color = color + baseColor * velvet * 0.25;",
			wantRow: "sheen-gain",
		},
		{
			name:    "the shader moves the iridescence frequency",
			from:    "NdotV * 8.0)",
			to:      "NdotV * 6.2831853)",
			wantRow: "iridescence-frequency",
		},
		{
			name:    "the shader lowers the transmission blend",
			from:    "transmission * 0.55)",
			to:      "transmission * 0.35)",
			wantRow: "transmission-mix-gain",
		},
		{
			name:    "the shader deletes the anisotropy narrowing",
			from:    "clamp(roughness * (1.0 - abs(anisotropy) * 0.28), 0.04, 1.0)",
			to:      "clamp(roughness, 0.04, 1.0)",
			wantRow: "anisotropy-roughness-gain",
		},
		{
			name:    "the shader changes the range window exponent",
			from:    "pow(dist / range, 4.0)",
			to:      "pow(dist / range, 2.0)",
			wantRow: "range-window-exponent",
		},
		{
			name:    "the shader raises the attenuation floor",
			from:    "max(dist * dist, 0.0001)",
			to:      "max(dist * dist, 0.01)",
			wantRow: "attenuation-floor",
		},
		{
			name:    "the shader raises the spot cone floor",
			from:    "max(innerCos - outerCos, 0.001)",
			to:      "max(innerCos - outerCos, 0.01)",
			wantRow: "spot-cone-floor",
		},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			if !strings.Contains(shader, mutation.from) {
				t.Fatalf("mutation cannot apply: %s does not contain %q", litShaderPath, mutation.from)
			}
			problems := checkLitConstants(litSharedConstants,
				strings.Replace(shader, mutation.from, mutation.to, 1))
			if len(problems) == 0 {
				t.Fatalf("the mutation produced no problem; the guard does not cover row %q", mutation.wantRow)
			}
			for _, problem := range problems {
				if strings.Contains(problem, mutation.wantRow) {
					return
				}
			}
			t.Fatalf("the mutation produced problems, but none names row %q:\n%s",
				mutation.wantRow, strings.Join(problems, "\n"))
		})
	}
}

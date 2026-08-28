package ibl

// # Which record is authoritative
//
// Three places name the split-sum convention:
//
//  1. BRDFModel, the Go constant in this package.
//  2. The "brdfModel" field of the .ibl.json sidecar.
//  3. The "GoSXiblModel" key/value entry of every generated KTX2 container.
//
// BRDFModel is authoritative. The other two are copies the executor writes from
// it, and TestIBLSidecarPinsBRDFModel in the assetpipe package asserts that the
// copies agree with the source. A consumer that reads the sidecar or the
// container key is reading a copy, and a mismatch is a build bug, not a
// convention change.
//
// A consumer must not infer the convention from the lookup table's values. Two
// different Smith geometry terms produce tables that differ by a few percent,
// which looks like noise and shades wrong at grazing angles.

// ConsumerRequirement is one piece a renderer needs before it can claim image
// based lighting. The list exists so the gap is reported, not assumed.
type ConsumerRequirement struct {
	// ID is a stable identifier for a tracker or a report.
	ID string `json:"id"`
	// Title is one line naming the piece.
	Title string `json:"title"`
	// Detail says what the piece must do.
	Detail string `json:"detail"`
	// Where names the file that must change.
	Where string `json:"where"`
	// Present is true when the shipped runtime already has the piece.
	Present bool `json:"present"`
}

// ConsumerRequirements lists everything a correct image-based-lighting consumer
// needs for the products this package bakes.
//
// The IR carrier and both renderer consumers are present. WebGL2 activates the
// full path when at least 18 fragment texture units are available and reports a
// deterministic degradation otherwise; the unconditional FeatureIBL matrix
// cell therefore remains false even though the consumer pieces below exist.
//
// The build side is complete: the specular cubemap, the diffuse irradiance
// cubemap, the spherical-harmonic sidecar, and the split-sum lookup table all
// ship as KTX2 files with the convention recorded in three places.
func ConsumerRequirements() []ConsumerRequirement {
	return []ConsumerRequirement{
		{
			ID:    "ktx2-reader-js",
			Title: "a JavaScript KTX2 reader for uncompressed containers",
			Detail: "Read the 12-byte identifier, the 68-byte header, and the 24-byte-per-level " +
				"index. Take vkFormat, pixelWidth, faceCount, levelCount, and " +
				"supercompressionScheme from the header, then slice each level payload by " +
				"byteOffset and byteLength. Inflate scheme 3 with DecompressionStream(\"deflate\"). " +
				"Read the key/value block to check GoSXiblRole, GoSXColorSpace and GoSXiblModel for the formats this " +
				"pipeline writes: R16G16B16A16_SFLOAT and R16G16_SFLOAT.",
			Where:   "client/js/bootstrap-src/19a-scene-ktx2.ts",
			Present: true,
		},
		{
			ID:    "cube-upload",
			Title: "a cube texture upload with the full mip chain",
			Detail: "The specular container holds 6 faces and one mip level per roughness step, " +
				"stored smallest level first inside the file but indexed level 0 first. On WebGL2 " +
				"bind TEXTURE_CUBE_MAP, upload each face at each level with internalFormat RGBA16F, " +
				"format RGBA, type HALF_FLOAT, and set TEXTURE_MIN_FILTER to LINEAR_MIPMAP_LINEAR. " +
				"Require OES_texture_float_linear before enabling linear half-float sampling. " +
				"KTX2 face order is +X, -X, +Y, -Y, +Z, -Z, which matches the GL cube face enums in " +
				"order. Upload the irradiance cube the same way with one level.",
			Where:   "client/js/bootstrap-src/19a-scene-ktx2.ts, 16-scene-webgl.js and 16a-scene-webgpu.js",
			Present: true,
		},
		{
			ID:    "brdf-lut-upload",
			Title: "a two-channel split-sum lookup table upload",
			Detail: "The table is RG16F, 256 by 256 by default, one level. X is NdotV and Y is " +
				"roughness, both sampled at texel centres, so the shader must sample with " +
				"CLAMP_TO_EDGE and LINEAR filtering and must not offset the coordinates. The table " +
				"is scene independent, so one upload serves every page in a project.",
			Where:   "client/js/bootstrap-src/19a-scene-ktx2.ts, 16-scene-webgl.js and 16a-scene-webgpu.js",
			Present: true,
		},
		{
			ID:    "shader-combine",
			Title: "the split-sum combine in the fragment shader",
			Detail: "Replace the two equirectangular taps with:\n" +
				"  prefiltered = textureCubeLod(specular, R, roughness * (levels - 1)).rgb\n" +
				"  ab          = texture(brdfLUT, vec2(NdotV, roughness)).rg\n" +
				"  specularIBL = prefiltered * (F0 * ab.x + ab.y)\n" +
				"  diffuseIBL  = texture(irradianceCube, N).rgb * albedo * kD\n" +
				"The roughness-to-level mapping must match roughnessPerLevel in the sidecar, which " +
				"is level / (levels - 1). The current code multiplies the specular tap by " +
				"(1 - roughness * 0.65), an ad hoc falloff with no derivation; that term goes away.",
			Where:   "client/js/bootstrap-src/16-scene-webgl.js and 16a-scene-webgpu.js",
			Present: true,
		},
		{
			ID:    "ir-carrier",
			Title: "an IR carrier for the generated products",
			Detail: "scene.EnvironmentIBL now carries radiance, irradiance and BRDF-LUT texture " +
				"descriptors, mip levels, the BRDF model, roughness mapping, and spherical " +
				"harmonics through compatibility and canonical SceneIR. Renderer binding is " +
				"tracked by the other present requirements.",
			Where:   "scene/texture_contract.go, scene/ir.go, and scene/scene_ir.go",
			Present: true,
		},
	}
}

// BRDFConvention states the exact convention a shader must match. Every field is
// a claim a test can check, and the string form is BRDFModel.
type BRDFConvention struct {
	Distribution string `json:"distribution"`
	Alpha        string `json:"alpha"`
	Geometry     string `json:"geometry"`
	GeometryK    string `json:"geometryK"`
	Fresnel      string `json:"fresnel"`
	LUTAxisX     string `json:"lutAxisX"`
	LUTAxisY     string `json:"lutAxisY"`
	LUTSampling  string `json:"lutSampling"`
	Combine      string `json:"combine"`
}

// Convention returns the pinned convention. Do not change a field without
// changing BRDFModel, because a consumer keys off the string.
func Convention() BRDFConvention {
	return BRDFConvention{
		Distribution: "GGX (Trowbridge-Reitz)",
		Alpha:        "alpha = roughness * roughness",
		Geometry:     "Smith-Schlick",
		GeometryK:    "k = alpha / 2, the image-based-lighting value, not (roughness+1)^2/8",
		Fresnel:      "Schlick, factored so F = F0 * A + B",
		LUTAxisX:     "NdotV, texel centre: NdotV = (x + 0.5) / size",
		LUTAxisY:     "roughness, texel centre: roughness = (y + 0.5) / size",
		LUTSampling:  "CLAMP_TO_EDGE, LINEAR, no coordinate offset",
		Combine:      "specular = prefiltered * (F0 * A + B)",
	}
}

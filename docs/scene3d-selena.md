# Selena materials in Scene3D

Selena is the shader language for custom Scene3D looks. You write a material
once in a `.sel` file; the Selena compiler emits WGSL for WebGPU, GLSL ES 3.00
for WebGL2, and a binding layout the renderers wire uniforms and textures
from. This page covers the two ways to get a compiled material into a scene,
surface materials that the standard lighting shades, and how a full Selena
material reads the scene's lights, shadows and fog.

A Selena material is one of two kinds:

| Kind | Source | Lighting |
|---|---|---|
| Surface material | `surface(geo) -> Surface` | The renderer's standard lighting, exactly as for a `StandardMaterial`: every light, shadows, image-based lighting, fog, exposure and tone mapping. Start here for a custom look on lit geometry. |
| Full material | `surface(geo) -> color` | The material's own; it replaces the whole shader. Use it for unlit effects and stylized lighting. |

## Compile at build time, load without the compiler

Browser code should load Selena materials from artifact files. An artifact
file (`rock.sel.json` next to `rock.sel`) holds the binding layout and the
shaders for every material in the source, the source's SHA-256, and the Selena
version that wrote it. Loading one links no compiler, so a js/wasm build stays
small: a test program that imports `scene` measures 4.10 MB raw (0.85 MB
brotli) with an artifact loaded, against 20.5 MB raw (3.62 MB brotli) when it
calls `CompileSelenaMaterial` (go1.26, GOOS=js GOARCH=wasm).

1. Put the source in a Go package directory, for example `materials/rock.sel`.
2. Run `gosx build`, or `gosx selena build`. Either writes
   `materials/rock.sel.json`. `gosx build` runs this step before it compiles Go.
3. Embed the artifact and build the material:

```go
package materials

import (
	_ "embed"

	"m31labs.dev/gosx/scene"
)

//go:embed rock.sel.json
var rockArtifact []byte

func Rock() (scene.CustomMaterial, error) {
	file, err := scene.DecodeSelenaArtifact(rockArtifact)
	if err != nil {
		return scene.CustomMaterial{}, err
	}
	material, _, err := scene.MaterialFromSelenaArtifact(file, scene.SelenaMaterialOptions{
		Material: "Rock", // empty selects the last material in the file
		Uniforms: map[string]any{"albedo": []float64{0.5, 0.45, 0.4}},
	})
	return material, err
}
```

`scene.SelenaBundleFromArtifact(file, opts...)` returns several named
materials from one file, like `CompileSelenaBundle`.

4. Commit the `.sel.json` files, and run `gosx selena check` in CI. It fails
   when an artifact is missing, when its source changed, when it was built with
   options other than the config's, or when the linked compiler no longer emits
   the same layout and shaders. A Go test can run the same check:

```go
func TestSelenaArtifacts(t *testing.T) {
	if err := selenabuild.Check(".", selenabuild.Options{Modules: []string{"lighting"}}); err != nil {
		t.Fatal(err)
	}
}
```

`selenabuild` (`m31labs.dev/gosx/scene/selenabuild`) links the compiler; import
it only from tools and tests.

### Configuration

`build.selena` in `gosx.config.json` controls the step:

```json
{
  "build": {
    "selena": {
      "targets": ["wgsl", "gles"],
      "modules": ["lighting"],
      "specularMRT": false
    }
  }
}
```

| Field | Default | Meaning |
|---|---|---|
| `targets` | `["wgsl", "gles"]` | Shader targets to emit. Add `"glsl"` (WebGL 1) or `"metal"` to ship them. |
| `modules` | none | Selena material library modules linked into every source, such as `"lighting"` or `"brdf"`. An unused module changes nothing. |
| `specularMRT` | `false` | Adds the color/specular fragment variant for selective bloom. A material that asks for `SpecularMRT` needs it. |
| `disabled` | `false` | Skips the step in `gosx build`; `gosx selena check` still runs. |

The step finds every `.sel` file under the project, except in directories Go
ignores (names starting with `.` or `_`, and `testdata`), `vendor`,
`node_modules`, `dist`, and nested Go modules. A file that declares only
functions is a library and gets no artifact.

The Selena CLI writes the same files: `selena artifact -targets wgsl,gles
-modules lighting materials/rock.sel`, and `selena artifact -check` checks one.

## Compile at run time

Native and server code can still compile source directly:

```go
material, layout, err := scene.CompileSelenaMaterial(source, scene.SelenaMaterialOptions{
	Modules: []string{"lighting"},
})
```

`CompileSelenaMaterial`, `CompileSelenaBundle`, `CompileSelenaPoints`,
`CompileSelenaParticleRender` and `CompileSelenaPost` are not part of js/wasm
builds, because they link the Selena parser, the tree-sitter runtime and the
emitters. A browser build that must compile at run time can opt back in with
`-tags gosx_selena_compiler`. The artifact path and run-time compilation give
equal materials for equal source and options; `scene` tests check that.

## Surface materials

A surface material returns a typed `Surface` record instead of a color. Both
renderers splice it into their standard PBR shader in place of the
StandardMaterial's map and factor reads, so the record is lit by the same code:
a surface material configured like a StandardMaterial renders the same pixels
(the real-GPU test below measures a largest difference of 1/255).

```selena
material WetRock {
    param albedo : texture2d
    param wet : float = 0.0

    surface(geo) -> Surface {
        let s = pbr(sample(albedo, geo.uv).rgb, 0.85, 0.0)
        return withRoughness(s, mix(s.roughness, 0.2, wet))
    }
}
```

```go
material, _, err := scene.MaterialFromSelenaArtifact(file, scene.SelenaMaterialOptions{
	Uniforms: map[string]any{
		"albedo": scene.SelenaTexture{URL: "/rock.png", ColorSpace: scene.TextureColorSpaceSRGB},
		"wet":    0.3,
	},
})
```

`pbr(baseColor, roughness, metallic)` builds the record. `withBaseColor`,
`withAlpha`, `withNormal`, `withRoughness`, `withMetallic`, `withOcclusion`,
`withEmissive`, `withSheen`, `withTransmission` and `withClearcoat(s, amount,
roughness)` replace fields; `mixSurface(a, b, t)` blends two records; `s.field`
reads one. Selena's
[interop page](https://github.com/M31-Labs/selena/blob/main/docs/standard-material-interop.md)
lists every field and rule.

| `geo` field | Meaning |
|---|---|
| `geo.worldPos`, `geo.worldNormal` | World position and the unit geometric normal. |
| `geo.uv` | First UV set. |
| `geo.tangent`, `geo.bitangent` | Mesh tangent frame, not normalized; zero when the mesh has no tangents. |
| `geo.viewDir` | Unit vector toward the camera. |
| `geo.color` | Instance or vertex color; the renderer already multiplies the base color by its rgb. |

Textures:

- Bind each `texture2d` param with a `scene.SelenaTexture` in `Uniforms`. Set
  `ColorSpace: scene.TextureColorSpaceSRGB` for color images (base color,
  emissive); data maps (normal, roughness, occlusion, height, masks) keep the
  linear default. A plain URL string also works and samples as linear data.
- Textures repeat (REPEAT wrap), so tiling and triplanar projection work.
- A surface material samples at most six textures. The renderers bind them in
  the six standard material map slots, so they cost no extra texture units.

`opts.Standard` still supplies the state the record does not: blend mode,
alpha cutoff, opacity, IOR and specular factors, rim light, wireframe. Its map
fields (`Texture`, `NormalMap`, ...) are rejected; declare `texture2d` params
instead.

Limits today:

- Meshes, models and skinned meshes draw surface materials on both renderers.
  Instanced meshes still draw them with the standard material.
- WebGL2 binds the occlusion slot (the sixth texture) only where the context
  supports half-float image-based lighting, which every desktop GPU does.
- No `vertex()` hook, engine textures, early `return`, or WebGL 1 and Metal
  targets; Selena reports each at compile time.

### Authoring helpers

Link the Selena `surface` module (`"modules": ["surface"]` in
`build.selena`, or `SelenaMaterialOptions.Modules`) for the helpers a
sculpted-asset game needs:

| Need | Helpers |
|---|---|
| A UV-mapped PBR texture set (base color, normal map, glTF ORM map) | `mlPBRSet`, `mlSurfaceFromSet`, `mlUnpackNormal`, `mlTangentToWorld` |
| Seamless projection for sculpts without clean UVs | `mlTriplanar`, `mlTriplanarNormal`, `mlTriplanarSet`, `mlTriplanarWeights` |
| Rock, moss and sand layers | `mlSlopeMask`, `mlHeightMask`, `mlHeightBlend`, `mlLayer` |
| Detail normals | `mlBlendNormalsRNM`, `mlBlendNormalsUDN`, `mlDetailNormal`, `mlDetailWorldNormal` |

The module reference is Selena's
[material-library/surface.md](https://github.com/M31-Labs/selena/blob/main/docs/material-library/surface.md).

### Example: a sculpted rock

A boulder sculpted in Blender and exported as glTF rarely has clean UVs. This
material projects a rock texture set triplanar, adds a finer detail normal,
grows moss on upward faces into the cracks first, blends sand around the base
and darkens a wet band at the water line. It uses all six texture slots and
reads 21 texels per pixel.

```selena
material SculptedRock {
    param rockAlbedo : texture2d
    param rockNormal : texture2d
    param rockORM    : texture2d
    param mossAlbedo : texture2d
    param mossNormal : texture2d
    param sandAlbedo : texture2d
    param scale      : float = 0.5
    param mossCover  : float = 0.6
    param sandLine   : float = 0.4
    param waterLine  : float = 0.0

    surface(geo) -> Surface {
        let n = normalize(geo.worldNormal)
        let rockOrm = mlTriplanar(rockORM, geo.worldPos, n, scale, 4.0)
        let rockN = mlTriplanarNormal(rockNormal, geo.worldPos, n, scale, 4.0, 1.0)
        let detailN = mlTriplanarNormal(rockNormal, geo.worldPos, n, scale * 6.0, 4.0, 0.5)
        let rock = mlSurfaceFromSet(mlTriplanar(rockAlbedo, geo.worldPos, n, scale, 4.0), mlDetailWorldNormal(rockN, detailN, n, 0.6), rockOrm.rgb)

        let mossC = mlTriplanar(mossAlbedo, geo.worldPos, n, scale * 2.0, 4.0)
        let moss = withNormal(pbr(mossC.rgb, 0.9, 0.0), mlTriplanarNormal(mossNormal, geo.worldPos, n, scale * 2.0, 4.0, 0.7))
        let mossWeight = mlSlopeMask(n, vec3f(0.0, 1.0, 0.0), 0.55, 0.85) * mossCover
        let mossy = mlLayer(rock, moss, rockOrm.a, mossC.a, mossWeight, 0.08)

        let sand = pbr(mlTriplanar(sandAlbedo, geo.worldPos, n, scale * 3.0, 2.0).rgb, 0.95, 0.0)
        let sandy = mixSurface(mossy, sand, 1.0 - mlHeightMask(geo.worldPos.y, sandLine - 0.3, sandLine))

        let wet = 1.0 - mlHeightMask(geo.worldPos.y, waterLine, waterLine + 0.25)
        return withRoughness(withBaseColor(sandy, sandy.baseColor * (1.0 - 0.45 * wet)), mix(sandy.roughness, 0.15, wet))
    }
}
```

`rockORM` packs occlusion (r), roughness (g), metallic (b) and height (a);
`mossAlbedo` carries the moss height in alpha. In Go:

```go
srgb := func(url string) scene.SelenaTexture {
	return scene.SelenaTexture{URL: url, ColorSpace: scene.TextureColorSpaceSRGB}
}
rock, _, err := scene.MaterialFromSelenaArtifact(file, scene.SelenaMaterialOptions{
	Uniforms: map[string]any{
		"rockAlbedo": srgb("/textures/rock_albedo.png"),
		"rockNormal": "/textures/rock_normal.png",
		"rockORM":    "/textures/rock_orm.png",
		"mossAlbedo": srgb("/textures/moss_albedo.png"),
		"mossNormal": "/textures/moss_normal.png",
		"sandAlbedo": srgb("/textures/sand_albedo.png"),
		"waterLine":  0.1,
	},
})
```

### Standard lighting energy

The standard lighting that both StandardMaterial and surface materials use
conserves energy across roughness:

- Multiple scattering for the GGX lobe (Fdez-Agüera 2019). Image light adds
  the energy single-scattering microfacets lose, and direct lights scale the
  lobe by `1 + F0 (1/Ess - 1)`. A white, fully rough, fully metallic sphere
  under uniform white image light now reflects all of it (the white-furnace
  test below). Single scattering alone returns 1 − ln 2 = 0.31 of the light
  at roughness 1 facing the camera, so rough metals look up to about three
  times brighter than before this change; smooth materials barely change.
- Clear coat is a layer: its own GGX lobe with F0 0.04, over a base attenuated
  by the coat's Fresnel, for direct and image light. It replaces the earlier
  additive highlight, so clear-coated materials look different. A
  StandardMaterial's coat roughness is 0.04 (glTF has no coat-roughness field
  in GoSX yet); a surface material sets it with `withClearcoat`.

### Tests

`e2e/selena_surface_e2e_test.go` runs on the real-GPU runners:

```sh
/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- env GOSX_SELENA_SURFACE_WEBGPU=1 go test -tags e2e -timeout 25m -run 'TestSelenaSurface|TestStandardLightingWhiteFurnace' ./e2e/
```

`TestSelenaSurfaceMatchesStandardMaterial` renders one scene (sky image
light, a shadowing directional light, a point and a spot light, fog, a texture
and a clear coat) with a StandardMaterial and with its surface twin, and
compares the pixels. `TestStandardLightingWhiteFurnace` is the furnace check
above, for both material kinds.

## Scene lights, shadows and fog

A full Selena mesh material reads the renderer's lighting by declaring engine
inputs in its `context` block. Both renderers fill them every frame:

| Input | What the renderer writes |
|---|---|
| `cameraPos`, `cameraForward` | Camera position and view direction (world). |
| `sunDir`, `sunColor` | Toward the first directional light; its color times intensity. |
| `ambient` | Environment ambient color times intensity. |
| `fog` | Fog color in xyz, density in w (0 is off). The renderers' fog is `exp(-(density * distance)^2)`. |
| `lightCount`, `lights` | Up to 8 lights, 4 vec4 each: ambient, directional, point, spot and hemisphere. Rect-area lights are not packed; light probes pack as ambient. |
| `shadow0`, `shadow1` | Per shadow slot: cascade count, bias, filter radius in shadow-map uv (`(1 + 8 * shadowSoftness) / mapSize`), and the casting light's index in `lights`. |
| `shadowSplits0/1`, `shadowMatrices0/1` | Cascade far splits and world-to-shadow-map matrices. |
| `shadowMap0`, `shadowMap1` | The shadow depth maps, one layer per cascade. |

The Selena [lighting module](https://github.com/M31-Labs/selena/blob/main/docs/material-library/lighting.md)
does the per-light math; link it with `"modules": ["lighting"]`. Selena's
`docs/engine-inputs.md#scene-lighting` documents the exact packing. A minimal
lit material:

```selena
material Rock {
    context {
        cameraPos       : vec3
        cameraForward   : vec3
        ambient         : vec3
        fog             : vec4
        lightCount      : float
        lights          : array<vec4, 32>
        shadow0         : vec4
        shadow1         : vec4
        shadowSplits0   : vec4
        shadowSplits1   : vec4
        shadowMatrices0 : array<vec4, 16>
        shadowMatrices1 : array<vec4, 16>
        shadowMap0      : textureDepth2dArray
        shadowMap1      : textureDepth2dArray
    }
    param albedo : color = rgb(0.5, 0.45, 0.4)

    surface(geo) -> color {
        let n = normalize(geo.worldNormal)
        let s0 = mlShadow0(geo.worldPos)
        let s1 = mlShadow1(geo.worldPos)
        var lit = ambient * albedo
        for (var i = 0i; i < 8i; i = i + 1i) {
            lit = lit + albedo * mlLightDiffuse(i, n, geo.worldPos) * mlLightShadow(i, s0, s1)
        }
        return mlFog(lit, geo.worldPos)
    }
}
```

What each renderer provides today:

| | WebGPU | WebGL2 |
|---|---|---|
| Lights, fog, camera, key light | yes | yes |
| Shadow slots | 2 | 2 |
| Cascades per slot | 1 (each slot binds its map as a one-layer array) | up to 4 |
| Shadow casters | directional and spot | directional and spot |

The cascade count is data, not part of the compiled shader, so the same
artifact uses more WebGPU cascades when the renderer provides them. Only
declared names cost anything: a material that declares no lighting inputs
binds nothing extra.

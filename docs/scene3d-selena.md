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

`e2e/selena_surface_e2e_test.go` needs a browser with real WebGPU:

```sh
env GOSX_SELENA_SURFACE_WEBGPU=1 go test -tags e2e -timeout 25m -run 'TestSelenaSurface|TestStandardLightingWhiteFurnace' ./e2e/
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

Pages download the lighting code only when they use it. GoSX advertises a
small on-demand chunk, `bootstrap-feature-scene3d-selena-lighting.js` (about
1.7 KB brotli), only on pages whose Selena materials declare `lights`,
`lightCount`, a shadow input, `fog` or `cameraForward`. The renderer loads it on
its first frame and draws without those inputs until it arrives, usually one
or two frames. Without the chunk, WebGL still supplies `cameraPos`, `sunDir`,
`sunColor` and `ambient` as before, and WebGPU supplies none of them.

## Data textures

A Selena `texture2d` param can hold numbers instead of an image: a spectrum row
per audio frame, a scope trace, a lookup table. The shader reads exact texels
with `load(tex, x, y)`; see the Data Textures section of the Selena language
guide. GoSX lets a Go/WASM engine own the texels and update them while the
scene runs.

```selena
material SpectrogramBand {
    param spectrum : texture2d format r16float
    surface(geo) -> color {
        let sz = size(spectrum)
        let mag = load(spectrum, int(geo.uv.x * sz.x), int(geo.uv.y * sz.y)).r
        return rgb(mag, mag * 0.6, 1.0 - mag)
    }
}
```

**Bind the texture by key.** Set the texture param's uniform to
`scene.DataTextureRef(key)`, which is the string `gosx:data:<key>`:

```go
material, _, err := scene.CompileSelenaMaterial(source, scene.SelenaMaterialOptions{
	Uniforms: map[string]any{"spectrum": scene.DataTextureRef("spectrum")},
})
```

Material creation fails if the param is not a 2D texture or the key is empty,
too long or contains whitespace. Until the engine writes the key, the shader
reads a 1x1 placeholder.

**Write texels from the engine.** `scene.TextureWriter` (js/wasm) sends binary
frames to the Scene3D mount with the given element id:

```go
tw := scene.NewTextureWriter("studio-scene")
if !tw.Prepare() { return } // loads the chunk; call each frame until true

tw.Create("spectrum", 128, 16, scene.TextureR16Float) // 128 wide, 16 tall, zeroed
row := scene.AppendFloat16(nil, magnitudes)           // 128 half floats
applied, err := tw.WriteTexture("spectrum", 128, 1, scene.TextureR16Float, row, 0, 3)
```

- `Create(key, width, height, format)` declares the texture. Creating a key
  again clears it, and replaces it if the size or format changed.
- `WriteTexture(key, width, height, format, bytes, originX, originY)` copies a
  `width` x `height` rectangle whose top-left texel lands at
  `(originX, originY)`. `format` must match the texture. `bytes` holds
  `width*height` texels, top row first, little-endian. Texel `(0,0)` is the one
  `load(tex, 0, 0)` reads.
- `Delete(key)` releases the texture.
- `Prepare` returns false until the renderer can take writes. `Err` says why the
  chunk failed to load, for example because no material on the page names a data
  texture.
- A write returns `(false, nil)` while the renderer is not ready, and an error
  for bad arguments (size, format, byte length, rectangle outside the texture)
  or a frame the renderer rejects.

| Format | Bytes per texel | Pack from `[]float32` |
|---|---|---|
| `TextureRGBA8Unorm` | 4 | write the bytes directly |
| `TextureRGBA16Float` | 8 | `scene.AppendFloat16` |
| `TextureRGBA32Float` | 16 | `scene.AppendFloat32` |
| `TextureR16Float` | 2 | `scene.AppendFloat16` |
| `TextureR32Float` | 4 | `scene.AppendFloat32` |

A texture is at most 8192 x 8192 texels and 256 MiB. Keys are page-wide: two
Scene3D mounts that write the same key share one texture, so give each mount its
own prefix. The material's declared `format` must equal the texture's format
(a param with no `format` means `rgba8unorm`); on a mismatch the renderer logs
the error, fires `gosx:scene3d:data-texture-error` and binds the placeholder.

Call `Prepare` and the writes from the frame callback that also writes poses
(see [engine frame loop](engine-frame-loop.md)): a write applies synchronously
and the renderer draws it in that frame.

**How texels reach the GPU.** Every call becomes one binary `GSXT` frame (not
JSON), copied once into a reused JavaScript buffer and applied synchronously, the
same path `StreamWriter` uses for instance transforms. In an engine worker the
frame is transferred to the page. The renderer keeps one CPU copy per key, then
uploads only the changed rectangle:

| | WebGPU | WebGL2 |
|---|---|---|
| Create | `createTexture` in the matching format | `texImage2D` with `RGBA8`, `RGBA16F`, `RGBA32F`, `R16F` or `R32F` |
| Update | `queue.writeTexture` | `texSubImage2D` with `UNPACK_ROW_LENGTH` |
| Filter | nearest sampler for `sampleMode: "load"` and for 32-bit formats | `NEAREST`, `CLAMP_TO_EDGE` |
| Extensions | none | none (WebGL2 core) |

For a 32-bit float texture the WebGPU bind-group entry is `unfilterable-float`
with a `non-filtering` sampler, as the Selena descriptor requires. A WebGL
context that lacks WebGL2 formats fails visibly instead of drawing wrong
values. `requires.glTexSizeUniforms` (GLSL ES 1.00) is honored when a program
declares the `_texSize_<name>` uniform; the WebGL2 renderer uses the GLES 3.00
target, which reads the size from the texture. Data textures in a custom
post-processing pass are not bound yet.

Pages download the upload code only when they use it. GoSX advertises the
on-demand chunk `bootstrap-feature-scene3d-data-texture.js` (about 2.7 KB
brotli) only on pages with a Selena material that has `sampleMode` `load` or
`both`, an explicit float `format`, or a `gosx:data:` uniform.

## Compile shaders before the first frame

A browser compiles a shader program the first time something draws with it.
Both renderers compile in the background (WebGL2 with
`KHR_parallel_shader_compile`, WebGPU with `createRenderPipelineAsync`), but a
frame still waits until every program it needs is ready. So an object that
appears mid-game with a material no earlier frame used stops the scene from
drawing while its program links. A lit Selena material held the scene for
100 to 273 ms that way on an RTX 5070 Ti and on an Apple M4 Pro, in both
backends.

`Props.ShaderWarmup` moves that work to boot. List the materials and draw
variants the scene uses after its first frame; the first frame then waits
until all of them are compiled, and later first uses find their programs
ready:

```go
props := scene.Props{
    Graph: scene.NewGraph(ground, player),
    ShaderWarmup: &scene.ShaderWarmup{Materials: []scene.WarmupMaterial{
        {Material: lava, CastShadow: true},                 // spawns later
        {Material: hologram},                               // material swap
        {Material: skin, Skinned: true, DoubleSided: true}, // rigged enemies
        {Material: grass, Instances: 4096},                 // instanced scatter
    }},
}
```

Each entry is one material in one variant; a material drawn both static and
skinned needs two entries. Materials already on visible objects compile for
the first frame anyway, so list what is not on screen at boot: spawned and
hidden objects, material swaps, and the skinned, double-sided, shadow-casting
or instanced copies of a material that already shows.

The warm-up code ships in its own small script, which a page fetches only when
a scene sets `ShaderWarmup`. The mount loads it before the first frame, so
pages without a warm-up download nothing extra.

The mount reports progress while it compiles, for a loading screen:

| Attribute or event | Value |
|---|---|
| `data-gosx-scene3d-shaders` | `compiling`, then `ready` |
| `data-gosx-scene3d-shaders-done` | programs compiled so far |
| `data-gosx-scene3d-shaders-total` | programs requested so far |
| `gosx:scene3d:shader-progress` | bubbling event, detail `{done, total, failed, ready, backend}` |

`total` grows while the first frame discovers programs, so show the phase
("preparing shaders") with the ratio as a hint. In a Go game, add the
warm-up to the boot gate; the task finishes when a frame has drawn with every
listed variant:

```go
surface := scene.NewSurface("arena")
gate.Add(boot.CountTask("shaders", 1, surface.ShaderWarmupProgress))
```

With the four lit materials and two standard ones of
`e2e/shader_warmup_hitch_e2e_test.go` listed, each lit material's first use
drew within 18 to 32 ms (one or two frames) on both GPUs and both backends,
down from 100 to 273 ms. The first frame came up to about 220 ms later than
without the warm-up, because the compiling moved into boot.

The first transmission material is a special case: it adds a screen-space
pass and switches the renderer to an HDR target, and on WebGPU every pipeline
then compiles again for that format. With the warm-up alone it still took
about 100 ms on WebGPU and 110 to 120 ms on WebGL on the Apple M4 Pro (20 to
31 ms on the RTX 5070 Ti). Give the scene a post effect from the start, such as tone
mapping, so the post chain and target format exist before the first
transmission object appears; with that, it drew within 21 to 33 ms on both GPUs:

```go
props.PostFX = scene.PostFX{Effects: []scene.PostEffect{scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1}}}
```

A scene without `ShaderWarmup` pays one property read per frame and publishes
nothing. If the renderer is replaced (a WebGPU device loss, or a fallback to
WebGL2), the warm-up runs again on the new renderer.

## Hot reload in gosx dev

`gosx dev` watches `.sel` sources. When you save one, it rebuilds the
artifacts with the `build.selena` options from `gosx.config.json` and swaps
the new shaders into the open page: every Scene3D object whose Selena
material has the rebuilt name gets the new program, and keeps its uniform
values, camera and game state. Nothing reloads and the app does not restart.

- A compile error prints in the terminal and in the browser console, for
  example `[gosx dev] build failed: selena materials/lava.sel: SEL2001 at
  3:16: unknown name "missing"`. The page keeps drawing the last good
  shaders.
- The running app still embeds the artifacts it started with. The next full
  page load through the dev proxy rebuilds and restarts it first, so a
  refresh never shows old shaders.
- A save that changes `.sel` files together with Go or `.gsx` files takes the
  usual path: rebuild, restart and reload.
- `gosx dev` also runs the Selena build step when it starts, like `gosx
  build`. `build.selena.disabled` turns both off, and `.sel` saves then
  reload the page.

Materials match by name (`material Lava` in the source and
`shaderLayout.material` in the scene), so keep material names unique across
the project's `.sel` files. Mesh objects are patched in place; a Selena post
effect or points material has no in-place patch, so editing one reloads the
page.

## Editor support

`gosx lsp` serves `.sel` files with the Selena language server, and `selena
lsp` runs it on its own for editors outside a GoSX project. It provides:

- diagnostics from the Selena compiler as you type, with codes such as
  `SEL2001` and the compiler's hint;
- hover with the type of params, context fields, locals and `geo` fields,
  the registry entry of engine inputs such as `sunDir` or `shadowMap0`, and
  the signature and documentation of builtins and material library helpers;
- completion of builtins, material library helpers, the file's own
  functions, params and locals, `geo.` fields for the material kind, engine
  inputs inside a `context` block, and type names after `:`;
- go-to-definition for locals, params, functions, materials, and material
  library helpers, whose sources it writes to the user cache directory.

A file that calls a material library helper links that helper's module
automatically, so `mlFog` resolves without configuration.

To use it, register the `.sel` extension with your editor's LSP client and
point it at `gosx lsp` (or `selena lsp`). For example, in Neovim:

```lua
vim.filetype.add({ extension = { sel = "selena" } })
vim.lsp.config("selena", { cmd = { "gosx", "lsp" }, filetypes = { "selena" } })
vim.lsp.enable("selena")
```

## Game materials

Selena's material library has a `game` module with helpers and example
materials for common game looks: foliage that sways in the wind, toon shading
with outlines, dissolve, hologram, force field, ice, a water surface and
subsurface skin. Link it with the lighting module:

```json
{ "build": { "selena": { "modules": ["game", "lighting"] } } }
```

The examples live in Selena's `examples/material-library` and the helpers are
listed in Selena's `docs/material-library/game.md`. Some need a draw setting on
the GoSX side:

| Example | Set on the material or mesh |
|---|---|
| `foliage.sel` | draw both faces: export the leaves from Blender with Backface Culling off (glTF `doubleSided`); a `scene.Mesh` built in Go has no double-sided switch yet |
| `hologram.sel`, `force-field.sel` | `Standard: scene.StandardMaterial{BlendMode: scene.BlendAdditive}` in `SelenaMaterialOptions`; for the force field also `DepthWrite: scene.Bool(false)` on the mesh |
| `dissolve.sel` | animate the `progress` uniform from Go |

Foliage reads the scene wind through the `wind` and `windGust` engine inputs,
in the encoding of `scene.Wind`. The renderers do not fill those two inputs
for Selena materials yet, so plants stand still until they do; the shaders
compile and draw either way. Add every game material that appears after the
first frame to `ShaderWarmup`.

## Jersey numbers and nameplates from one texture

A `scene.TextAtlas` is one texture that holds many short labels, such as
jersey numbers, player names or unit tags. The browser draws it with the canvas
2D API when the scene mounts, so a team of 44 players needs one texture and one
material, not 44 textures. Each label gets one cell of a grid; a material picks
its player's cell by UV region.

```go
numbers := scene.TextAtlas{ID: "numbers", CellWidth: 128, CellHeight: 96}
plates := scene.TextAtlas{ID: "plates", CellWidth: 192, CellHeight: 48,
    Background: "#f2d14b", Color: "#111111"}
for _, p := range roster {
    numbers.Labels = append(numbers.Labels, strconv.Itoa(p.Number))
    plates.Labels = append(plates.Labels, p.Name) // "" leaves the plate blank
}
props.TextAtlases = []scene.TextAtlas{numbers, plates}
```

Use `Texture()` as the texture URL and choose the cell per player:

- **Selena materials.** Pass `Texture()` as the `texture2d` param and
  `RegionUniform(i)` as a `vec4` param. The `game` module's `mlAtlasUV`,
  `mlRectLocal` and `mlRectMask` map a part of the mesh's UVs onto the cell;
  Selena's `examples/material-library/jersey.sel` puts a number on a shirt
  this way:

  ```go
  shirt := jersey // one compiled Jersey material
  shirt.Uniforms = maps.Clone(jersey.Uniforms)
  shirt.Uniforms["labels"] = numbers.Texture()
  shirt.Uniforms["labelRect"] = numbers.RegionUniform(i)
  ```

- **Standard materials.** Set `Texture: plates.Texture()` and draw the label on
  `plates.Quad(i, width, height)`, a rectangle whose UVs cover cell `i`. Use
  `BlendMode: scene.BlendAlpha` so a blank cell stays see-through.

Regions use the image convention: (0, 0) is the texture's top-left corner and
v grows downward. glTF models follow it, so `numberRect` in `jersey.sel` is in
the same UV space a Blender export writes. A mesh you build in Go needs UVs in
that convention too.

Things to know:

- Each label shrinks to fit its cell inside `Padding`. An empty label leaves
  its cell transparent. Give the cells the aspect ratio of the area that
  shows them, or the label is stretched and loses detail.
- An atlas may be at most 4096 pixels on a side; `Validate` reports a larger
  one, and `Props` drops it. Use smaller cells or a second atlas.
- The texture URL carries a hash of the labels and style, so changing a label
  makes a new texture, which the renderer loads like a new image.
- The browser draws with the fonts it has when the scene mounts. Load a web
  font before mounting, or the labels use the fallback font.
- `InstancedMesh` has no per-instance UV region, so instanced copies cannot
  pick different cells. Use one `Mesh` per character; the meshes still share
  one shader and one texture.
- The drawing code ships in its own small script, which a page fetches only
  when a scene declares `TextAtlases`; the mount loads it before the first
  frame. Atlases added by a later update load it then and appear a moment
  later.

`TestTextAtlasDrawsDistinctNumbersOnCharacters` in `e2e/` draws 44 characters
this way and checks that every jersey shows a different number.

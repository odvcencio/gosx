# Selena materials in Scene3D

Selena is the shader language for custom Scene3D looks. You write a material
once in a `.sel` file; the Selena compiler emits WGSL for WebGPU, GLSL ES 3.00
for WebGL2, and a binding layout the renderers wire uniforms and textures
from. This page covers the two ways to get a compiled material into a scene,
and how a Selena material reads the scene's lights, shadows and fog.

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

## Scene lights, shadows and fog

A Selena mesh material reads the renderer's lighting by declaring engine
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

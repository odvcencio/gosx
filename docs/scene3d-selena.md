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
numbers := scene.TextAtlas{ID: "numbers", CellWidth: 96, CellHeight: 96}
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
  its cell transparent.
- An atlas may be at most 4096 pixels on a side; `Validate` reports a larger
  one, and `Props` drops it. Use smaller cells or a second atlas.
- The texture URL carries a hash of the labels and style, so changing a label
  makes a new texture, which the renderer loads like a new image.
- The browser draws with the fonts it has when the scene mounts. Load a web
  font before mounting, or the labels use the fallback font.
- `InstancedMesh` has no per-instance UV region, so instanced copies cannot
  pick different cells. Use one `Mesh` per character; the meshes still share
  one shader and one texture.

`TestTextAtlasDrawsDistinctNumbersOnCharacters` in `e2e/` draws 44 characters
this way and checks that every jersey shows a different number.

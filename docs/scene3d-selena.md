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

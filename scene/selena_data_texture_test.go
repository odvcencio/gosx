//go:build !js || !wasm

package scene

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/sceneprobe"
)

const spectrogramSource = `material Spectrogram {
    param spectrum : texture2d format r16float
    param head : float
    surface(geo) -> color {
        let sz = size(spectrum)
        let col = int(mod(geo.uv.x * sz.x + head, sz.x))
        let row = int(geo.uv.y * sz.y)
        let mag = load(spectrum, col, row).r
        return rgb(mag, mag * 0.6, 1.0 - mag)
    }
}`

// A material that loads texels carries format and sampleMode in its shader
// layout, binds a data texture by reference, and makes the page advertise the
// data-texture chunk.
func TestSelenaDataTextureMaterialCarriesFormatAndGatesChunk(t *testing.T) {
	material, layout, err := CompileSelenaMaterial([]byte(spectrogramSource), SelenaMaterialOptions{Uniforms: map[string]any{"spectrum": DataTextureRef("spectrum")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Textures) != 1 || layout.Textures[0].Format != "r16float" || layout.Textures[0].SampleMode != "load" {
		t.Fatalf("texture descriptor = %+v", layout.Textures)
	}
	if !strings.Contains(material.FragmentWGSL, "textureLoad") || !strings.Contains(material.FragmentGLSL, "texelFetch") {
		t.Fatal("load() must lower to textureLoad (WGSL) and texelFetch (GLES)")
	}
	if got := material.Uniforms["spectrum"]; got != "gosx:data:spectrum" {
		t.Fatalf("uniform = %v, want the data reference", got)
	}
	props, err := json.Marshal(map[string]any{"scene": Props{Graph: NewGraph(Mesh{ID: "wall", Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: material})}.SceneIR()})
	if err != nil {
		t.Fatal(err)
	}
	if !sceneprobe.Decode(props).NeedsSelenaDataTexture() {
		t.Fatalf("a scene with a data-texture material must request the chunk: %s", props)
	}
}

func TestSelenaDataTextureRefRejectsCubeTextures(t *testing.T) {
	source := `material Sky {
    param sky : textureCube
    surface(geo) -> color { return sampleCube(sky, geo.worldNormal).rgb }
}`
	_, _, err := CompileSelenaMaterial([]byte(source), SelenaMaterialOptions{Uniforms: map[string]any{"sky": DataTextureRef("x")}})
	if err == nil || !strings.Contains(err.Error(), "requires a 2d texture") {
		t.Fatalf("error = %v, want a 2d requirement", err)
	}
}

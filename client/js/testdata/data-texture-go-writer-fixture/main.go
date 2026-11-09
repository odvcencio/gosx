// Command data-texture-go-writer-fixture writes real scene.DataTextureEncoder
// output as base64 JSON on stdout, so the JS decoder and uploaders
// (client/runtime/scene3d/data-texture.ts) are tested against bytes the real Go
// encoder produced instead of a JS-side copy of the wire format.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"m31labs.dev/gosx/scene"
)

type frame struct {
	Name  string `json:"name"`
	Bytes string `json:"bytes"`
}

func main() {
	enc := scene.NewDataTextureEncoder()
	var out []frame
	add := func(name string, data []byte, err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, name, err)
			os.Exit(1)
		}
		out = append(out, frame{name, base64.StdEncoding.EncodeToString(data)})
	}

	// A 4x2 half-float ring: create, a 2x2 rectangle at (1,0), then a full row at (0,1).
	data, err := enc.Create("ring", 4, 2, scene.TextureR16Float)
	add("ring-create", data, err)
	data, err = enc.WriteTexture("ring", 2, 2, scene.TextureR16Float, scene.AppendFloat16(nil, []float32{0.25, 0.5, 1, 2}), 1, 0)
	add("ring-rect", data, err)
	data, err = enc.WriteTexture("ring", 4, 1, scene.TextureR16Float, scene.AppendFloat16(nil, []float32{-1, 0, 65504, 0.000061}), 0, 1)
	add("ring-row", data, err)

	// A 2x2 rgba8 texture written whole, with a key that needs id padding.
	data, err = enc.Create("rgba/tiny", 2, 2, scene.TextureRGBA8Unorm)
	add("tiny-create", data, err)
	texels := make([]byte, 16)
	for i := range texels {
		texels[i] = byte(i * 10)
	}
	data, err = enc.WriteTexture("rgba/tiny", 2, 2, scene.TextureRGBA8Unorm, texels, 0, 0)
	add("tiny-full", data, err)

	// A 3x1 float32 texture.
	data, err = enc.Create("f32", 3, 1, scene.TextureR32Float)
	add("f32-create", data, err)
	data, err = enc.WriteTexture("f32", 2, 1, scene.TextureR32Float, scene.AppendFloat32(nil, []float32{1.5, -2.5}), 1, 0)
	add("f32-tail", data, err)

	data, err = enc.Delete("f32")
	add("f32-delete", data, err)

	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

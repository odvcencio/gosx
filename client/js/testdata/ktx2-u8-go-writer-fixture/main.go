package main

import (
	"encoding/base64"
	"encoding/json"
	"os"

	"m31labs.dev/gosx/render/bundle/ktx2"
)

func main() {
	rgb := make([]byte, 3*3*3)
	for i := range rgb {
		rgb[i] = byte(i)
	}
	images := map[string]*ktx2.Image{
		"normal": {
			Format: ktx2.VkFormatR8G8B8Unorm, Width: 3, Height: 3, Faces: 1,
			Levels: []ktx2.Level{{Bytes: rgb}, {Bytes: []byte{128, 128, 255}}},
		},
		"alpha": {
			Format: ktx2.VkFormatR8G8B8A8SRGB, Width: 1, Height: 1, Faces: 1,
			Levels: []ktx2.Level{{Bytes: []byte{210, 120, 30, 77}}},
		},
	}
	out := make(map[string]string, len(images))
	for name, image := range images {
		data, err := ktx2.Encode(image, ktx2.EncodeOptions{Supercompression: ktx2.SupercompressionZlib})
		if err != nil {
			panic(err)
		}
		out[name] = base64.StdEncoding.EncodeToString(data)
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		panic(err)
	}
}

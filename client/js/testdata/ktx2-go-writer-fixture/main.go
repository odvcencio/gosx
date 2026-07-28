package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"m31labs.dev/gosx/render/bundle/ktx2"
)

func main() {
	data, err := ktx2.Encode(&ktx2.Image{
		Format: ktx2.VkFormatR16G16Sfloat,
		Width:  1,
		Height: 1,
		Faces:  1,
		Levels: []ktx2.Level{{Bytes: make([]byte, 4)}},
	}, ktx2.EncodeOptions{KeyValues: map[string]string{
		"GoSXiblRole":    "brdf-lut",
		"GoSXColorSpace": "linear",
		"GoSXiblModel":   "ggx-split-sum/smith-schlick-k=alpha-over-2/schlick-fresnel",
	}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(base64.StdEncoding.EncodeToString(data))
}

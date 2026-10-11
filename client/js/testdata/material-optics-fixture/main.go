// Emits real GoSX-lowered Selena materials, including shader-library references,
// for the browser optics oracle. No handwritten GLSL/WGSL substitutes are used.
package main

import (
	"encoding/json"
	"fmt"
	"m31labs.dev/gosx/scene"
	"os"
)

func main() {
	sources := map[string]string{
		"contributing": `material Coating {
   surface(g) -> color { return rgb(0.02, 0.02, 0.02) + rgb(4.0, 3.0, 2.0) * max(0.0, 1.0 - length(g.uv - vec2f(0.5, 0.5)) * 8.0) }
   specular(g) -> color { return rgb(4.0, 3.0, 2.0) * max(0.0, 1.0 - length(g.uv - vec2f(0.5, 0.5)) * 8.0) }
  }`,
		"plain": `material Wood { surface(g) -> color { return rgb(0.03, 0.015, 0.005) } }`,
	}
	result := map[string]json.RawMessage{}
	for name, source := range sources {
		material, _, err := scene.CompileSelenaMaterial([]byte(source), scene.SelenaMaterialOptions{SpecularMRT: true})
		if err != nil {
			panic(err)
		}
		ir := scene.NewGraph(scene.Mesh{ID: name, Geometry: scene.BoxGeometry{}, Material: material}).SceneIR()
		data, err := ir.MarshalJSON()
		if err != nil {
			panic(err)
		}
		result[name] = data
	}
	data, err := json.Marshal(result)
	if err != nil {
		panic(err)
	}
	if _, err = fmt.Fprintln(os.Stdout, string(data)); err != nil {
		panic(err)
	}
}

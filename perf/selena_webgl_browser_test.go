//go:build browser

package perf

import (
	"encoding/json"
	"m31labs.dev/gosx/scene"
	"testing"
	"time"
)

func TestSelenaDerivativeShaderLinksOnWebGL2(t *testing.T) {
	material, _, err := scene.CompileSelenaMaterial([]byte(`material Filtered {
		surface(geo) -> color {
			let edge = fwidth(geo.uv.x)
			return rgb(edge, edge, edge)
		}
	}`), scene.SelenaMaterialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sources, err := json.Marshal([]string{material.VertexGLSL, material.FragmentGLSL})
	if err != nil {
		t.Fatal(err)
	}
	driver := requireDriver(t, 30*time.Second)
	var result struct {
		WebGL2 bool
		Linked bool
		Error  string
	}
	err = driver.Evaluate(`(() => {
		const gl = document.createElement("canvas").getContext("webgl2");
		if (!gl) return {webGL2:false, linked:false, error:"no WebGL2 context"};
		const sources = `+string(sources)+`;
		const program = gl.createProgram();
		for (const [i, source] of sources.entries()) {
			const shader = gl.createShader(i === 0 ? gl.VERTEX_SHADER : gl.FRAGMENT_SHADER);
			gl.shaderSource(shader, source); gl.compileShader(shader);
			if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
				return {webGL2:true, linked:false, error:gl.getShaderInfoLog(shader)};
			}
			gl.attachShader(program, shader);
		}
		gl.linkProgram(program);
		return {webGL2:true, linked:gl.getProgramParameter(program, gl.LINK_STATUS), error:gl.getProgramInfoLog(program)};
	})()`, &result)
	if err != nil {
		t.Fatal(err)
	}
	if !result.WebGL2 || !result.Linked {
		t.Fatalf("authored derivative shader failed: %+v", result)
	}
}

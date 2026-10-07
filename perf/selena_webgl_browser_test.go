//go:build browser

package perf

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"m31labs.dev/gosx/scene"
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

func TestSelenaPostPreservesWebGLTextureOrigin(t *testing.T) {
	material, _, err := scene.CompileSelenaPost([]byte(`material Passthrough kind post {
		surface(post) -> color { return sceneColor(post.uv) }
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
		Pixels []int
		Error  string
	}
	err = driver.Evaluate(`(() => {
		const canvas = document.createElement("canvas"); canvas.width=2; canvas.height=2;
		const gl = canvas.getContext("webgl2", {antialias:false});
		if (!gl) return {error:"no WebGL2"};
		const sources = `+string(sources)+`;
		const program=gl.createProgram();
		for (const [i, source] of sources.entries()) {
			const shader=gl.createShader(i===0 ? gl.VERTEX_SHADER : gl.FRAGMENT_SHADER);
			gl.shaderSource(shader, source); gl.compileShader(shader);
			if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) return {error:gl.getShaderInfoLog(shader)};
			gl.attachShader(program, shader);
		}
		gl.linkProgram(program);
		if (!gl.getProgramParameter(program, gl.LINK_STATUS)) return {error:gl.getProgramInfoLog(program)};
		gl.useProgram(program);
		const texture=gl.createTexture(); gl.bindTexture(gl.TEXTURE_2D, texture);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
		gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
		const input=new Uint8Array([255,0,0,255, 0,255,0,255, 0,0,255,255, 255,255,0,255]);
		gl.texImage2D(gl.TEXTURE_2D,0,gl.RGBA,2,2,0,gl.RGBA,gl.UNSIGNED_BYTE,input);
		gl.uniform1i(gl.getUniformLocation(program,"_sceneColor"),0);
		const buffer=gl.createBuffer(); gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
		gl.bufferData(gl.ARRAY_BUFFER,new Float32Array([-1,-1,1,-1,-1,1,1,1]),gl.STATIC_DRAW);
		const position=gl.getAttribLocation(program,"a_position");
		if (position<0) return {error:"missing quad attribute"};
		gl.enableVertexAttribArray(position); gl.vertexAttribPointer(position,2,gl.FLOAT,false,0,0);
		gl.viewport(0,0,2,2); gl.drawArrays(gl.TRIANGLE_STRIP,0,4);
		const output=new Uint8Array(16); gl.readPixels(0,0,2,2,gl.RGBA,gl.UNSIGNED_BYTE,output);
		const error=gl.getError(); if (error!==gl.NO_ERROR) return {error:"GL error "+error};
		return {pixels:Array.from(output)};
	})()`, &result)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 0, 255}
	if result.Error != "" || !reflect.DeepEqual(result.Pixels, want) {
		t.Fatalf("post changed texture orientation: %+v", result)
	}
}

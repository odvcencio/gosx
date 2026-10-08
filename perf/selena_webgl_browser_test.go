//go:build browser

package perf

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/scene3drenderersource"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/selena/bindings"
)

func selenaWebGLLinkScript(t *testing.T) string {
	t.Helper()
	webgl := scene3drenderersource.ReadBackend(t, "webgl")
	start := strings.Index(webgl, "function sceneWebGLNormalizeCustomShaderSource(")
	if start < 0 {
		t.Fatal("missing shader normalizer")
	}
	end := strings.Index(webgl[start:], "// createSceneCustomPostProgram")
	if end < 0 {
		t.Fatal("missing shader normalizer boundary")
	}
	return webgl[start:start+end] + `
	function linkSelenaProgram(gl, sources) {
		const program = gl.createProgram();
		for (const [i, source] of sources.entries()) {
			const shader = gl.createShader(i === 0 ? gl.VERTEX_SHADER : gl.FRAGMENT_SHADER);
			gl.shaderSource(shader, sceneWebGLNormalizeCustomShaderSource(source)); gl.compileShader(shader);
			if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(shader));
			gl.attachShader(program, shader);
		}
		gl.linkProgram(program);
		if (!gl.getProgramParameter(program, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(program));
		return program;
	}`
}

func TestSelenaProgramsLinkOnWebGL2(t *testing.T) {
	linkScript := selenaWebGLLinkScript(t)
	driver := requireDriver(t, 30*time.Second)
	for _, tc := range []struct {
		name, source string
		compile      func([]byte, scene.SelenaMaterialOptions) (scene.CustomMaterial, bindings.Layout, error)
	}{
		{"derivative", `material Filtered {
			surface(geo) -> color {
                let edge = fwidth(geo.uv.x)
                return rgb(edge, edge, edge)
            }
		}`, scene.CompileSelenaMaterial},
		{"glow-points", `material GlowPoints kind points {
    param fogColor : vec3 = rgb(0, 0, 0)
    surface(pt) -> color {
        let centered  = pt.pointUV - vec2f(0.5, 0.5)
        let radial    = length(centered) * 2.0
        let sizeFocus = clamp((pt.pointSize - 4.0) / 48.0, 0.0, 1.0)
        let falloff   = mix(4.2, 3.2, sizeFocus)
        let core      = exp(-(radial * radial * falloff))
        let edge      = 1.0 - smoothstep(0.78, 1.0, radial)
        let a         = core * edge * pt.alpha
        let foggedRGB = mix(fogColor, pt.color, pt.fogFactor)
        return rgb(foggedRGB.r, foggedRGB.g, foggedRGB.b, a)
    }
}`, scene.CompileSelenaPoints},
		{"post-frost", `material PostFrost kind post {
    param rects     : array<vec4, 4>
    param blurLevel : float = 3.0
    param mixAmount : float = 0.85
    surface(post) -> color {
        let size = sceneSize()
        let px = 1.0 / max(size.x, 1.0)
        var best = 1.0
        for (var i = 0i; i < 4i; i = i + 1i) {
            let r = rects[i]
            let dx = abs(post.uv.x - r.x) - r.z
            let dy = abs(post.uv.y - r.y) - r.w
            let d = length(vec2f(max(dx, 0.0), max(dy, 0.0))) + min(max(dx, dy), 0.0)
            best = min(best, d)
            if (d < 0.0) {
                break
            }
        }
        let aa = max(fwidth(best), px)
        let inside = 1.0 - smoothstep(0.0 - aa, 0.0 + aa, best)
        let blurred = sceneColorLevel(post.uv, blurLevel)
        let plain = sceneColor(post.uv)
        let k = inside * mixAmount
        return rgb(mix(plain.r, blurred.r, k), mix(plain.g, blurred.g, k), mix(plain.b, blurred.b, k), plain.a)
    }
}`, scene.CompileSelenaPost},
		{"texture-origin", `material Passthrough kind post {
            surface(post) -> color { return sceneColor(post.uv) }
        }`, scene.CompileSelenaPost},
	} {
		t.Run(tc.name, func(t *testing.T) {
			material, _, err := tc.compile([]byte(tc.source), scene.SelenaMaterialOptions{})
			if err != nil {
				t.Fatal(err)
			}
			sources, err := json.Marshal([]string{material.VertexGLSL, material.FragmentGLSL})
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Pixels []int
				Error  string
			}
			err = driver.Evaluate(`(() => {
		`+linkScript+`
        const canvas = document.createElement("canvas"); canvas.width=2; canvas.height=2;
        const gl = canvas.getContext("webgl2", {antialias:false});
        if (!gl) return {error:"no WebGL2 context"};
		const sources = `+string(sources)+`;
        const program = linkSelenaProgram(gl, sources);
        if ("`+tc.name+`" !== "texture-origin") return {};
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
			if result.Error != "" {
				t.Fatalf("authored shader failed: %+v", result)
			}
			if tc.name == "texture-origin" {
				want := []int{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 0, 255}
				if !reflect.DeepEqual(result.Pixels, want) {
					t.Fatalf("post changed texture orientation: %+v", result)
				}
			}
		})
	}
}

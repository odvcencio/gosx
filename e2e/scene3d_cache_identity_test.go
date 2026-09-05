//go:build e2e

package e2e

import (
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/scene3drenderersource"
)

// This is a real WebGL2 depth-texture pixel oracle, not a full app/mount or
// physical-GPU benchmark. Serve the generated bootstrap for shared culling,
// and the exact production shadow pass/shaders (internal closure functions)
// as a test-only script. No diagnostic export is added to the shipped runtime.
func TestScene3DCacheIdentityPixels(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	source := scene3drenderersource.ReadBackend(t, "webgl")
	slice := func(start, end string) string {
		t.Helper()
		first := strings.Index(source, start)
		if first < 0 {
			t.Fatalf("missing source start %q", start)
		}
		last := strings.Index(source[first:], end)
		if last < 0 {
			t.Fatalf("missing source end %q", end)
		}
		return source[first : first+last]
	}
	passSource := "var sceneAffineDeterminant = window.__gosx_scene3d_api.sceneAffineDeterminant;\n" +
		"function sceneFiniteNumber(v, fallback) { return Number.isFinite(Number(v)) ? Number(v) : fallback; }\n" +
		slice("  const SCENE_SHADOW_VERTEX_SOURCE", "  // --- Points Shader Sources ---") +
		slice("  function sceneShadowPass", "  // Compile the shadow depth shader program.")
	mux := http.NewServeMux()
	mux.HandleFunc("/bootstrap.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(root, "client/js/bootstrap.js"))
	})
	mux.HandleFunc("/shadow-pass.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(passSource))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(sceneCacheIdentityHTML))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	page := newBrowserPage(t, chrome, map[string]any{
		"use-gl": "angle", "use-angle": "swiftshader", "enable-unsafe-swiftshader": true,
		"enable-webgl": true, "ignore-gpu-blocklist": true,
	}, 320, 320, "", 60*time.Second)
	if status := page.navigate(t, server.URL); status != http.StatusOK {
		t.Fatalf("page status %d", status)
	}
	var result struct {
		OK       bool              `json:"ok"`
		Error    string            `json:"error"`
		Renderer string            `json:"renderer"`
		Moved    int               `json:"moved"`
		Revision int               `json:"revision"`
		Culled   bool              `json:"culled"`
		Images   map[string][]byte `json:"images"`
	}
	page.eval(t, "window.runCacheIdentityProbe()", &result)
	if !result.OK || !result.Culled || result.Moved < 100 || result.Revision < 100 {
		t.Fatalf("pixel controls failed: %+v\nconsole:\n%s", result, page.Console())
	}
	t.Logf("WebGL2 depth pixels: moved=%d revised=%d renderer=%s (software graphics, not hardware performance)", result.Moved, result.Revision, result.Renderer)
	// Optional bounded capture for manual inspection; no artifact writes unless
	// the caller explicitly supplies a directory. Readback rows are bottom-up.
	if dir := os.Getenv("GOSX_E2E_CACHE_ARTIFACT_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"baseline", "moved", "revised"} {
			raw := result.Images[name]
			if len(raw) != 64*64*4 {
				t.Fatalf("invalid %s readback size %d", name, len(raw))
			}
			img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
			for row := 0; row < 64; row++ {
				copy(img.Pix[row*256:(row+1)*256], raw[(63-row)*256:(64-row)*256])
			}
			path := filepath.Join(dir, "g1a-depth-"+name+".png")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			encodeErr := png.Encode(file, img)
			closeErr := file.Close()
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			t.Logf("depth artifact: %s", path)
		}
	}
}

const sceneCacheIdentityHTML = `<!doctype html><meta charset="utf-8">
<canvas id="c" width="64" height="64"></canvas>
<script src="/bootstrap.js"></script><script src="/shadow-pass.js"></script>
<script>
window.runCacheIdentityProbe = function() {
  const out = {ok:false};
  try {
    const api = window.__gosx_scene3d_api;
    const owner = {}, camera = {x:0,y:0,z:5,near:0.1,far:10};
    const bounds = {minX:-1,maxX:1,minY:-1,maxY:1,minZ:-1,maxZ:1};
    if (api.sceneBoundsViewCulled(bounds,camera,owner)) throw Error('initial cull');
    bounds.minX+=10; bounds.maxX+=10; bounds.minZ-=10; bounds.maxZ-=10;
    out.culled = api.sceneBoundsViewCulled(bounds,camera,owner);
    if (!out.culled) throw Error('stale served-bootstrap cull');
    const gl = document.getElementById('c').getContext('webgl2', {antialias:false, preserveDrawingBuffer:true});
    if (!gl) throw Error('WebGL2 unavailable');
    const debug = gl.getExtension('WEBGL_debug_renderer_info');
    out.renderer = gl.getParameter(debug ? debug.UNMASKED_RENDERER_WEBGL : gl.RENDERER);
    const programs=[], buffers=[], textures=[], framebuffers=[];
    function compile(vs,fs) {
      const p=gl.createProgram(); programs.push(p);
      for (const [type,src] of [[gl.VERTEX_SHADER,vs],[gl.FRAGMENT_SHADER,fs]]) {
        const s=gl.createShader(type); gl.shaderSource(s,src); gl.compileShader(s);
        if (!gl.getShaderParameter(s,gl.COMPILE_STATUS)) throw Error(gl.getShaderInfoLog(s));
        gl.attachShader(p,s); gl.deleteShader(s);
      }
      gl.linkProgram(p);
      if (!gl.getProgramParameter(p,gl.LINK_STATUS)) throw Error(gl.getProgramInfoLog(p));
      return p;
    }
    function buffer() {const b=gl.createBuffer(); buffers.push(b); return b;}
    function resource() {
      const texture=gl.createTexture(); textures.push(texture);
      gl.bindTexture(gl.TEXTURE_2D,texture);
      gl.texImage2D(gl.TEXTURE_2D,0,gl.DEPTH_COMPONENT24,64,64,0,gl.DEPTH_COMPONENT,gl.UNSIGNED_INT,null);
      gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MIN_FILTER,gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MAG_FILTER,gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_WRAP_S,gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_WRAP_T,gl.CLAMP_TO_EDGE);
      const framebuffer=gl.createFramebuffer(); framebuffers.push(framebuffer);
      gl.bindFramebuffer(gl.FRAMEBUFFER,framebuffer);
      gl.framebufferTexture2D(gl.FRAMEBUFFER,gl.DEPTH_ATTACHMENT,gl.TEXTURE_2D,texture,0);
      gl.drawBuffers([gl.NONE]); gl.readBuffer(gl.NONE);
      if(gl.checkFramebufferStatus(gl.FRAMEBUFFER)!==gl.FRAMEBUFFER_COMPLETE) throw Error('incomplete depth framebuffer');
      gl.bindFramebuffer(gl.FRAMEBUFFER,null);
      return {framebuffer,depthTexture:texture,size:64,cascadeIndex:0,splitNear:0,splitFar:1};
    }
    const program=compile(SCENE_SHADOW_VERTEX_SOURCE,SCENE_SHADOW_FRAGMENT_SOURCE);
    const shadowProgram={program,attributes:{position:gl.getAttribLocation(program,'a_position')},uniforms:{
      lightViewProjection:gl.getUniformLocation(program,'u_lightViewProjection'),modelMatrix:gl.getUniformLocation(program,'u_modelMatrix')}};
    const sample=compile('#version 300 es\nprecision highp float; out vec2 uv; void main(){vec2 p=vec2((gl_VertexID<<1)&2,gl_VertexID&2); uv=p; gl_Position=vec4(p*2.0-1.0,0,1);}',
      '#version 300 es\nprecision highp float; in vec2 uv; uniform sampler2D depthMap; out vec4 c; void main(){c=vec4(vec3(texture(depthMap,uv).r),1);}');
    const identity=()=>new Float32Array([1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]);
    const vertices={immutable:true,revision:0,positions:new Float32Array([-.8,-.4,0,-.1,-.4,0,-.1,.6,0,-.8,.6,0]),indices:new Uint32Array([0,2,1])};
    const caster={castShadow:true,directVertices:true,retainedGeometry:true,vertices,geometryRevision:0,vertexCount:4,vertexOffset:0,depthNear:4,depthFar:6,modelMatrix:identity()};
    const bundle={meshObjects:[caster]}, light=identity(), state={buffer:buffer()}, positions=buffer(), indices=buffer();
    let draws=0;
    const originalDraw=gl.drawElements.bind(gl);
    gl.drawElements=function(...args){draws++; return originalDraw(...args);};
    const bind=function(object,p) {
      gl.bindBuffer(gl.ARRAY_BUFFER,positions); gl.bufferData(gl.ARRAY_BUFFER,object.vertices.positions,gl.STATIC_DRAW);
      gl.enableVertexAttribArray(p.attributes.position); gl.vertexAttribPointer(p.attributes.position,3,gl.FLOAT,false,0,0);
      gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER,indices); gl.bufferData(gl.ELEMENT_ARRAY_BUFFER,object.vertices.indices,gl.STATIC_DRAW);
      gl.uniformMatrix4fv(p.uniforms.modelMatrix,false,object.modelMatrix);
      return {indexed:true,count:object.vertices.indices.length};
    };
    function pixels(res) {
      renderSceneShadowPass(gl,shadowProgram,res,light,bundle,state,bind);
      gl.bindFramebuffer(gl.FRAMEBUFFER,null); gl.viewport(0,0,64,64);
      gl.disable(gl.DEPTH_TEST); gl.disable(gl.CULL_FACE); gl.disable(gl.BLEND);
      gl.useProgram(sample); gl.activeTexture(gl.TEXTURE0); gl.bindTexture(gl.TEXTURE_2D,res.depthTexture);
      gl.uniform1i(gl.getUniformLocation(sample,'depthMap'),0); gl.drawArrays(gl.TRIANGLES,0,3);
      const bytes=new Uint8Array(64*64*4); gl.readPixels(0,0,64,64,gl.RGBA,gl.UNSIGNED_BYTE,bytes);
      const error=gl.getError(); if(error!==gl.NO_ERROR) throw Error('GL error '+error);
      return bytes;
    }
    function changed(a,b) {let n=0;for(let i=0;i<a.length;i+=4) if(Math.abs(a[i]-b[i])>8)n++;return n;}
    function equal(a,b) {return a.length===b.length && a.every((value,i)=>value===b[i]);}
    const cached=resource(), baseline=pixels(cached), initialDraws=draws;
    if(!baseline.some((v,i)=>i%4===0&&v<200)) throw Error('blank depth baseline');
    if(!equal(baseline,pixels(cached))||draws!==initialDraws) throw Error('static pass not reused byte-exactly');
    caster.modelMatrix[12]=.4; caster.modelMatrix[13]=-.4;
    const moved=pixels(cached); out.moved=changed(baseline,moved);
    if(!equal(moved,pixels(resource()))) throw Error('moved cached map differs bytewise from fresh oracle');
    vertices.indices.set([0,3,2]); caster.geometryRevision++;
    const revised=pixels(cached); out.revision=changed(moved,revised);
    if(!equal(revised,pixels(resource()))) throw Error('same-AABB revision differs bytewise from fresh oracle');
    out.images={baseline:Array.from(baseline),moved:Array.from(moved),revised:Array.from(revised)};
    for(const p of programs)gl.deleteProgram(p); for(const b of buffers)gl.deleteBuffer(b);
    for(const f of framebuffers)gl.deleteFramebuffer(f); for(const t of textures)gl.deleteTexture(t);
    out.ok=true;
  } catch(error) {out.error=String(error&&error.stack||error);}
  return out;
};
</script>`

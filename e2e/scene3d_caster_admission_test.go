//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

const scene3DShadowCasterAdmissionHTML = `<!doctype html>
<html><head><meta charset="utf-8"></head>
<body style="margin:0;background:#111">
<canvas id="c" width="160" height="160" style="width:160px;height:160px"></canvas>
<script src="/bootstrap.js"></script>
<script src="/bootstrap-feature-scene3d-webgl.js"></script>
<script>
window.__runShadowCasterAdmissionProbe = function() {
  const out = {ok:false, cases:[]};
  let renderer = null;
  try {
    const api = window.__gosx_scene3d_api;
    if (!api) throw Error("missing scene3d api");
    const canvas = document.getElementById("c");
    const backend = api.sceneBackendRegistry.select({webgl:true, webgl2:true, webgpu:false, canvas:false, canvas2d:false});
    if (!backend) throw Error("no webgl backend");
    out.backend = backend.kind || "unknown";
    if (out.backend !== "webgl") throw Error("expected webgl backend, got " + out.backend);
    renderer = backend.create(canvas, {background:"#111111"}, {tier:"full"});
    out.supportsRetainedGeometry = renderer && renderer.supportsRetainedGeometry === true;

    const gl = canvas.getContext("webgl2");
    if (!gl) throw Error("webgl2 readback unavailable");
    let drawArraysCalls = 0;
    let drawElementsCalls = 0;
    const originalDrawArrays = gl.drawArrays.bind(gl);
    const originalDrawElements = gl.drawElements.bind(gl);
    gl.drawArrays = function() {
      drawArraysCalls++;
      return originalDrawArrays.apply(null, arguments);
    };
    gl.drawElements = function() {
      drawElementsCalls++;
      return originalDrawElements.apply(null, arguments);
    };
    const debug = gl.getExtension("WEBGL_debug_renderer_info");
    out.renderer = gl.getParameter(debug ? debug.UNMASKED_RENDERER_WEBGL : gl.RENDERER);

    const scene = {positions: [], normals: [], uvs: [], tangents: []};
    function pushVertex(x, y, z, nx, ny, nz, u, v) {
      scene.positions.push(x, y, z);
      scene.normals.push(nx, ny, nz);
      scene.uvs.push(u, v);
      scene.tangents.push(1, 0, 0, 1);
    }
    function pushQuad(cx, cy, cz, halfX, halfY, nz) {
      const base = scene.positions.length / 3;
      const nx = 0, ny = 0;
      if (nz < 0) {
        pushVertex(cx - halfX, cy - halfY, cz, nx, ny, nz, 0, 0);
        pushVertex(cx + halfX, cy + halfY, cz, nx, ny, nz, 1, 1);
        pushVertex(cx + halfX, cy - halfY, cz, nx, ny, nz, 1, 0);
        pushVertex(cx - halfX, cy - halfY, cz, nx, ny, nz, 0, 0);
        pushVertex(cx - halfX, cy + halfY, cz, nx, ny, nz, 0, 1);
        pushVertex(cx + halfX, cy + halfY, cz, nx, ny, nz, 1, 1);
      } else {
        pushVertex(cx - halfX, cy - halfY, cz, nx, ny, nz, 0, 0);
        pushVertex(cx + halfX, cy - halfY, cz, nx, ny, nz, 1, 0);
        pushVertex(cx + halfX, cy + halfY, cz, nx, ny, nz, 1, 1);
        pushVertex(cx - halfX, cy - halfY, cz, nx, ny, nz, 0, 0);
        pushVertex(cx + halfX, cy + halfY, cz, nx, ny, nz, 1, 1);
        pushVertex(cx - halfX, cy + halfY, cz, nx, ny, nz, 0, 1);
      }
      return {offset: base, count: 6};
    }
    function pushBox(cx, cy, cz, halfX, halfY, halfZ) {
      const base = scene.positions.length / 3;
      pushQuad(cx, cy, cz - halfZ, halfX, halfY, -1);
      pushQuad(cx, cy, cz + halfZ, halfX, halfY, 1);
      return {offset: base, count: (scene.positions.length / 3) - base};
    }
    const transformBounds = (halfX, halfY, halfZ, z, x) => ({
      minX: x - halfX, maxX: x + halfX,
      minY: -halfY, maxY: halfY,
      minZ: z - halfZ, maxZ: z + halfZ,
    });

    function object(id, materialIndex, geometry, x, z, options) {
      const opts = options || {};
      const bounds = transformBounds(opts.halfX || 0.5, opts.halfY || 0.5, opts.halfZ || 0, z, x);
      const depth = api.sceneBoundsDepthMetrics(bounds, opts.camera, {});
      const viewCulled = api.sceneBoundsViewCulled(bounds, opts.camera, {});
      const count = Math.max(0, Math.floor(geometry.count || 0));
      return {
        id,
        materialIndex,
        renderPass: "opaque",
        static: true,
        castShadow: !!opts.castShadow,
        receiveShadow: !!opts.receiveShadow,
        bounds,
        depthNear: depth.near,
        depthFar: depth.far,
        depthCenter: depth.center,
        viewCulled,
        directVertices: false,
        retainedGeometry: false,
        modelMatrix: null,
        vertices: null,
        geometryRevision: 0,
        vertexOffset: geometry.offset,
        vertexCount: count,
        doubleSided: true,
      };
    }

    function renderCase(name, opts) {
      const camera = {x:0, y:0, z:6, fov:36, near: opts.cameraNear == null ? 5.5 : opts.cameraNear, far:6.5};
      scene.positions.length = 0;
      scene.normals.length = 0;
      scene.uvs.length = 0;
      scene.tangents.length = 0;
      const receiverGeom = pushQuad(0, 0, 0, 1.6, 1.6, 1);
      // The WebGL shadow pass culls the lit/front face as a peter-panning
      // mitigation. Use a tiny solid slab, not a zero-thickness plane, so the
      // caster has a real depth silhouette while remaining invisible to the
      // main color pass via camera far-plane culling.
      const receiver = object("receiver", 0, receiverGeom, 0, 0, {
        camera, halfX: 1.6, halfY: 1.6, castShadow: false, receiveShadow: true,
      });
      const casterX = opts.outsideProjection ? 2.4 : -0.35;
      const casterGeom = pushBox(casterX, 0, 1, 0.32, 0.55, 0.12);
      const caster = object("near-culled-caster", 1, casterGeom, casterX, 1, {
        camera, halfX: 0.32, halfY: 0.55, halfZ: 0.12, castShadow: opts.casterShadow, receiveShadow: false,
      });
      const light = {
        id: "sun",
        kind: "directional",
        color: "#ffffff",
        intensity: opts.lightIntensity == null ? 4 : opts.lightIntensity,
        directionX: 0,
        directionY: 0,
        directionZ: -1,
        castShadow: !!opts.lightShadow,
        shadowSize: 512,
        shadowBias: 0.0005,
        shadowSoftness: 0,
        shadowCascades: opts.cascades,
      };
      const bundle = {
        bundleVersion: 1,
        camera,
        environment: {background:"#111111", ambientColor:"#050505", ambientIntensity:0.02},
        background: "#111111",
        materials: [
          {kind:"standard", color:"#d6d6d6", roughness:1, metalness:0},
          {kind:"standard", color:"#ff0000", roughness:1, metalness:0},
        ],
        lights: [light],
        meshObjects: [receiver, caster],
        instancedMeshes: [], computeParticles: [], objects: [],
        points: [], labels: [], sprites: [], postEffects: [],
        positions: new Float32Array(0), colors: new Float32Array(0),
        worldPositions: new Float32Array(0), worldColors: new Float32Array(0),
        worldLineWidths: new Float32Array(0),
        worldMeshPositions: new Float32Array(scene.positions),
        worldMeshNormals: new Float32Array(scene.normals),
        worldMeshUVs: new Float32Array(scene.uvs),
        worldMeshTangents: new Float32Array(scene.tangents),
        vertexCount: 0,
        worldVertexCount: 0,
        shadowMaxPixels: 1024 * 1024,
      };
      const viewport = {cssWidth:160, cssHeight:160, pixelWidth:160, pixelHeight:160, pixelRatio:1};
      const beforeArrays = drawArraysCalls;
      const beforeElements = drawElementsCalls;
      for (let i = 0; i < 4; i++) renderer.render(bundle, viewport);
      const diagnostics = renderer && typeof renderer.diagnostics === "function" ? renderer.diagnostics() : null;
      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      const bytes = new Uint8Array(160 * 160 * 4);
      gl.readPixels(0, 0, 160, 160, gl.RGBA, gl.UNSIGNED_BYTE, bytes);
      const err = gl.getError();
      if (err !== gl.NO_ERROR) throw Error(name + " GL error " + err);
      let center = 0, patch = 0, red = 0, lit = 0, global = 0, samples = 0;
      for (let y = 64; y < 96; y++) {
        for (let x = 64; x < 96; x++) {
          const p = (y * 160 + x) * 4;
          center += (bytes[p] + bytes[p + 1] + bytes[p + 2]) / 3;
          patch++;
          if (bytes[p] > 120 && bytes[p + 1] < 70 && bytes[p + 2] < 70) red++;
          if (bytes[p] > 35 || bytes[p + 1] > 35 || bytes[p + 2] > 35) lit++;
        }
      }
      for (let p = 0; p < bytes.length; p += 4) {
        global += (bytes[p] + bytes[p + 1] + bytes[p + 2]) / 3;
        samples++;
      }
      const result = {
        name,
        cascades: opts.cascades,
        casterViewCulled: caster.viewCulled,
        casterDepthNear: caster.depthNear,
        casterDepthFar: caster.depthFar,
        receiverViewCulled: receiver.viewCulled,
        centerMean: center / patch,
        globalMean: global / samples,
        redPixels: red,
        litPixels: lit,
        drawArraysCalls: drawArraysCalls - beforeArrays,
        drawElementsCalls: drawElementsCalls - beforeElements,
        meshDrawn: diagnostics && diagnostics.renderTruth ? diagnostics.renderTruth.meshDrawn : null,
        meshViewCulled: diagnostics && diagnostics.renderTruth ? diagnostics.renderTruth.meshViewCulled : null,
      };
      Object.defineProperty(result, "_bytes", {value: bytes});
      out.cases.push(result);
      return result;
    }

    function darkerPixels(a, b) {
      let count = 0, sum = 0;
      for (let p = 0; p < a._bytes.length; p += 4) {
        const av = (a._bytes[p] + a._bytes[p + 1] + a._bytes[p + 2]) / 3;
        const bv = (b._bytes[p] + b._bytes[p + 1] + b._bytes[p + 2]) / 3;
        if (av - bv > 12) {
          count++;
          sum += av - bv;
        }
      }
      return {count, meanDrop: count ? sum / count : 0};
    }

    function assertShadow(cascades) {
      const unshadowed = renderCase("unshadowed-c" + cascades, {cascades, casterShadow:false, lightShadow:true});
      const shadowed = renderCase("shadowed-c" + cascades, {cascades, casterShadow:true, lightShadow:true});
      const noShadowLight = renderCase("no-shadow-light-c" + cascades, {cascades, casterShadow:true, lightShadow:false});
      const outsideProjection = renderCase("outside-projection-c" + cascades, {cascades, casterShadow:true, lightShadow:true, outsideProjection:true});
      const directLightOff = renderCase("direct-light-off-c" + cascades, {cascades, casterShadow:false, lightShadow:false, lightIntensity:0});
      const visibleCaster = renderCase("visible-caster-c" + cascades, {cascades, casterShadow:false, lightShadow:false, cameraNear:0.1});
      if (unshadowed.receiverViewCulled) throw Error("receiver unexpectedly camera-culled for cascades=" + cascades);
      if (!shadowed.casterViewCulled) throw Error("caster was not actually camera-culled for cascades=" + cascades);
      if (shadowed.redPixels > 0) throw Error("camera color pass exposed red caster pixels for cascades=" + cascades + ": " + shadowed.redPixels);
      if (visibleCaster.casterViewCulled || visibleCaster.redPixels < 20) {
        throw Error("camera-visible control did not expose red caster for cascades=" + cascades + ": culled=" + visibleCaster.casterViewCulled + " redPixels=" + visibleCaster.redPixels);
      }
      if (unshadowed.centerMean - directLightOff.centerMean < 20) {
        throw Error("front-lit control did not prove direct illumination for cascades=" + cascades + ": unshadowed=" + unshadowed.centerMean.toFixed(2) + " directLightOff=" + directLightOff.centerMean.toFixed(2));
      }
      const drop = unshadowed.centerMean - shadowed.centerMean;
      const globalShadow = darkerPixels(unshadowed, shadowed);
      const noLightDelta = Math.abs(noShadowLight.centerMean - unshadowed.centerMean);
      const outsideDelta = Math.abs(outsideProjection.centerMean - unshadowed.centerMean);
      if (drop < 12 && globalShadow.count < 80) {
        throw Error("off-camera caster did not darken receiver for cascades=" + cascades + ": centerDrop=" + drop.toFixed(2) + " darkPixels=" + globalShadow.count + " darkMeanDrop=" + globalShadow.meanDrop.toFixed(2) + " unshadowed=" + unshadowed.centerMean.toFixed(2) + " shadowed=" + shadowed.centerMean.toFixed(2));
      }
      shadowed.globalDarkPixels = globalShadow.count;
      shadowed.globalDarkMeanDrop = globalShadow.meanDrop;
      if (noLightDelta > 5) throw Error("non-directional/no-shadow control changed receiver for cascades=" + cascades + ": delta=" + noLightDelta.toFixed(2));
      if (outsideDelta > 8) throw Error("outside-projection control changed receiver for cascades=" + cascades + ": delta=" + outsideDelta.toFixed(2));
      const repeat = renderCase("shadowed-repeat-c" + cascades, {cascades, casterShadow:true, lightShadow:true});
      if (Math.abs(repeat.centerMean - shadowed.centerMean) > 3) {
        throw Error("shadow result not repeatable for cascades=" + cascades + ": first=" + shadowed.centerMean.toFixed(2) + " repeat=" + repeat.centerMean.toFixed(2));
      }
    }

    const failures = [];
    for (const cascades of [1, 2]) {
      try {
        assertShadow(cascades);
      } catch (error) {
        failures.push(String(error && error.stack || error));
      }
    }
    if (failures.length) throw Error(failures.join("\n---\n"));
    if (typeof renderer.dispose === "function") renderer.dispose();
    renderer = null;
    out.ok = true;
  } catch (error) {
    out.error = String(error && error.stack || error);
    try {
      if (renderer && typeof renderer.dispose === "function") renderer.dispose();
    } catch (_) {}
  }
  return out;
};
</script>
</body></html>`

type scene3DShadowCasterAdmissionResult struct {
	OK                       bool                                     `json:"ok"`
	Error                    string                                   `json:"error"`
	Backend                  string                                   `json:"backend"`
	Renderer                 string                                   `json:"renderer"`
	SupportsRetainedGeometry bool                                     `json:"supportsRetainedGeometry"`
	Cases                    []scene3DShadowCasterAdmissionCaseResult `json:"cases"`
}

type scene3DShadowCasterAdmissionCaseResult struct {
	Name               string  `json:"name"`
	Cascades           int     `json:"cascades"`
	CasterViewCulled   bool    `json:"casterViewCulled"`
	CasterDepthNear    float64 `json:"casterDepthNear"`
	CasterDepthFar     float64 `json:"casterDepthFar"`
	ReceiverViewCulled bool    `json:"receiverViewCulled"`
	CenterMean         float64 `json:"centerMean"`
	GlobalMean         float64 `json:"globalMean"`
	RedPixels          int     `json:"redPixels"`
	LitPixels          int     `json:"litPixels"`
	DrawArraysCalls    int     `json:"drawArraysCalls"`
	DrawElementsCalls  int     `json:"drawElementsCalls"`
}

func TestScene3DWebGLShadowCasterAdmissionPixels(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrapPath := filepath.Join(root, "client", "js", "bootstrap.js")
	webglPath := filepath.Join(root, "client", "js", "bootstrap-feature-scene3d-webgl.js")

	mux := http.NewServeMux()
	mux.HandleFunc("/bootstrap.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, bootstrapPath)
	})
	mux.HandleFunc("/bootstrap-feature-scene3d-webgl.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, webglPath)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(scene3DShadowCasterAdmissionHTML))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	page := newBrowserPage(t, chrome, map[string]any{
		"use-gl":                    "angle",
		"use-angle":                 "swiftshader",
		"enable-unsafe-swiftshader": true,
		"enable-webgl":              true,
		"ignore-gpu-blocklist":      true,
	}, 320, 320, "", 90*time.Second)
	if status := page.navigate(t, server.URL+"/"); status != http.StatusOK {
		t.Fatalf("test page status %d\n\nConsole:\n%s", status, page.Console())
	}
	page.waitFor(t, `!!window.__gosx_scene3d_api && !!window.__runShadowCasterAdmissionProbe`, 10*time.Second, "scene3d shadow-caster probe")
	var result scene3DShadowCasterAdmissionResult
	page.eval(t, "window.__runShadowCasterAdmissionProbe()", &result)
	if !result.OK {
		blob, _ := json.MarshalIndent(result, "", "  ")
		t.Fatalf("shadow caster admission pixel probe failed: %s\n\nResult:\n%s\n\nConsole:\n%s\n\nPage errors:\n%v",
			result.Error, blob, page.Console(), page.PageErrors())
	}
	if result.Backend != "webgl" {
		t.Fatalf("expected webgl backend, got %q: %+v", result.Backend, result)
	}
	t.Logf("WebGL2 shadow caster admission pixel proof: renderer=%s cases=%+v", result.Renderer, result.Cases)
}

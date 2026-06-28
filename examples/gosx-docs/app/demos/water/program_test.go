package docs

import (
	"encoding/json"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestWaterDemoPreloadHead(t *testing.T) {
	ctx := &route.RouteContext{}
	addWaterDemoPreloadHead(ctx)
	head := gosx.RenderHTML(ctx.Head())
	for _, href := range []string{
		"/water/tiles.jpg",
		"/water/xpos.jpg",
		"/water/xneg.jpg",
		"/water/ypos.jpg",
		"/water/zpos.jpg",
		"/water/zneg.jpg",
	} {
		want := `rel="preload" as="image" href="` + href + `" crossorigin="anonymous"`
		if !strings.Contains(head, want) {
			t.Fatalf("preload head missing %s in %s", want, head)
		}
	}
}

func TestWaterDemoDataCompiles(t *testing.T) {
	data, err := WaterDemoData()
	if err != nil {
		t.Fatalf("WaterDemoData returned error: %v", err)
	}
	for _, key := range []string{"waterControlData", "waterComputeSource", "waterMaterialSource", "waterComputeSourceFiles", "waterMaterialSourceFiles", "waterSeedWGSL", "waterDropWGSL", "waterDisplacementWGSL", "waterSimulationWGSL", "waterNormalWGSL", "waterCausticsWGSL", "waterPoolVertexWGSL", "waterPoolFragmentWGSL", "waterSurfaceVertexWGSL", "waterSurfaceFragmentWGSL", "waterSurfaceBelowFragmentWGSL", "waterObjectShadowWGSL", "waterObjectMeshShadowVertexWGSL", "waterObjectMeshShadowFragmentWGSL", "waterObjectMaterialSource", "waterObjectMaterialSourceFiles", "waterObjectMaterialWGSL", "waterObjectMaterialLayout", "waterObjectMaterialUniforms", "waterDuckMaterialSource", "waterDuckMaterialSourceFiles", "waterDuckMaterialWGSL", "waterDuckMaterialLayout", "waterDuckMaterialUniforms"} {
		if data[key] == nil {
			t.Fatalf("WaterDemoData missing %q", key)
		}
	}
	controlJSON, ok := data["waterControlData"].(string)
	if !ok || controlJSON == "" {
		t.Fatalf("waterControlData = %T %#v, want non-empty string", data["waterControlData"], data["waterControlData"])
	}
	if got := data["waterComputeSource"]; got != waterComputeSourceID {
		t.Fatalf("waterComputeSource = %#v", got)
	}
	if got := data["waterMaterialSource"]; got != waterMaterialSourceID {
		t.Fatalf("waterMaterialSource = %#v", got)
	}
	if got, ok := data["waterComputeSourceFiles"].(map[string]string); !ok || !reflect.DeepEqual(got, waterComputeSourceFiles) {
		t.Fatalf("waterComputeSourceFiles = %#v, want %#v", data["waterComputeSourceFiles"], waterComputeSourceFiles)
	}
	if got, ok := data["waterMaterialSourceFiles"].(map[string]string); !ok || !reflect.DeepEqual(got, waterMaterialSourceFiles) {
		t.Fatalf("waterMaterialSourceFiles = %#v, want %#v", data["waterMaterialSourceFiles"], waterMaterialSourceFiles)
	}
	if got := data["waterObjectMaterialSource"]; got != waterObjectMaterialSourceID {
		t.Fatalf("waterObjectMaterialSource = %#v", got)
	}
	if got, ok := data["waterObjectMaterialSourceFiles"].(map[string]string); !ok || !reflect.DeepEqual(got, waterObjectMaterialSourceFiles) {
		t.Fatalf("waterObjectMaterialSourceFiles = %#v, want %#v", data["waterObjectMaterialSourceFiles"], waterObjectMaterialSourceFiles)
	}
	if got := data["waterDuckMaterialSource"]; got != waterDuckMaterialSourceID {
		t.Fatalf("waterDuckMaterialSource = %#v", got)
	}
	if got, ok := data["waterDuckMaterialSourceFiles"].(map[string]string); !ok || !reflect.DeepEqual(got, waterDuckMaterialSourceFiles) {
		t.Fatalf("waterDuckMaterialSourceFiles = %#v, want %#v", data["waterDuckMaterialSourceFiles"], waterDuckMaterialSourceFiles)
	}
	embeddedSources, err := waterShaderSources()
	if err != nil {
		t.Fatalf("waterShaderSources returned error: %v", err)
	}
	if len(embeddedSources) != len(waterShaderSourceFiles) {
		t.Fatalf("waterShaderSources length = %d, want %d", len(embeddedSources), len(waterShaderSourceFiles))
	}
	for key, filename := range waterShaderSourceFiles {
		if !strings.HasPrefix(filename, "shaders/jeantimex-water.") || (!strings.HasSuffix(filename, ".elio") && !strings.HasSuffix(filename, ".sel")) {
			t.Fatalf("water shader source %s has non-Elio/Selena file %q", key, filename)
		}
		body := embeddedSources[key]
		if strings.TrimSpace(body) == "" {
			t.Fatalf("water shader source %s from %s is empty", key, filename)
		}
		if got, ok := data[key].(string); !ok || got != body {
			t.Fatalf("WaterDemoData[%s] does not match embedded source %s", key, filename)
		}
	}
	seedWGSL, ok := data["waterSeedWGSL"].(string)
	if !ok || !strings.Contains(seedWGSL, "fn seedDrops") || !strings.Contains(seedWGSL, "Authored water seed-drop pass") {
		t.Fatalf("waterSeedWGSL = %T len=%d, want authored seedDrops WGSL", data["waterSeedWGSL"], len(seedWGSL))
	}
	if !strings.Contains(seedWGSL, "let polarity = select(1.0, -1.0, (j & 1u) == 0u)") || strings.Contains(seedWGSL, "select(-1.0, 1.0") {
		t.Fatalf("waterSeedWGSL lost upstream initial negative drop polarity")
	}
	if !strings.Contains(seedWGSL, "seedSalt: f32") || !strings.Contains(seedWGSL, "let seedSalt = params.seedSalt") || !strings.Contains(seedWGSL, "hash01(jf * 12.9898 + seedSalt + 0.173)") {
		t.Fatalf("waterSeedWGSL lost upstream-style randomized initial seed centers")
	}
	texelCenterWaterCoord := "(vec2f(f32(x), f32(y)) + vec2f(0.5)) / max(vec2f(f32(res)), vec2f(1.0))"
	if !strings.Contains(seedWGSL, "let uv = "+texelCenterWaterCoord) || strings.Contains(seedWGSL, "f32(res - 1u)") {
		t.Fatalf("waterSeedWGSL lost upstream texel-center simulation coordinates")
	}
	dropWGSL, ok := data["waterDropWGSL"].(string)
	if !ok || !strings.Contains(dropWGSL, "fn addDrop") || !strings.Contains(dropWGSL, "Authored water interactive-drop pass") {
		t.Fatalf("waterDropWGSL = %T len=%d, want authored addDrop WGSL", data["waterDropWGSL"], len(dropWGSL))
	}
	if !strings.Contains(dropWGSL, "return "+texelCenterWaterCoord) || strings.Contains(dropWGSL, "f32(res - 1u)") {
		t.Fatalf("waterDropWGSL lost upstream texel-center simulation coordinates")
	}
	displacementWGSL, ok := data["waterDisplacementWGSL"].(string)
	if !ok || !strings.Contains(displacementWGSL, "fn displaceObject") || !strings.Contains(displacementWGSL, "WaterSystem compute binding contract") {
		t.Fatalf("waterDisplacementWGSL = %T len=%d, want authored displaceObject WGSL", data["waterDisplacementWGSL"], len(displacementWGSL))
	}
	if !strings.Contains(displacementWGSL, "return "+texelCenterWaterCoord) || strings.Contains(displacementWGSL, "f32(res - 1u)") {
		t.Fatalf("waterDisplacementWGSL lost upstream texel-center simulation coordinates")
	}
	simulationWGSL, ok := data["waterSimulationWGSL"].(string)
	if !ok || !strings.Contains(simulationWGSL, "fn stepSimulation") || !strings.Contains(simulationWGSL, "WaterSystem compute binding contract") {
		t.Fatalf("waterSimulationWGSL = %T len=%d, want authored stepSimulation WGSL", data["waterSimulationWGSL"], len(simulationWGSL))
	}
	if !strings.Contains(simulationWGSL, "(average - info.x) * 2.0 * params.waveSpeed") || !strings.Contains(simulationWGSL, "* params.damping") {
		t.Fatalf("waterSimulationWGSL lost upstream wave coefficient/damping contract")
	}
	normalWGSL, ok := data["waterNormalWGSL"].(string)
	if !ok || !strings.Contains(normalWGSL, "fn updateNormals") || !strings.Contains(normalWGSL, "WaterSystem compute binding contract") {
		t.Fatalf("waterNormalWGSL = %T len=%d, want authored updateNormals WGSL", data["waterNormalWGSL"], len(normalWGSL))
	}
	if !strings.Contains(normalWGSL, "let dx = vec3f(delta, inState[waterIndex(eastX, y)].x - info.x, 0.0)") ||
		!strings.Contains(normalWGSL, "let dz = vec3f(0.0, inState[waterIndex(x, northY)].x - info.x, delta)") ||
		strings.Contains(normalWGSL, "2.0 * delta") || strings.Contains(normalWGSL, "westX") || strings.Contains(normalWGSL, "southY") {
		t.Fatalf("waterNormalWGSL lost upstream forward-difference normal stencil")
	}
	causticsWGSL, ok := data["waterCausticsWGSL"].(string)
	if !ok || !strings.Contains(causticsWGSL, "fn fragmentMain") || !strings.Contains(causticsWGSL, "WaterSystem caustics material binding contract") || !strings.Contains(causticsWGSL, "fn roundedPoolCausticMask") || !strings.Contains(causticsWGSL, "fn intersectRoundedRectangle2D") || !strings.Contains(causticsWGSL, "fn intersectRoundedBox") || !strings.Contains(causticsWGSL, "fn roundedPoolEdgeAttenuation") || !strings.Contains(causticsWGSL, "1.0 / (1.0 + exp(exponent))") || !strings.Contains(causticsWGSL, "point.y - refractedLight.y * t.y - 2.0 / 12.0") || !strings.Contains(causticsWGSL, "fn projectCausticFloor") || !strings.Contains(causticsWGSL, "fn intersectCube") || !strings.Contains(causticsWGSL, "fn cubeOcclusion") || !strings.Contains(causticsWGSL, "fn sphereSoftShadowOcclusion") || !strings.Contains(causticsWGSL, "fn cubeSoftShadowOcclusion") || !strings.Contains(causticsWGSL, "fn meshShadowTextureOcclusion") || !strings.Contains(causticsWGSL, "let area = cross(dir, refractedLight)") || !strings.Contains(causticsWGSL, "1.0 / (1.0 + exp(-shadow))") || !strings.Contains(causticsWGSL, "let shadowRay = -refractedLight") || !strings.Contains(causticsWGSL, "for (var x = -1; x <= 1; x = x + 1)") || !strings.Contains(causticsWGSL, "return occlusion / 9.0") || !strings.Contains(causticsWGSL, "let shadowUV = 0.75 * (point.xz - point.y * refractedLight.xz / safeLightY)") || !strings.Contains(causticsWGSL, "textureSample(objectShadowTexture, objectShadowSampler, shadowUV + vec2f(-d, -d))") || !strings.Contains(causticsWGSL, "return 0.8 * occlusion / 9.0") || !strings.Contains(causticsWGSL, "let analyticSphereShadow = sphereSoftShadowOcclusion(newPos, flatRay)") || !strings.Contains(causticsWGSL, "let analyticCubeShadow = cubeSoftShadowOcclusion(newPos, flatRay)") || !strings.Contains(causticsWGSL, "let meshTextureShadow = meshShadowTextureOcclusion(newPos, flatRay)") || !strings.Contains(causticsWGSL, "let edgeAttenuation = roundedPoolEdgeAttenuation(newPos, flatRay)") || !strings.Contains(causticsWGSL, "if (poolMask <= 0.001)") || !strings.Contains(causticsWGSL, "let oldArea = max(length(dpdx(oldPos)) * length(dpdy(oldPos)), 0.000001)") || !strings.Contains(causticsWGSL, "let newArea = max(length(dpdx(newPos)) * length(dpdy(newPos)), 0.000001)") || !strings.Contains(causticsWGSL, "oldArea / newArea * 0.2") || !strings.Contains(causticsWGSL, "intensity = intensity * poolMask * edgeAttenuation") {
		t.Fatalf("waterCausticsWGSL = %T len=%d, want authored Selena caustics WGSL", data["waterCausticsWGSL"], len(causticsWGSL))
	}
	if strings.Index(causticsWGSL, "let oldArea = max(length(dpdx(oldPos))") > strings.Index(causticsWGSL, "if (poolMask <= 0.001)") {
		t.Fatalf("waterCausticsWGSL branches on poolMask before derivative evaluation")
	}
	poolVertexWGSL, ok := data["waterPoolVertexWGSL"].(string)
	if !ok || !strings.Contains(poolVertexWGSL, "fn vertexMain") || !strings.Contains(poolVertexWGSL, "Authored Selena water pool vertex pass") || !strings.Contains(poolVertexWGSL, "waterPoolRoundedVertex") {
		t.Fatalf("waterPoolVertexWGSL = %T len=%d, want authored Selena pool vertex WGSL", data["waterPoolVertexWGSL"], len(poolVertexWGSL))
	}
	poolFragmentWGSL, ok := data["waterPoolFragmentWGSL"].(string)
	if !ok || !strings.Contains(poolFragmentWGSL, "fn fragmentMain") || !strings.Contains(poolFragmentWGSL, "Authored Selena water pool fragment pass") || !strings.Contains(poolFragmentWGSL, "tileTexture") || !strings.Contains(poolFragmentWGSL, "fn objectAmbientOcclusion") || !strings.Contains(poolFragmentWGSL, "1.0 - 0.9 / pow(max(distanceRatio, 1.0), 4.0)") {
		t.Fatalf("waterPoolFragmentWGSL = %T len=%d, want authored Selena pool fragment WGSL", data["waterPoolFragmentWGSL"], len(poolFragmentWGSL))
	}
	surfaceVertexWGSL, ok := data["waterSurfaceVertexWGSL"].(string)
	if !ok || !strings.Contains(surfaceVertexWGSL, "fn vertexMain") || !strings.Contains(surfaceVertexWGSL, "Authored Selena water surface vertex pass") || !strings.Contains(surfaceVertexWGSL, "state: array<vec4f>") {
		t.Fatalf("waterSurfaceVertexWGSL = %T len=%d, want authored Selena surface vertex WGSL", data["waterSurfaceVertexWGSL"], len(surfaceVertexWGSL))
	}
	surfaceFragmentWGSL, ok := data["waterSurfaceFragmentWGSL"].(string)
	if !ok || !strings.Contains(surfaceFragmentWGSL, "fn fragmentMain") || !strings.Contains(surfaceFragmentWGSL, "WaterSystem surface material binding contract") || !strings.Contains(surfaceFragmentWGSL, "WaterObjectTextureMatrices") || !strings.Contains(surfaceFragmentWGSL, "@group(1) @binding(1) var<storage, read> waterState: array<vec4f>") || !strings.Contains(surfaceFragmentWGSL, "@group(1) @binding(9) var tileTexture: texture_2d<f32>") || !strings.Contains(surfaceFragmentWGSL, "fn intersectSurfaceSphereBounds") || !strings.Contains(surfaceFragmentWGSL, "fn intersectSurfaceSphere(origin: vec3f, ray: vec3f, center: vec3f, radius: f32)") || !strings.Contains(surfaceFragmentWGSL, "fn intersectSurfaceBox(origin: vec3f, ray: vec3f, cubeMin: vec3f, cubeMax: vec3f)") || !strings.Contains(surfaceFragmentWGSL, "fn sampleObjectRefraction") || !strings.Contains(surfaceFragmentWGSL, "fn sampleObjectReflection") || !strings.Contains(surfaceFragmentWGSL, "origin + ray * hit") || !strings.Contains(surfaceFragmentWGSL, "fn surfaceWaterHeightAt(point: vec3f) -> f32") || !strings.Contains(surfaceFragmentWGSL, "return waterState[cell.y * res + cell.x].x") || !strings.Contains(surfaceFragmentWGSL, "fn surfaceObjectHitDistance(origin: vec3f, ray: vec3f) -> vec2f") || !strings.Contains(surfaceFragmentWGSL, "intersectSurfaceSphere(origin, ray, surfaceObjectCenterWorld(), surfaceObjectRadiusWorld())") || !strings.Contains(surfaceFragmentWGSL, "intersectSurfaceBox(origin, ray, center - halfSize, center + halfSize)") || !strings.Contains(surfaceFragmentWGSL, "fn surfaceObjectColor(point: vec3f, hitKind: f32, lightDir: vec3f) -> vec3f") || !strings.Contains(surfaceFragmentWGSL, "let waterHeight = surfaceWaterHeightAt(point)") || !strings.Contains(surfaceFragmentWGSL, "diffuse = (diffuse + select(0.0, 0.06, hitKind >= 1.5)) * caustic.r * 4.0") || !strings.Contains(surfaceFragmentWGSL, "let objectHit = surfaceObjectHitDistance(origin, ray)") || !strings.Contains(surfaceFragmentWGSL, "surfaceObjectColor(origin + ray * objectHit.x, objectHit.y, lightDir)") || !strings.Contains(surfaceFragmentWGSL, "fn intersectSurfacePoolBox") || !strings.Contains(surfaceFragmentWGSL, "fn intersectSurfaceRoundedRectangle2D") || !strings.Contains(surfaceFragmentWGSL, "fn intersectSurfaceRoundedPool") || !strings.Contains(surfaceFragmentWGSL, "fn intersectSurfacePool(origin: vec3f, ray: vec3f) -> vec2f") || !strings.Contains(surfaceFragmentWGSL, "return intersectSurfaceRoundedPool(origin, ray, params.cornerRadius)") || !strings.Contains(surfaceFragmentWGSL, "let t = intersectSurfacePool(origin, ray)") || !strings.Contains(surfaceFragmentWGSL, "struct SurfaceWallSample") || !strings.Contains(surfaceFragmentWGSL, "fn surfaceRoundedWallSample(point: vec3f) -> SurfaceWallSample") || !strings.Contains(surfaceFragmentWGSL, "result.normal = vec3f(-cornerNormal.x, 0.0, -cornerNormal.y)") || !strings.Contains(surfaceFragmentWGSL, "radius * atan2(cd.y, cd.x)") || !strings.Contains(surfaceFragmentWGSL, "result.uv = vec2f(point.y, s) * 0.5 + vec2f(1.0, 0.5)") || !strings.Contains(surfaceFragmentWGSL, "fn getWallColor(point: vec3f, lightDir: vec3f) -> vec3f") || !strings.Contains(surfaceFragmentWGSL, "let wall = surfaceRoundedWallSample(point)") || !strings.Contains(surfaceFragmentWGSL, "textureSampleLevel(tileTexture, waterSampler, fract(wall.uv), 0.0)") || !strings.Contains(surfaceFragmentWGSL, "let refractedLight = -refract(-lightDir") || !strings.Contains(surfaceFragmentWGSL, "scale = scale + diffuse * caustic.r * 2.0 * caustic.g") || !strings.Contains(surfaceFragmentWGSL, "let exponent = -200.0 / (1.0 + 10.0 * span)") || !strings.Contains(surfaceFragmentWGSL, "fn getSurfaceRayColor(origin: vec3f, ray: vec3f, waterColor: vec3f, lightDir: vec3f)") || !strings.Contains(surfaceFragmentWGSL, "color = getWallColor(hit, lightDir)") || !strings.Contains(surfaceFragmentWGSL, "getSurfaceRayColor(in.worldPos, reflectDir, vec3f(0.25, 1.0, 1.25), lightDir)") || !strings.Contains(surfaceFragmentWGSL, "let refractDir = refract(-viewDir, n, 1.0 / 1.333)") || !strings.Contains(surfaceFragmentWGSL, "sampleObjectRefraction(in.worldPos, refractDir)") || !strings.Contains(surfaceFragmentWGSL, "sampleObjectReflection(in.worldPos, reflectDir)") || !strings.Contains(surfaceFragmentWGSL, "let inBounds = step(0.0, uv.x)") || !strings.Contains(surfaceFragmentWGSL, "fn waterSunGlint") || !strings.Contains(surfaceFragmentWGSL, "pow(max(dot(lightDir, normalize(ray)), 0.0), 5000.0)") || !strings.Contains(surfaceFragmentWGSL, "vec3f(10.0, 8.0, 6.0)") {
		t.Fatalf("waterSurfaceFragmentWGSL = %T len=%d, want authored Selena surface WGSL", data["waterSurfaceFragmentWGSL"], len(surfaceFragmentWGSL))
	}
	surfaceBelowFragmentWGSL, ok := data["waterSurfaceBelowFragmentWGSL"].(string)
	if !ok || !strings.Contains(surfaceBelowFragmentWGSL, "fn fragmentMain") || !strings.Contains(surfaceBelowFragmentWGSL, "WATER_SURFACE_VIEW_BELOW: bool = true") || !strings.Contains(surfaceBelowFragmentWGSL, "sampleProjectedTexture") || !strings.Contains(surfaceBelowFragmentWGSL, "@group(1) @binding(1) var<storage, read> waterState: array<vec4f>") || !strings.Contains(surfaceBelowFragmentWGSL, "@group(1) @binding(9) var tileTexture: texture_2d<f32>") || !strings.Contains(surfaceBelowFragmentWGSL, "fn intersectSurfaceSphereBounds") || !strings.Contains(surfaceBelowFragmentWGSL, "fn intersectSurfaceSphere(origin: vec3f, ray: vec3f, center: vec3f, radius: f32)") || !strings.Contains(surfaceBelowFragmentWGSL, "fn intersectSurfaceBox(origin: vec3f, ray: vec3f, cubeMin: vec3f, cubeMax: vec3f)") || !strings.Contains(surfaceBelowFragmentWGSL, "fn sampleObjectRefraction") || !strings.Contains(surfaceBelowFragmentWGSL, "fn sampleObjectReflection") || !strings.Contains(surfaceBelowFragmentWGSL, "origin + ray * hit") || !strings.Contains(surfaceBelowFragmentWGSL, "fn surfaceWaterHeightAt(point: vec3f) -> f32") || !strings.Contains(surfaceBelowFragmentWGSL, "return waterState[cell.y * res + cell.x].x") || !strings.Contains(surfaceBelowFragmentWGSL, "fn surfaceObjectHitDistance(origin: vec3f, ray: vec3f) -> vec2f") || !strings.Contains(surfaceBelowFragmentWGSL, "fn surfaceObjectColor(point: vec3f, hitKind: f32, lightDir: vec3f) -> vec3f") || !strings.Contains(surfaceBelowFragmentWGSL, "let waterHeight = surfaceWaterHeightAt(point)") || !strings.Contains(surfaceBelowFragmentWGSL, "let objectHit = surfaceObjectHitDistance(origin, ray)") || !strings.Contains(surfaceBelowFragmentWGSL, "surfaceObjectColor(origin + ray * objectHit.x, objectHit.y, lightDir)") || !strings.Contains(surfaceBelowFragmentWGSL, "fn intersectSurfacePoolBox") || !strings.Contains(surfaceBelowFragmentWGSL, "fn intersectSurfaceRoundedRectangle2D") || !strings.Contains(surfaceBelowFragmentWGSL, "fn intersectSurfaceRoundedPool") || !strings.Contains(surfaceBelowFragmentWGSL, "fn intersectSurfacePool(origin: vec3f, ray: vec3f) -> vec2f") || !strings.Contains(surfaceBelowFragmentWGSL, "return intersectSurfaceRoundedPool(origin, ray, params.cornerRadius)") || !strings.Contains(surfaceBelowFragmentWGSL, "let t = intersectSurfacePool(origin, ray)") || !strings.Contains(surfaceBelowFragmentWGSL, "struct SurfaceWallSample") || !strings.Contains(surfaceBelowFragmentWGSL, "fn surfaceRoundedWallSample(point: vec3f) -> SurfaceWallSample") || !strings.Contains(surfaceBelowFragmentWGSL, "result.normal = vec3f(-cornerNormal.x, 0.0, -cornerNormal.y)") || !strings.Contains(surfaceBelowFragmentWGSL, "radius * atan2(cd.y, cd.x)") || !strings.Contains(surfaceBelowFragmentWGSL, "result.uv = vec2f(point.y, s) * 0.5 + vec2f(1.0, 0.5)") || !strings.Contains(surfaceBelowFragmentWGSL, "fn getWallColor(point: vec3f, lightDir: vec3f) -> vec3f") || !strings.Contains(surfaceBelowFragmentWGSL, "let wall = surfaceRoundedWallSample(point)") || !strings.Contains(surfaceBelowFragmentWGSL, "textureSampleLevel(tileTexture, waterSampler, fract(wall.uv), 0.0)") || !strings.Contains(surfaceBelowFragmentWGSL, "let refractedLight = -refract(-lightDir") || !strings.Contains(surfaceBelowFragmentWGSL, "scale = scale + diffuse * caustic.r * 2.0 * caustic.g") || !strings.Contains(surfaceBelowFragmentWGSL, "let exponent = -200.0 / (1.0 + 10.0 * span)") || !strings.Contains(surfaceBelowFragmentWGSL, "fn getSurfaceRayColor(origin: vec3f, ray: vec3f, waterColor: vec3f, lightDir: vec3f)") || !strings.Contains(surfaceBelowFragmentWGSL, "getSurfaceRayColor(in.worldPos, refractDir, vec3f(1.0), lightDir) * vec3f(0.8, 1.0, 1.1)") || !strings.Contains(surfaceBelowFragmentWGSL, "let refractDir = refract(-viewDir, n, 1.333 / 1.0)") || !strings.Contains(surfaceBelowFragmentWGSL, "sampleObjectRefraction(in.worldPos, refractDir)") || !strings.Contains(surfaceBelowFragmentWGSL, "sampleObjectReflection(in.worldPos, reflectDir)") || !strings.Contains(surfaceBelowFragmentWGSL, "let inBounds = step(0.0, uv.x)") || !strings.Contains(surfaceBelowFragmentWGSL, "fn waterSunGlint") || !strings.Contains(surfaceBelowFragmentWGSL, "pow(max(dot(lightDir, normalize(ray)), 0.0), 5000.0)") || !strings.Contains(surfaceBelowFragmentWGSL, "vec3f(10.0, 8.0, 6.0)") {
		t.Fatalf("waterSurfaceBelowFragmentWGSL = %T len=%d, want authored Selena below-surface WGSL", data["waterSurfaceBelowFragmentWGSL"], len(surfaceBelowFragmentWGSL))
	}
	for name, shader := range map[string]string{"above": surfaceFragmentWGSL, "below": surfaceBelowFragmentWGSL} {
		for _, want := range []string{
			"fn surfaceStateAtUV(uv: vec2f) -> vec4f",
			"fn surfaceParallaxState(uv: vec2f) -> vec4f",
			"coord = clamp(coord + info.zw * 0.005",
			"fn surfaceNormalFromState(info: vec4f) -> vec3f",
			"let surfaceInfo = surfaceParallaxState(in.uv)",
		} {
			if !strings.Contains(shader, want) {
				t.Fatalf("waterSurface%sFragmentWGSL missing upstream surface composite contract %q", name, want)
			}
		}
	}
	if !strings.Contains(surfaceFragmentWGSL, "return vec4f(mix(refractedColor, reflectedColor, fresnel), shapeAlpha)") {
		t.Fatalf("waterSurfaceFragmentWGSL missing upstream above-surface Fresnel composite")
	}
	if !strings.Contains(surfaceBelowFragmentWGSL, "return vec4f(mix(reflectedColor, refractedColor, (1.0 - fresnel) * length(refractDir)), shapeAlpha)") {
		t.Fatalf("waterSurfaceBelowFragmentWGSL missing upstream below-surface Fresnel composite")
	}
	if !strings.Contains(surfaceFragmentWGSL, "@group(1) @binding(5) var objectClippedReflectionTexture: texture_2d<f32>") || !strings.Contains(surfaceFragmentWGSL, "sampleProjectedTexture(objectClippedReflectionTexture, objectTextureMatrices.reflectionViewProjectionMatrix, in.worldPos)") {
		t.Fatalf("waterSurfaceFragmentWGSL missing upstream clipped mesh reflection path")
	}
	if strings.Contains(surfaceFragmentWGSL, "objectOpticalFootprint") || strings.Contains(surfaceBelowFragmentWGSL, "objectOpticalFootprint") {
		t.Fatalf("route-authored surface WGSL still uses object footprint tint approximation")
	}
	for name, shader := range map[string]string{"above": surfaceFragmentWGSL, "below": surfaceBelowFragmentWGSL} {
		for _, want := range []string{
			"fn surfaceObjectWallOcclusion(point: vec3f) -> f32",
			"distanceRatio = length(point - center) / max(surfaceObjectRadiusWorld(), 0.001)",
			"distanceRatio = length((point - center) / max(surfaceObjectHalfSizeWorld(), vec3f(0.001)))",
			"distanceRatio = length(point - center) / max(objectShadowRadiusWorld(), 0.001)",
			"surfaceObjectWallOcclusion(point)",
		} {
			if !strings.Contains(shader, want) {
				t.Fatalf("waterSurface%sFragmentWGSL missing wall occlusion contract %q", name, want)
			}
		}
	}
	for name, shader := range map[string]string{"above": surfaceFragmentWGSL, "below": surfaceBelowFragmentWGSL} {
		for _, want := range []string{
			"@group(1) @binding(10) var<storage, read> objectSpheres: array<WaterDisplacementSphere>",
			"fn surfaceObjectCenterWorld() -> vec3f",
			"params.objectCenter.x * params.poolWidth",
			"fn objectSubtypeIsTorusKnot() -> bool",
			"fn objectTextureRadiusWorld() -> f32",
			"fn objectShadowRadiusWorld() -> f32",
			"let halfSize = surfaceObjectHalfSizeWorld()",
			"return 0.13",
			"return max(surfaceObjectRadiusWorld(), 0.31)",
			"return surfaceObjectRadiusWorld()",
			"intersectSurfaceSphereBounds(origin, ray, surfaceObjectCenterWorld(), objectTextureRadiusWorld())",
			"fn surfaceObjectRadiusWorld() -> f32",
			"fn surfaceTorusKnotSDF(point: vec3f) -> f32",
			"let segments = 64u",
			"let rad = 0.17 * (2.0 + cos(3.0 * theta)) * 0.5",
			"return (minDist - 0.045) * radiusScale",
			"fn intersectSurfaceTorusKnot(origin: vec3f, ray: vec3f) -> f32",
			"for (var i = 0u; i < 30u; i = i + 1u)",
			"fn surfaceTorusKnotNormal(point: vec3f) -> vec3f",
			"params.objectParams.w > 0.5 && params.objectParams.w < 1.5",
			"fn surfaceCompoundObjectHit(origin: vec3f, ray: vec3f) -> vec2f",
			"let sphere = objectSpheres[index].offsetRadius",
			"intersectSurfaceSphere(origin, ray, center, surfaceObjectSphereRadiusWorld(i))",
			"fn surfaceCompoundObjectNormal(point: vec3f) -> vec3f",
			"bestNormal = normalize(toPoint)",
			"let compoundHit = surfaceCompoundObjectHit(origin, ray)",
			"normal = surfaceCompoundObjectNormal(point)",
		} {
			if !strings.Contains(shader, want) {
				t.Fatalf("waterSurface%sFragmentWGSL missing %q", name, want)
			}
		}
		if strings.Contains(shader, "intersectSurfaceSphereBounds(origin, ray, params.objectCenter.xyz, objectTextureRadius())") {
			t.Fatalf("waterSurface%sFragmentWGSL still uses normalized object texture bounds", name)
		}
	}
	objectShadowWGSL, ok := data["waterObjectShadowWGSL"].(string)
	if !ok || !strings.Contains(objectShadowWGSL, "fn shadowMain") || !strings.Contains(objectShadowWGSL, "Authored Selena object-shadow pass") || !strings.Contains(objectShadowWGSL, "objectSpheres") {
		t.Fatalf("waterObjectShadowWGSL = %T len=%d, want authored Selena object shadow WGSL", data["waterObjectShadowWGSL"], len(objectShadowWGSL))
	}
	objectMeshShadowVertexWGSL, ok := data["waterObjectMeshShadowVertexWGSL"].(string)
	if !ok || !strings.Contains(objectMeshShadowVertexWGSL, "fn vertexMain") || !strings.Contains(objectMeshShadowVertexWGSL, "ObjectMeshShadowUniforms") || !strings.Contains(objectMeshShadowVertexWGSL, "refract") {
		t.Fatalf("waterObjectMeshShadowVertexWGSL = %T len=%d, want authored Selena object mesh shadow vertex WGSL", data["waterObjectMeshShadowVertexWGSL"], len(objectMeshShadowVertexWGSL))
	}
	objectMeshShadowFragmentWGSL, ok := data["waterObjectMeshShadowFragmentWGSL"].(string)
	if !ok || !strings.Contains(objectMeshShadowFragmentWGSL, "fn fragmentMain") || !strings.Contains(objectMeshShadowFragmentWGSL, "Authored Selena projected object-mesh shadow fragment pass") {
		t.Fatalf("waterObjectMeshShadowFragmentWGSL = %T len=%d, want authored Selena object mesh shadow fragment WGSL", data["waterObjectMeshShadowFragmentWGSL"], len(objectMeshShadowFragmentWGSL))
	}
	objectWGSL, ok := data["waterObjectMaterialWGSL"].(string)
	if !ok || !strings.Contains(objectWGSL, "Authored Selena object material") || !strings.Contains(objectWGSL, "waterState: array<vec4f>") || !strings.Contains(objectWGSL, "causticTexture") || !strings.Contains(objectWGSL, "texturePassMode") || !strings.Contains(objectWGSL, "isTexturePass") || !strings.Contains(objectWGSL, "isObjectTexturePass() && objectTexturePassMode() == 2u") || !strings.Contains(objectWGSL, "in.worldPos.y < waterHeight") || strings.Contains(objectWGSL, "in.worldPos.y < 0.0") || !strings.Contains(objectWGSL, "fn objectPoolAmbientOcclusion") || !strings.Contains(objectWGSL, "1.0 - 0.9 / pow(max((halfSize.x + radius - abs(point.x)) / radius, 1.0), 3.0)") {
		t.Fatalf("waterObjectMaterialWGSL = %T len=%d, want authored water-aware object material WGSL", data["waterObjectMaterialWGSL"], len(objectWGSL))
	}
	if !strings.Contains(objectWGSL, "fn upstreamObjectDiffuse") || !strings.Contains(objectWGSL, "return diffuse * causticR * 4.0") || !strings.Contains(objectWGSL, "return (diffuse + 0.06) * causticR * 4.0") || !strings.Contains(objectWGSL, "if (isSphereObject())") || strings.Contains(objectWGSL, "let objectShadow = textureSample") || strings.Contains(objectWGSL, "mix(lit") {
		t.Fatalf("waterObjectMaterialWGSL lost upstream per-object lighting semantics")
	}
	objectLayout, ok := data["waterObjectMaterialLayout"].(map[string]any)
	if !ok {
		t.Fatalf("waterObjectMaterialLayout = %T, want map[string]any", data["waterObjectMaterialLayout"])
	}
	if objectLayout["material"] != "WaterObject" {
		t.Fatalf("waterObjectMaterialLayout material = %#v, want WaterObject", objectLayout["material"])
	}
	storageBuffers, ok := objectLayout["storageBuffers"].([]map[string]any)
	if !ok || len(storageBuffers) != 1 || storageBuffers[0]["name"] != "waterState" {
		t.Fatalf("waterObjectMaterialLayout storageBuffers = %#v, want waterState binding", objectLayout["storageBuffers"])
	}
	uniformBlock, ok := objectLayout["uniformBlock"].(map[string]any)
	if !ok || uniformBlock["size"] != 224 {
		t.Fatalf("waterObjectMaterialLayout uniformBlock = %#v, want size 224 with model matrix and texture pass uniforms", objectLayout["uniformBlock"])
	}
	fields, ok := uniformBlock["fields"].([]map[string]any)
	if !ok {
		t.Fatalf("waterObjectMaterialLayout fields = %#v, want field descriptors", uniformBlock["fields"])
	}
	foundModelMatrix := false
	foundIsTexturePass := false
	for _, field := range fields {
		if field["name"] == "modelMatrix" && field["type"] == "mat4" && field["offset"] == 64 {
			foundModelMatrix = true
		}
		if field["name"] == "isTexturePass" && field["type"] == "vec4" && field["offset"] == 208 {
			foundIsTexturePass = true
		}
	}
	if !foundModelMatrix {
		t.Fatalf("waterObjectMaterialLayout fields = %#v, want modelMatrix mat4 at offset 64", fields)
	}
	if !foundIsTexturePass {
		t.Fatalf("waterObjectMaterialLayout fields = %#v, want isTexturePass vec4 at offset 208", fields)
	}
	objectUniforms, ok := data["waterObjectMaterialUniforms"].(map[string]any)
	if !ok {
		t.Fatalf("waterObjectMaterialUniforms = %T, want map[string]any", data["waterObjectMaterialUniforms"])
	}
	objectParams, ok := objectUniforms["params"].([]float64)
	if !ok || len(objectParams) != 4 || objectParams[0] != 256 || objectParams[1] != 0.25 {
		t.Fatalf("waterObjectMaterialUniforms params = %#v, want resolution and object AO radius", objectUniforms["params"])
	}
	objectPassFlag, ok := objectUniforms["isTexturePass"].([]float64)
	if !ok || len(objectPassFlag) != 4 || objectPassFlag[0] != 0 {
		t.Fatalf("waterObjectMaterialUniforms isTexturePass = %#v, want disabled texture pass flag", objectUniforms["isTexturePass"])
	}
	for _, want := range []string{"gosx:water:water-main:state", "gosx:water:water-main:caustics", "gosx:water:water-main:shadow"} {
		found := false
		for _, value := range objectUniforms {
			if value == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("waterObjectMaterialUniforms missing resource ref %q: %#v", want, objectUniforms)
		}
	}
	duckWGSL, ok := data["waterDuckMaterialWGSL"].(string)
	if !ok || !strings.Contains(duckWGSL, "modelTexture") || !strings.Contains(duckWGSL, "DuckCM.png") || !strings.Contains(duckWGSL, "waterState: array<vec4f>") || !strings.Contains(duckWGSL, "texturePassMode") || !strings.Contains(duckWGSL, "isTexturePass") || !strings.Contains(duckWGSL, "isObjectTexturePass() && objectTexturePassMode() == 2u") || !strings.Contains(duckWGSL, "in.worldPos.y < waterHeight") || strings.Contains(duckWGSL, "in.worldPos.y < 0.0") {
		t.Fatalf("waterDuckMaterialWGSL = %T len=%d, want authored textured water-aware duck material WGSL", data["waterDuckMaterialWGSL"], len(duckWGSL))
	}
	for name, shader := range map[string]string{"object": objectWGSL, "duck": duckWGSL} {
		for _, want := range []string{
			"modelMatrix: mat4x4f",
			"let world = uniforms.modelMatrix * vec4f(in.position, 1.0)",
			"out.worldPos = world.xyz",
			"out.normal = normalize((uniforms.modelMatrix * vec4f(in.normal, 0.0)).xyz)",
			"out.clipPos = uniforms.mvp * world",
		} {
			if !strings.Contains(shader, want) {
				t.Fatalf("water %s material missing upstream world-space object contract %q", name, want)
			}
		}
		if strings.Contains(shader, "out.worldPos = in.position") || strings.Contains(shader, "uniforms.mvp * vec4f(in.position, 1.0)") {
			t.Fatalf("water %s material still uses local-space object pass coordinates", name)
		}
	}
	if !strings.Contains(duckWGSL, "var diffuse = max(dot(-refractedLight, n), 0.0) * 0.6") || !strings.Contains(duckWGSL, "diffuse = diffuse * caustic * 4.0") || !strings.Contains(duckWGSL, "var color = albedo * (0.4 + diffuse)") || strings.Contains(duckWGSL, "let objectShadow = textureSample") || strings.Contains(duckWGSL, "mix(lit") {
		t.Fatalf("waterDuckMaterialWGSL lost upstream duck lighting semantics")
	}
	duckLayout, ok := data["waterDuckMaterialLayout"].(map[string]any)
	if !ok || duckLayout["material"] != "WaterDuck" {
		t.Fatalf("waterDuckMaterialLayout = %#v, want WaterDuck layout", data["waterDuckMaterialLayout"])
	}
	duckUniforms, ok := data["waterDuckMaterialUniforms"].(map[string]any)
	if !ok || duckUniforms["modelTexture"] != "/water/models/duck/DuckCM.png" || duckUniforms["waterState"] != "gosx:water:water-main:state" {
		t.Fatalf("waterDuckMaterialUniforms = %#v, want duck texture and live water refs", data["waterDuckMaterialUniforms"])
	}
	duckPassFlag, ok := duckUniforms["isTexturePass"].([]float64)
	if !ok || len(duckPassFlag) != 4 || duckPassFlag[0] != 0 {
		t.Fatalf("waterDuckMaterialUniforms isTexturePass = %#v, want disabled texture pass flag", duckUniforms["isTexturePass"])
	}
	var controlProfile map[string]any
	if err := json.Unmarshal([]byte(controlJSON), &controlProfile); err != nil {
		t.Fatalf("waterControlData is not valid JSON: %v", err)
	}
	if waterDemoHiddenY != 10 {
		t.Fatalf("waterDemoHiddenY = %v, want upstream inactive object Y=10", waterDemoHiddenY)
	}
	if hiddenY, ok := controlProfile["hiddenY"].(float64); !ok || hiddenY != waterDemoHiddenY {
		t.Fatalf("waterControlData hiddenY = %#v, want %v", controlProfile["hiddenY"], waterDemoHiddenY)
	}
	if inactiveY, ok := controlProfile["inactiveY"].(float64); !ok || inactiveY != waterDemoHiddenY {
		t.Fatalf("waterControlData inactiveY = %#v, want %v", controlProfile["inactiveY"], waterDemoHiddenY)
	}
	physics, ok := controlProfile["physics"].(map[string]any)
	if !ok {
		t.Fatalf("waterControlData physics = %T, want map[string]any", controlProfile["physics"])
	}
	if physics["gravityY"] != float64(-4) || physics["bounce"] != 0.7 || physics["defaultBuoyancyScale"] != 1.1 {
		t.Fatalf("waterControlData physics = %#v", physics)
	}
	interaction, ok := controlProfile["interaction"].(map[string]any)
	if !ok {
		t.Fatalf("waterControlData interaction = %T, want map[string]any", controlProfile["interaction"])
	}
	if interaction["profile"] != "water-object-drop-orbit" || interaction["pointerDrops"] != true || interaction["keyboard"] != true || interaction["dropRadius"] != 0.03 || interaction["dropStrength"] != 0.01 {
		t.Fatalf("waterControlData interaction = %#v", interaction)
	}
	objects, ok := controlProfile["objects"].(map[string]any)
	if !ok {
		t.Fatalf("waterControlData objects = %T, want map[string]any", controlProfile["objects"])
	}
	for _, name := range []string{"Sphere", "Cube", "TorusKnot", "Rubber Duck"} {
		object, ok := objects[name].(map[string]any)
		if !ok {
			t.Fatalf("waterControlData missing object profile %q", name)
		}
		for _, field := range []string{"buoyancyRadius", "floorClearance", "xLimitRadius", "zLimitRadius", "meshYOffset"} {
			if _, ok := object[field].(float64); !ok {
				t.Fatalf("%s missing physics field %q: %#v", name, field, object[field])
			}
		}
		mesh, ok := object["mesh"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing dynamic mesh patch: %#v", name, object["mesh"])
		}
		if mesh["visible"] != true {
			t.Fatalf("%s mesh visible = %#v, want true active patch", name, mesh["visible"])
		}
	}
	for name, want := range map[string]string{
		"Sphere":      "sphere",
		"Cube":        "box",
		"TorusKnot":   "mesh",
		"Rubber Duck": "sphere",
	} {
		object := objects[name].(map[string]any)
		if got := object["objectHitTest"]; got != want {
			t.Fatalf("%s objectHitTest = %#v, want %q", name, got, want)
		}
	}
	duck := objects["Rubber Duck"].(map[string]any)
	if duck["src"] != "/water/models/duck/Duck.gltf" {
		t.Fatalf("Duck src = %#v, want route-authored Duck glTF URL", duck["src"])
	}
	if duck["objectSubtype"] != "duck" {
		t.Fatalf("Duck objectSubtype = %#v, want duck", duck["objectSubtype"])
	}
	duckMesh, ok := duck["mesh"].(map[string]any)
	if !ok || duckMesh["material"] != "water-duck-material" || duckMesh["castShadow"] != true || duckMesh["receiveShadow"] != true {
		t.Fatalf("Duck mesh patch = %#v, want material and shadow flags for dynamic model patches", duck["mesh"])
	}
	if duckMesh["rotationY"] != float64(0) || duckMesh["scaleX"] != float64(1) || duckMesh["scaleY"] != float64(1) || duckMesh["scaleZ"] != float64(1) {
		t.Fatalf("Duck mesh transform = %#v, want upstream-neutral transform before Scene3D model fit", duckMesh)
	}
	if duckMesh["bounds"] != float64(0.5) || duckMesh["fit"] != "contain" || duckMesh["fitAlign"] != "center-min-y" {
		t.Fatalf("Duck mesh fit metadata = %#v, want upstream duck normalization metadata", duckMesh)
	}
	duckSpheres, ok := duck["objectDisplacementSpheres"].([]any)
	if !ok || len(duckSpheres) != 3 {
		t.Fatalf("Duck objectDisplacementSpheres = %#v, want upstream 3-sphere volume approximation", duck["objectDisplacementSpheres"])
	}
	for index, want := range []map[string]float64{
		{"offsetX": 0, "offsetY": 0, "offsetZ": 0, "radius": 0.15},
		{"offsetX": 0, "offsetY": 0.1, "offsetZ": 0.1, "radius": 0.08},
		{"offsetX": 0, "offsetY": -0.08, "offsetZ": -0.05, "radius": 0.1},
	} {
		sphere, ok := duckSpheres[index].(map[string]any)
		if !ok {
			t.Fatalf("Duck displacement sphere %d = %T, want map[string]any", index, duckSpheres[index])
		}
		for field, wantValue := range want {
			if got, ok := sphere[field].(float64); !ok || got != wantValue {
				t.Fatalf("Duck displacement sphere %d field %s = %#v, want %v", index, field, sphere[field], wantValue)
			}
		}
	}
	torus := objects["TorusKnot"].(map[string]any)
	if torus["objectSubtype"] != "torusKnot" {
		t.Fatalf("TorusKnot objectSubtype = %#v, want torusKnot", torus["objectSubtype"])
	}
	torusMesh := torus["mesh"].(map[string]any)
	if got, ok := torusMesh["rotationX"].(float64); !ok || math.Abs(got-math.Pi/2) > 0.0000001 {
		t.Fatalf("TorusKnot mesh rotationX = %#v, want upstream flat Math.PI/2 orientation", torusMesh["rotationX"])
	}
	for _, name := range []string{"TorusKnot", "Rubber Duck"} {
		object := objects[name].(map[string]any)
		spheres, ok := object["objectDisplacementSpheres"].([]any)
		if !ok || len(spheres) == 0 {
			t.Fatalf("%s objectDisplacementSpheres = %#v, want non-empty list", name, object["objectDisplacementSpheres"])
		}
		if object["objectKind"] != "compound" {
			t.Fatalf("%s objectKind = %#v, want compound", name, object["objectKind"])
		}
	}
	if _, ok := data["waterMaterial"]; ok {
		t.Fatal("WaterDemoData still exposes route-side waterMaterial instead of using WaterSystem")
	}
	if _, ok := data["causticPrisms"]; ok {
		t.Fatal("WaterDemoData still exposes route-side causticPrisms instead of using WaterSystem caustics")
	}
	if _, ok := data["roundedPoolFloor"]; ok {
		t.Fatal("WaterDemoData still exposes route-side roundedPoolFloor geometry")
	}
	if _, ok := data["poolRimPoints"]; ok {
		t.Fatal("WaterDemoData still exposes route-side pool rim geometry instead of using WaterSystem pool pass")
	}
}

func TestWaterDemoControlsContract(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	pageBytes, err := os.ReadFile(filepath.Join(dir, "page.gsx"))
	if err != nil {
		t.Fatalf("read page.gsx: %v", err)
	}
	cssBytes, err := os.ReadFile(filepath.Join(dir, "page.css"))
	if err != nil {
		t.Fatalf("read page.css: %v", err)
	}
	runtimeBytes, err := os.ReadFile(filepath.Join(dir, "../../../../../client/js/bootstrap-src/19b-scene-control-forms.js"))
	if err != nil {
		t.Fatalf("read managed water controls runtime: %v", err)
	}
	webgpuBytes, err := os.ReadFile(filepath.Join(dir, "../../../../../client/js/bootstrap-src/16a-scene-webgpu.js"))
	if err != nil {
		t.Fatalf("read Scene3D WebGPU runtime: %v", err)
	}
	programBytes, err := os.ReadFile(filepath.Join(dir, "program.go"))
	if err != nil {
		t.Fatalf("read program.go: %v", err)
	}
	shaderSourceBytes, err := os.ReadFile(filepath.Join(dir, "shader_sources.go"))
	if err != nil {
		t.Fatalf("read shader_sources.go: %v", err)
	}
	page := string(pageBytes)
	css := string(cssBytes)
	runtimeSource := string(runtimeBytes)
	webgpuSource := string(webgpuBytes)
	program := string(programBytes)
	shaderSourceManifest := string(shaderSourceBytes)
	embeddedSources, err := waterShaderSources()
	if err != nil {
		t.Fatalf("read embedded water shader sources: %v", err)
	}
	authoredWaterSource := program + "\n" + shaderSourceManifest
	for _, body := range embeddedSources {
		authoredWaterSource += "\n" + body
	}
	if got := strings.Count(page, `y={10}`); got != 3 {
		t.Fatalf("water page hidden object y count = %d, want 3 upstream inactive y=10 placements", got)
	}
	if got := strings.Count(page, `visible={false}`); got != 3 {
		t.Fatalf("water page hidden object visibility count = %d, want 3 upstream inactive visible=false placements", got)
	}

	if strings.Contains(page, `data-water-`) {
		t.Fatalf("page.gsx still declares route-specific data-water attributes")
	}
	if strings.Contains(page, `<PostFX.`) {
		t.Fatalf("page.gsx still declares route-level post-processing for the upstream water demo")
	}

	for _, want := range []string{
		`data-gosx-scene3d-controls="true"`,
		`data-gosx-scene3d-control-form="fluid-object"`,
		`data-gosx-scene3d-control-subject="water-main"`,
		`data-gosx-scene3d-control-target="water-demo-scene"`,
		`data-gosx-scene3d-control-data={data.waterControlData}`,
		`data-gosx-scene3d-control-scope="true"`,
		`data-gosx-scene3d-panel-scope="true"`,
		`data-gosx-scene3d-control-open="false"`,
		`data-gosx-scene3d-control-toggle="true"`,
		`data-gosx-scene3d-control-body="true"`,
		`data-gosx-scene3d-control-group="Scene"`,
		`data-gosx-scene3d-control-group="Object"`,
		`data-gosx-scene3d-control-group="Pool"`,
		`data-gosx-scene3d-control-group="Lights"`,
		`data-gosx-scene3d-panel-toggle="water-demo-help"`,
		`data-gosx-scene3d-help-panel="true"`,
		`GoSX Water`,
		`jeantimex/threejs-water`,
		`Ported to GoSX by`,
		`Scroll or pinch to zoom`,
		`Press SPACEBAR to pause and unpause`,
		`controlTargetY={-0.5}`,
		`controlRotateMode="pixel-degrees"`,
		`controlMinDistance={2}`,
		`controlMaxDistance={10}`,
		`controlPitchLimit={1.5707788735}`,
		`<Camera x={1.2695827068526726} y={1.1904730469627978} z={3.395653196065958} fov={45} near={0.01} far={100} />`,
		`interactionProfile="water-object-drop-orbit"`,
		`interactionTarget="water-main"`,
		`interactionObject="Sphere"`,
		`poolWidth={1.0}`,
		`poolHeight={1.0}`,
		`poolLength={1.0}`,
		`objectX={-0.4}`,
		`objectY={-0.75}`,
		`objectZ={0.2}`,
		`objectRadius={0.25}`,
		`name="object"`,
		`value="TorusKnot"`,
		`value="Rubber Duck"`,
		`src="/water/models/duck/Duck.gltf"`,
		`rotationY={0}`,
		`scaleX={1}`,
		`scaleY={1}`,
		`scaleZ={1}`,
		`bounds={0.5}`,
		`fit="contain"`,
		`fitAlign="center-min-y"`,
		`name="gravity"`,
		`name="densityEnabled"`,
		`name="poolShape"`,
		`name="cornerRadius" min="0" max="1" step="0.01" value="0.1"`,
		`name="poolWidth" min="0.5" max="3" step="0.05" value="1"`,
		`name="poolHeight" min="0.3" max="2" step="0.05" value="1"`,
		`name="poolLength" min="0.5" max="3" step="0.05" value="1"`,
		`radius={0.25}`,
		`width={0.5}`,
		`tubularSegments={64}`,
		`radialSegments={8}`,
		`rotationX={1.5707963267948966}`,
		`data-gosx-scene3d-rounded-control="true"`,
		`data-gosx-scene3d-pool-boundary-control="true"`,
		`cubeMap="/water/"`,
		`shallowColor="#7ad1eb"`,
		`deepColor="#082e57"`,
		`lightDirectionX={2}`,
		`lightDirectionY={2}`,
		`lightDirectionZ={-1}`,
		`waveSpeed={1.0}`,
		`damping={0.995}`,
		`causticsResolution={1024}`,
		`objectTextureResolutionMode="viewport"`,
		`objectShadowResolution={1024}`,
		`computeSource={data.waterComputeSource}`,
		`materialSource={data.waterMaterialSource}`,
		`computeSourceFiles={data.waterComputeSourceFiles}`,
		`materialSourceFiles={data.waterMaterialSourceFiles}`,
		`seedWGSL={data.waterSeedWGSL}`,
		`dropWGSL={data.waterDropWGSL}`,
		`displacementWGSL={data.waterDisplacementWGSL}`,
		`simulationWGSL={data.waterSimulationWGSL}`,
		`normalWGSL={data.waterNormalWGSL}`,
		`causticsWGSL={data.waterCausticsWGSL}`,
		`poolVertexWGSL={data.waterPoolVertexWGSL}`,
		`poolFragmentWGSL={data.waterPoolFragmentWGSL}`,
		`surfaceVertexWGSL={data.waterSurfaceVertexWGSL}`,
		`surfaceFragmentWGSL={data.waterSurfaceFragmentWGSL}`,
		`surfaceBelowFragmentWGSL={data.waterSurfaceBelowFragmentWGSL}`,
		`objectShadowWGSL={data.waterObjectShadowWGSL}`,
		`objectMeshShadowVertexWGSL={data.waterObjectMeshShadowVertexWGSL}`,
		`objectMeshShadowFragmentWGSL={data.waterObjectMeshShadowFragmentWGSL}`,
		`name="water-object-material"`,
		`kind="custom"`,
		`shaderBackend="selena"`,
		`shaderSource={data.waterObjectMaterialSource}`,
		`shaderSourceFiles={data.waterObjectMaterialSourceFiles}`,
		`customVertexWGSL={data.waterObjectMaterialWGSL}`,
		`customFragmentWGSL={data.waterObjectMaterialWGSL}`,
		`shaderLayout={data.waterObjectMaterialLayout}`,
		`customUniforms={data.waterObjectMaterialUniforms}`,
		`name="water-duck-material"`,
		`shaderSource={data.waterDuckMaterialSource}`,
		`shaderSourceFiles={data.waterDuckMaterialSourceFiles}`,
		`customVertexWGSL={data.waterDuckMaterialWGSL}`,
		`customFragmentWGSL={data.waterDuckMaterialWGSL}`,
		`shaderLayout={data.waterDuckMaterialLayout}`,
		`customUniforms={data.waterDuckMaterialUniforms}`,
		`material="water-object-material"`,
		`material="water-duck-material"`,
		`castShadow={true}`,
		`receiveShadow={true}`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("page.gsx missing %q", want)
		}
	}
	objectOptionOrder := []string{`value="None"`, `value="Sphere"`, `value="Cube"`, `value="TorusKnot"`, `value="Rubber Duck"`}
	lastObjectOptionIndex := -1
	for _, option := range objectOptionOrder {
		index := strings.Index(page, option)
		if index < 0 {
			t.Fatalf("page.gsx missing object option %q", option)
		}
		if index <= lastObjectOptionIndex {
			t.Fatalf("page.gsx object option %q appears out of upstream order", option)
		}
		lastObjectOptionIndex = index
	}
	for _, unwanted := range []string{
		`water-demo__overlay`,
		`water-demo__readout`,
		`Selena Surface`,
		`splash kernel`,
		`rotationY={-0.45}`,
		`scaleX={0.018}`,
		`scaleY={0.018}`,
		`scaleZ={0.018}`,
	} {
		if strings.Contains(page, unwanted) {
			t.Fatalf("page.gsx contains non-upstream overlay marker %q", unwanted)
		}
	}
	for _, asset := range []string{"xpos.jpg", "xneg.jpg", "ypos.jpg", "zpos.jpg", "zneg.jpg"} {
		path := filepath.Join(dir, "../../../public/water", asset)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("missing upstream cubemap asset %s: %v", asset, err)
		}
		if info.Size() == 0 {
			t.Fatalf("upstream cubemap asset %s is empty", asset)
		}
	}
	if strings.Contains(page, `src="/water-controls.js"`) {
		t.Fatalf("page.gsx still references route-specific water-controls.js")
	}
	for _, bad := range []string{
		`name="gravity" checked={true}`,
		`name="densityEnabled" checked={true}`,
	} {
		if strings.Contains(page, bad) {
			t.Fatalf("water controls should match upstream unchecked defaults, found %q", bad)
		}
	}
	for _, want := range []string{
		".water-demo__scene {\n  position: absolute !important;\n  top: 0 !important;\n  right: min(20rem, calc(100vw - 3.5rem)) !important;",
		".water-demo__help[data-gosx-scene3d-panel-open=\"false\"] {\n  transform: translateX(0);",
		".water-demo__help-toggle {\n  position: absolute;",
		"display: none;",
		"@media (max-width: 720px)",
		".water-demo__scene {\n    right: 0 !important;\n    width: 100% !important;",
		".water-demo__help[data-gosx-scene3d-panel-open=\"false\"] {\n    transform: translateX(100%);",
		".water-demo__help-toggle {\n    display: grid;",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("page.css missing upstream-style responsive help panel contract %q", want)
		}
	}
	for _, stale := range []string{
		`pool-north-wall`,
		`pool-south-wall`,
		`pool-west-wall`,
		`pool-east-wall`,
		`pool-rim-lines`,
		`pool-wall`,
		`pool-rim`,
		`data.poolRimPoints`,
		`data.poolRimSegments`,
	} {
		if strings.Contains(page, stale) {
			t.Fatalf("page.gsx still contains route-authored pool geometry %q", stale)
		}
	}
	for _, want := range []string{
		"bindSceneManagedControlForms",
		"registerSceneManagedControlProfile",
		"sceneManagedControlProfiles",
		"sceneManagedControlData",
		"sceneManagedControlScope",
		"sceneManagedControlBindDisclosure",
		"sceneManagedControlBindPanelToggles",
		"data-gosx-scene3d-control-toggle",
		"data-gosx-scene3d-control-open",
		"data-gosx-scene3d-panel-toggle",
		"data-gosx-scene3d-panel-open",
		"data-gosx-scene3d-control-scope",
		"data-gosx-scene3d-panel-scope",
		"data-gosx-scene3d-active-panel",
		"sceneManagedFluidObjectProfile",
		"data-gosx-scene3d-control-data",
		"data-gosx-scene3d-control-data-ref",
		"SCENE_CMD_SET_PARTICLES",
		"SCENE_CMD_SET_MODELS",
		"applyCommands",
		"objectDisplacementScale",
		"objectDisplacementSpheres",
		"sceneManagedFluidObjectControlState",
		"sceneManagedFluidObjectQueueObjectDisplacementEvent",
		"objectDisplacementEvents",
		"sceneManagedFluidObjectPoolKey",
		"controlState.poolKey = poolKey",
		"sceneManagedFluidObjectObserveSelection",
		"sceneManagedFluidObjectInactiveY",
		"sceneManagedFluidObjectStepPhysics",
		"sceneManagedFluidObjectPhysicsSettings",
		"sceneManagedFluidObjectInteractionSettings",
		"SCENE_MANAGED_FLUID_OBJECT_MIN_STRAIGHT_POOL_EDGE",
		"sceneManagedFluidObjectMaxCornerRadius",
		"sceneManagedFluidObjectClampCornerRadius",
		"cornerField.max = String(maxCornerRadius)",
		"form.dataset.maxCornerRadius = String(rounded ? maxCornerRadius : 0)",
		"sceneManagedFluidObjectQueueDrop",
		"sceneManagedFluidObjectCameraLightDirection",
		"sceneManagedFluidObjectSyncLightDirection",
		"return { x: 2, y: 2, z: -1 };",
		"next.lightDirectionY = sceneNumber(lightDirection.y, 2);",
		"data-gosx-scene3d-fluid-light-y",
		"sceneNumber(light.y, 2)",
		"settingLightDirection",
		"sceneManagedFluidObjectApply(form, sceneState, applyCommands, options)",
		"sceneManagedFluidObjectInteractionProfile",
		"data-gosx-scene3d-interaction-profile",
		"water-object-drop-orbit",
		"sceneManagedFluidObjectStartInteraction",
		"sceneManagedFluidObjectDragObject",
		"sceneRayIntersectPlane",
		"pointerMode = \"MoveObject\"",
		"pointerMode = \"AddDrops\"",
		"pointerMode = \"OrbitCamera\"",
		"dropEventID",
		"dropEventRadius",
		"dropEventStrength",
		"data-gosx-scene3d-fluid-drop-events",
		"event.code === \"Space\"",
		"event.code === \"KeyG\"",
		"event.code === \"KeyL\"",
		"sceneManagedFluidObjectObjectStep",
		"poolChanged",
		"syncPoolPrevious",
		"objectPreviousSet",
		"objectPreviousY",
		"sceneManagedFluidObjectSetDisabled",
		"const physicsAvailable = active !== \"None\"",
		"form.dataset.physicsAvailable = String(physicsAvailable)",
		"form.dataset.densityOpen = String(physicsAvailable && controls.densityEnabled)",
		"if (field.disabled) return false",
		"form.dataset.gosxScene3dFluidObjectControlsReady",
		"data-gosx-scene3d-fluid-object-controls-ready",
		"paused",
	} {
		if !strings.Contains(runtimeSource, want) {
			t.Fatalf("managed water controls runtime missing %q", want)
		}
	}
	for _, want := range []string{
		"sceneWaterAuthoredComputePipeline",
		"seedWGSL",
		"dropWGSL",
		"displacementWGSL",
		"simulationWGSL",
		"normalWGSL",
		"causticsWGSL",
		"poolVertexWGSL",
		"poolFragmentWGSL",
		"waterAuthoredComputeDispatches",
		"data-gosx-scene3d-webgpu-water-authored-compute-dispatches",
		"sceneWaterAuthoredCausticsPipeline",
		"waterAuthoredCausticPasses",
		"data-gosx-scene3d-webgpu-water-authored-caustic-passes",
		"sceneWaterAuthoredPoolVertexSource",
		"sceneWaterAuthoredPoolFragmentSource",
		"data-gosx-scene3d-webgpu-water-authored-pool-passes",
		"data-gosx-scene3d-webgpu-water-authored-pool-fragment-passes",
		"sceneWaterAuthoredSurfaceFragmentSource",
		"surfaceFragmentWGSL",
		"surfaceVertexWGSL",
		"surfaceBelowFragmentWGSL",
		"objectShadowWGSL",
		"objectMeshShadowVertexWGSL",
		"objectMeshShadowFragmentWGSL",
		"sceneWaterAuthoredObjectShadowPipeline",
		"sceneWaterAuthoredObjectMeshShadowPipeline",
		"data-gosx-scene3d-webgpu-water-authored-object-shadow-passes",
		"data-gosx-scene3d-webgpu-water-authored-object-mesh-shadow-passes",
		"objectTextureMatrixBuffer",
		"writeWaterObjectTextureMatrices",
		"objectReflectionViewProjectionMatrix",
		"binding: 8",
		"sceneSelenaLiveTextureView",
		"sceneSelenaLiveBuffer",
		"sceneSelenaStorageBufferDescriptors",
		"gosx:",
		"waterAuthoredSurfaceDrawCalls",
		"data-gosx-scene3d-webgpu-water-authored-surface-draw-calls",
		"data-gosx-scene3d-webgpu-water-authored-surface-vertex-draw-calls",
	} {
		if !strings.Contains(webgpuSource, want) {
			t.Fatalf("Scene3D WebGPU water runtime missing %q", want)
		}
	}
	for _, stale := range []string{
		"SCENE_MANAGED_WATER_OBJECTS",
		"sceneManagedFluidObjectTorusKnotDisplacementSpheres",
		"sceneManagedFluidObjectDuckDisplacementSpheres",
		"/water/models/duck/Duck.gltf",
		`objectKind: "compound"`,
		"float-duck",
		"config.objectY - 0.08",
		"controls.densityEnabled ? controls.density",
		"sceneManagedFluidObjectRoundedFloorVertices",
		"sceneManagedFluidObjectRoundedWallVertices",
		"pool-rounded-floor",
		"pool-rounded-walls",
		"water-surface",
		"caustic-floor",
		"foam-rings",
		"pool-north-wall",
		"pool-south-wall",
		"pool-west-wall",
		"pool-east-wall",
		"pool-rim-lines",
		"controls.followCamera ? -0.45 : 2",
		`"rotationY":-0.45`,
		`"scaleX":0.018`,
		`"scaleY":0.018`,
		`"scaleZ":0.018`,
		"return { x: 2, y: 3, z: -1 };",
		"next.lightDirectionY = sceneNumber(lightDirection.y, 3);",
		"sceneNumber(light.y, 3)",
		`sceneManagedFluidObjectSetChecked(form, "followCamera", true)`,
		`sceneManagedFluidObjectSetChecked(form, "followCamera", false)`,
	} {
		if strings.Contains(runtimeSource, stale) {
			t.Fatalf("managed controls runtime still hardcodes water profile data %q", stale)
		}
	}
	if strings.Contains(runtimeSource, "bindsceneManagedFluidObjectControls") {
		t.Fatalf("managed controls runtime still exports water-specific bootstrap binder")
	}
	if strings.Contains(runtimeSource, "sceneManagedWater") ||
		strings.Contains(runtimeSource, `registerSceneManagedControlProfile("water"`) {
		t.Fatalf("managed controls runtime still exposes water-specific control profile symbols")
	}
	if strings.Contains(runtimeSource, `data-gosx-scene3d-water-controls"`) ||
		strings.Contains(runtimeSource, `data-gosx-scene3d-water-controls]`) {
		t.Fatalf("managed controls runtime still accepts the old water-specific form hook")
	}
	if strings.Contains(runtimeSource, `data-water-`) {
		t.Fatalf("managed controls runtime still depends on route-specific data-water attributes")
	}
	for _, want := range []string{
		"waterControlDataJSON",
		"waterShaderSources()",
		"waterShaderSourceFiles",
		"waterComputeSourceFiles",
		"waterMaterialSourceFiles",
		"shaders/jeantimex-water.elio/displacement.elio",
		"shaders/jeantimex-water.elio/simulation.elio",
		"shaders/jeantimex-water.elio/normal.elio",
		"shaders/jeantimex-water.sel/caustics.sel",
		"shaders/jeantimex-water.sel/surface.fragment.sel",
		"shaders/jeantimex-water.sel/surface-below.fragment.sel",
		"WaterObjectTextureMatrices",
		"sampleProjectedTexture",
		"fn displaceObject",
		"fn stepSimulation",
		"fn fragmentMain",
		"torusKnotDisplacementSpheres",
		"duckDisplacementSpheres",
		"/water/models/duck/Duck.gltf",
		`"bounds": 0.5`,
		`"fit": "contain"`,
		`"fitAlign": "center-min-y"`,
	} {
		if !strings.Contains(authoredWaterSource, want) {
			t.Fatalf("authored water source missing %q", want)
		}
	}
	for _, stale := range []string{
		"const waterElio",
		"const waterSelena",
		"const waterObjectMaterialWGSL",
		"const waterDuckMaterialWGSL",
		`"pool-rounded-floor"`,
		`"pool-rounded-walls"`,
		"roundedPoolFloorGeometry",
		"roundedPoolWallGeometry",
		"dropletKernelWGSL",
		"causticPrismProps",
		"materialAttrs",
		"poolRimPoints",
		"poolRimSegments",
		`"rotationY":-0.45`,
		`"scaleX":0.018`,
		`"scaleY":0.018`,
		`"scaleZ":0.018`,
	} {
		if strings.Contains(program, stale) {
			t.Fatalf("program.go still contains route-side water approximation %q", stale)
		}
	}
}

// Rigid imported-model regression fixture. All scene authoring is Go/GoSX.
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

func main() {
	address := flag.String("listen", "127.0.0.1:8179", "fixture listen address")
	flag.Parse()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Dir(source)
	framework := filepath.Clean(filepath.Join(root, "../.."))
	err := route.RegisterFileModule(route.FileModuleFor(filepath.Join(root, "app/page.gsx"), route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, _ route.FilePage) (any, error) {
			count, _ := strconv.Atoi(ctx.Query("count"))
			if count < 1 || count > 2048 {
				count = 256
			}
			mode := ctx.Query("mode")
			backend := ctx.Query("backend")
			return map[string]any{"scene": crowdScene(count, mode == "models", backend == "webgl", ctx.Query("shadows") == "true"), "count": count, "mode": mode, "backend": backend}, nil
		},
	}))
	if err != nil {
		log.Fatal(err)
	}
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(ctx.Document("Rigid GLB crowd", body))
	})
	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		log.Fatal(err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		log.Fatal(err)
	}
	app := server.New()
	app.SetRuntimeRoot(framework)
	app.Mount("/", handler)
	mux := http.NewServeMux()
	// Serve the exact generated framework chunks from this checkout.
	mux.Handle("/gosx/", http.StripPrefix("/gosx/", http.FileServer(http.Dir(filepath.Join(framework, "client/js")))))
	model := crowdGLB()
	mux.HandleFunc("/actor.glb", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "model/gltf-binary")
		_, _ = w.Write(model)
	})
	mux.Handle("/", app.Build())
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	log.Printf("rigid GLB fixture: http://%s", *address)
	log.Fatal(http.ListenAndServe(*address, mux))
}

func crowdScene(count int, ordinary, webgl, shadows bool) scene.Props {
	columns := int(math.Ceil(math.Sqrt(float64(count))))
	instances := make([]scene.MeshInstance, count)
	for i := range instances {
		instances[i] = scene.MeshInstance{ID: fmt.Sprintf("actor-%d", i), Position: scene.Vec3((float64(i%columns)-float64(columns-1)/2)*1.4, (float64(i/columns)-float64(columns-1)/2)*1.4, 0), Scale: scene.Vec3(0.48, 0.65, 0.4), Rotation: scene.Rotate(0.2, float64(i%7)*0.12, 0.15)}
	}
	nodes := []scene.Node{
		scene.DirectionalLight{ID: "key", Color: "#f5daaf", Intensity: 3, Direction: scene.Vec3(-0.4, -0.6, -1), CastShadow: shadows},
		scene.Mesh{ID: "receiver", Geometry: scene.PlaneGeometry{Width: float64(columns) * 1.5, Height: float64(columns) * 1.5}, Position: scene.Vec3(0, 0, -1), Material: scene.StandardMaterial{Color: "#182534", Roughness: 0.8}, ReceiveShadow: true},
	}
	if ordinary {
		for _, instance := range instances {
			nodes = append(nodes, scene.Model{ID: instance.ID, Src: "/actor.glb", Position: instance.Position, Scale: instance.Scale, Rotation: instance.Rotation, Pickable: scene.Bool(true), CastShadow: true, ReceiveShadow: true})
		}
	} else {
		nodes = append(nodes, scene.InstancedGLBMesh{ID: "crowd", Src: "/actor.glb", Instances: instances, Pickable: scene.Bool(true)})
	}
	return scene.Props{Width: 1280, Height: 720, Background: "#070c13", Controls: "orbit", AutoRotate: scene.Bool(true), ForceWebGL: scene.Bool(webgl), PreferWebGPU: scene.Bool(!webgl),
		Camera:      scene.PerspectiveCamera{Position: scene.Vec3(0, 0, float64(columns)*1.55), FOV: 60, Near: 0.1, Far: 200},
		Environment: scene.Environment{AmbientColor: "#8bc5ee", AmbientIntensity: 0.6}, Graph: scene.NewGraph(nodes...)}
}

// A two-material indexed glTF primitive fixture, with no external assets.
func crowdGLB() []byte {
	positions := []float32{0, 1, 0, 1, 0, 0, 0, 0, 1, -1, 0, 0, 0, 0, -1, 0, -1, 0}
	indices := []uint16{0, 2, 1, 0, 3, 2, 0, 4, 3, 0, 1, 4, 5, 1, 2, 5, 2, 3, 5, 3, 4, 5, 4, 1}
	bin := make([]byte, len(positions)*8+len(indices)*2)
	for i, v := range positions {
		binary.LittleEndian.PutUint32(bin[i*4:], math.Float32bits(v))
		binary.LittleEndian.PutUint32(bin[len(positions)*4+i*4:], math.Float32bits(v))
	}
	for i, v := range indices {
		binary.LittleEndian.PutUint16(bin[len(positions)*8+i*2:], v)
	}
	root := map[string]any{
		"asset": map[string]any{"version": "2.0", "generator": "GoSX rigid GLB fixture"}, "scene": 0, "scenes": []any{map[string]any{"nodes": []int{0}}},
		"nodes":       []any{map[string]any{"mesh": 0, "extras": map[string]any{"castShadow": true, "receiveShadow": true}}},
		"buffers":     []any{map[string]any{"byteLength": len(bin)}},
		"bufferViews": []any{map[string]any{"buffer": 0, "byteLength": 72}, map[string]any{"buffer": 0, "byteOffset": 72, "byteLength": 72}, map[string]any{"buffer": 0, "byteOffset": 144, "byteLength": 24}, map[string]any{"buffer": 0, "byteOffset": 168, "byteLength": 24}},
		"accessors":   []any{map[string]any{"bufferView": 0, "componentType": 5126, "count": 6, "type": "VEC3", "min": []int{-1, -1, -1}, "max": []int{1, 1, 1}}, map[string]any{"bufferView": 1, "componentType": 5126, "count": 6, "type": "VEC3"}, map[string]any{"bufferView": 2, "componentType": 5123, "count": 12, "type": "SCALAR"}, map[string]any{"bufferView": 3, "componentType": 5123, "count": 12, "type": "SCALAR"}},
		"meshes":      []any{map[string]any{"primitives": []any{map[string]any{"attributes": map[string]int{"POSITION": 0, "NORMAL": 1}, "indices": 2, "material": 0}, map[string]any{"attributes": map[string]int{"POSITION": 0, "NORMAL": 1}, "indices": 3, "material": 1}}}},
		"materials":   []any{map[string]any{"pbrMetallicRoughness": map[string]any{"baseColorFactor": []float64{0.05, 0.8, 0.65, 1}, "metallicFactor": 0.6, "roughnessFactor": 0.3}}, map[string]any{"pbrMetallicRoughness": map[string]any{"baseColorFactor": []float64{0.55, 0.15, 0.8, 1}, "metallicFactor": 0.2, "roughnessFactor": 0.6}}},
	}
	jsonBytes, err := json.Marshal(root)
	if err != nil {
		panic(err)
	}
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, ' ')
	}
	out := make([]byte, 12+8+len(jsonBytes)+8+len(bin))
	copy(out, "glTF")
	binary.LittleEndian.PutUint32(out[4:], 2)
	binary.LittleEndian.PutUint32(out[8:], uint32(len(out)))
	binary.LittleEndian.PutUint32(out[12:], uint32(len(jsonBytes)))
	copy(out[16:], "JSON")
	copy(out[20:], jsonBytes)
	offset := 20 + len(jsonBytes)
	binary.LittleEndian.PutUint32(out[offset:], uint32(len(bin)))
	copy(out[offset+4:], "BIN\x00")
	copy(out[offset+8:], bin)
	return out
}

package docs

import (
	"encoding/binary"
	"encoding/json"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type waterDuckGLTF struct {
	Accessors []struct {
		BufferView    int       `json:"bufferView"`
		ByteOffset    int       `json:"byteOffset"`
		ComponentType int       `json:"componentType"`
		Count         int       `json:"count"`
		Type          string    `json:"type"`
		Min           []float64 `json:"min"`
		Max           []float64 `json:"max"`
	} `json:"accessors"`
	BufferViews []struct {
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
		ByteStride int `json:"byteStride"`
	} `json:"bufferViews"`
	Buffers []struct {
		URI        string `json:"uri"`
		ByteLength int    `json:"byteLength"`
	} `json:"buffers"`
	Images []struct {
		URI string `json:"uri"`
	} `json:"images"`
	Meshes []struct {
		Primitives []struct {
			Attributes map[string]int `json:"attributes"`
			Indices    int            `json:"indices"`
		} `json:"primitives"`
	} `json:"meshes"`
}

func waterDuckFloatAccessor(t *testing.T, asset waterDuckGLTF, bin []byte, index, width int) []float32 {
	t.Helper()
	if index < 0 || index >= len(asset.Accessors) {
		t.Fatalf("duck accessor index %d is out of range", index)
	}
	accessor := asset.Accessors[index]
	if accessor.ComponentType != 5126 {
		t.Fatalf("duck accessor %d component type = %d, want FLOAT", index, accessor.ComponentType)
	}
	view := asset.BufferViews[accessor.BufferView]
	stride := view.ByteStride
	if stride == 0 {
		stride = width * 4
	}
	out := make([]float32, 0, accessor.Count*width)
	for row := 0; row < accessor.Count; row++ {
		base := view.ByteOffset + accessor.ByteOffset + row*stride
		for component := 0; component < width; component++ {
			offset := base + component*4
			if offset < 0 || offset+4 > len(bin) {
				t.Fatalf("duck accessor %d reads beyond Duck0.bin at byte %d", index, offset)
			}
			out = append(out, math.Float32frombits(binary.LittleEndian.Uint32(bin[offset:offset+4])))
		}
	}
	return out
}

func TestWaterDuckAssetGeometryAndAlbedoContract(t *testing.T) {
	assetDir := filepath.Join("..", "..", "..", "public", "water", "models", "duck")
	gltfBytes, err := os.ReadFile(filepath.Join(assetDir, "Duck.gltf"))
	if err != nil {
		t.Fatal(err)
	}
	var asset waterDuckGLTF
	if err := json.Unmarshal(gltfBytes, &asset); err != nil {
		t.Fatalf("decode Duck.gltf: %v", err)
	}
	if len(asset.Meshes) != 1 || len(asset.Meshes[0].Primitives) != 1 {
		t.Fatalf("Duck.gltf meshes/primitives = %d/%d, want one textured primitive", len(asset.Meshes), len(asset.Meshes[0].Primitives))
	}
	primitive := asset.Meshes[0].Primitives[0]
	positionIndex, hasPosition := primitive.Attributes["POSITION"]
	normalIndex, hasNormal := primitive.Attributes["NORMAL"]
	uvIndex, hasUV := primitive.Attributes["TEXCOORD_0"]
	if !hasPosition || !hasNormal || !hasUV || primitive.Indices < 0 {
		t.Fatalf("duck primitive attributes = %#v, indices = %d; want POSITION/NORMAL/TEXCOORD_0/index data", primitive.Attributes, primitive.Indices)
	}
	if asset.Accessors[positionIndex].Count < 2000 || asset.Accessors[primitive.Indices].Count/3 < 4000 {
		t.Fatalf("duck geometry is too coarse: %d vertices, %d triangles", asset.Accessors[positionIndex].Count, asset.Accessors[primitive.Indices].Count/3)
	}
	if asset.Accessors[normalIndex].Count != asset.Accessors[positionIndex].Count || asset.Accessors[uvIndex].Count != asset.Accessors[positionIndex].Count {
		t.Fatalf("duck attribute counts diverge: position=%d normal=%d uv=%d", asset.Accessors[positionIndex].Count, asset.Accessors[normalIndex].Count, asset.Accessors[uvIndex].Count)
	}
	if len(asset.Buffers) != 1 || asset.Buffers[0].URI != "Duck0.bin" || len(asset.Images) != 1 || asset.Images[0].URI != "DuckCM.png" {
		t.Fatalf("duck external resources = buffers %#v images %#v, want Duck0.bin + DuckCM.png", asset.Buffers, asset.Images)
	}

	bin, err := os.ReadFile(filepath.Join(assetDir, asset.Buffers[0].URI))
	if err != nil {
		t.Fatal(err)
	}
	if len(bin) < asset.Buffers[0].ByteLength {
		t.Fatalf("Duck0.bin length = %d, glTF declares %d", len(bin), asset.Buffers[0].ByteLength)
	}
	normals := waterDuckFloatAccessor(t, asset, bin, normalIndex, 3)
	unitNormals := 0
	for index := 0; index < len(normals); index += 3 {
		length := math.Sqrt(float64(normals[index]*normals[index] + normals[index+1]*normals[index+1] + normals[index+2]*normals[index+2]))
		if length >= 0.95 && length <= 1.05 {
			unitNormals++
		}
	}
	if float64(unitNormals) < float64(len(normals)/3)*0.99 {
		t.Fatalf("only %d/%d duck normals are unit length", unitNormals, len(normals)/3)
	}
	uvs := waterDuckFloatAccessor(t, asset, bin, uvIndex, 2)
	minUV, maxUV := [2]float32{1, 1}, [2]float32{0, 0}
	for index := 0; index < len(uvs); index += 2 {
		for component := 0; component < 2; component++ {
			value := uvs[index+component]
			if value < -0.001 || value > 1.001 {
				t.Fatalf("duck UV[%d] = %f, want normalized atlas coordinates", index+component, value)
			}
			minUV[component] = min(minUV[component], value)
			maxUV[component] = max(maxUV[component], value)
		}
	}
	if maxUV[0]-minUV[0] < 0.9 || maxUV[1]-minUV[1] < 0.9 {
		t.Fatalf("duck UV atlas coverage = U %.3f V %.3f, want broad texture coverage", maxUV[0]-minUV[0], maxUV[1]-minUV[1])
	}

	textureFile, err := os.Open(filepath.Join(assetDir, asset.Images[0].URI))
	if err != nil {
		t.Fatal(err)
	}
	defer textureFile.Close()
	texture, err := png.Decode(textureFile)
	if err != nil {
		t.Fatalf("decode DuckCM.png: %v", err)
	}
	bounds := texture.Bounds()
	if bounds.Dx() < 512 || bounds.Dy() < 512 {
		t.Fatalf("DuckCM.png = %dx%d, want at least 512x512", bounds.Dx(), bounds.Dy())
	}
	yellow, orange, dark := 0, 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r16, g16, b16, a16 := texture.At(x, y).RGBA()
			if a16 < 0x8000 {
				continue
			}
			r, g, b := uint8(r16>>8), uint8(g16>>8), uint8(b16>>8)
			switch {
			case r > 180 && g > 130 && b < 110:
				yellow++
			case r > 180 && g > 45 && g < 150 && b < 80:
				orange++
			case r < 55 && g < 55 && b < 55:
				dark++
			}
		}
	}
	if yellow < 10000 || orange < 100 || dark < 100 {
		t.Fatalf("DuckCM.png color evidence yellow/orange/dark = %d/%d/%d, want recognizable body/bill/eye detail", yellow, orange, dark)
	}
}

func TestWaterDuckStartsAtBuoyantReadableWaterline(t *testing.T) {
	encoded, err := waterControlDataJSON()
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Physics struct {
			DefaultBuoyancyScale float64 `json:"defaultBuoyancyScale"`
		} `json:"physics"`
		Objects map[string]struct {
			ObjectY        float64 `json:"objectY"`
			ObjectRadius   float64 `json:"objectRadius"`
			BuoyancyRadius float64 `json:"buoyancyRadius"`
			FloorClearance float64 `json:"floorClearance"`
			XLimitRadius   float64 `json:"xLimitRadius"`
			ZLimitRadius   float64 `json:"zLimitRadius"`
			Mesh           struct {
				Y      float64 `json:"y"`
				Bounds float64 `json:"bounds"`
			} `json:"mesh"`
		} `json:"objects"`
	}
	if err := json.Unmarshal([]byte(encoded), &profile); err != nil {
		t.Fatal(err)
	}
	duck, ok := profile.Objects["Rubber Duck"]
	if !ok {
		t.Fatal("water control profile lost Rubber Duck")
	}
	equilibriumY := duck.BuoyancyRadius - 2*duck.BuoyancyRadius/profile.Physics.DefaultBuoyancyScale
	if math.Abs(duck.ObjectY-equilibriumY) > 0.02 {
		t.Fatalf("duck objectY = %.3f, natural buoyancy line = %.3f", duck.ObjectY, equilibriumY)
	}
	if math.Abs((duck.FloorClearance-1)-duck.ObjectY) > 0.000001 {
		t.Fatalf("duck switch clamp = %.3f, objectY = %.3f; a preceding floor object would submerge it", duck.FloorClearance-1, duck.ObjectY)
	}
	if duck.Mesh.Y != duck.ObjectY {
		t.Fatalf("duck mesh Y = %.3f, physics Y = %.3f", duck.Mesh.Y, duck.ObjectY)
	}
	if duck.Mesh.Bounds != 0.65 || duck.ObjectRadius != 0.32 || duck.XLimitRadius != 0.32 || duck.ZLimitRadius != 0.32 {
		t.Fatalf("duck visual/collision bounds = mesh %.2f object %.2f limits %.2f/%.2f, want coherent 0.65/0.32 scale", duck.Mesh.Bounds, duck.ObjectRadius, duck.XLimitRadius, duck.ZLimitRadius)
	}

	shader, err := waterSelenaFS.ReadFile("shaders/jeantimex-water.selena/duck-material.sel")
	if err != nil {
		t.Fatal(err)
	}
	source := string(shader)
	if !strings.Contains(source, "let underwater = vec3f(0.86, 0.96, 1.0)") || strings.Contains(source, "vec3f(0.4, 0.9, 1.0)") {
		t.Fatal("duck material no longer preserves yellow albedo in shallow water")
	}
}

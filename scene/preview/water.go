package preview

import (
	"fmt"
	"math"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
)

// appendNativeWaterPreview lowers WaterSystem into a deterministic analytic
// heightfield plus pool geometry. It deliberately uses the ordinary native
// mesh/material pipeline, making water framing, motion, topology, lighting,
// and nonblank optics observable without a browser or platform GPU. Selena
// artifact execution remains separately evidenced by scene/harness hashes and
// descriptors; this fallback does not claim to interpret WGSL on the CPU.
func appendNativeWaterPreview(frame *engine.RenderBundle, materials map[string]int, water scene.WaterSystemIR, time float64) {
	width := positiveWaterValue(water.PoolWidth, 1)
	length := positiveWaterValue(water.PoolLength, 1)
	depth := positiveWaterValue(water.PoolHeight, 1)
	grid := water.SurfaceResolution
	if grid < 2 {
		grid = water.Resolution
	}
	grid = min(max(grid, 12), 64)

	poolMaterial := engine.RenderMaterial{
		Key: "native-water-pool:" + water.ID, Kind: "standard", Color: firstWaterColor(water.DeepColor, "#082e57"),
		Opacity: 1, Roughness: 0.72, Metalness: 0.04,
	}
	poolMaterialIndex := ensureMaterial(frame, materials, poolMaterial)
	appendWaterBox(frame, water.ID+"-native-floor", poolMaterialIndex, 0, -depth-0.035, 0, width*2.08, 0.07, length*2.08)
	wallMaterial := engine.RenderMaterial{
		Key: "native-water-wall:" + water.ID, Kind: "glass", Color: firstWaterColor(water.DeepColor, "#0d5270"),
		Opacity: 0.48, BlendMode: "alpha", RenderPass: "alpha", Roughness: 0.2, Transmission: 0.35, Clearcoat: 0.3,
	}
	wallMaterialIndex := ensureMaterial(frame, materials, wallMaterial)
	wallThickness := math.Max(math.Min(width, length)*0.055, 0.035)
	wallHeight := depth + 0.18
	appendWaterBox(frame, water.ID+"-native-wall-left", wallMaterialIndex, -width-wallThickness/2, -depth/2, 0, wallThickness, wallHeight, length*2.12)
	appendWaterBox(frame, water.ID+"-native-wall-right", wallMaterialIndex, width+wallThickness/2, -depth/2, 0, wallThickness, wallHeight, length*2.12)
	appendWaterBox(frame, water.ID+"-native-wall-back", wallMaterialIndex, 0, -depth/2, -length-wallThickness/2, width*2.12, wallHeight, wallThickness)
	appendWaterBox(frame, water.ID+"-native-wall-front", wallMaterialIndex, 0, -depth/2, length+wallThickness/2, width*2.12, wallHeight, wallThickness)

	surfaceMaterial := engine.RenderMaterial{
		Key: "native-water-surface:" + water.ID, Kind: "glass", Color: firstWaterColor(water.ShallowColor, "#7ad1eb"),
		Opacity: 0.68, BlendMode: "alpha", RenderPass: "alpha", Roughness: 0.08,
		Clearcoat: 0.92, Transmission: 0.78, Iridescence: 0.08,
	}
	surfaceMaterialIndex := ensureMaterial(frame, materials, surfaceMaterial)
	cellWidth := width * 2 / float64(grid)
	cellLength := length * 2 / float64(grid)
	transforms := make([]float64, 0, grid*grid*16)
	for z := 0; z < grid; z++ {
		wz := -length + (float64(z)+0.5)*cellLength
		for x := 0; x < grid; x++ {
			wx := -width + (float64(x)+0.5)*cellWidth
			height, slopeX, slopeZ := nativeWaterSample(water, wx/width, wz/length, time)
			rx := -math.Atan(slopeZ)
			rz := math.Atan(slopeX)
			transforms = append(transforms, trsMatrix(wx, height, wz, rx, 0, rz, 1, 1, 1)...)
		}
	}
	frame.InstancedMeshes = append(frame.InstancedMeshes, engine.RenderInstancedMesh{
		ID: water.ID + "-native-surface", Kind: "box", Width: cellWidth * 1.08, Height: 0.008, Depth: cellLength * 1.08,
		MaterialIndex: surfaceMaterialIndex, InstanceCount: grid * grid, Transforms: transforms, ReceiveShadow: true,
	})

	frame.Diagnostics = append(frame.Diagnostics, engine.RenderDiagnostic{
		Severity: "info", Code: "scene.preview.analytic_water", Backend: "headless", Target: water.ID,
		Message: fmt.Sprintf("native analytic water preview: %dx%d heightfield; Selena artifacts certified separately", grid, grid),
	})
}

func appendWaterBox(frame *engine.RenderBundle, id string, materialIndex int, x, y, z, width, height, depth float64) {
	frame.InstancedMeshes = append(frame.InstancedMeshes, engine.RenderInstancedMesh{
		ID: id, Kind: "box", Width: width, Height: height, Depth: depth,
		MaterialIndex: materialIndex, InstanceCount: 1, Transforms: trsMatrix(x, y, z, 0, 0, 0, 1, 1, 1), ReceiveShadow: true,
	})
}

func nativeWaterSample(water scene.WaterSystemIR, x, z, time float64) (height, slopeX, slopeZ float64) {
	count := water.SeedDrops
	if count <= 0 {
		count = 7
	}
	count = min(count, 32)
	strength := math.Abs(water.DropStrength)
	if strength <= 0 {
		strength = 0.01
	}
	for i := 0; i < count; i++ {
		index := float64(i + 1)
		cx := nativeWaterHash(index*12.9898+0.173)*1.6 - 0.8
		cz := nativeWaterHash(index*78.233+0.719)*1.6 - 0.8
		dx, dz := x-cx, z-cz
		distance := math.Hypot(dx, dz)
		phase := distance*22 - time*4.8 + index*0.37
		envelope := math.Exp(-distance*3.2) * math.Exp(-time*0.16)
		amplitude := strength * 4.5 * envelope
		height += math.Sin(phase) * amplitude
		if distance > 1e-6 {
			derivative := amplitude * (22*math.Cos(phase) - 3.2*math.Sin(phase))
			slopeX += derivative * dx / distance
			slopeZ += derivative * dz / distance
		}
	}
	return height, slopeX, slopeZ
}

func nativeWaterHash(value float64) float64 {
	v := math.Sin(value) * 43758.5453123
	return v - math.Floor(v)
}

func positiveWaterValue(value, fallback float64) float64 {
	if value <= 0 {
		return fallback
	}
	return value
}

func firstWaterColor(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

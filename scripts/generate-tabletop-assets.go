//go:build ignore

// Generate the CC0 model, texture-variant, and IBL assets for the GoSX
// tabletop demo. Run from the repository root with:
//
//	GOWORK=off go run scripts/generate-tabletop-assets.go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"m31labs.dev/gosx/assetpipe"
	"m31labs.dev/gosx/assetpipe/texture"
	"m31labs.dev/gosx/scene"
)

const (
	polyAPI = "https://api.polyhaven.com"
	public  = "examples/gosx-docs/public"
	appDir  = "examples/gosx-docs/app/demos/tabletop"
)

var modelIDs = []string{"potted_plant_04"}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--contact-shadow-only" {
		if err := generateContactShadow(filepath.Join(public, "tabletop", "contact-shadow.png")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	for _, path := range []string{filepath.Join(public, "tabletop", "models"), filepath.Join(public, "tabletop", "env"), filepath.Join(public, "tabletop", "surfaces")} {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	source, err := os.MkdirTemp("", "gosx-tabletop-assets-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(source)
	if err := os.MkdirAll(filepath.Join(source, "tabletop", "models"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(source, "tabletop", "env"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(source, "tabletop", "surfaces", "wood_table_001"), 0755); err != nil {
		return err
	}

	for _, id := range modelIDs {
		if err := downloadModel(client, source, id); err != nil {
			return err
		}
		if err := rewriteGLTFImageURIs(filepath.Join(source, "tabletop", "models", id, id+"_1k.gltf")); err != nil {
			return err
		}
	}
	if err := downloadTabletopWood(client, source); err != nil {
		return err
	}
	files, err := getRawJSON(client, polyAPI+"/files/studio_small_09")
	if err != nil {
		return err
	}
	var hdriFiles map[string]any
	if err := json.Unmarshal(files, &hdriFiles); err != nil {
		return err
	}
	hdri := hdriFiles["hdri"].(map[string]any)["1k"].(map[string]any)["hdr"].(map[string]any)
	hdriURL, _ := hdri["url"].(string)
	if hdriURL == "" {
		return fmt.Errorf("Poly Haven did not return the 1K studio_small_09 HDRI")
	}
	if err := download(client, hdriURL, filepath.Join(source, "tabletop", "env", "studio_small_09.hdr")); err != nil {
		return err
	}

	// Keep compact PNG fallbacks alongside the optimized mesh. The pipeline
	// variants below build 256 px and 128 px KTX2 levels from a 256 px working
	// image; PNG carries color, alpha, and linear maps without BC support.
	textureInputs := make(map[string][]byte)
	textureDirs := make([]string, 0, len(modelIDs)+1)
	for _, id := range modelIDs {
		textureDirs = append(textureDirs, filepath.Join(source, "tabletop", "models", id, "textures"))
	}
	textureDirs = append(textureDirs, filepath.Join(source, "tabletop", "surfaces", "wood_table_001"))
	for _, texDir := range textureDirs {
		entries, err := os.ReadDir(texDir)
		if err != nil {
			return fmt.Errorf("read %s: %w", texDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jpg") {
				continue
			}
			path := filepath.Join(texDir, entry.Name())
			original, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, colorSpace := textureRole(entry.Name())
			space := texture.SRGB
			if colorSpace == "linear" {
				space = texture.Linear
			}
			img, _, err := texture.Decode(original, space)
			if err != nil {
				return fmt.Errorf("decode %s: %w", path, err)
			}
			work, err := resizeTexture(img, 256)
			if err != nil {
				return err
			}
			workPNG, err := pngBytes(work, space)
			if err != nil {
				return err
			}
			fallback, err := resizeTexture(img, 128)
			if err != nil {
				return err
			}
			fallbackPNG, err := pngBytes(fallback, space)
			if err != nil {
				return err
			}
			pngPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".png"
			if err := os.WriteFile(pngPath, fallbackPNG, 0644); err != nil {
				return err
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			rel = strings.TrimSuffix(rel, filepath.Ext(rel)) + ".png"
			textureInputs[rel] = workPNG
		}
	}

	report, err := assetpipe.Plan([]string{source}, assetpipe.Options{})
	if err != nil {
		return err
	}
	_, execution, err := assetpipe.Execute(report, assetpipe.ExecuteOptions{
		Root: source, OutputDir: public,
		Only: []string{"optimize-mesh", "prefilter-ibl-ggx", "generate-split-sum-lut"},
		IBL: assetpipe.IBLOptions{
			CubeSize: 32, Samples: 64, ProjectionSamples: 2,
			IrradianceSize: 16, BRDFLUTSize: 64, BRDFSamples: 256,
			BRDFLUTPath: "tabletop/env/brdf-lut.ktx2", Supercompress: true,
		},
	})
	if err != nil {
		return err
	}
	if execution.Totals.Failed != 0 {
		return fmt.Errorf("asset pipeline failed: %+v", execution.Results)
	}
	if err := generateStudioSweep(filepath.Join(public, "tabletop")); err != nil {
		return err
	}
	if err := generateContactShadow(filepath.Join(public, "tabletop", "contact-shadow.png")); err != nil {
		return err
	}
	textureReport := assetpipe.Report{SchemaVersion: assetpipe.SchemaVersion}
	for rel, workingPNG := range textureInputs {
		role, colorSpace := textureRole(filepath.Base(rel))
		build, variants, err := assetpipe.BuildTexture(rel, workingPNG, assetpipe.TextureOptions{
			Filter:           "lanczos3",
			Tiers:            []texture.Tier{{Name: "standard", MaxEdge: 256}, {Name: "low", MaxEdge: 128}},
			BlockCompression: true, BlockQuality: "balanced", Role: role, ColorSpace: colorSpace,
		})
		if err != nil {
			return fmt.Errorf("build texture variants for %s: %w", rel, err)
		}
		kept := make([]assetpipe.Variant, 0, len(variants))
		for index, variant := range variants {
			if !build.Variants[index].Block {
				continue
			}
			encoded := build.Variants[index].Data
			out := filepath.Join(public, filepath.FromSlash(variant.URI))
			if err := writeFile(out, encoded); err != nil {
				return err
			}
			variant.URI = "/" + strings.TrimLeft(filepath.ToSlash(variant.URI), "/")
			kept = append(kept, variant)
		}
		textureReport.Assets = append(textureReport.Assets, assetpipe.Asset{
			Path: rel, Kind: "texture", Bytes: int64(len(workingPNG)), Variants: kept,
		})
	}
	manifest := assetpipe.BuildVariantManifest(textureReport)
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(public, "tabletop", "texture-variants.json"), append(manifestBytes, '\n')); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(appDir, "texture-variants.json"), append(manifestBytes, '\n')); err != nil {
		return err
	}

	// Publish only the IBL descriptor and baked products. The original HDRI was
	// downloaded into the temporary source tree and is never served to visitors.
	var sidecar struct {
		IBL scene.EnvironmentIBL `json:"ibl"`
	}
	sidecarPath := filepath.Join(public, "tabletop", "env", "studio_small_09.ibl.json")
	sidecarBytes, err := os.ReadFile(sidecarPath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(sidecarBytes, &sidecar); err != nil {
		return err
	}
	for _, descriptor := range []*scene.TextureDescriptor{&sidecar.IBL.Radiance, &sidecar.IBL.Irradiance, &sidecar.IBL.BRDFLUT} {
		descriptor.URI = "/tabletop/env/" + filepath.Base(descriptor.URI)
	}
	sidecar.IBL.Source = "Poly Haven Studio Small 09 (CC0 1.0)"
	iblBytes, err := json.MarshalIndent(sidecar.IBL, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(appDir, "ibl.json"), append(iblBytes, '\n')); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(public, "tabletop", "ibl.json"), append(iblBytes, '\n')); err != nil {
		return err
	}

	// Copy only the reduced PNG fallback sources. Raw glTF JSON, source BINs,
	// and the HDRI stay in the temporary build tree.
	for _, textureRoot := range []string{filepath.Join(source, "tabletop", "models"), filepath.Join(source, "tabletop", "surfaces")} {
		if err := filepath.WalkDir(textureRoot, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".png") {
				return err
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return writeFile(filepath.Join(public, rel), data)
		}); err != nil {
			return err
		}
	}

	assets, err := committedAssetBytes(filepath.Join(public, "tabletop"))
	if err != nil {
		return err
	}
	if assets > 6<<20 {
		return fmt.Errorf("tabletop binary budget exceeded: %d bytes (limit %d)", assets, 6<<20)
	}
	desktopFirstView, phoneFirstView, err := estimateFirstView(filepath.Join(public, "tabletop"))
	if err != nil {
		return err
	}
	if desktopFirstView > 3<<20 || phoneFirstView > 1536<<10 {
		return fmt.Errorf("first-view transfer estimate exceeds budget: desktop %d bytes, phone %d bytes", desktopFirstView, phoneFirstView)
	}
	fmt.Printf("tabletop assets: optimized models and IBL %d bytes; texture variants %d; committed %d bytes; first view desktop %d bytes / phone %d bytes\n",
		execution.Totals.OutputBytes, len(textureReport.Assets), assets, desktopFirstView, phoneFirstView)
	return nil
}

func generateContactShadow(path string) error {
	const size = 128
	const innerRadius, strength = 0.68, 0.72
	image := image.NewNRGBA(image.Rect(0, 0, size, size))
	center := float64(size-1) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			radius := math.Hypot(float64(x)-center, float64(y)-center) / center
			falloff := 0.0
			switch {
			case radius <= innerRadius:
				falloff = 1
			case radius < 1:
				t := (radius - innerRadius) / (1 - innerRadius)
				falloff = 1 - t*t*(3-2*t)
			}
			alpha := uint8(math.Round(strength * falloff * 255))
			image.SetNRGBA(x, y, color.NRGBA{R: 18, G: 13, B: 11, A: alpha})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image); err != nil {
		return err
	}
	return writeFile(path, encoded.Bytes())
}

func downloadModel(client *http.Client, root, id string) error {
	apiFiles, err := getRawJSON(client, polyAPI+"/files/"+id)
	if err != nil {
		return err
	}
	var top map[string]any
	if err := json.Unmarshal(apiFiles, &top); err != nil {
		return err
	}
	gltf := top["gltf"].(map[string]any)["1k"].(map[string]any)["gltf"].(map[string]any)
	modelURL, _ := gltf["url"].(string)
	if modelURL == "" {
		return fmt.Errorf("Poly Haven has no 1K glTF for %s", id)
	}
	dir := filepath.Join(root, "tabletop", "models", id)
	if err := download(client, modelURL, filepath.Join(dir, id+"_1k.gltf")); err != nil {
		return err
	}
	// The API gives the complete downloadable bundle, including its authored
	// 1K textures and the glTF's geometry buffer.
	include := gltf["include"].(map[string]any)
	for rel, item := range include {
		meta := item.(map[string]any)
		url, _ := meta["url"].(string)
		if url == "" {
			return fmt.Errorf("Poly Haven omitted URL for %s/%s", id, rel)
		}
		if err := download(client, url, filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	return nil
}

// downloadTabletopWood fetches the 1K albedo, OpenGL normal and roughness maps
// from Poly Haven's Wood Table 001 CC0 texture set.
func downloadTabletopWood(client *http.Client, root string) error {
	data, err := getRawJSON(client, polyAPI+"/files/wood_table_001")
	if err != nil {
		return err
	}
	var files map[string]any
	if err := json.Unmarshal(data, &files); err != nil {
		return err
	}
	directory := filepath.Join(root, "tabletop", "surfaces", "wood_table_001")
	for _, item := range []struct{ key, name string }{
		{key: "Diffuse", name: "wood_table_001_diff.jpg"},
		{key: "nor_gl", name: "wood_table_001_nor_gl.jpg"},
		{key: "Rough", name: "wood_table_001_rough.jpg"},
	} {
		maps, ok := files[item.key].(map[string]any)
		if !ok {
			return fmt.Errorf("Poly Haven did not return wood_table_001 %s maps", item.key)
		}
		oneK, ok := maps["1k"].(map[string]any)
		if !ok {
			return fmt.Errorf("Poly Haven did not return 1K wood_table_001 %s maps", item.key)
		}
		jpg, ok := oneK["jpg"].(map[string]any)
		if !ok {
			return fmt.Errorf("Poly Haven did not return a JPEG for wood_table_001 %s", item.key)
		}
		url, _ := jpg["url"].(string)
		if url == "" {
			return fmt.Errorf("Poly Haven omitted URL for wood_table_001 %s", item.key)
		}
		if err := download(client, url, filepath.Join(directory, item.name)); err != nil {
			return err
		}
	}
	return nil
}

func getRawJSON(client *http.Client, url string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "GoSX tabletop asset builder")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: %s", url, response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 4<<20))
}

func download(client *http.Client, url, path string) error {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "GoSX tabletop asset builder")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("GET %s: %s", url, response.Status)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, 128<<20))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func textureRole(name string) (string, string) {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "_nor") || strings.Contains(lower, "_normal"):
		return "normal", "linear"
	case strings.Contains(lower, "_rough"):
		return "mask", "linear"
	case strings.Contains(lower, "_arm") || strings.Contains(lower, "_orm"):
		return "packed", "linear"
	default:
		return "base-color", "srgb"
	}
}

func resizeTexture(source *texture.Image, maxEdge int) (*texture.Image, error) {
	width, height := source.Width, source.Height
	longEdge := width
	if height > longEdge {
		longEdge = height
	}
	if longEdge <= maxEdge {
		return texture.Resize(source, width, height, texture.Lanczos3)
	}
	scale := float64(maxEdge) / float64(longEdge)
	width = max(1, int(float64(width)*scale+0.5))
	height = max(1, int(float64(height)*scale+0.5))
	return texture.Resize(source, width, height, texture.Lanczos3)
}

func pngBytes(img *texture.Image, space texture.ColorSpace) ([]byte, error) {
	encoded, err := texture.EncodeBytes(img, space, 4)
	if err != nil {
		return nil, err
	}
	raster := image.NewNRGBA(image.Rect(0, 0, img.Width, img.Height))
	for index := 0; index < img.Width*img.Height; index++ {
		offset := index * 4
		raster.Pix[offset] = encoded[offset]
		raster.Pix[offset+1] = encoded[offset+1]
		raster.Pix[offset+2] = encoded[offset+2]
		raster.Pix[offset+3] = encoded[offset+3]
	}
	var out bytes.Buffer
	if err := png.Encode(&out, raster); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func rewriteGLTFImageURIs(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	images, _ := document["images"].([]any)
	for _, imageValue := range images {
		entry, _ := imageValue.(map[string]any)
		uri, _ := entry["uri"].(string)
		if strings.EqualFold(filepath.Ext(uri), ".jpg") || strings.EqualFold(filepath.Ext(uri), ".jpeg") {
			entry["uri"] = strings.TrimSuffix(uri, filepath.Ext(uri)) + ".png"
		}
	}
	updated, err := json.Marshal(document)
	if err != nil {
		return err
	}
	return os.WriteFile(path, updated, 0644)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// generateStudioSweep writes a local cool-to-warm gradient texture for the
// tabletop's camera-facing backdrop. The image is authored here, not sourced.
func generateStudioSweep(root string) error {
	const side = 256
	backdrop := image.NewNRGBA(image.Rect(0, 0, side, side))
	slate := [3]float64{96, 121, 140}
	warm := [3]float64{224, 185, 147}
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			dx := (float64(x)/float64(side-1) - 0.5) / 0.42
			dy := (float64(y)/float64(side-1) - 0.68) / 0.24
			pool := math.Exp(-0.5 * (dx*dx + dy*dy))
			offset := backdrop.PixOffset(x, y)
			for channel := 0; channel < 3; channel++ {
				backdrop.Pix[offset+channel] = uint8(math.Round(slate[channel] + (warm[channel]-slate[channel])*pool))
			}
			backdrop.Pix[offset+3] = 255
		}
	}
	return writePNG(filepath.Join(root, "studio-sweep.png"), backdrop)
}

func writePNG(path string, img image.Image) error {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return err
	}
	return writeFile(path, encoded.Bytes())
}

func committedAssetBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err == nil {
			total += info.Size()
		}
		return err
	})
	return total, err
}

func estimateFirstView(root string) (desktop, phone int64, err error) {
	// WebGPU with BC support receives one 256 px texture variant per texture.
	// A WebGL2 device without BC support receives the compact 128 px PNG source.
	for _, id := range modelIDs {
		model := filepath.Join(root, "models", id, id+"_1k.opt.glb")
		if info, statErr := os.Stat(model); statErr == nil {
			desktop += info.Size()
			phone += info.Size()
		}
		textureDir := filepath.Join(root, "models", id, "textures")
		entries, readErr := os.ReadDir(textureDir)
		if readErr != nil {
			return 0, 0, readErr
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".png") {
				continue
			}
			base := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			variant, _ := filepath.Glob(filepath.Join(textureDir, base+".standard.*.ktx2"))
			if len(variant) > 0 {
				var best int64
				for _, path := range variant {
					info, statErr := os.Stat(path)
					if statErr == nil && (best == 0 || info.Size() < best) {
						best = info.Size()
					}
				}
				desktop += best
			}
			info, statErr := entry.Info()
			if statErr == nil {
				phone += info.Size()
			}
		}
	}
	for _, name := range []string{"studio_small_09.ibl.ktx2", "studio_small_09.irradiance.ktx2", "brdf-lut.ktx2"} {
		info, statErr := os.Stat(filepath.Join(root, "env", name))
		if statErr == nil {
			desktop += info.Size()
			phone += info.Size()
		}
	}
	if info, statErr := os.Stat(filepath.Join(root, "studio-sweep.png")); statErr == nil {
		desktop += info.Size()
		phone += info.Size()
	}
	woodDir := filepath.Join(root, "surfaces", "wood_table_001")
	entries, readErr := os.ReadDir(woodDir)
	if readErr != nil {
		return 0, 0, readErr
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".png") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		variant, _ := filepath.Glob(filepath.Join(woodDir, base+".standard.*.ktx2"))
		if len(variant) > 0 {
			var best int64
			for _, path := range variant {
				info, statErr := os.Stat(path)
				if statErr == nil && (best == 0 || info.Size() < best) {
					best = info.Size()
				}
			}
			desktop += best
		}
		info, statErr := entry.Info()
		if statErr == nil {
			phone += info.Size()
		}
	}
	return desktop, phone, nil
}

func init() {
	sort.Strings(modelIDs)
}

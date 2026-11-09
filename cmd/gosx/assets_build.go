package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"m31labs.dev/gosx/assetpipe"
	"m31labs.dev/gosx/assetpipe/manifest"
)

// assetBuildSpec is the optional JSON file that lists models explicitly.
// Paths are relative to the spec file.
type assetBuildSpec struct {
	Models    []assetBuildSpecModel `json:"models"`
	Budget    assetpipe.Budget      `json:"budget"`
	LODRatios []float64             `json:"lodRatios"`
}

type assetBuildSpecModel struct {
	Src       string            `json:"src"`
	Name      string            `json:"name"`
	LODRatios []float64         `json:"lodRatios"`
	Budget    *assetpipe.Budget `json:"budget"`
}

func runAssetBuildCommand(args []string, stdout io.Writer) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		assetBuildUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("gosx assets build", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "dist/assets", "output directory for GLBs, KTX2 textures, and the manifest")
	lods := fs.String("lods", "", "comma-separated triangle fractions for the LOD chain (default 0.5,0.25)")
	noLOD := fs.Bool("no-lod", false, "skip the LOD chain")
	noTextures := fs.Bool("no-textures", false, "skip KTX2 texture generation")
	noOptimize := fs.Bool("no-optimize", false, "skip weld, reorder, and quantize")
	distanceScale := fs.Float64("lod-distance-scale", assetpipe.DefaultLODDistanceScale, "scale for LOD switch distances")
	maxTriangles := fs.Int64("max-triangles", 0, "budget: triangles per model (0 = unlimited)")
	maxTextureMB := fs.Float64("max-texture-mb", 0, "budget: GPU texture MiB per model, RGBA8 with mips (0 = unlimited)")
	maxJoints := fs.Int("max-joints", 0, "budget: joints in the largest skin (0 = unlimited)")
	maxDrawCalls := fs.Int("max-draw-calls", 0, "budget: draw calls per model (0 = unlimited)")
	jsonOut := fs.Bool("json", false, "print the manifest as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts := assetpipe.BuildOptions{
		OutputDir:        *out,
		NoLOD:            *noLOD,
		NoTextures:       *noTextures,
		NoOptimize:       *noOptimize,
		LODDistanceScale: *distanceScale,
		Budget: assetpipe.Budget{
			MaxTriangles:    *maxTriangles,
			MaxTextureBytes: int64(*maxTextureMB * (1 << 20)),
			MaxJoints:       *maxJoints,
			MaxDrawCalls:    *maxDrawCalls,
		},
	}
	if *lods != "" {
		ratios, err := parseLODRatios(*lods)
		if err != nil {
			return err
		}
		opts.LODRatios = ratios
	}
	paths := fs.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	for _, p := range paths {
		if strings.EqualFold(filepath.Ext(p), ".json") {
			if err := applyAssetBuildSpec(&opts, p); err != nil {
				return err
			}
			continue
		}
		opts.Inputs = append(opts.Inputs, p)
	}
	result, err := assetpipe.BuildModels(opts)
	if err != nil {
		return err
	}
	if *jsonOut {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		if _, err := stdout.Write(append(data, '\n')); err != nil {
			return err
		}
	} else {
		printAssetBuild(stdout, result, filepath.Join(opts.OutputDir, assetpipe.ManifestFileName))
	}
	if len(result.Violations) > 0 {
		return fmt.Errorf("%d budget violation(s); see the list above and %s", len(result.Violations), filepath.Join(opts.OutputDir, assetpipe.ManifestFileName))
	}
	return nil
}

func parseLODRatios(value string) ([]float64, error) {
	var out []float64
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ratio, err := strconv.ParseFloat(part, 64)
		if err != nil || ratio <= 0 || ratio >= 1 {
			return nil, fmt.Errorf("--lods: %q is not a fraction between 0 and 1", part)
		}
		out = append(out, ratio)
	}
	return out, nil
}

func applyAssetBuildSpec(opts *assetpipe.BuildOptions, specPath string) error {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	var spec assetBuildSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("%s: %w", specPath, err)
	}
	dir := filepath.Dir(specPath)
	if spec.Budget != (assetpipe.Budget{}) && opts.Budget == (assetpipe.Budget{}) {
		opts.Budget = spec.Budget
	}
	if len(spec.LODRatios) > 0 && opts.LODRatios == nil {
		opts.LODRatios = spec.LODRatios
	}
	for _, model := range spec.Models {
		if strings.TrimSpace(model.Src) == "" {
			return fmt.Errorf("%s: a model entry has no src", specPath)
		}
		opts.Models = append(opts.Models, assetpipe.BuildInput{
			Path:      filepath.Join(dir, filepath.FromSlash(model.Src)),
			Name:      model.Name,
			LODRatios: model.LODRatios,
			Budget:    model.Budget,
		})
	}
	return nil
}

func printAssetBuild(w io.Writer, m manifest.Manifest, manifestPath string) {
	fmt.Fprintf(w, "Built %d model(s)\n", len(m.Models))
	for _, model := range m.Models {
		fmt.Fprintf(w, "\n%s -> %s (%d bytes)\n", model.Name, model.URI, model.Bytes)
		fmt.Fprintf(w, "  %d triangles, %d draw calls, %d joints, %.1f MiB texture memory\n",
			model.Stats.Triangles, model.Stats.DrawCalls, model.Stats.Joints, float64(model.Stats.TextureBytes)/(1<<20))
		for _, lod := range model.LODs[min(1, len(model.LODs)):] {
			fmt.Fprintf(w, "  lod%d: %s, %d triangles, from %.2f units\n", lod.Level, lod.URI, lod.Triangles, lod.Distance)
		}
		for _, tex := range model.Textures {
			fmt.Fprintf(w, "  texture %s (%dx%d, %s): %d KTX2 variant(s)\n", tex.Image, tex.Width, tex.Height, tex.Role, len(tex.Variants))
		}
	}
	for _, warning := range m.Warnings {
		fmt.Fprintf(w, "warning: %s\n", warning)
	}
	for _, v := range m.Violations {
		fmt.Fprintf(w, "over budget: %s\n", v)
	}
	fmt.Fprintf(w, "\nManifest: %s\n", manifestPath)
}

func assetBuildUsage(w io.Writer) {
	fmt.Fprintf(w, `gosx assets build - Turn glTF/GLB exports into shippable assets

Usage:
  gosx assets build [flags] [path|spec.json...]

Each .glb or .gltf below a path is welded, reordered, and quantized, then
written as <out>/<name>.glb with an LOD chain (<name>.lod1.glb, ...), KTX2
textures (<name>.textures/), and <out>/assets.manifest.json. A spec.json lists
models explicitly:

  {"models":[{"src":"hero.glb","name":"hero","budget":{"maxTriangles":20000}}],
   "budget":{"maxJoints":128},"lodRatios":[0.5,0.25]}

Budgets apply to the full-detail model. Exceeding one prints the model and the
number, still writes every file, and exits 1.

Flags:
  --out dir               output directory (default dist/assets)
  --lods 0.5,0.25         triangle fractions of the LOD chain
  --no-lod --no-textures --no-optimize
  --lod-distance-scale n  scale for switch distances (default %g)
  --max-triangles n       --max-texture-mb n
  --max-joints n          --max-draw-calls n
  --json                  print the manifest as JSON

Example:
  gosx assets build --out public/assets --max-triangles 30000 --max-joints 128 art/exports

`, assetpipe.DefaultLODDistanceScale)
}

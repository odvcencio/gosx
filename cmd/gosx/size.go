package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gosx/buildmanifest"
)

type sizeReport struct {
	Manifest       string           `json:"manifest"`
	RuntimeDir     string           `json:"runtimeDir"`
	ColdStartBytes int64            `json:"coldStartBytes"`
	ColdStartGzip  int64            `json:"coldStartGzipBytes"`
	TotalBytes     int64            `json:"totalBytes"`
	TotalGzip      int64            `json:"totalGzipBytes"`
	Assets         []sizeReportFile `json:"assets"`
}

type sizeReportFile struct {
	Name      string `json:"name"`
	File      string `json:"file"`
	Role      string `json:"role"`
	Bytes     int64  `json:"bytes"`
	GzipBytes int64  `json:"gzipBytes"`
	ColdStart bool   `json:"coldStart"`
}

func cmdSizeReport() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: gosx size [--json] <dist|build.json>")
		os.Exit(1)
	}
	jsonOut := false
	target := ""
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--json":
			jsonOut = true
		default:
			if strings.HasPrefix(arg, "--") {
				fmt.Fprintf(os.Stderr, "size error: unknown flag %s\n", arg)
				os.Exit(1)
			}
			target = arg
		}
	}
	if target == "" {
		fmt.Fprintln(os.Stderr, "size error: missing dist directory or build.json")
		os.Exit(1)
	}
	report, err := buildSizeReport(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "size error: %v\n", err)
		os.Exit(1)
	}
	if jsonOut {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "size error: encode report: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(data))
		return
	}
	printSizeReport(report)
}

func buildSizeReport(target string) (sizeReport, error) {
	manifestPath, distDir, err := resolveSizeReportTarget(target)
	if err != nil {
		return sizeReport{}, err
	}
	manifest, err := buildmanifest.Load(manifestPath)
	if err != nil {
		return sizeReport{}, err
	}
	runtimeDir := filepath.Join(distDir, "assets", "runtime")
	report := sizeReport{
		Manifest:   manifestPath,
		RuntimeDir: runtimeDir,
	}
	for _, asset := range runtimeSizeAssets(manifest) {
		if asset.file == "" {
			continue
		}
		entry, err := sizeReportEntry(runtimeDir, asset)
		if err != nil {
			return sizeReport{}, err
		}
		report.Assets = append(report.Assets, entry)
		report.TotalBytes += entry.Bytes
		report.TotalGzip += entry.GzipBytes
		if entry.ColdStart {
			report.ColdStartBytes += entry.Bytes
			report.ColdStartGzip += entry.GzipBytes
		}
	}
	return report, nil
}

func resolveSizeReportTarget(target string) (manifestPath string, distDir string, err error) {
	info, err := os.Stat(target)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return filepath.Join(target, "build.json"), target, nil
	}
	return target, filepath.Dir(target), nil
}

type runtimeSizeAsset struct {
	name      string
	file      string
	role      string
	coldStart bool
}

func runtimeSizeAssets(manifest *buildmanifest.Manifest) []runtimeSizeAsset {
	if manifest == nil {
		return nil
	}
	rt := manifest.Runtime
	return []runtimeSizeAsset{
		{name: "runtime.wasm", file: rt.WASM.File, role: "core wasm", coldStart: true},
		{name: "runtime-islands.wasm", file: rt.WASMIslands.File, role: "islands wasm"},
		{name: "wasm_exec.js", file: rt.WASMExec.File, role: "wasm loader", coldStart: true},
		{name: "bootstrap.js", file: rt.Bootstrap.File, role: "full bootstrap"},
		{name: "bootstrap-lite.js", file: rt.BootstrapLite.File, role: "lite bootstrap"},
		{name: "bootstrap-runtime.js", file: rt.BootstrapRuntime.File, role: "runtime bootstrap", coldStart: true},
		{name: "bootstrap-feature-islands.js", file: rt.BootstrapFeatureIslands.File, role: "feature chunk"},
		{name: "bootstrap-feature-engines.js", file: rt.BootstrapFeatureEngines.File, role: "feature chunk"},
		{name: "bootstrap-feature-hubs.js", file: rt.BootstrapFeatureHubs.File, role: "feature chunk"},
		{name: "bootstrap-feature-scene3d.js", file: rt.BootstrapFeatureScene3D.File, role: "scene3d chunk"},
		{name: "bootstrap-feature-scene3d-webgpu.js", file: rt.BootstrapFeatureScene3DWebGPU.File, role: "scene3d webgpu chunk"},
		{name: "bootstrap-feature-scene3d-gltf.js", file: rt.BootstrapFeatureScene3DGLTF.File, role: "scene3d gltf chunk"},
		{name: "bootstrap-feature-scene3d-animation.js", file: rt.BootstrapFeatureScene3DAnimation.File, role: "scene3d animation chunk"},
		{name: "patch.js", file: rt.Patch.File, role: "navigation patch"},
		{name: "hls.min.js", file: rt.VideoHLS.File, role: "video hls chunk"},
	}
}

func sizeReportEntry(runtimeDir string, asset runtimeSizeAsset) (sizeReportFile, error) {
	path := filepath.Join(runtimeDir, asset.file)
	data, err := os.ReadFile(path)
	if err != nil {
		return sizeReportFile{}, fmt.Errorf("read runtime asset %s: %w", path, err)
	}
	return sizeReportFile{
		Name:      asset.name,
		File:      asset.file,
		Role:      asset.role,
		Bytes:     int64(len(data)),
		GzipBytes: int64(gzip_c_len(data)),
		ColdStart: asset.coldStart,
	}, nil
}

func printSizeReport(report sizeReport) {
	fmt.Printf("GoSX runtime size report\n")
	fmt.Printf("  Manifest: %s\n", report.Manifest)
	fmt.Printf("  Cold start: %s raw, %s gzip\n", humanBytes(report.ColdStartBytes), humanBytes(report.ColdStartGzip))
	fmt.Printf("  Runtime total: %s raw, %s gzip\n", humanBytes(report.TotalBytes), humanBytes(report.TotalGzip))
	for _, asset := range report.Assets {
		marker := " "
		if asset.ColdStart {
			marker = "*"
		}
		fmt.Printf("  %s %-42s %8s raw %8s gzip  %s\n", marker, asset.File, humanBytes(asset.Bytes), humanBytes(asset.GzipBytes), asset.Role)
	}
}

func humanBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%dB", value)
	}
	kb := float64(value) / unit
	if kb < unit {
		return fmt.Sprintf("%.1fKB", kb)
	}
	return fmt.Sprintf("%.2fMB", kb/unit)
}

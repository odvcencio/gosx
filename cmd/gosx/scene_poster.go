package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"m31labs.dev/gosx/internal/version"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/harness"
	"m31labs.dev/gosx/scene/preview"
)

// gosx scene poster renders a scene to a still image that a page paints while
// the interactive renderer boots.
//
// The command follows the shape of gosx scene check: one report, one verdict,
// and a record of which steps ran. A step that did not run is never counted as
// a pass. Two rules make this command different from gosx scene render.
//
//  1. It gates the frame. The CPU rasterizer cannot draw every authored record,
//     and a poster that omits half the scene tells a reader and a crawler
//     something false. A failed gate refuses to write the file.
//  2. It proves reproducibility on request. A poster is a build artifact with a
//     cache entry and a content hash, so it must reproduce byte for byte.

const scenePosterSchema = "gosx.scene3d.poster/v1"

type scenePosterReport struct {
	Schema  string `json:"schema"`
	Version string `json:"version"`
	// Valid is the single verdict. It is false when any poster failed.
	Valid   bool                    `json:"valid"`
	Steps   scenePosterSteps        `json:"steps"`
	Posters []scenePosterFileReport `json:"posters"`
	Totals  scenePosterTotals       `json:"totals"`
	// Machine records that every duration in this report was measured here.
	// A reader who quotes one of these numbers is describing this machine.
	Machine  string   `json:"machineNote"`
	Problems []string `json:"problems,omitempty"`
}

type scenePosterSteps struct {
	Scenes          int  `json:"scenes"`
	Rendered        int  `json:"rendered"`
	Gated           int  `json:"gated"`
	Written         int  `json:"written"`
	Refused         int  `json:"refused"`
	DeterminismRuns int  `json:"determinismRuns"`
	PixelCertified  bool `json:"pixelCertified"`
	ByteCertified   bool `json:"byteCertified"`
}

type scenePosterFileReport struct {
	Scene  string `json:"scene"`
	Output string `json:"output,omitempty"`
	// Written is false when the gate refused. The file on disk is untouched.
	Written  bool                    `json:"written"`
	Format   string                  `json:"format"`
	Width    int                     `json:"width"`
	Height   int                     `json:"height"`
	Time     float64                 `json:"time"`
	Bytes    int                     `json:"bytes"`
	SHA256   string                  `json:"sha256"`
	Fidelity preview.FidelityReport  `json:"fidelity"`
	Cost     scenePosterCost         `json:"cost"`
	Repeat   *scenePosterRepeat      `json:"repeat,omitempty"`
	Harness  *harness.FrameTelemetry `json:"frame,omitempty"`
}

type scenePosterCost struct {
	RenderMS float64 `json:"renderMS"`
	EncodeMS float64 `json:"encodeMS"`
}

// scenePosterRepeat carries both halves of the reproducibility claim. Pixel
// identity comes from scene/harness CertifyDeterminism. Byte identity comes from
// re-encoding the poster and comparing hashes, because a build artifact is a
// file: identical pixels with different bytes still change an ETag and a cache
// entry.
type scenePosterRepeat struct {
	Runs           int    `json:"runs"`
	PixelIdentical bool   `json:"pixelIdentical"`
	ByteIdentical  bool   `json:"byteIdentical"`
	PixelSHA256    string `json:"pixelSHA256"`
	FirstSHA256    string `json:"firstSHA256"`
	// Divergence names the first run whose bytes differed, and is empty when
	// every run reproduced.
	Divergence string `json:"divergence,omitempty"`
}

type scenePosterTotals struct {
	Bytes      int     `json:"bytes"`
	RenderMS   float64 `json:"renderMS"`
	EncodeMS   float64 `json:"encodeMS"`
	WallTimeMS float64 `json:"wallTimeMS"`
}

func runScenePosterCommand(args []string, stdout io.Writer) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		scenePosterUsage(stdout)
		return nil
	}
	fs := flag.NewFlagSet("gosx scene poster", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "emit the whole report as JSON")
	outPath := fs.String("out", "", "write a single poster to this PNG path")
	outDir := fs.String("out-dir", "", "write one poster per input scene into this directory")
	width := fs.Int("width", 640, "poster width in pixels")
	height := fs.Int("height", 360, "poster height in pixels")
	timeSeconds := fs.Float64("time", 0, "animation time in seconds for the captured frame")
	background := fs.String("background", "", "override scene background color")
	fast := fs.Bool("fast", false, "skip shadows and post-FX and cap curved geometry")
	maxSegments := fs.Int("max-segments", 0, "cap curved primitive tessellation (0 preserves authored values)")
	assetsRoot := fs.String("assets", "", "asset root that base color texture paths resolve against")
	repeat := fs.Int("repeat", 1, "render the poster this many times to prove it reproduces")
	minCoverage := fs.Float64("min-coverage", -1, "smallest fraction of non-background pixels that counts as drawn")
	minColors := fs.Int("min-colors", -1, "smallest number of distinct colours that counts as shaded")
	minVariance := fs.Float64("min-variance", -1, "smallest luminance variance that counts as lit")
	allowDropped := fs.Bool("allow-dropped", false, "publish a poster even when the rasterizer dropped authored records")
	cameraX := fs.Float64("camera-x", 0, "override camera X")
	cameraY := fs.Float64("camera-y", 0, "override camera Y")
	cameraZ := fs.Float64("camera-z", 0, "override camera Z")
	fov := fs.Float64("fov", 0, "override vertical field of view in degrees")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("scene poster requires at least one SceneIR or Scene3D props JSON file or directory")
	}
	if *width <= 0 || *height <= 0 {
		return errors.New("scene poster width and height must be positive")
	}
	if strings.TrimSpace(*outPath) != "" && strings.TrimSpace(*outDir) != "" {
		return errors.New("scene poster accepts --out or --out-dir, not both")
	}

	files, err := collectSceneJSONFiles(fs.Args())
	if err != nil {
		return err
	}
	if strings.TrimSpace(*outPath) != "" && len(files) != 1 {
		return fmt.Errorf("--out names one file but %d scenes were selected; use --out-dir", len(files))
	}

	render := preview.Options{Width: *width, Height: *height, Time: *timeSeconds, Background: *background,
		DisableShadows: *fast, DisablePostFX: *fast, MaxSegments: *maxSegments}
	if *fast && render.MaxSegments == 0 {
		render.MaxSegments = 12
	}
	if root := strings.TrimSpace(*assetsRoot); root != "" {
		render.AssetRoots = []string{root}
	}
	if *cameraX != 0 || *cameraY != 0 || *cameraZ != 0 || *fov != 0 {
		cameraFOV := *fov
		if cameraFOV == 0 {
			cameraFOV = 50
		}
		render.Camera = &scene.PerspectiveCamera{Position: scene.Vec3(*cameraX, *cameraY, *cameraZ), FOV: cameraFOV, Near: 0.1, Far: 100}
	}

	opts := preview.NewPosterOptions(render)
	if *minCoverage >= 0 {
		opts.Floors.Coverage = *minCoverage
	}
	if *minColors >= 0 {
		opts.Floors.UniqueColors = *minColors
	}
	if *minVariance >= 0 {
		opts.Floors.LuminanceVariance = *minVariance
	}
	opts.Floors.AllowDroppedRecords = *allowDropped

	report := scenePosterReport{
		Schema: scenePosterSchema, Version: version.Current, Valid: true,
		Posters: []scenePosterFileReport{},
		Machine: "every duration in this report was measured on the machine that ran the command",
	}
	report.Steps.Scenes = len(files)
	started := time.Now()
	for _, file := range files {
		entry, err := renderScenePoster(file, opts, *outPath, *outDir, *repeat, &report)
		if err != nil {
			return err
		}
		report.Posters = append(report.Posters, entry)
	}
	report.Totals.WallTimeMS = float64(time.Since(started).Microseconds()) / 1000
	if *repeat > 1 {
		report.Steps.DeterminismRuns = *repeat
	}

	if *jsonOut {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal scene poster report: %w", err)
		}
		if _, err := stdout.Write(append(encoded, '\n')); err != nil {
			return err
		}
	} else {
		printScenePoster(stdout, report)
	}
	if !report.Valid {
		return errors.New("scene poster failed")
	}
	return nil
}

func renderScenePoster(file string, opts preview.PosterOptions, outPath, outDir string, repeat int, report *scenePosterReport) (scenePosterFileReport, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return scenePosterFileReport{}, fmt.Errorf("read scene %s: %w", file, err)
	}
	poster, err := preview.PosterFromJSON(data, opts)
	if err != nil {
		return scenePosterFileReport{}, err
	}
	report.Steps.Rendered++
	report.Steps.Gated++
	entry := scenePosterFileReport{
		Scene: file, Format: poster.Format, Width: poster.Width, Height: poster.Height,
		Time: poster.Time, Bytes: poster.ByteSize(), SHA256: poster.SHA256,
		Fidelity: poster.Fidelity,
		Cost: scenePosterCost{
			RenderMS: float64(poster.RenderDuration.Microseconds()) / 1000,
			EncodeMS: float64(poster.EncodeDuration.Microseconds()) / 1000,
		},
	}
	report.Totals.Bytes += entry.Bytes
	report.Totals.RenderMS += entry.Cost.RenderMS
	report.Totals.EncodeMS += entry.Cost.EncodeMS

	// Record the same frame telemetry that gosx scene check records, so one
	// report format explains both commands.
	if session, sessionErr := harness.NewFromJSON(data, opts.Render); sessionErr == nil {
		if _, renderErr := session.Render(opts.Render.Time); renderErr == nil {
			if events := session.Report().Events; len(events) > 0 && events[0].Frame != nil {
				entry.Harness = events[0].Frame
			}
		}
		if repeat > 1 {
			certified, certifyErr := session.CertifyDeterminism("poster", opts.Render.Time, repeat)
			if certifyErr != nil {
				return entry, certifyErr
			}
			repeatEvidence := &scenePosterRepeat{
				Runs: certified.Runs, PixelIdentical: certified.Identical,
				PixelSHA256: certified.PixelSHA256, FirstSHA256: poster.SHA256, ByteIdentical: true,
			}
			// Pixel identity does not imply byte identity. Re-encode and compare
			// the file hash, because the artifact this command ships is bytes.
			for run := 2; run <= repeat; run++ {
				next, repeatErr := preview.PosterFromJSON(data, opts)
				if repeatErr != nil {
					return entry, repeatErr
				}
				if next.SHA256 != poster.SHA256 {
					repeatEvidence.ByteIdentical = false
					repeatEvidence.Divergence = fmt.Sprintf("run %d produced %s", run, next.SHA256)
					break
				}
			}
			entry.Repeat = repeatEvidence
			report.Steps.PixelCertified = certified.Identical
			report.Steps.ByteCertified = repeatEvidence.ByteIdentical
			if !certified.Identical {
				report.Valid = false
				report.Problems = append(report.Problems,
					fmt.Sprintf("%s: repeated renders did not reproduce the same pixels", file))
			}
			if !repeatEvidence.ByteIdentical {
				report.Valid = false
				report.Problems = append(report.Problems,
					fmt.Sprintf("%s: repeated renders did not reproduce the same bytes: %s", file, repeatEvidence.Divergence))
			}
		}
	}

	if !poster.Fidelity.OK {
		report.Valid = false
		report.Steps.Refused++
		for _, failure := range poster.Fidelity.Failures {
			report.Problems = append(report.Problems, file+": "+failure)
		}
		return entry, nil
	}

	destination := scenePosterDestination(file, outPath, outDir)
	if destination == "" {
		return entry, nil
	}
	target, err := createSceneOutputFile(destination)
	if err != nil {
		return entry, err
	}
	writeErr := preview.WritePoster(target, poster)
	closeErr := target.Close()
	if writeErr != nil {
		return entry, fmt.Errorf("write poster %s: %w", destination, writeErr)
	}
	if closeErr != nil {
		return entry, closeErr
	}
	entry.Output = destination
	entry.Written = true
	report.Steps.Written++
	return entry, nil
}

// scenePosterDestination resolves where one poster goes. It returns an empty
// string when the caller asked for no file, which makes the command usable as a
// gate in continuous integration without writing anything.
func scenePosterDestination(scenePath, outPath, outDir string) string {
	if trimmed := strings.TrimSpace(outPath); trimmed != "" {
		return trimmed
	}
	dir := strings.TrimSpace(outDir)
	if dir == "" {
		return ""
	}
	base := filepath.Base(scenePath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.TrimSuffix(base, ".scene")
	base = strings.TrimSuffix(base, ".sceneir")
	return filepath.Join(dir, base+".poster.png")
}

func printScenePoster(w io.Writer, report scenePosterReport) {
	fmt.Fprintf(w, "Scene3D poster: %s\n", passFail(report.Valid))
	fmt.Fprintf(w, "Scenes: %d rendered, %d written, %d refused\n",
		report.Steps.Rendered, report.Steps.Written, report.Steps.Refused)
	for _, entry := range report.Posters {
		fmt.Fprintf(w, "\n%s\n", entry.Scene)
		fmt.Fprintf(w, "  Frame: %dx%d at t=%.3f, %s, %s\n",
			entry.Width, entry.Height, entry.Time, strings.ToUpper(entry.Format),
			formatByteCount(int64(entry.Bytes)))
		metrics := entry.Fidelity.Metrics
		fmt.Fprintf(w, "  Fidelity: %s, coverage %.4f, %d unique colours, luminance variance %.6f\n",
			passFail(entry.Fidelity.OK), metrics.Coverage, metrics.UniqueColors, metrics.LuminanceVariance)
		for _, dropped := range entry.Fidelity.Dropped {
			fmt.Fprintf(w, "    dropped %s", dropped.Code)
			if dropped.Target != "" {
				fmt.Fprintf(w, " [%s]", dropped.Target)
			}
			fmt.Fprintf(w, ": %s\n", dropped.Message)
		}
		for _, failure := range entry.Fidelity.Failures {
			fmt.Fprintf(w, "    refused: %s\n", failure)
		}
		if repeat := entry.Repeat; repeat != nil {
			fmt.Fprintf(w, "  Determinism: %d runs, pixels %s, bytes %s\n",
				repeat.Runs, passFail(repeat.PixelIdentical), passFail(repeat.ByteIdentical))
			if repeat.Divergence != "" {
				fmt.Fprintf(w, "    %s, first run produced %s\n", repeat.Divergence, repeat.FirstSHA256)
			}
		}
		fmt.Fprintf(w, "  Cost on this machine: %.2f ms render, %.2f ms encode\n",
			entry.Cost.RenderMS, entry.Cost.EncodeMS)
		fmt.Fprintf(w, "  SHA-256: %s\n", entry.SHA256)
		if entry.Written {
			fmt.Fprintf(w, "  Wrote: %s\n", entry.Output)
		} else if entry.Output == "" && entry.Fidelity.OK {
			fmt.Fprintln(w, "  Wrote nothing: pass --out or --out-dir to save the poster")
		}
	}
	fmt.Fprintf(w, "\nTotals: %s, %.2f ms render, %.2f ms encode, %.2f ms wall clock on this machine\n",
		formatByteCount(int64(report.Totals.Bytes)), report.Totals.RenderMS,
		report.Totals.EncodeMS, report.Totals.WallTimeMS)
	for _, problem := range report.Problems {
		fmt.Fprintf(w, "Problem: %s\n", problem)
	}
}

func scenePosterUsage(w io.Writer) {
	fmt.Fprint(w, `gosx scene poster - Render a build-time still image that paints before the renderer boots

Usage:
  gosx scene poster [--json] [--out poster.png | --out-dir dir]
                    [--width 640] [--height 360] [--time seconds] [--fast]
                    [--assets root] [--max-segments N] [--background color]
                    [--repeat N]
                    [--min-coverage F] [--min-colors N] [--min-variance F] [--allow-dropped]
                    [--camera-x N --camera-y N --camera-z N --fov degrees]
                    <file-or-dir>...

A poster is a still PNG of the scene. A page paints it immediately and the live
canvas covers it once the renderer draws its first frame.

The command gates every frame before it writes a file. It refuses a frame that
draws nothing, a frame that carries no shading or light, and a frame whose
scene contains a record the CPU rasterizer cannot draw. A poster that omits
half the scene tells a reader and a crawler something false, so a refusal is
the correct result, not a bug. Read the refusal, fix the scene, or pass
--allow-dropped after deciding the poster is still honest.

The poster captures one instant. The default is t=0, because the browser
renderer also starts its clock at zero, so a t=0 poster matches the first live
frame. Pass --time to capture any other instant of an animated scene.

Pass --repeat N to prove the poster reproduces. The report carries two
verdicts: identical pixels, and identical file bytes. Both must hold, because a
poster is a build artifact with a content hash and a cache entry.

Every duration the command prints was measured on the machine that ran it.

`)
}

package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"

	"m31labs.dev/gosx/scene/imagediff"
)

// This file adds the two pieces that turn a frame hash into usable evidence:
// a localized golden-image comparison, and a repeated-render determinism check.
// A hash proves that a frame changed. A diff says where. A repeat run proves
// that the hash means anything at all.

// GoldenTelemetry records one golden-image comparison.
type GoldenTelemetry struct {
	Label string `json:"label,omitempty"`
	// Source names where the reference came from, usually a file path.
	Source string `json:"source,omitempty"`
	// Required is true when a difference invalidates the whole report.
	Required bool             `json:"required"`
	Diff     imagediff.Result `json:"diff"`
}

// DeterminismTelemetry records a repeated render of the same frame.
type DeterminismTelemetry struct {
	Label string  `json:"label,omitempty"`
	Time  float64 `json:"time"`
	// Runs is how many frames were rendered, including the first.
	Runs int `json:"runs"`
	// Identical is true when every run produced the same pixels.
	Identical bool `json:"identical"`
	// PixelSHA256 hashes the raw RGBA pixels of the first run.
	PixelSHA256 string `json:"pixelSHA256"`
	// Divergences lists each run that did not match the first run. It is empty
	// when the render is reproducible.
	Divergences []DeterminismDivergence `json:"divergences,omitempty"`
}

// DeterminismDivergence names one run that did not reproduce the first frame.
type DeterminismDivergence struct {
	Run             int               `json:"run"`
	PixelSHA256     string            `json:"pixelSHA256"`
	ChangedPixels   int               `json:"changedPixels"`
	MaxChannelDelta int               `json:"maxChannelDelta"`
	Bounds          *imagediff.Region `json:"bounds,omitempty"`
}

// CompareGolden records a localized comparison between the last rendered frame
// and a reference image. It does not change the report verdict; use
// RequireGolden when a difference must fail the run.
func (s *Session) CompareGolden(label, source string, reference image.Image, opts imagediff.Options) (imagediff.Result, error) {
	return s.compareGolden(label, source, reference, opts, false)
}

// RequireGolden records the same comparison as CompareGolden and marks the
// report invalid when the frame differs from the reference.
func (s *Session) RequireGolden(label, source string, reference image.Image, opts imagediff.Options) (imagediff.Result, error) {
	return s.compareGolden(label, source, reference, opts, true)
}

// RequireGoldenFile reads a reference PNG from disk and requires a match. When
// the file does not exist the session records the absence as a problem instead
// of silently passing, so a missing baseline can never look like a pass.
func (s *Session) RequireGoldenFile(label, path string, opts imagediff.Options) (imagediff.Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		s.problem(fmt.Sprintf("golden %q: read reference %s: %v", label, path, err))
		return imagediff.Result{}, err
	}
	reference, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		s.problem(fmt.Sprintf("golden %q: decode reference %s: %v", label, path, err))
		return imagediff.Result{}, err
	}
	return s.compareGolden(label, path, reference, opts, true)
}

func (s *Session) compareGolden(label, source string, reference image.Image, opts imagediff.Options, required bool) (imagediff.Result, error) {
	if s.lastFrame == nil {
		err := fmt.Errorf("scene harness: golden %q needs a rendered frame first", label)
		s.problem(err.Error())
		return imagediff.Result{}, err
	}
	result, err := imagediff.Compare(reference, s.lastFrame, opts)
	if err != nil {
		s.problem(fmt.Sprintf("golden %q: %v", label, err))
		return imagediff.Result{}, err
	}
	s.report.Events = append(s.report.Events, Event{
		Sequence: len(s.report.Events) + 1,
		Kind:     "golden",
		Golden:   &GoldenTelemetry{Label: label, Source: source, Required: required, Diff: result},
	})
	if required && !result.Identical {
		s.problem(fmt.Sprintf("golden %q differs from %s: %s", label, sourceOrReference(source), result.Summary()))
	}
	return result, nil
}

// CertifyDeterminism renders the same frame several times and records whether
// every run produced the same pixels. The verdict comes from real repeated
// renders, not from an assumption about the backend.
//
// Read the scope before you quote the result. This function proves reproducibility
// inside one process, on one machine, with one Go build. It does not prove:
//
//   - Reproducibility in a fresh process. Go randomizes map iteration order per
//     process, so a lowering step that walked a map would look stable here.
//     TestFrameRepeatsInAFreshProcess in package render/gpu/headless covers that.
//   - Identical pixels on a second processor architecture. The rasterizer runs
//     float32 and float64 arithmetic, and a compiler may fuse a multiply and an
//     add on a target that offers one instruction for both. A fused result differs
//     in the last bit, and one bit in a depth comparison flips a pixel.
//   - Identical pixels on a different Go release.
//
// Closing the cross-architecture gap needs a committed golden frame checked on a
// second architecture in continuous integration. Until such a job exists and is
// green, do not describe a certified report as bit-reproducible across machines.
func (s *Session) CertifyDeterminism(label string, time float64, runs int) (DeterminismTelemetry, error) {
	if runs < 2 {
		runs = 2
	}
	telemetry := DeterminismTelemetry{Label: label, Time: time, Runs: runs, Identical: true}
	opts := s.options
	opts.Time = time
	var first *image.RGBA
	for run := 0; run < runs; run++ {
		result, err := s.renderFrame(opts)
		if err != nil {
			s.problem(fmt.Sprintf("determinism %q: run %d: %v", label, run+1, err))
			return telemetry, err
		}
		if run == 0 {
			first = cloneHarnessRGBA(result.Image)
			telemetry.PixelSHA256 = rawPixelHash(first)
			continue
		}
		hash := rawPixelHash(result.Image)
		if hash == telemetry.PixelSHA256 {
			continue
		}
		diff, diffErr := imagediff.Compare(first, result.Image, imagediff.Options{})
		divergence := DeterminismDivergence{Run: run + 1, PixelSHA256: hash}
		if diffErr == nil {
			divergence.ChangedPixels = diff.ChangedPixels
			divergence.MaxChannelDelta = diff.MaxChannelDelta
			divergence.Bounds = diff.Bounds
		}
		telemetry.Identical = false
		telemetry.Divergences = append(telemetry.Divergences, divergence)
	}
	s.report.Events = append(s.report.Events, Event{
		Sequence:    len(s.report.Events) + 1,
		Kind:        "determinism",
		Determinism: &telemetry,
	})
	if !telemetry.Identical {
		s.problem(fmt.Sprintf("determinism %q: %d of %d runs did not reproduce the first frame",
			label, len(telemetry.Divergences), runs-1))
	}
	return telemetry, nil
}

// contentHash returns a stable hash of the whole report with the hash field
// cleared. Two runs that produce the same evidence produce the same string, so
// an agent can decide "did anything change" from one comparison.
func contentHash(report Report) string {
	report.ContentHash = ""
	data, err := json.Marshal(report)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// rawPixelHash hashes image dimensions and raw RGBA bytes. It skips the PNG
// container, so a re-encode never looks like a visual change.
func rawPixelHash(img *image.RGBA) string {
	if img == nil {
		return ""
	}
	h := sha256.New()
	bounds := img.Bounds()
	fmt.Fprintf(h, "rgba/%d/%d\n", bounds.Dx(), bounds.Dy())
	stride := bounds.Dx() * 4
	for y := 0; y < bounds.Dy(); y++ {
		offset := img.PixOffset(bounds.Min.X, bounds.Min.Y+y)
		h.Write(img.Pix[offset : offset+stride])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sourceOrReference(source string) string {
	if source == "" {
		return "its reference"
	}
	return source
}

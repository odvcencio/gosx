package preview

// This file turns one deterministic CPU frame into a poster: a still image that
// a page paints while the interactive renderer boots.
//
// A poster makes a promise to a reader and to a crawler. It says "this is what
// the scene looks like". The CPU rasterizer cannot draw every authored record,
// so an ungated poster can break that promise without a word. A poster that
// omits half the scene is worse than no poster, because a missing poster is an
// absence and a wrong poster is a false statement.
//
// Poster therefore refuses by default. It measures the frame it produced,
// compares that measurement to a floor, collects every record the frame dropped,
// and reports every failed check rather than the first one. WritePoster returns
// ErrLowFidelity when the gate fails, so the honest path is also the short path.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"sort"
	"strings"
	"time"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
)

// FormatPNG is the only poster container this package writes.
//
// PNG wins on four counts that matter for this job.
//
//   - It is lossless, so the poster equals the first live frame pixel for pixel.
//     The browser canvas paints over the poster with the same picture, so the
//     swap has nothing to reveal.
//   - Go encodes it deterministically, so the same scene produces the same bytes.
//     A lossy encoder would add a second source of drift on top of the renderer.
//   - Every browser and every crawler decodes it, with no <picture> fallback
//     chain. A fallback chain puts a second element in the paint path.
//   - assetpipe/texture also produces KTX2 and Basis. Those are GPU texture
//     containers. No browser <img> element decodes either one, so they cannot
//     carry a poster.
const FormatPNG = "png"

// ErrLowFidelity reports that a poster failed its fidelity gate. Callers that
// must inspect the failure should read Poster.Fidelity, which lists every
// failed check and every dropped record.
var ErrLowFidelity = errors.New("scene preview: poster failed its fidelity gate")

// FrameMetrics measures what a rendered frame actually contains.
//
// The three gate inputs answer three different lies.
//
//   - Coverage answers "did anything draw at all". A frame of pure background
//     has coverage zero.
//   - UniqueColors answers "did the frame draw shape or only a flat block". A
//     solid rectangle over a solid background has two colours.
//   - LuminanceVariance answers "does the frame carry light". A frame that draws
//     a shape in the background colour has coverage and colours but no contrast.
//
// The definitions match package scene/harness exactly, including the choice of
// the pixel at the image origin as the background reference.
// TestPosterMetricsMatchHarnessTelemetry fails when the two drift.
type FrameMetrics struct {
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	Pixels            int     `json:"pixels"`
	ChangedPixels     int     `json:"changedPixels"`
	Coverage          float64 `json:"coverage"`
	UniqueColors      int     `json:"uniqueColors"`
	LuminanceVariance float64 `json:"luminanceVariance"`
}

// MeasureFrame computes the poster fidelity inputs for one rendered image.
func MeasureFrame(img *image.RGBA) FrameMetrics {
	if img == nil {
		return FrameMetrics{}
	}
	bounds := img.Bounds()
	metrics := FrameMetrics{Width: bounds.Dx(), Height: bounds.Dy()}
	metrics.Pixels = metrics.Width * metrics.Height
	if metrics.Pixels == 0 {
		return metrics
	}
	background := img.RGBAAt(bounds.Min.X, bounds.Min.Y)
	colors := make(map[uint32]struct{})
	var sum, sumSquares float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			colors[uint32(c.R)<<24|uint32(c.G)<<16|uint32(c.B)<<8|uint32(c.A)] = struct{}{}
			if c != background {
				metrics.ChangedPixels++
			}
			lum := 0.2126*float64(c.R)/255 + 0.7152*float64(c.G)/255 + 0.0722*float64(c.B)/255
			sum += lum
			sumSquares += lum * lum
		}
	}
	count := float64(metrics.Pixels)
	mean := sum / count
	variance := sumSquares/count - mean*mean
	if variance < 0 {
		variance = 0
	}
	metrics.UniqueColors = len(colors)
	metrics.Coverage = float64(metrics.ChangedPixels) / count
	metrics.LuminanceVariance = variance
	return metrics
}

// FidelityFloors is the minimum evidence a poster must show before this package
// agrees to write it.
//
// The defaults come from the floors that the golden-frame matrix in package
// scene/harness already applies to a frame it calls drawn. They are deliberately
// low. They catch "nothing rendered" and "a flat block rendered"; they do not
// judge whether a scene is pretty.
type FidelityFloors struct {
	// Coverage is the smallest fraction of non-background pixels that counts as
	// a drawn frame.
	Coverage float64 `json:"coverage"`
	// UniqueColors is the smallest number of distinct RGBA values that counts as
	// a shaded frame.
	UniqueColors int `json:"uniqueColors"`
	// LuminanceVariance is the smallest light spread that counts as a lit frame.
	LuminanceVariance float64 `json:"luminanceVariance"`
	// AllowDroppedRecords keeps the gate open when the CPU rasterizer skipped an
	// authored record. Leave it false. Set it only when a caller has read every
	// dropped record and decided the poster still tells the truth.
	AllowDroppedRecords bool `json:"allowDroppedRecords"`
}

// DefaultFidelityFloors returns the floors that gate a poster by default.
func DefaultFidelityFloors() FidelityFloors {
	return FidelityFloors{Coverage: 0.005, UniqueColors: 8, LuminanceVariance: 0.0005}
}

// DroppedRecord names one authored record that the CPU rasterizer did not draw.
type DroppedRecord struct {
	Code    string `json:"code"`
	Target  string `json:"target,omitempty"`
	Message string `json:"message"`
}

// FidelityReport is the whole verdict for one poster. It lists every failed
// check, never only the first, so one run tells a reader everything that is
// wrong with the frame.
type FidelityReport struct {
	OK      bool           `json:"ok"`
	Metrics FrameMetrics   `json:"metrics"`
	Floors  FidelityFloors `json:"floors"`
	// Dropped lists every authored record the frame carried and never drew.
	Dropped []DroppedRecord `json:"dropped,omitempty"`
	// Failures names each gate that did not pass, in a stable order.
	Failures []string `json:"failures,omitempty"`
}

// Poster is a still image of a scene plus the evidence that it is honest.
type Poster struct {
	// Format is always FormatPNG. The field exists so a consumer reads the
	// container from the artifact instead of assuming one.
	Format string `json:"format"`
	// Bytes is the encoded image. It is empty when the render failed.
	Bytes []byte `json:"-"`
	// SHA256 hashes Bytes. Two runs of the same scene produce the same string.
	SHA256 string `json:"sha256"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	// Time is the animation time this frame was rendered at, in seconds.
	Time float64 `json:"time"`
	// Fidelity carries the gate verdict and every measurement behind it.
	Fidelity FidelityReport `json:"fidelity"`
	// Diagnostics is the complete diagnostic list from the render bundle.
	Diagnostics []engine.RenderDiagnostic `json:"diagnostics,omitempty"`
	// RenderDuration is the time this process spent inside the CPU rasterizer.
	// EncodeDuration is the time it spent in the PNG encoder. Both describe this
	// machine, not the scene. Do not publish either as a property of GoSX.
	RenderDuration time.Duration `json:"-"`
	EncodeDuration time.Duration `json:"-"`
	ObjectCount    int           `json:"objectCount"`
	Batches        int           `json:"batches"`
	Materials      int           `json:"materials"`
}

// ByteSize returns the encoded poster size.
func (p *Poster) ByteSize() int {
	if p == nil {
		return 0
	}
	return len(p.Bytes)
}

// PosterOptions configures one poster render and its fidelity gate.
type PosterOptions struct {
	// Render carries the frame settings. Options.Time selects which frame of an
	// animated scene the poster captures.
	Render Options
	// Floors gates the result. A zero Floors with FloorsSet false uses
	// DefaultFidelityFloors, so forgetting the gate still gates.
	Floors FidelityFloors
	// FloorsSet makes Floors authoritative even when it is the zero value. Set
	// it to open the gate on purpose, and record why.
	FloorsSet bool
}

// NewPosterOptions returns poster options with the default gate applied.
func NewPosterOptions(render Options) PosterOptions {
	return PosterOptions{Render: render, Floors: DefaultFidelityFloors(), FloorsSet: true}
}

// PosterFromJSON renders a poster from a SceneIR document or from the runtime
// props JSON that scene.Props emits.
//
// It calls RenderJSON, so a poster and a preview share one lowering path, one
// rasterizer, and one diagnostic set. A parallel path would let a poster and a
// preview disagree about the same scene.
func PosterFromJSON(data []byte, opts PosterOptions) (*Poster, error) {
	result, err := RenderJSON(data, opts.Render)
	if err != nil {
		return nil, err
	}
	return posterFromResult(result, opts)
}

// PosterFromProps renders a poster from a typed scene.
func PosterFromProps(props scene.Props, opts PosterOptions) (*Poster, error) {
	result, err := Render(props, opts.Render)
	if err != nil {
		return nil, err
	}
	return posterFromResult(result, opts)
}

// PosterFromResult encodes and gates a frame that a caller already rendered.
// Use it when one render must produce both a preview and a poster.
func PosterFromResult(result *Result, opts PosterOptions) (*Poster, error) {
	return posterFromResult(result, opts)
}

func posterFromResult(result *Result, opts PosterOptions) (*Poster, error) {
	if result == nil || result.Image == nil {
		return nil, fmt.Errorf("scene preview: poster needs a rendered frame")
	}
	floors := opts.Floors
	if floors == (FidelityFloors{}) && !opts.FloorsSet {
		floors = DefaultFidelityFloors()
	}
	var buffer bytes.Buffer
	// Reserve four bytes per pixel. A poster of a lit scene compresses well
	// below that, so one allocation covers the encode.
	buffer.Grow(result.Image.Bounds().Dx() * result.Image.Bounds().Dy() * 4)
	encoder := png.Encoder{CompressionLevel: png.DefaultCompression}
	started := time.Now()
	if err := encoder.Encode(&buffer, result.Image); err != nil {
		return nil, fmt.Errorf("scene preview: encode poster: %w", err)
	}
	encodeElapsed := time.Since(started)
	sum := sha256.Sum256(buffer.Bytes())
	poster := &Poster{
		Format: FormatPNG, Bytes: buffer.Bytes(), SHA256: hex.EncodeToString(sum[:]),
		Width: result.Image.Bounds().Dx(), Height: result.Image.Bounds().Dy(),
		Time:           opts.Render.Time,
		Diagnostics:    append([]engine.RenderDiagnostic(nil), result.Bundle.Diagnostics...),
		RenderDuration: result.Duration, EncodeDuration: encodeElapsed,
		ObjectCount: result.Bundle.ObjectCount, Batches: len(result.Bundle.InstancedMeshes),
		Materials: len(result.Bundle.Materials),
	}
	poster.Fidelity = gradeFidelity(MeasureFrame(result.Image), floors, result.Bundle.Diagnostics)
	return poster, nil
}

// gradeFidelity applies every floor and returns every failure. It never returns
// after the first failed check, because one run must tell the reader the whole
// story about the frame.
func gradeFidelity(metrics FrameMetrics, floors FidelityFloors, diagnostics []engine.RenderDiagnostic) FidelityReport {
	report := FidelityReport{OK: true, Metrics: metrics, Floors: floors}
	if metrics.Pixels == 0 {
		report.OK = false
		report.Failures = append(report.Failures, "the frame has no pixels")
		return report
	}
	if metrics.Coverage < floors.Coverage {
		report.OK = false
		report.Failures = append(report.Failures, fmt.Sprintf(
			"coverage %.6f is below the floor %.6f, so no camera-visible content reached the frame",
			metrics.Coverage, floors.Coverage))
	}
	if metrics.UniqueColors < floors.UniqueColors {
		report.OK = false
		report.Failures = append(report.Failures, fmt.Sprintf(
			"the frame holds %d unique colours, below the floor %d, so it carries no shading",
			metrics.UniqueColors, floors.UniqueColors))
	}
	if metrics.LuminanceVariance < floors.LuminanceVariance {
		report.OK = false
		report.Failures = append(report.Failures, fmt.Sprintf(
			"luminance variance %.8f is below the floor %.8f, so the frame carries no light",
			metrics.LuminanceVariance, floors.LuminanceVariance))
	}
	report.Dropped = droppedRecords(diagnostics)
	if len(report.Dropped) > 0 && !floors.AllowDroppedRecords {
		report.OK = false
		report.Failures = append(report.Failures, fmt.Sprintf(
			"the CPU rasterizer dropped %d authored record(s), so this poster shows less than the scene: %s",
			len(report.Dropped), summarizeDropped(report.Dropped)))
	}
	return report
}

// droppedRecords keeps the diagnostics that mean "this authored record produced
// no pixels" and discards the ones that only describe an ignored field.
//
// Every drop diagnostic in this package uses the scene.preview.unsupported_
// prefix, and unsupported() is the only function that builds one. A new drop
// therefore reaches this gate without a second edit here.
func droppedRecords(diagnostics []engine.RenderDiagnostic) []DroppedRecord {
	var out []DroppedRecord
	for _, diagnostic := range diagnostics {
		if !strings.HasPrefix(diagnostic.Code, "scene.preview.unsupported_") {
			continue
		}
		out = append(out, DroppedRecord{Code: diagnostic.Code, Target: diagnostic.Target, Message: diagnostic.Message})
	}
	return out
}

func summarizeDropped(dropped []DroppedRecord) string {
	counts := make(map[string]int)
	for _, record := range dropped {
		counts[strings.TrimPrefix(record.Code, "scene.preview.unsupported_")]++
	}
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		parts = append(parts, fmt.Sprintf("%s=%d", kind, counts[kind]))
	}
	return strings.Join(parts, ", ")
}

// WritePoster writes a poster that passed its fidelity gate. It returns
// ErrLowFidelity and writes nothing when the gate failed, so the default path
// cannot publish a misleading image.
func WritePoster(w io.Writer, poster *Poster) error {
	if poster == nil || len(poster.Bytes) == 0 {
		return fmt.Errorf("scene preview: no poster to write")
	}
	if !poster.Fidelity.OK {
		return fmt.Errorf("%w: %s", ErrLowFidelity, strings.Join(poster.Fidelity.Failures, "; "))
	}
	_, err := w.Write(poster.Bytes)
	return err
}

// WritePosterUnchecked writes a poster whatever its gate said. Call it only
// after reading the failures, and record why the frame is still honest.
func WritePosterUnchecked(w io.Writer, poster *Poster) error {
	if poster == nil || len(poster.Bytes) == 0 {
		return fmt.Errorf("scene preview: no poster to write")
	}
	_, err := w.Write(poster.Bytes)
	return err
}

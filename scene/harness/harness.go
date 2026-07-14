// Package harness provides an interactive, browser-free Scene3D test session.
// It combines deterministic native frames, exact scene queries, renderer
// payload inspection, and Selena artifact evidence in one JSON report that is
// designed to be useful to both humans and authoring agents.
package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"strings"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
)

const Schema = "gosx.scene3d.harness/v1"

type Session struct {
	props     scene.Props
	options   preview.Options
	report    Report
	lastFrame *image.RGBA
}

type Report struct {
	Schema    string           `json:"schema"`
	Backend   string           `json:"backend"`
	Scene     SceneSummary     `json:"scene"`
	Materials []SelenaEvidence `json:"selenaMaterials,omitempty"`
	Events    []Event          `json:"events"`
	Valid     bool             `json:"valid"`
	Problems  []string         `json:"problems,omitempty"`
}

type SceneSummary struct {
	Objects         int `json:"objects"`
	InstancedMeshes int `json:"instancedMeshes"`
	Instances       int `json:"instances"`
	Lights          int `json:"lights"`
}

type Event struct {
	Sequence    int                   `json:"sequence"`
	Kind        string                `json:"kind"`
	Frame       *FrameTelemetry       `json:"frame,omitempty"`
	Trace       *TraceTelemetry       `json:"trace,omitempty"`
	Interaction *InteractionTelemetry `json:"interaction,omitempty"`
	Water       *WaterTelemetry       `json:"water,omitempty"`
}

type FrameTelemetry struct {
	Time                  float64                   `json:"time"`
	Width                 int                       `json:"width"`
	Height                int                       `json:"height"`
	PNGHash               string                    `json:"pngSHA256"`
	Coverage              float64                   `json:"coverage"`
	ChangedPixels         int                       `json:"changedPixels"`
	VisibleBounds         *PixelBounds              `json:"visibleBounds,omitempty"`
	UniqueColors          int                       `json:"uniqueColors"`
	TemporalChangedPixels int                       `json:"temporalChangedPixels"`
	LuminanceVariance     float64                   `json:"luminanceVariance"`
	EdgeEnergy            float64                   `json:"edgeEnergy"`
	Batches               int                       `json:"batches"`
	Instances             int                       `json:"instances"`
	Materials             int                       `json:"materials"`
	DeviceLost            bool                      `json:"deviceLost"`
	Diagnostics           []engine.RenderDiagnostic `json:"diagnostics,omitempty"`
}

type PixelBounds struct {
	MinX int `json:"minX"`
	MinY int `json:"minY"`
	MaxX int `json:"maxX"`
	MaxY int `json:"maxY"`
}

type TraceTelemetry struct {
	Label string         `json:"label,omitempty"`
	Trace scene.RayTrace `json:"trace"`
}

type InteractionTelemetry struct {
	Label      string                  `json:"label,omitempty"`
	Kind       string                  `json:"kind"`
	OrbitDrag  *scene.OrbitDragResult  `json:"orbitDrag,omitempty"`
	ObjectDrag *scene.ObjectDragResult `json:"objectDrag,omitempty"`
}

type SelenaEvidence struct {
	MaterialIndex    int      `json:"materialIndex"`
	Material         string   `json:"material,omitempty"`
	Backend          string   `json:"backend"`
	Validation       string   `json:"validation"`
	WGSLVertexHash   string   `json:"wgslVertexSHA256"`
	WGSLFragmentHash string   `json:"wgslFragmentSHA256"`
	GLSLVertexHash   string   `json:"glslVertexSHA256"`
	GLSLFragmentHash string   `json:"glslFragmentSHA256"`
	Uniforms         []string `json:"uniforms,omitempty"`
	Valid            bool     `json:"valid"`
	Problems         []string `json:"problems,omitempty"`
}

// New starts a reusable native session. Render and Trace may be interleaved to
// model camera frames and pointer probes without launching a browser.
func New(props scene.Props, options preview.Options) *Session {
	ir := props.SceneIR()
	instances := 0
	for _, mesh := range ir.InstancedMeshes {
		instances += mesh.Count
	}
	return &Session{props: props, options: options, report: Report{
		Schema: Schema, Backend: "pure-go-headless",
		Scene:  SceneSummary{Objects: len(ir.Objects), InstancedMeshes: len(ir.InstancedMeshes), Instances: instances, Lights: len(ir.Lights)},
		Events: []Event{}, Valid: true,
	}}
}

// Render records one deterministic native frame and returns its pixels for
// optional golden-image assertions.
func (s *Session) Render(time float64) (*preview.Result, error) {
	opts := s.options
	opts.Time = time
	result, err := preview.Render(s.props, opts)
	if err != nil {
		s.problem("render: " + err.Error())
		return nil, err
	}
	frame, err := analyzeFrame(result, s.lastFrame)
	if err != nil {
		s.problem("frame telemetry: " + err.Error())
		return nil, err
	}
	frame.Time = time
	s.report.Events = append(s.report.Events, Event{Sequence: len(s.report.Events) + 1, Kind: "frame", Frame: &frame})
	s.lastFrame = cloneHarnessRGBA(result.Image)
	s.report.Materials = inspectSelena(result.Bundle.Materials)
	for _, material := range s.report.Materials {
		if !material.Valid {
			s.problem(fmt.Sprintf("Selena material %d failed artifact validation", material.MaterialIndex))
		}
	}
	if result.Stats.DeviceLost {
		s.problem("headless device reported loss")
	}
	return result, nil
}

// Trace records a ray traversal and its complete, nearest-first hit list.
func (s *Session) Trace(label string, ray scene.Ray, options ...scene.RaycastOption) scene.RayTrace {
	trace := scene.TraceGraph(s.props.Graph, ray, options...)
	s.report.Events = append(s.report.Events, Event{Sequence: len(s.report.Events) + 1, Kind: "raytrace", Trace: &TraceTelemetry{Label: label, Trace: trace}})
	return trace
}

// TracePointer converts a CSS-pixel pointer sample using Scene3D's camera
// contract and records the resulting exact native ray traversal.
func (s *Session) TracePointer(label string, pointerX, pointerY, width, height float64, camera scene.PerspectiveCamera, options ...scene.RaycastOption) scene.RayTrace {
	return s.Trace(label, scene.ScreenToRay(pointerX, pointerY, width, height, camera), options...)
}

// OrbitDrag records one deterministic pointer-drag interaction using the same
// public contract as the Scene3D runtime. It lets agents certify direction,
// sensitivity, and pitch clamping without browser input synthesis.
func (s *Session) OrbitDrag(label string, state scene.OrbitState, input scene.OrbitDragInput) scene.OrbitDragResult {
	result := scene.ApplyOrbitDrag(state, input)
	s.report.Events = append(s.report.Events, Event{
		Sequence: len(s.report.Events) + 1,
		Kind:     "interaction",
		Interaction: &InteractionTelemetry{
			Label: label, Kind: "orbit-drag", OrbitDrag: &result,
		},
	})
	return result
}

// ObjectDrag records one deterministic camera-facing manipulation sample
// using the same ray-plane and clamp contract as the managed Scene3D runtime.
func (s *Session) ObjectDrag(label string, state scene.ObjectDragState, input scene.ObjectDragInput) scene.ObjectDragResult {
	result := scene.ApplyObjectDrag(state, input)
	s.report.Events = append(s.report.Events, Event{
		Sequence: len(s.report.Events) + 1,
		Kind:     "interaction",
		Interaction: &InteractionTelemetry{
			Label: label, Kind: "object-drag", ObjectDrag: &result,
		},
	})
	if !result.Applied {
		s.problem("object drag did not intersect its manipulation plane")
	}
	return result
}

func (s *Session) Report() Report { return s.report }

// WriteJSON emits stable, indented telemetry. Runtime frame timings are
// intentionally excluded because they make agent snapshots machine-dependent.
func (s *Session) WriteJSON(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(s.report)
}

func (s *Session) Validate() error {
	if len(s.report.Events) == 0 {
		return fmt.Errorf("scene harness: no frames or traces recorded")
	}
	if !s.report.Valid {
		return fmt.Errorf("scene harness: %s", strings.Join(s.report.Problems, "; "))
	}
	return nil
}

func (s *Session) problem(message string) {
	s.report.Valid = false
	s.report.Problems = append(s.report.Problems, message)
}

func analyzeFrame(result *preview.Result, previous *image.RGBA) (FrameTelemetry, error) {
	if result == nil || result.Image == nil {
		return FrameTelemetry{}, fmt.Errorf("nil preview result")
	}
	hash, err := pngHash(result.Image)
	if err != nil {
		return FrameTelemetry{}, err
	}
	background := result.Image.RGBAAt(0, 0)
	changed, unique, bounds := pixelEvidence(result.Image, background)
	temporal, variance, edgeEnergy := frameVisualEvidence(result.Image, previous)
	instances := 0
	for _, mesh := range result.Bundle.InstancedMeshes {
		instances += mesh.InstanceCount
	}
	pixels := result.Image.Bounds().Dx() * result.Image.Bounds().Dy()
	coverage := 0.0
	if pixels > 0 {
		coverage = float64(changed) / float64(pixels)
	}
	return FrameTelemetry{
		Width: result.Image.Bounds().Dx(), Height: result.Image.Bounds().Dy(), PNGHash: hash,
		Coverage: coverage, ChangedPixels: changed, VisibleBounds: bounds, UniqueColors: unique,
		TemporalChangedPixels: temporal, LuminanceVariance: variance, EdgeEnergy: edgeEnergy,
		Batches: len(result.Bundle.InstancedMeshes), Instances: instances, Materials: len(result.Bundle.Materials),
		DeviceLost: result.Stats.DeviceLost, Diagnostics: append([]engine.RenderDiagnostic(nil), result.Bundle.Diagnostics...),
	}, nil
}

func pngHash(img image.Image) (string, error) {
	h := sha256.New()
	if err := png.Encode(h, img); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func pixelEvidence(img *image.RGBA, background interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) (int, int, *PixelBounds) {
	br, bg, bb, ba := background.RGBA()
	colors := make(map[uint32]struct{})
	changed := 0
	bounds := PixelBounds{MinX: img.Bounds().Max.X, MinY: img.Bounds().Max.Y, MaxX: -1, MaxY: -1}
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			c := img.RGBAAt(x, y)
			colors[uint32(c.R)<<24|uint32(c.G)<<16|uint32(c.B)<<8|uint32(c.A)] = struct{}{}
			r, g, b, a := c.RGBA()
			if r == br && g == bg && b == bb && a == ba {
				continue
			}
			changed++
			if x < bounds.MinX {
				bounds.MinX = x
			}
			if y < bounds.MinY {
				bounds.MinY = y
			}
			if x > bounds.MaxX {
				bounds.MaxX = x
			}
			if y > bounds.MaxY {
				bounds.MaxY = y
			}
		}
	}
	if changed == 0 {
		return 0, len(colors), nil
	}
	return changed, len(colors), &bounds
}

func frameVisualEvidence(img, previous *image.RGBA) (temporal int, luminanceVariance, edgeEnergy float64) {
	if img == nil {
		return 0, 0, 0
	}
	bounds := img.Bounds()
	count := bounds.Dx() * bounds.Dy()
	if count == 0 {
		return 0, 0, 0
	}
	var sum, sumSquares, edgeSum float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			lum := 0.2126*float64(c.R)/255 + 0.7152*float64(c.G)/255 + 0.0722*float64(c.B)/255
			sum += lum
			sumSquares += lum * lum
			if x > bounds.Min.X {
				left := img.RGBAAt(x-1, y)
				leftLum := 0.2126*float64(left.R)/255 + 0.7152*float64(left.G)/255 + 0.0722*float64(left.B)/255
				edgeSum += math.Abs(lum - leftLum)
			}
			if y > bounds.Min.Y {
				up := img.RGBAAt(x, y-1)
				upLum := 0.2126*float64(up.R)/255 + 0.7152*float64(up.G)/255 + 0.0722*float64(up.B)/255
				edgeSum += math.Abs(lum - upLum)
			}
			if previous != nil && image.Pt(x, y).In(previous.Bounds()) && c != previous.RGBAAt(x, y) {
				temporal++
			}
		}
	}
	mean := sum / float64(count)
	variance := sumSquares/float64(count) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return temporal, variance, edgeSum / float64(count)
}

func cloneHarnessRGBA(src *image.RGBA) *image.RGBA {
	if src == nil {
		return nil
	}
	out := image.NewRGBA(src.Bounds())
	copy(out.Pix, src.Pix)
	return out
}

func inspectSelena(materials []engine.RenderMaterial) []SelenaEvidence {
	out := []SelenaEvidence{}
	for index, material := range materials {
		if material.ShaderBackend != "selena" {
			continue
		}
		evidence := SelenaEvidence{MaterialIndex: index, Backend: material.ShaderBackend, Validation: "compiled-artifact-transport", Valid: true}
		if name, ok := material.ShaderLayout["material"].(string); ok {
			evidence.Material = name
		}
		evidence.WGSLVertexHash = sourceHash(material.CustomVertexWGSL)
		evidence.WGSLFragmentHash = sourceHash(material.CustomFragmentWGSL)
		evidence.GLSLVertexHash = sourceHash(material.CustomVertex)
		evidence.GLSLFragmentHash = sourceHash(material.CustomFragment)
		checks := []struct{ name, source, token string }{
			{"WGSL vertex", material.CustomVertexWGSL, "vertexMain"}, {"WGSL fragment", material.CustomFragmentWGSL, "fragmentMain"},
			{"GLSL vertex", material.CustomVertex, "void main"}, {"GLSL fragment", material.CustomFragment, "void main"},
		}
		for _, check := range checks {
			if strings.TrimSpace(check.source) == "" || !strings.Contains(check.source, check.token) {
				evidence.Valid = false
				evidence.Problems = append(evidence.Problems, check.name+" missing "+check.token)
			}
		}
		if evidence.Material == "" {
			evidence.Valid = false
			evidence.Problems = append(evidence.Problems, "shader layout missing material name")
		}
		for name := range material.CustomUniforms {
			evidence.Uniforms = append(evidence.Uniforms, name)
		}
		sortStrings(evidence.Uniforms)
		out = append(out, evidence)
	}
	return out
}

func sourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

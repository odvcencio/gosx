package scene

import "strings"

// SceneIR is the typed lowered scene payload emitted from a Graph before it is
// serialized into the current Scene3D compatibility contract.
type SceneIR struct {
	Objects []ObjectIR `json:"objects,omitempty"`
	Labels  []LabelIR  `json:"labels,omitempty"`
}

// ObjectIR is the typed compatibility record for one lowered scene object.
type ObjectIR struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Size       float64 `json:"size,omitempty"`
	Width      float64 `json:"width,omitempty"`
	Height     float64 `json:"height,omitempty"`
	Depth      float64 `json:"depth,omitempty"`
	Radius     float64 `json:"radius,omitempty"`
	Segments   int     `json:"segments,omitempty"`
	Color      string  `json:"color,omitempty"`
	X          float64 `json:"x,omitempty"`
	Y          float64 `json:"y,omitempty"`
	Z          float64 `json:"z,omitempty"`
	RotationX  float64 `json:"rotationX,omitempty"`
	RotationY  float64 `json:"rotationY,omitempty"`
	RotationZ  float64 `json:"rotationZ,omitempty"`
	SpinX      float64 `json:"spinX,omitempty"`
	SpinY      float64 `json:"spinY,omitempty"`
	SpinZ      float64 `json:"spinZ,omitempty"`
	ShiftX     float64 `json:"shiftX,omitempty"`
	ShiftY     float64 `json:"shiftY,omitempty"`
	ShiftZ     float64 `json:"shiftZ,omitempty"`
	DriftSpeed float64 `json:"driftSpeed,omitempty"`
	DriftPhase float64 `json:"driftPhase,omitempty"`
}

// LabelIR is the typed compatibility record for one lowered scene label.
type LabelIR struct {
	ID          string  `json:"id"`
	Text        string  `json:"text"`
	ClassName   string  `json:"className,omitempty"`
	X           float64 `json:"x,omitempty"`
	Y           float64 `json:"y,omitempty"`
	Z           float64 `json:"z,omitempty"`
	Priority    float64 `json:"priority,omitempty"`
	ShiftX      float64 `json:"shiftX,omitempty"`
	ShiftY      float64 `json:"shiftY,omitempty"`
	ShiftZ      float64 `json:"shiftZ,omitempty"`
	DriftSpeed  float64 `json:"driftSpeed,omitempty"`
	DriftPhase  float64 `json:"driftPhase,omitempty"`
	MaxWidth    float64 `json:"maxWidth,omitempty"`
	MaxLines    int     `json:"maxLines,omitempty"`
	Overflow    string  `json:"overflow,omitempty"`
	Font        string  `json:"font,omitempty"`
	LineHeight  float64 `json:"lineHeight,omitempty"`
	Color       string  `json:"color,omitempty"`
	Background  string  `json:"background,omitempty"`
	BorderColor string  `json:"borderColor,omitempty"`
	OffsetX     float64 `json:"offsetX,omitempty"`
	OffsetY     float64 `json:"offsetY,omitempty"`
	AnchorX     float64 `json:"anchorX,omitempty"`
	AnchorY     float64 `json:"anchorY,omitempty"`
	Collision   string  `json:"collision,omitempty"`
	Occlude     bool    `json:"occlude,omitempty"`
	WhiteSpace  string  `json:"whiteSpace,omitempty"`
	TextAlign   string  `json:"textAlign,omitempty"`
}

// SceneIR lowers typed scene props into a typed intermediate representation.
func (p Props) SceneIR() SceneIR {
	return p.Graph.SceneIR()
}

// SceneIR lowers a typed graph into a typed intermediate representation.
func (g Graph) SceneIR() SceneIR {
	if len(g.Nodes) == 0 {
		return SceneIR{}
	}

	lowerer := &graphLowerer{
		anchors: make(map[string]worldTransform),
	}
	for _, node := range g.Nodes {
		lowerer.lowerNode(node, identityTransform())
	}
	return SceneIR{
		Objects: append([]ObjectIR(nil), lowerer.objects...),
		Labels:  lowerer.resolveLabels(),
	}
}

func (ir SceneIR) isZero() bool {
	return len(ir.Objects) == 0 && len(ir.Labels) == 0
}

func (ir SceneIR) legacyProps() map[string]any {
	if ir.isZero() {
		return nil
	}
	out := map[string]any{}
	if objects := legacyObjects(ir.Objects); len(objects) > 0 {
		out["objects"] = objects
	}
	if labels := legacyLabels(ir.Labels); len(labels) > 0 {
		out["labels"] = labels
	}
	return out
}

func legacyObjects(items []ObjectIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.legacyProps())
	}
	return out
}

func (item ObjectIR) legacyProps() map[string]any {
	record := map[string]any{
		"id":   item.ID,
		"kind": item.Kind,
	}
	setNumeric(record, "size", item.Size)
	setNumeric(record, "width", item.Width)
	setNumeric(record, "height", item.Height)
	setNumeric(record, "depth", item.Depth)
	setNumeric(record, "radius", item.Radius)
	if item.Segments > 0 {
		record["segments"] = item.Segments
	}
	if color := strings.TrimSpace(item.Color); color != "" {
		record["color"] = color
	}
	setNumeric(record, "x", item.X)
	setNumeric(record, "y", item.Y)
	setNumeric(record, "z", item.Z)
	setNumeric(record, "rotationX", item.RotationX)
	setNumeric(record, "rotationY", item.RotationY)
	setNumeric(record, "rotationZ", item.RotationZ)
	setNumeric(record, "spinX", item.SpinX)
	setNumeric(record, "spinY", item.SpinY)
	setNumeric(record, "spinZ", item.SpinZ)
	setNumeric(record, "shiftX", item.ShiftX)
	setNumeric(record, "shiftY", item.ShiftY)
	setNumeric(record, "shiftZ", item.ShiftZ)
	setNumeric(record, "driftSpeed", item.DriftSpeed)
	setNumeric(record, "driftPhase", item.DriftPhase)
	return record
}

func legacyLabels(items []LabelIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if record := item.legacyProps(); record != nil {
			out = append(out, record)
		}
	}
	return out
}

func (item LabelIR) legacyProps() map[string]any {
	text := strings.TrimSpace(item.Text)
	if text == "" {
		return nil
	}
	record := map[string]any{
		"id":   item.ID,
		"text": text,
	}
	if className := strings.TrimSpace(item.ClassName); className != "" {
		record["className"] = className
	}
	setNumeric(record, "x", item.X)
	setNumeric(record, "y", item.Y)
	setNumeric(record, "z", item.Z)
	setNumeric(record, "priority", item.Priority)
	setNumeric(record, "shiftX", item.ShiftX)
	setNumeric(record, "shiftY", item.ShiftY)
	setNumeric(record, "shiftZ", item.ShiftZ)
	setNumeric(record, "driftSpeed", item.DriftSpeed)
	setNumeric(record, "driftPhase", item.DriftPhase)
	setNumeric(record, "maxWidth", item.MaxWidth)
	if item.MaxLines > 0 {
		record["maxLines"] = item.MaxLines
	}
	if overflow := strings.TrimSpace(item.Overflow); overflow != "" {
		record["overflow"] = overflow
	}
	if font := strings.TrimSpace(item.Font); font != "" {
		record["font"] = font
	}
	setNumeric(record, "lineHeight", item.LineHeight)
	if color := strings.TrimSpace(item.Color); color != "" {
		record["color"] = color
	}
	if background := strings.TrimSpace(item.Background); background != "" {
		record["background"] = background
	}
	if border := strings.TrimSpace(item.BorderColor); border != "" {
		record["borderColor"] = border
	}
	setNumeric(record, "offsetX", item.OffsetX)
	setNumeric(record, "offsetY", item.OffsetY)
	setNumeric(record, "anchorX", item.AnchorX)
	setNumeric(record, "anchorY", item.AnchorY)
	if collision := strings.TrimSpace(item.Collision); collision != "" {
		record["collision"] = collision
	}
	if item.Occlude {
		record["occlude"] = true
	}
	if whiteSpace := strings.TrimSpace(item.WhiteSpace); whiteSpace != "" {
		record["whiteSpace"] = whiteSpace
	}
	if align := strings.TrimSpace(item.TextAlign); align != "" {
		record["textAlign"] = align
	}
	return record
}

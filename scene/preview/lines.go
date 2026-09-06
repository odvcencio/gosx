package preview

import (
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
	"math"
)

func appendNativeLines(frame *engine.RenderBundle, object scene.ObjectIR) {
	reject := func(message string) {
		frame.Diagnostics = append(frame.Diagnostics, unsupported("geometry", object.ID, message))
	}
	if math.IsNaN(object.LineWidth) || math.IsInf(object.LineWidth, 0) || object.LineWidth < 0 || object.LineWidth > 1 || (object.LineDash != nil && *object.LineDash) || object.MaterialKind == "line-dashed" || object.DashSize != 0 || object.GapSize != 0 {
		reject("native bundle.worldLine line-list pipeline requires finite width 0 (default) or 0<width<=1 and zero dash/gap metadata; invalid, thick or dashed lines are omitted")
		return
	}
	m := objectMatrix(object)
	material := materialFromObject(object)
	r, g, b, _ := parseColor(material.Color)
	a := material.Opacity
	if math.IsNaN(a) || math.IsInf(a, 0) {
		reject("native line opacity is nonfinite; line omitted")
		return
	}
	a = math.Max(0, math.Min(1, a))
	valid := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= math.MaxFloat32 }
	invalid := false
	start := len(frame.WorldPositions)
	for _, pair := range object.LineSegments {
		var points [6]float64
		ok := true
		for j, index := range pair {
			if index < 0 || index >= len(object.Points) {
				ok = false
				break
			}
			p := object.Points[index]
			for axis := 0; axis < 3; axis++ {
				v := m[axis]*p.X + m[4+axis]*p.Y + m[8+axis]*p.Z + m[12+axis]
				points[j*3+axis] = v
				if !valid(v) {
					ok = false
				}
			}
		}
		if !ok {
			invalid = true
			continue
		}
		frame.WorldPositions = append(frame.WorldPositions, points[:]...)
		frame.WorldColors = append(frame.WorldColors, r, g, b, a, r, g, b, a)
	}
	frame.WorldVertexCount = len(frame.WorldPositions) / 3
	if invalid || len(frame.WorldPositions) == start {
		reject("native line data contains invalid or empty indexed segments; affected segments omitted")
	}
}

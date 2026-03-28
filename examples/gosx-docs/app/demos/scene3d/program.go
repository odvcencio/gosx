package docs

import (
	rootengine "github.com/odvcencio/gosx/engine"
	islandprogram "github.com/odvcencio/gosx/island/program"
)

func GeometryZooProgram() *rootengine.Program {
	builder := rootengine.NewBuilder("GoSXGeometryZoo")

	width := builder.Prop("width", islandprogram.TypeFloat)
	height := builder.Prop("height", islandprogram.TypeFloat)
	input := builder.DeclareViewportInputSignals(width, height)

	normX := builder.Div(builder.Sub(input.PointerX, input.CenterX), input.CenterX)
	normY := builder.Div(builder.Sub(input.CenterY, input.PointerY), input.CenterY)
	boxX := builder.Add(builder.Float(-2.1), builder.Mul(normX, builder.Float(1.05)))
	boxY := builder.Add(builder.Float(0.48), builder.Mul(normY, builder.Float(0.72)))
	orbX := builder.Add(builder.Float(0.0), builder.Mul(normX, builder.Float(-0.42)))
	orbY := builder.Add(builder.Float(-0.16), builder.Mul(normY, builder.Float(0.34)))
	pyramidX := builder.Add(builder.Float(2.16), builder.Mul(normX, builder.Float(-1.02)))
	pyramidY := builder.Add(builder.Float(0.12), builder.Mul(normY, builder.Float(0.46)))

	builder.Camera(map[string]islandprogram.ExprID{
		"x":   builder.Mul(normX, builder.Float(0.62)),
		"y":   builder.Mul(normY, builder.Float(0.34)),
		"z":   builder.Cond(input.ArrowUp, builder.Float(5.1), builder.Float(6.5), islandprogram.TypeFloat),
		"fov": builder.Float(72),
	})

	builder.Mesh("box", "flat", map[string]islandprogram.ExprID{
		"x":         boxX,
		"y":         boxY,
		"z":         builder.Float(0.15),
		"size":      builder.Float(1.5),
		"color":     builder.Cond(input.ArrowLeft, builder.String("#86d8ff"), builder.String("#6ab6ff"), islandprogram.TypeString),
		"rotationX": builder.Mul(normY, builder.Float(0.44)),
		"rotationY": builder.Mul(normX, builder.Float(0.74)),
		"spinX":     builder.Float(0.12),
		"spinY":     builder.Cond(input.ArrowLeft, builder.Float(1.24), builder.Float(0.62), islandprogram.TypeFloat),
	}, rootengine.MeshOptions{})

	builder.Mesh("sphere", "glow", map[string]islandprogram.ExprID{
		"x":          orbX,
		"y":          orbY,
		"z":          builder.Float(1.52),
		"radius":     builder.Float(0.86),
		"color":      builder.Cond(input.ArrowUp, builder.String("#ffd48f"), builder.String("#c4f39c"), islandprogram.TypeString),
		"spinY":      builder.Float(0.84),
		"spinZ":      builder.Float(0.18),
		"shiftY":     builder.Float(0.16),
		"driftSpeed": builder.Float(1.08),
		"driftPhase": builder.Float(0.3),
		"emissive":   builder.Float(0.55),
	}, rootengine.MeshOptions{})

	builder.Mesh("pyramid", "flat", map[string]islandprogram.ExprID{
		"x":          pyramidX,
		"y":          pyramidY,
		"z":          builder.Float(0.18),
		"size":       builder.Float(1.72),
		"color":      builder.Cond(input.ArrowRight, builder.String("#ffab9f"), builder.String("#d6ee82"), islandprogram.TypeString),
		"rotationY":  builder.Mul(normX, builder.Float(-0.42)),
		"rotationX":  builder.Mul(normY, builder.Float(0.28)),
		"spinX":      builder.Float(0.16),
		"spinY":      builder.Cond(input.ArrowRight, builder.Float(-1.08), builder.Float(-0.44), islandprogram.TypeFloat),
		"wireframe":  builder.Cond(input.ArrowRight, builder.Bool(true), builder.Bool(false), islandprogram.TypeBool),
		"driftSpeed": builder.Float(0.72),
		"driftPhase": builder.Float(0.7),
	}, rootengine.MeshOptions{})

	builder.Label(map[string]islandprogram.ExprID{
		"text": builder.Cond(
			input.ArrowLeft,
			builder.String("Left key leans the cube into a cooler palette."),
			builder.String("Pointer motion pulls the cube through the frame."),
			islandprogram.TypeString,
		),
		"x":           boxX,
		"y":           builder.Add(boxY, builder.Float(1.34)),
		"z":           builder.Float(0.78),
		"maxWidth":    builder.Float(188),
		"font":        builder.String(`600 13px "IBM Plex Sans", "Segoe UI", sans-serif`),
		"lineHeight":  builder.Float(18),
		"textAlign":   builder.String("center"),
		"anchorX":     builder.Float(0.5),
		"anchorY":     builder.Float(1),
		"offsetY":     builder.Float(-18),
		"background":  builder.String("rgba(8, 21, 31, 0.82)"),
		"borderColor": builder.String("rgba(106, 182, 255, 0.28)"),
		"color":       builder.String("#ecf7ff"),
	})

	builder.Label(map[string]islandprogram.ExprID{
		"text": builder.Cond(
			input.ArrowUp,
			builder.String("Arrow up brightens the orb and tightens the camera."),
			builder.String("The orb drifts to show motion, glow, and depth."),
			islandprogram.TypeString,
		),
		"x":           orbX,
		"y":           builder.Add(orbY, builder.Float(1.38)),
		"z":           builder.Float(2.3),
		"maxWidth":    builder.Float(194),
		"font":        builder.String(`600 13px "IBM Plex Sans", "Segoe UI", sans-serif`),
		"lineHeight":  builder.Float(18),
		"textAlign":   builder.String("center"),
		"anchorX":     builder.Float(0.5),
		"anchorY":     builder.Float(1),
		"offsetY":     builder.Float(-18),
		"background":  builder.String("rgba(8, 21, 31, 0.82)"),
		"borderColor": builder.String("rgba(255, 212, 143, 0.28)"),
		"color":       builder.String("#fff4de"),
	})

	builder.Label(map[string]islandprogram.ExprID{
		"text": builder.Cond(
			input.ArrowRight,
			builder.String("Right key opens the pyramid wireframe and heats the tint."),
			builder.String("The pyramid keeps the wireframe path in the same route."),
			islandprogram.TypeString,
		),
		"x":           pyramidX,
		"y":           builder.Add(pyramidY, builder.Float(1.5)),
		"z":           builder.Float(0.95),
		"maxWidth":    builder.Float(204),
		"font":        builder.String(`600 13px "IBM Plex Sans", "Segoe UI", sans-serif`),
		"lineHeight":  builder.Float(18),
		"textAlign":   builder.String("center"),
		"anchorX":     builder.Float(0.5),
		"anchorY":     builder.Float(1),
		"offsetY":     builder.Float(-18),
		"background":  builder.String("rgba(8, 21, 31, 0.82)"),
		"borderColor": builder.String("rgba(255, 171, 159, 0.28)"),
		"color":       builder.String("#fff0eb"),
	})

	builder.Mesh("plane", "flat", map[string]islandprogram.ExprID{
		"y":         builder.Float(-1.76),
		"z":         builder.Float(0.15),
		"width":     builder.Float(7.4),
		"depth":     builder.Float(7.4),
		"color":     builder.String("#173044"),
		"rotationX": builder.Float(-1.16),
	}, rootengine.MeshOptions{Static: true})

	return builder.Build()
}

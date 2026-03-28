package docs

import (
	rootengine "github.com/odvcencio/gosx/engine"
	islandprogram "github.com/odvcencio/gosx/island/program"
)

func SceneDemoProgram() *rootengine.Program {
	builder := rootengine.NewBuilder("GoSXRuntimeScene")

	width := builder.Prop("width", islandprogram.TypeFloat)
	height := builder.Prop("height", islandprogram.TypeFloat)
	input := builder.DeclareViewportInputSignals(width, height)

	normX := builder.Div(builder.Sub(input.PointerX, input.CenterX), input.CenterX)
	normY := builder.Div(builder.Sub(input.CenterY, input.PointerY), input.CenterY)
	boxX := builder.Add(builder.Float(-1.25), builder.Mul(normX, builder.Float(1.35)))
	boxY := builder.Add(builder.Float(0.35), builder.Mul(normY, builder.Float(0.85)))
	orbX := builder.Add(builder.Float(1.7), builder.Mul(normX, builder.Float(-0.9)))
	orbY := builder.Add(builder.Float(-0.8), builder.Mul(normY, builder.Float(0.55)))

	builder.Camera(map[string]islandprogram.ExprID{
		"x":   builder.Mul(normX, builder.Float(0.35)),
		"y":   builder.Mul(normY, builder.Float(0.24)),
		"z":   builder.Cond(input.ArrowUp, builder.Float(5.7), builder.Float(6.4), islandprogram.TypeFloat),
		"fov": builder.Float(74),
	})

	builder.Mesh("box", "flat", map[string]islandprogram.ExprID{
		"x":         boxX,
		"y":         boxY,
		"z":         builder.Float(0),
		"size":      builder.Float(1.9),
		"color":     builder.Cond(input.ArrowUp, builder.String("#ffe08f"), builder.String("#8de1ff"), islandprogram.TypeString),
		"rotationX": builder.Mul(normY, builder.Float(0.45)),
		"rotationY": builder.Mul(normX, builder.Float(0.65)),
		"spinX":     builder.Float(0.32),
		"spinY":     builder.Cond(input.ArrowLeft, builder.Float(1.25), builder.Cond(input.ArrowRight, builder.Float(-1.25), builder.Float(0.72), islandprogram.TypeFloat), islandprogram.TypeFloat),
		"spinZ":     builder.Float(0.14),
	}, rootengine.MeshOptions{})

	builder.Mesh("sphere", "glow", map[string]islandprogram.ExprID{
		"x":         orbX,
		"y":         orbY,
		"z":         builder.Float(1.45),
		"radius":    builder.Float(0.72),
		"color":     builder.Cond(input.ArrowLeft, builder.String("#8dffb3"), builder.Cond(input.ArrowRight, builder.String("#ff9c8f"), builder.String("#ffd48f"), islandprogram.TypeString), islandprogram.TypeString),
		"rotationY": builder.Mul(normX, builder.Float(-0.55)),
		"spinX":     builder.Float(-0.24),
		"spinY":     builder.Cond(input.ArrowUp, builder.Float(0.95), builder.Float(0.44), islandprogram.TypeFloat),
		"spinZ":     builder.Float(0.11),
	}, rootengine.MeshOptions{})

	builder.Label(map[string]islandprogram.ExprID{
		"text":        builder.String("Pointer drag shifts the camera and box."),
		"x":           boxX,
		"y":           builder.Add(boxY, builder.Float(1.42)),
		"z":           builder.Float(0.45),
		"maxWidth":    builder.Float(174),
		"font":        builder.String(`600 13px "IBM Plex Sans", "Segoe UI", sans-serif`),
		"lineHeight":  builder.Float(18),
		"textAlign":   builder.String("center"),
		"anchorX":     builder.Float(0.5),
		"anchorY":     builder.Float(1),
		"offsetY":     builder.Float(-18),
		"background":  builder.String("rgba(8, 21, 31, 0.82)"),
		"borderColor": builder.String("rgba(141, 225, 255, 0.24)"),
		"color":       builder.String("#ecf7ff"),
	})

	builder.Label(map[string]islandprogram.ExprID{
		"text": builder.Cond(
			input.ArrowUp,
			builder.String("Arrow up warms the orb and tightens the lens."),
			builder.String("Arrow keys rebalance the orb tint and spin."),
			islandprogram.TypeString,
		),
		"x":           orbX,
		"y":           builder.Add(orbY, builder.Float(1.28)),
		"z":           builder.Float(2.1),
		"maxWidth":    builder.Float(178),
		"font":        builder.String(`600 13px "IBM Plex Sans", "Segoe UI", sans-serif`),
		"lineHeight":  builder.Float(18),
		"textAlign":   builder.String("center"),
		"anchorX":     builder.Float(0.5),
		"anchorY":     builder.Float(1),
		"offsetY":     builder.Float(-18),
		"background":  builder.String("rgba(8, 21, 31, 0.82)"),
		"borderColor": builder.String("rgba(255, 212, 143, 0.26)"),
		"color":       builder.String("#fff5de"),
	})

	builder.Mesh("plane", "flat", map[string]islandprogram.ExprID{
		"y":         builder.Float(-1.7),
		"z":         builder.Float(0.2),
		"width":     builder.Float(5.8),
		"depth":     builder.Float(5.8),
		"color":     builder.String("#173044"),
		"rotationX": builder.Float(-1.14),
	}, rootengine.MeshOptions{Static: true})

	return builder.Build()
}

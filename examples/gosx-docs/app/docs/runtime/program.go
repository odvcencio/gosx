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

	builder.Camera(map[string]islandprogram.ExprID{
		"x":   builder.Mul(normX, builder.Float(0.35)),
		"y":   builder.Mul(normY, builder.Float(0.24)),
		"z":   builder.Cond(input.ArrowUp, builder.Float(5.7), builder.Float(6.4), islandprogram.TypeFloat),
		"fov": builder.Float(74),
	})

	builder.Mesh("box", "flat", map[string]islandprogram.ExprID{
		"x":         builder.Add(builder.Float(-1.25), builder.Mul(normX, builder.Float(1.35))),
		"y":         builder.Add(builder.Float(0.35), builder.Mul(normY, builder.Float(0.85))),
		"z":         builder.Float(0),
		"size":      builder.Float(1.9),
		"color":     builder.Cond(input.ArrowUp, builder.String("#ffe08f"), builder.String("#8de1ff"), islandprogram.TypeString),
		"rotationX": builder.Mul(normY, builder.Float(0.45)),
		"rotationY": builder.Mul(normX, builder.Float(0.65)),
		"spinX":     builder.Float(0.32),
		"spinY":     builder.Cond(input.ArrowLeft, builder.Float(1.25), builder.Cond(input.ArrowRight, builder.Float(-1.25), builder.Float(0.72), islandprogram.TypeFloat), islandprogram.TypeFloat),
		"spinZ":     builder.Float(0.14),
	}, rootengine.MeshOptions{})

	builder.Mesh("sphere", "flat", map[string]islandprogram.ExprID{
		"x":         builder.Add(builder.Float(1.7), builder.Mul(normX, builder.Float(-0.9))),
		"y":         builder.Add(builder.Float(-0.8), builder.Mul(normY, builder.Float(0.55))),
		"z":         builder.Float(1.45),
		"radius":    builder.Float(0.72),
		"color":     builder.Cond(input.ArrowLeft, builder.String("#8dffb3"), builder.Cond(input.ArrowRight, builder.String("#ff9c8f"), builder.String("#ffd48f"), islandprogram.TypeString), islandprogram.TypeString),
		"rotationY": builder.Mul(normX, builder.Float(-0.55)),
		"spinX":     builder.Float(-0.24),
		"spinY":     builder.Cond(input.ArrowUp, builder.Float(0.95), builder.Float(0.44), islandprogram.TypeFloat),
		"spinZ":     builder.Float(0.11),
	}, rootengine.MeshOptions{})

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

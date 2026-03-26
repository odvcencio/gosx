package docs

import (
	"strconv"

	rootengine "github.com/odvcencio/gosx/engine"
	islandprogram "github.com/odvcencio/gosx/island/program"
)

func SceneDemoProgram() *rootengine.Program {
	builder := &sceneProgramBuilder{}

	width := builder.prop("width", islandprogram.TypeFloat)
	height := builder.prop("height", islandprogram.TypeFloat)
	half := builder.float(0.5)
	centerX := builder.mul(width, half)
	centerY := builder.mul(height, half)

	pointerX := builder.signal("$input.pointer.x", islandprogram.TypeFloat)
	pointerY := builder.signal("$input.pointer.y", islandprogram.TypeFloat)
	arrowLeft := builder.signal("$input.key.arrowleft", islandprogram.TypeBool)
	arrowRight := builder.signal("$input.key.arrowright", islandprogram.TypeBool)
	arrowUp := builder.signal("$input.key.arrowup", islandprogram.TypeBool)

	normX := builder.div(builder.sub(pointerX, centerX), centerX)
	normY := builder.div(builder.sub(centerY, pointerY), centerY)

	return &rootengine.Program{
		Name: "GoSXRuntimeScene",
		Signals: []islandprogram.SignalDef{
			{Name: "$input.pointer.x", Type: islandprogram.TypeFloat, Init: centerX},
			{Name: "$input.pointer.y", Type: islandprogram.TypeFloat, Init: centerY},
			{Name: "$input.key.arrowleft", Type: islandprogram.TypeBool, Init: builder.bool(false)},
			{Name: "$input.key.arrowright", Type: islandprogram.TypeBool, Init: builder.bool(false)},
			{Name: "$input.key.arrowup", Type: islandprogram.TypeBool, Init: builder.bool(false)},
		},
		Nodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"x":   builder.mul(normX, builder.float(0.35)),
					"y":   builder.mul(normY, builder.float(0.24)),
					"z":   builder.cond(arrowUp, builder.float(5.7), builder.float(6.4), islandprogram.TypeFloat),
					"fov": builder.float(74),
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":         builder.add(builder.float(-1.25), builder.mul(normX, builder.float(1.35))),
					"y":         builder.add(builder.float(0.35), builder.mul(normY, builder.float(0.85))),
					"z":         builder.float(0),
					"size":      builder.float(1.9),
					"color":     builder.cond(arrowUp, builder.string("#ffe08f"), builder.string("#8de1ff"), islandprogram.TypeString),
					"rotationX": builder.mul(normY, builder.float(0.45)),
					"rotationY": builder.mul(normX, builder.float(0.65)),
					"spinX":     builder.float(0.32),
					"spinY":     builder.cond(arrowLeft, builder.float(1.25), builder.cond(arrowRight, builder.float(-1.25), builder.float(0.72), islandprogram.TypeFloat), islandprogram.TypeFloat),
					"spinZ":     builder.float(0.14),
				},
			},
			{
				Kind:     "mesh",
				Geometry: "sphere",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":         builder.add(builder.float(1.7), builder.mul(normX, builder.float(-0.9))),
					"y":         builder.add(builder.float(-0.8), builder.mul(normY, builder.float(0.55))),
					"z":         builder.float(1.45),
					"radius":    builder.float(0.72),
					"color":     builder.cond(arrowLeft, builder.string("#8dffb3"), builder.cond(arrowRight, builder.string("#ff9c8f"), builder.string("#ffd48f"), islandprogram.TypeString), islandprogram.TypeString),
					"rotationY": builder.mul(normX, builder.float(-0.55)),
					"spinX":     builder.float(-0.24),
					"spinY":     builder.cond(arrowUp, builder.float(0.95), builder.float(0.44), islandprogram.TypeFloat),
					"spinZ":     builder.float(0.11),
				},
			},
			{
				Kind:     "mesh",
				Geometry: "plane",
				Material: "flat",
				Static:   true,
				Props: map[string]islandprogram.ExprID{
					"y":         builder.float(-1.7),
					"z":         builder.float(0.2),
					"width":     builder.float(5.8),
					"depth":     builder.float(5.8),
					"color":     builder.string("#173044"),
					"rotationX": builder.float(-1.14),
				},
			},
		},
		Exprs: builder.exprs,
	}
}

type sceneProgramBuilder struct {
	exprs []islandprogram.Expr
}

func (b *sceneProgramBuilder) addExpr(expr islandprogram.Expr) islandprogram.ExprID {
	id := islandprogram.ExprID(len(b.exprs))
	b.exprs = append(b.exprs, expr)
	return id
}

func (b *sceneProgramBuilder) value(op islandprogram.OpCode, value string, typ islandprogram.ExprType, operands ...islandprogram.ExprID) islandprogram.ExprID {
	return b.addExpr(islandprogram.Expr{
		Op:       op,
		Operands: append([]islandprogram.ExprID(nil), operands...),
		Value:    value,
		Type:     typ,
	})
}

func (b *sceneProgramBuilder) prop(name string, typ islandprogram.ExprType) islandprogram.ExprID {
	return b.value(islandprogram.OpPropGet, name, typ)
}

func (b *sceneProgramBuilder) signal(name string, typ islandprogram.ExprType) islandprogram.ExprID {
	return b.value(islandprogram.OpSignalGet, name, typ)
}

func (b *sceneProgramBuilder) string(value string) islandprogram.ExprID {
	return b.value(islandprogram.OpLitString, value, islandprogram.TypeString)
}

func (b *sceneProgramBuilder) bool(value bool) islandprogram.ExprID {
	return b.value(islandprogram.OpLitBool, strconv.FormatBool(value), islandprogram.TypeBool)
}

func (b *sceneProgramBuilder) float(value float64) islandprogram.ExprID {
	return b.value(islandprogram.OpLitFloat, strconv.FormatFloat(value, 'f', -1, 64), islandprogram.TypeFloat)
}

func (b *sceneProgramBuilder) add(left, right islandprogram.ExprID) islandprogram.ExprID {
	return b.value(islandprogram.OpAdd, "", islandprogram.TypeFloat, left, right)
}

func (b *sceneProgramBuilder) sub(left, right islandprogram.ExprID) islandprogram.ExprID {
	return b.value(islandprogram.OpSub, "", islandprogram.TypeFloat, left, right)
}

func (b *sceneProgramBuilder) mul(left, right islandprogram.ExprID) islandprogram.ExprID {
	return b.value(islandprogram.OpMul, "", islandprogram.TypeFloat, left, right)
}

func (b *sceneProgramBuilder) div(left, right islandprogram.ExprID) islandprogram.ExprID {
	return b.value(islandprogram.OpDiv, "", islandprogram.TypeFloat, left, right)
}

func (b *sceneProgramBuilder) cond(test, whenTrue, whenFalse islandprogram.ExprID, typ islandprogram.ExprType) islandprogram.ExprID {
	return b.value(islandprogram.OpCond, "", typ, test, whenTrue, whenFalse)
}

package docs

import (
	"strconv"

	rootengine "github.com/odvcencio/gosx/engine"
	islandprogram "github.com/odvcencio/gosx/island/program"
)

func SceneDemoProgram() *rootengine.Program {
	return &rootengine.Program{
		Name: "GoSXRuntimeScene",
		Nodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"x":   0,
					"y":   1,
					"z":   2,
					"fov": 3,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":         4,
					"y":         5,
					"z":         6,
					"size":      7,
					"color":     8,
					"spinX":     9,
					"spinY":     10,
					"spinZ":     11,
					"rotationY": 12,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "sphere",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":         13,
					"y":         14,
					"z":         15,
					"radius":    16,
					"color":     17,
					"spinX":     18,
					"spinY":     19,
					"spinZ":     20,
					"rotationY": 21,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			litFloat(0),
			litFloat(0),
			litFloat(6.4),
			litFloat(75),
			litFloat(-1.1),
			litFloat(0.4),
			litFloat(0),
			litFloat(1.9),
			litString("#8de1ff"),
			litFloat(0.46),
			litFloat(0.72),
			litFloat(0.14),
			litFloat(0.16),
			litFloat(1.75),
			litFloat(-0.85),
			litFloat(1.45),
			litFloat(0.72),
			litString("#ffd48f"),
			litFloat(-0.24),
			litFloat(0.44),
			litFloat(0.11),
			litFloat(-0.18),
		},
	}
}

func litFloat(value float64) islandprogram.Expr {
	return islandprogram.Expr{
		Op:    islandprogram.OpLitFloat,
		Value: strconv.FormatFloat(value, 'f', -1, 64),
		Type:  islandprogram.TypeFloat,
	}
}

func litString(value string) islandprogram.Expr {
	return islandprogram.Expr{
		Op:    islandprogram.OpLitString,
		Value: value,
		Type:  islandprogram.TypeString,
	}
}

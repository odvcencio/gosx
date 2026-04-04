package engine

import (
	"testing"

	islandprogram "github.com/odvcencio/gosx/island/program"
)

func TestBuilderDeclaresViewportInputSignals(t *testing.T) {
	builder := NewBuilder("demo")
	width := builder.Prop("width", islandprogram.TypeFloat)
	height := builder.Prop("height", islandprogram.TypeFloat)

	input := builder.DeclareViewportInputSignals(width, height)
	prog := builder.Build()

	if len(prog.Signals) != 5 {
		t.Fatalf("expected five input signals, got %d", len(prog.Signals))
	}
	if input.CenterX < 0 || input.CenterY < 0 {
		t.Fatalf("expected center expressions to be initialized, got %#v", input)
	}
	if input.PointerX < 0 || input.PointerY < 0 {
		t.Fatalf("expected pointer signals to be usable expressions, got %#v", input)
	}
}

func TestBuilderIncludeRemapsExprsAndChildren(t *testing.T) {
	child := NewBuilder("child")
	mesh := child.Mesh("box", "flat", map[string]islandprogram.ExprID{
		"x": child.Add(child.Float(1), child.Float(2)),
	}, MeshOptions{})
	child.AddNode(Node{
		Kind:     "group",
		Children: []int{int(mesh)},
	})
	childProgram := child.Build()

	parent := NewBuilder("parent")
	camera := parent.Camera(map[string]islandprogram.ExprID{
		"z": parent.Float(6),
	})
	included := parent.Include(childProgram)
	prog := parent.Build()

	if camera != 0 {
		t.Fatalf("expected first parent handle to be 0, got %d", camera)
	}
	if len(included) != 2 {
		t.Fatalf("expected two included handles, got %d", len(included))
	}

	includedMesh := prog.Nodes[included[0]]
	if includedMesh.Props["x"] < 1 {
		t.Fatalf("expected included expression ids to be remapped, got %d", includedMesh.Props["x"])
	}

	includedGroup := prog.Nodes[included[1]]
	if len(includedGroup.Children) != 1 || includedGroup.Children[0] != int(included[0]) {
		t.Fatalf("expected included child handle to be remapped, got %#v", includedGroup.Children)
	}
}

func TestBuilderDeclaresSceneEventSignals(t *testing.T) {
	builder := NewBuilder("scene")

	events := builder.DeclareSceneEventSignals("$scene.runtime.event")
	core := builder.DeclareSceneObjectSignals("$scene.runtime.event", "Runtime Core")
	prog := builder.Build()

	if len(prog.Signals) != 24 {
		t.Fatalf("expected 24 scene event signals, got %d", len(prog.Signals))
	}
	if events.Type < 0 || events.SelectedID < 0 || core.Hovered < 0 || core.ClickCount < 0 {
		t.Fatalf("expected usable scene event expressions, got %#v / %#v", events, core)
	}
	if got := prog.Signals[len(prog.Signals)-4].Name; got != "$scene.runtime.event.object.runtime-core.hovered" {
		t.Fatalf("expected canonical object signal slug, got %q", got)
	}
}

func TestBuilderSpriteCreatesSpriteNode(t *testing.T) {
	builder := NewBuilder("sprite")
	handle := builder.Sprite(map[string]islandprogram.ExprID{
		"src": builder.String("/paper-card.png"),
		"x":   builder.Float(1.2),
	})
	prog := builder.Build()

	if handle != 0 {
		t.Fatalf("expected first sprite handle to be 0, got %d", handle)
	}
	if len(prog.Nodes) != 1 || prog.Nodes[0].Kind != "sprite" {
		t.Fatalf("expected one sprite node, got %#v", prog.Nodes)
	}
}

package bridge

import (
	"testing"
	"time"

	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/game/actions"
)

func TestPublish(t *testing.T) {
	m := actions.NewMap()
	m.Define(actions.Context{Name: "g", Actions: []actions.ActionDef{
		{Name: "jump", Bindings: []actions.Binding{{Source: actions.Key("Space")}}},
		{Name: "moveX", Kind: actions.Axis, Bindings: []actions.Binding{{Source: actions.Key("KeyA"), Scale: -1}}},
	}})
	m.Push("g")
	in := game.NewInput()
	p := New("jump", "moveX")
	frame := func() {
		m.Update(16 * time.Millisecond)
		p.Publish(m, in, "p1")
	}
	m.Feed(actions.Event{Source: actions.Key("Space"), Value: 1})
	m.Feed(actions.Event{Source: actions.Key("KeyA"), Value: 1})
	frame()
	if !in.Pressed("jump") || !in.Down("jump") {
		t.Fatalf("jump %+v", in.Action("jump"))
	}
	if v := in.Action("moveX").Value; v != -1 {
		t.Fatalf("moveX %v", v)
	}
	in.EndFrame()
	m.Feed(actions.Event{Source: actions.Key("Space")})
	frame()
	if !in.Released("jump") || in.Down("jump") {
		t.Fatalf("jump %+v", in.Action("jump"))
	}
}

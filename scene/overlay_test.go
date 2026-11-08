package scene

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func testOverlay() Overlay {
	return Overlay{ID: "bubble", Target: "seat", Position: Vec3(0, 1, 0),
		Content:   gosx.El("span", gosx.Attrs(gosx.Attr("role", "status"), gosx.Attr("aria-live", "polite")), gosx.Text("Your <turn>")),
		ClassName: "seat-bubble", Width: 2, Height: .5, OffsetY: -12}
}

func TestOverlayLowersGoContentAndTargetLocalOffset(t *testing.T) {
	o := testOverlay()
	ir := NewGraph(Mesh{ID: "seat", Position: Vec3(2, 0, 0), Rotation: Euler{Z: math.Pi / 2}, Geometry: BoxGeometry{}}, o).SceneIR()
	if len(ir.HTML) != 1 {
		t.Fatal("missing overlay")
	}
	h := ir.HTML[0]
	if h.TargetOffset == nil || *h.TargetOffset != [3]float64{0, 1, 0} || math.Abs(h.X-1) > 1e-9 || math.Abs(h.Y) > 1e-9 {
		t.Fatalf("target placement: %#v", h)
	}
	if h.Mode != "dom" || h.PointerEvents != "none" || !strings.Contains(h.HTML, "Your &lt;turn&gt;") || !strings.Contains(h.HTML, `aria-live="polite"`) {
		t.Fatalf("content: %#v", h)
	}
	if !reflect.DeepEqual(h.legacyProps()["targetOffset"], [3]float64{0, 1, 0}) {
		t.Fatal("legacy commands lost the anchor")
	}
	canonical := (Props{Graph: NewGraph(o)}).CanonicalIR()
	if len(canonical.Nodes) != 1 || canonical.Nodes[0].HTML.TargetOffset == nil || *canonical.Nodes[0].HTML.TargetOffset != *h.TargetOffset {
		t.Fatal("canonical IR lost the target offset")
	}
	o.Target = ""
	o.Interactive = true
	group := NewGraph(Group{Position: Vec3(4, 0, 0), Children: []Node{&o, (*Overlay)(nil)}}).SceneIR()
	if len(group.HTML) != 1 || group.HTML[0].X != 4 || group.HTML[0].Y != 1 || group.HTML[0].TargetOffset != nil || group.HTML[0].PointerEvents != "auto" {
		t.Fatalf("fixed grouped overlay: %#v", group.HTML)
	}
	legacy := NewGraph(HTML{ID: "legacy", Target: "seat", Markup: "<span>fixed</span>"}).SceneIR()
	if legacy.HTML[0].TargetOffset != nil {
		t.Fatal("existing HTML must retain static placement")
	}
}

func TestOverlayWireFixtureAndContentDiff(t *testing.T) {
	previous := NewGraph(testOverlay()).SceneIR()
	data, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 1024 {
		t.Fatalf("small bubble wire exceeds 1 KiB: %d", len(data))
	}
	fixture, err := os.ReadFile("testdata/overlay.json")
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fixture changed: %s", data)
	}
	next := testOverlay()
	next.Content = gosx.Text("Ready")
	commands := DiffCommands(previous, NewGraph(next).SceneIR())
	commandJSON, _ := json.Marshal(commands)
	if len(commands) != 2 || !strings.Contains(string(commandJSON), `"targetOffset":[0,1,0]`) {
		t.Fatalf("content diff: %#v", commands)
	}
}

func TestOverlayRejectsInvalidPlacementAndUnboundedContent(t *testing.T) {
	for i, mutate := range []func(*Overlay){
		func(o *Overlay) { o.Position.Y = math.NaN() }, func(o *Overlay) { o.Width = -1 },
		func(o *Overlay) { o.OffsetX = math.Inf(1) }, func(o *Overlay) { o.Content = gosx.Text("") },
		func(o *Overlay) { o.Content = gosx.Text(strings.Repeat("a", 65537)) }, func(o *Overlay) { o.Target = strings.Repeat("a", 257) },
	} {
		o := testOverlay()
		mutate(&o)
		if _, err := o.Surface(); err == nil {
			t.Errorf("case %d accepted", i)
		}
		if len(NewGraph(o).SceneIR().HTML) != 0 {
			t.Errorf("case %d lowered", i)
		}
	}
}

package scene

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func testText3D() Text3D {
	return Text3D{ID: "score", Text: "Team <A>\n12 & 8", Position: Vec3(1, 2, 3), Rotation: Euler{X: -math.Pi / 2}, Width: 2, Height: .5, Font: "48px monospace", Color: "#f0eee6"}
}

func TestText3DUsesWorldTextureSurfaceAndEscapesContent(t *testing.T) {
	text := testText3D()
	surface, err := text.Surface()
	if err != nil {
		t.Fatal(err)
	}
	if surface.Mode != HTMLTexture || surface.SurfaceWidth != 2 || surface.SurfaceHeight != .5 || surface.TextureWidth != 512 || surface.TextureHeight != 128 || surface.MaxTexturePixels != 256*1024 || surface.PointerEvents != "none" {
		t.Fatalf("surface: %#v", surface)
	}
	if !strings.Contains(surface.Markup, "Team &lt;A&gt;\n12 &amp; 8") || strings.Contains(surface.Markup, "<A>") {
		t.Fatalf("text was not escaped: %s", surface.Markup)
	}
	text.ID = ""
	ir := NewGraph(Group{Position: Vec3(4, 0, 0), Children: []Node{testText3D(), &text}}).SceneIR()
	if len(ir.HTML) != 2 || ir.HTML[0].ID != "score" || ir.HTML[1].ID == "" || ir.HTML[0].X != 5 || ir.HTML[0].RotationX != -math.Pi/2 {
		t.Fatalf("lowered text: %#v", ir.HTML)
	}
	if len(ir.Objects) != 0 || len(ir.Labels) != 0 {
		t.Fatal("text must use the existing textured world plane")
	}
}

func TestText3DWireFixtureAndDiff(t *testing.T) {
	previous := NewGraph(testText3D()).SceneIR()
	fixture, err := os.ReadFile("testdata/text3d.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 1024 {
		t.Fatalf("short score text exceeded its wire budget: %d bytes", len(data))
	}
	var got, want any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("text wire fixture changed: %s", data)
	}
	changed := testText3D()
	changed.Text = "13"
	commands := DiffCommands(previous, NewGraph(changed).SceneIR())
	if len(commands) != 2 || commands[0].Kind != CommandRemoveObject || commands[1].Kind != CommandCreateObject || commands[1].Data.(CommandPayload).Kind != "html" {
		t.Fatalf("text update: %#v", commands)
	}
}

func TestText3DInheritsGroupRotation(t *testing.T) {
	text := testText3D()
	parent := Euler{Y: math.Pi / 2}
	ir := NewGraph(Group{Rotation: parent, Children: []Node{text}}).SceneIR()
	if len(ir.HTML) != 1 {
		t.Fatal("text was lost")
	}
	got := quaternionFromEuler(Euler{X: ir.HTML[0].RotationX, Y: ir.HTML[0].RotationY, Z: ir.HTML[0].RotationZ}).rotate(Vec3(0, 1, 0))
	want := quaternionFromEuler(parent).mul(quaternionFromEuler(text.Rotation)).rotate(Vec3(0, 1, 0))
	if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 || math.Abs(got.Z-want.Z) > 1e-9 {
		t.Fatalf("group rotation: %v want %v", got, want)
	}
}

func TestText3DRejectsInvalidDimensionsAndDeclarations(t *testing.T) {
	cases := []func(*Text3D){
		func(t *Text3D) { t.Text = strings.Repeat("x", 4097) }, func(t *Text3D) { t.Text = string([]byte{0xff}) },
		func(t *Text3D) { t.Width = -1 }, func(t *Text3D) { t.Height = math.NaN() }, func(t *Text3D) { t.Position.Y = math.Inf(1) },
		func(t *Text3D) { t.TextureWidth = 2049 }, func(t *Text3D) { t.TextureWidth = 2048; t.TextureHeight = 2048 },
		func(t *Text3D) { t.MaxTexturePixels = 100 }, func(t *Text3D) { t.Align = "justify" }, func(t *Text3D) { t.LineHeight = 9 },
		func(t *Text3D) { t.Font = "48px serif;color:red" }, func(t *Text3D) { t.Color = "red\nbackground:black" },
	}
	for i, mutate := range cases {
		text := testText3D()
		mutate(&text)
		if _, err := text.Surface(); err == nil {
			t.Errorf("case %d accepted", i)
		}
		if len(NewGraph(text).SceneIR().HTML) != 0 {
			t.Errorf("case %d lowered invalid text", i)
		}
	}
}

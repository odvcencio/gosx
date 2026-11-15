package scene

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func numberAtlas(n int) TextAtlas {
	labels := make([]string, n)
	for i := range labels {
		labels[i] = fmt.Sprint(i + 1)
	}
	return TextAtlas{ID: "numbers", Labels: labels}
}

func TestTextAtlasGridAndRegions(t *testing.T) {
	a := numberAtlas(44)
	a.Columns = 8
	if w, h := a.Size(); w != 8*128 || h != 6*64 {
		t.Fatalf("size = %dx%d, want 1024x384", w, h)
	}
	// Cell 9 is row 1, column 1.
	r := a.Region(9)
	want := [4]float64{128.0 / 1024, 64.0 / 384, 256.0 / 1024, 128.0 / 384}
	if r != want {
		t.Fatalf("Region(9) = %v, want %v", r, want)
	}
	if got := a.RegionUniform(9); len(got) != 4 || got[0] != want[0] || got[3] != want[3] {
		t.Fatalf("RegionUniform(9) = %v", got)
	}
	if a.Region(44) != ([4]float64{}) || a.Region(-1) != ([4]float64{}) {
		t.Fatal("out-of-range regions must be zero")
	}
	// Default columns fill a 4096-pixel row but never exceed the label count.
	if w, _ := numberAtlas(5).Size(); w != 5*128 {
		t.Fatalf("5 labels default to one row, got width %d", w)
	}
	if w, h := numberAtlas(100).Size(); w != 32*128 || h != 4*64 {
		t.Fatalf("100 labels = %dx%d, want 4096x256", w, h)
	}
}

func TestTextAtlasQuadCoversItsCell(t *testing.T) {
	a := numberAtlas(4)
	q := a.Quad(2, 2, 1)
	r := a.Region(2)
	if len(q.Positions) != 12 || len(q.UVs) != 8 || len(q.Indices) != 6 {
		t.Fatalf("quad = %+v", q)
	}
	// Bottom-left corner reads the cell's bottom (v1); top-right its top (v0).
	if q.UVs[0] != r[0] || q.UVs[1] != r[3] || q.UVs[4] != r[2] || q.UVs[5] != r[1] {
		t.Fatalf("quad UVs %v do not cover region %v", q.UVs, r)
	}
	if q.Positions[0] != -1 || q.Positions[7] != 0.5 {
		t.Fatalf("quad is not 2 by 1: %v", q.Positions)
	}
}

func TestTextAtlasTextureTracksContent(t *testing.T) {
	a := numberAtlas(3)
	b := numberAtlas(3)
	if a.Texture() != b.Texture() {
		t.Fatal("equal atlases must share a texture URL")
	}
	if !strings.HasPrefix(a.Texture(), TextAtlasScheme+"numbers#") {
		t.Fatalf("texture = %q", a.Texture())
	}
	b.Labels[1] = "99"
	if a.Texture() == b.Texture() {
		t.Fatal("a changed label must change the texture URL")
	}
	c := numberAtlas(3)
	c.Color = "#ff0000"
	if a.Texture() == c.Texture() {
		t.Fatal("a changed style must change the texture URL")
	}
}

func TestTextAtlasValidate(t *testing.T) {
	for name, a := range map[string]TextAtlas{
		"no id":    {Labels: []string{"1"}},
		"bad id":   {ID: "a b", Labels: []string{"1"}},
		"too tall": {ID: "big", Labels: make([]string, 64*65), CellWidth: 64, CellHeight: 64, Columns: 64},
	} {
		if a.Validate() == nil {
			t.Errorf("%s: Validate accepted %+v", name, a.ID)
		}
	}
	if err := numberAtlas(44).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTextAtlasPropsLowering(t *testing.T) {
	good := TextAtlas{ID: "plates", Labels: []string{"ALEX", "", "SAM"}, OutlineColor: "#000000", OutlineWidth: 3}
	data, err := Props{TextAtlases: []TextAtlas{good, {Labels: []string{"no id"}}}}.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		TextAtlases []map[string]any `json:"textAtlases"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.TextAtlases) != 1 {
		t.Fatalf("got %d atlases, want the valid one: %s", len(decoded.TextAtlases), data)
	}
	got := decoded.TextAtlases[0]
	if got["texture"] != good.Texture() || got["id"] != "plates" || got["cellWidth"].(float64) != 128 || got["font"] != "system-ui, sans-serif" {
		t.Fatalf("atlas record = %v", got)
	}
	labels := got["labels"].([]any)
	if len(labels) != 3 || labels[1] != "" {
		t.Fatalf("blank nameplate lost: %v", labels)
	}
	empty, _ := Props{}.MarshalJSON()
	if strings.Contains(string(empty), "textAtlases") {
		t.Fatal("props without atlases carry textAtlases")
	}
}

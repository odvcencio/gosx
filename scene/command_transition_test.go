package scene

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCommandTransitionSharedGoldenAndIndependentSamples(t *testing.T) {
	data, err := os.ReadFile("testdata/command-transition.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ From, To, Midpoint []Command }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	source := CommandTransition{From: fixture.From, To: fixture.To}
	player, err := source.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	mid, err := player.Sample(.5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mid, fixture.Midpoint) {
		t.Fatalf("midpoint: %#v", mid)
	}
	mid[0].Data.(map[string]any)["x"] = float64(999)
	fixture.From[0].Data.(map[string]any)["x"] = float64(888)
	for _, at := range []float64{1, 0, .9, .5} {
		got, err := player.Sample(at)
		if err != nil {
			t.Fatal(err)
		}
		if at == .5 && !reflect.DeepEqual(got, fixture.Midpoint) {
			t.Fatal("sample/source mutation or seek order changed the pose")
		}
	}
	for _, at := range []float64{math.NaN(), math.Inf(1)} {
		if _, err := player.Sample(at); err == nil {
			t.Fatal("accepted nonfinite progress")
		}
	}
	a, _ := player.Sample(-1)
	b, _ := player.Sample(0)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("did not clamp negative progress")
	}
}

func TestCommandTransitionGeometryVisibilityAndRewind(t *testing.T) {
	// Canonical IR omits zero coordinates; absolute upserts must restore them.
	label := func(z float64) Command {
		return Command{Kind: CommandCreateObject, ObjectID: "caption", Data: map[string]any{"kind": "label", "props": map[string]any{"text": "route", "z": z}}}
	}
	from := []Command{{Kind: CommandRemoveObject, ObjectID: "caption"}, label(0), {Kind: CommandCreateObject, ObjectID: "edge", Data: map[string]any{"geometry": "lines", "props": map[string]any{"points": []any{map[string]any{"x": 1}}, "lineSegments": [][]int{{0, 1}}}}}}
	to := []Command{label(2), {Kind: CommandRemoveObject, ObjectID: "edge"}}
	p, err := (CommandTransition{From: from, To: to}).Prepare()
	if err != nil {
		t.Fatal(err)
	}
	mid, _ := p.Sample(.5)
	if len(mid) != 2 || mid[0].Data.(map[string]any)["props"].(map[string]any)["z"] != float64(1) || mid[1].Kind != CommandCreateObject {
		t.Fatalf("bad retained pose: %+v", mid)
	}
	end, _ := p.Sample(1)
	if end[1].Kind != CommandRemoveObject {
		t.Fatal("explicit remove did not settle")
	}
	start, _ := p.Sample(0)
	props := start[1].Data.(map[string]any)["props"].(map[string]any)
	if props["z"] != float64(0) || props["scaleX"] != float64(1) || props["points"].([]any)[0].(map[string]any)["z"] != float64(0) {
		t.Fatalf("missing normalized geometry defaults: %+v", props)
	}
	// Geometry types and index topology never become fractional or mixed types.
	a := Command{Kind: CommandCreateObject, ObjectID: "mesh", Data: map[string]any{"geometry": "box", "props": map[string]any{"width": 1}}}
	b := Command{Kind: CommandCreateObject, ObjectID: "mesh", Data: map[string]any{"geometry": "sphere", "props": map[string]any{"radius": 2}}}
	p, err = (CommandTransition{From: []Command{a}, To: []Command{b}}).Prepare()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := p.Sample(.5)
	if got[0].Data.(map[string]any)["geometry"] != "box" {
		t.Fatal("interpolated incompatible object kinds")
	}
}

func TestCommandTransitionRejectsUnmatchedUnsafeAndUnboundedData(t *testing.T) {
	deep := any(float64(1))
	for range 34 {
		deep = []any{deep}
	}
	for _, c := range []Command{
		{Kind: CommandCreateObject, Data: map[string]any{}},
		{Kind: 99, Data: map[string]any{}},
		{Kind: CommandSetCamera, Data: map[string]any{"constructor": map[string]any{"polluted": true}}},
		{Kind: CommandSetCamera, Data: deep},
		{Kind: CommandSetCamera, Data: map[string]any{"z": math.NaN()}},
		{Kind: CommandSetCamera, Data: map[string]any{"text": strings.Repeat("x", 4<<20)}},
	} {
		if _, err := (CommandTransition{To: []Command{c}}).Prepare(); err == nil {
			t.Fatalf("accepted invalid unmatched command: %v", c.Kind)
		}
	}
	if _, err := (CommandTransition{From: make([]Command, 10001)}).Prepare(); err == nil {
		t.Fatal("accepted too many commands")
	}
}

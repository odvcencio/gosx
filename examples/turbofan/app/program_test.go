package app

import (
	"encoding/json"
	"math"
	"net/url"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestTurbofanSceneContract(t *testing.T) {
	for _, exterior := range []bool{false, true} {
		props := Turbofan(Options{Exterior: exterior})
		canonical := props.CanonicalIR()
		if err := canonical.Validate(); err != nil {
			t.Fatal(err)
		}
		ir := props.SceneIR()
		if len(ir.Groups) != 2 || len(ir.SpinTracks) != 2 {
			t.Fatalf("expected two independently driven spools: groups=%d tracks=%d", len(ir.Groups), len(ir.SpinTracks))
		}
		parents := map[string]string{}
		blades := 0
		rows := 0
		for _, object := range ir.Objects {
			parents[object.ID] = object.ParentNode
			if object.ID == "fan" || object.ID == "hp-turbine" || strings.HasPrefix(object.ID, "lp-turbine-") || (strings.HasPrefix(object.ID, "compressor-") && !strings.HasPrefix(object.ID, "compressor-disc-")) || strings.HasPrefix(object.ID, "stator-") {
				if object.Vertices == nil || object.Vertices.Count == 0 || len(object.Vertices.Indices) == 0 {
					t.Fatalf("blade row %s lost its authored geometry", object.ID)
				}
				blades += object.Vertices.Count / 54 // (5+1) × (8+1) samples per airfoil
				rows++
			}
		}
		if parents["fan"] != "low-spool" || parents["compressor-0"] != "high-spool" || parents["hp-turbine"] != "high-spool" || parents["lp-turbine-0"] != "low-spool" || parents["stator-0"] != "" {
			t.Fatalf("rotors and stators have incorrect parents: %v", parents)
		}
		if blades != 498 || rows != 18 {
			t.Fatalf("unexpected blade budget: %d blades in %d rows", blades, rows)
		}
		// The near half is resident but hidden in cutaway mode, so switching
		// presentation needs no geometry replacement or network request.
		for _, object := range ir.Objects {
			if object.ID != "nacelle-cover" {
				continue
			}
			nearSide := false
			for i := 2; i < len(object.Vertices.Positions); i += 3 {
				nearSide = nearSide || object.Vertices.Positions[i] > .01
			}
			if !nearSide || object.Visible == nil || *object.Visible != exterior {
				t.Fatalf("exterior=%v: near-side shell residency/visibility incorrect", exterior)
			}
		}
		encoded, err := json.Marshal(ir)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) > 3_500_000 {
			t.Fatalf("scene payload exceeded 3.5 MB: %d bytes", len(encoded))
		}
		t.Logf("exterior=%v: %d objects, %d indexed blade rows / %d blades, %d SceneIR bytes", exterior, len(ir.Objects), rows, blades, len(encoded))
	}
}

func TestTurbofanSharedPivot(t *testing.T) {
	ir := Turbofan(Options{}).SceneIR()
	atStart, err := scene.GroupWorldMatrices(ir.Groups, 0)
	if err != nil {
		t.Fatal(err)
	}
	atTime, err := scene.GroupWorldMatrices(ir.Groups, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"low-spool", "high-spool"} {
		if atStart[id] == atTime[id] {
			t.Fatalf("%s did not rotate", id)
		}
		m := atTime[id]
		if math.Abs(m[0]-1) > 1e-12 || m[12] != 0 || m[13] != 0 || m[14] != 0 {
			t.Fatalf("%s did not keep its shaft-axis pivot: %v", id, m)
		}
	}
	if atTime["low-spool"] == atTime["high-spool"] {
		t.Fatal("spools must turn independently")
	}
}

func TestPresentationChangesOnlyVisibility(t *testing.T) {
	props := Turbofan(Options{})
	presentation := props.Presentation
	if presentation.Endpoint != "" || len(presentation.Views) != 3 || len(presentation.Periods) != 2 {
		t.Fatal("presentation must keep its data on the client")
	}
	for _, period := range presentation.Periods {
		if len(period.Commands) != 22 {
			t.Fatalf("%s: expected 10 covers and 12 section edges, got %d", period.ID, len(period.Commands))
		}
		for _, command := range period.Commands {
			patch := command.Data.(map[string]any)
			if command.Kind != scene.CommandSetTransform || len(patch) != 1 {
				t.Fatalf("casing selection changed more than visibility: %+v", command)
			}
			cover := strings.HasSuffix(command.ObjectID, "-cover")
			if patch["visible"] != ((period.ID == "exterior") == cover) {
				t.Fatalf("%s has wrong visibility for %s", period.ID, command.ObjectID)
			}
		}
	}
	data := pageData(url.Values{"view": {"side"}, "shell": {"full"}})
	if data["view"] != "side" || data["period"] != "exterior" {
		t.Fatal("legacy camera/casing links lost their initial selection")
	}
	invalid := pageData(url.Values{"view": {"bad"}, "period": {"bad"}})
	if invalid["view"] != "perspective" || invalid["period"] != "cutaway" {
		t.Fatal("invalid choices did not fall back to the default study")
	}
}

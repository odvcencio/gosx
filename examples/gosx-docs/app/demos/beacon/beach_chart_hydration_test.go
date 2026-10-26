package docs

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/client/bridge"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

// Island programs load concurrently. Either chart reader can create the shared
// navigation signal before the renderer publishes its first instrument reading.
func TestBeachChartHydrationOrderPreservesSVGInstruments(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	coordinates := blackglassBeachChartIslandCoordinates()
	if len(coordinates) != 6 {
		t.Fatalf("chart coordinates = %v", coordinates)
	}
	props := map[string]float64{
		"DriftX": coordinates["driftX"], "DriftZ": coordinates["driftZ"],
		"BellX": coordinates["bellX"], "BellZ": coordinates["bellZ"],
		"StarX": coordinates["starglassX"], "StarZ": coordinates["starglassZ"],
	}
	for prop := range props {
		if !strings.Contains(string(source), prop+"={data.chartIslands.") {
			t.Errorf("server-to-island prop %s does not match the typed field name", prop)
		}
	}
	propsJSON, _ := json.Marshal(props)
	programs := map[string][]byte{}
	for i, component := range compiled.Components {
		if component.Name != "CoastChart" && component.Name != "CoastPocket" {
			continue
		}
		lowered, err := ir.LowerIsland(compiled, i)
		if err != nil {
			t.Fatal(err)
		}
		programs[component.Name], err = program.EncodeJSON(lowered)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(programs) != 2 {
		t.Fatal("chart and pocket island programs are required")
	}
	for _, order := range [][]string{{"CoastChart", "CoastPocket"}, {"CoastPocket", "CoastChart"}} {
		t.Run(order[0]+"First", func(t *testing.T) {
			b := bridge.New()
			defer b.DisposeIsland("CoastChart")
			defer b.DisposeIsland("CoastPocket")
			b.SetPatchCallback(func(id, raw string) {
				if id != "CoastChart" {
					return
				}
				var patches []vm.PatchOp
				if err := json.Unmarshal([]byte(raw), &patches); err != nil {
					t.Fatal(err)
				}
				for _, patch := range patches {
					if (patch.AttrName == "viewBox" || patch.AttrName == "transform") && patch.Text == "0" {
						t.Errorf("hydration emitted invalid SVG %s=%q", patch.AttrName, patch.Text)
					}
				}
			})
			for _, name := range order {
				if err := b.HydrateIsland(name, name, string(propsJSON), programs[name], "json"); err != nil {
					t.Fatal(err)
				}
			}
			reconciler, ok := b.LookupReconciler("CoastChart")
			if !ok {
				t.Fatal("chart island was not hydrated")
			}
			chart := reconciler.(*vm.Island)
			assertMarkers := func() {
				t.Helper()
				var positions [][2]float64
				for _, node := range chart.CurrentTree().Nodes {
					if node.Tag != "circle" {
						continue
					}
					var point [2]float64
					for _, attr := range node.Attrs {
						if attr.Name == "cx" {
							point[0], err = strconv.ParseFloat(attr.Value, 64)
						}
						if attr.Name == "cy" {
							point[1], err = strconv.ParseFloat(attr.Value, 64)
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					positions = append(positions, point)
				}
				want := [][2]float64{{props["DriftX"], props["DriftZ"]}, {props["BellX"], props["BellZ"]}, {props["StarX"], props["StarZ"]}}
				if len(positions) != len(want) {
					t.Fatalf("chart markers = %v", positions)
				}
				for i := range want {
					if positions[i] != want[i] || positions[i] == [2]float64{} {
						t.Errorf("marker %d = %v, want %v", i, positions[i], want[i])
					}
				}
			}
			assertMarkers()
			assertChart := func(wantViewBox, wantTransform string) {
				t.Helper()
				got := map[string]string{}
				for _, node := range chart.CurrentTree().Nodes {
					for _, attr := range node.Attrs {
						if (node.Tag == "svg" && attr.Name == "viewBox") || (node.Tag == "g" && attr.Name == "transform") {
							got[attr.Name] = attr.Value
						}
					}
				}
				if got["viewBox"] != wantViewBox || got["transform"] != wantTransform {
					t.Errorf("chart SVG attributes = %v, want viewBox=%q transform=%q", got, wantViewBox, wantTransform)
				}
			}
			assertChart("-70 -80 160 120", "translate(21 -42)")
			if err := b.SetSharedSignalJSON("$blackglass.navigation", `{"Ready":true,"Held":true,"MapViewBox":"-59 -102 160 120","MapTransform":"translate(21 -42) rotate(60)"}`); err != nil {
				t.Fatal(err)
			}
			assertChart("-59 -102 160 120", "translate(21 -42) rotate(60)")
			if _, err := b.DispatchAction("CoastChart", "survey", `{}`); err != nil {
				t.Fatal(err)
			}
			assertChart("-245 -430 600 480", "translate(21 -42) rotate(60) scale(3)")
			assertMarkers()
			for _, island := range []string{"drift-isle", "bell-rock", "starglass"} {
				if !strings.Contains(string(source), `data-gosx-event-value="`+island+`"`) {
					t.Fatalf("missing actual navigation ID %s", island)
				}
				payload, _ := json.Marshal(map[string]string{"eventData": island})
				if _, err := b.DispatchAction("CoastChart", "plot", string(payload)); err != nil {
					t.Fatal(err)
				}
				got, err := b.GetSharedSignalJSON("$blackglass.destination")
				if err != nil || got != `"`+island+`"` {
					t.Fatalf("plot %s: destination=%s, err=%v", island, got, err)
				}
			}
		})
	}
}

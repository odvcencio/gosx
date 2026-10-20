package budgets_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"

	docs "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/perf/wire"
)

var updateSceneWorkFixtures = flag.Bool("update-scene3d-fixtures", false, "refresh the docs hero props fixture")

//go:generate go test -run ^TestScene3DWorkHeroFixture$ -args -update-scene3d-fixtures

func TestScene3DWorkHeroFixture(t *testing.T) {
	data, err := json.MarshalIndent(docs.HeroScene(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := "../../client/js/testdata/scene3d-work/hero.json"
	if *updateSceneWorkFixtures {
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("hero props changed; run GOWORK=off go generate ./perf/budgets and measure the Scene3D work budget")
	}
}

// Use the wire gate's ratchet with no raise map: work limits may only fall.
// CI fetches the exact base revision before setting PERF_BUDGET_BASE_REF.
func TestScene3DWorkDownOnlyRatchet(t *testing.T) {
	ref := os.Getenv("PERF_BUDGET_BASE_REF")
	if ref == "" {
		t.Skip("base comparison runs in js-tests with PERF_BUDGET_BASE_REF")
	}
	path := "perf/budgets/scene3d-work.json"
	tree, err := exec.Command("git", "ls-tree", ref, "--", path).CombinedOutput()
	if err != nil {
		t.Fatalf("verify base tree: %s: %v", tree, err)
	}
	if len(tree) == 0 {
		return
	} // Only a verified missing file permits introduction.
	base, err := exec.Command("git", "show", ref+":"+path).Output()
	if err != nil {
		t.Fatal(err)
	}
	head, err := os.ReadFile("scene3d-work.json")
	if err != nil {
		t.Fatal(err)
	}
	findings := wire.Ratchet(sceneWorkLimits(t, base), sceneWorkLimits(t, head))
	for _, finding := range findings {
		t.Error(finding.String())
	}
}

func sceneWorkLimits(t *testing.T, data []byte) wire.Budget {
	t.Helper()
	var budget struct {
		Schema string                    `json:"schema"`
		Cases  map[string]map[string]any `json:"cases"`
	}
	if err := json.Unmarshal(data, &budget); err != nil {
		t.Fatal(err)
	}
	if budget.Schema != "gosx.scene3d-work/v1" || len(budget.Cases) == 0 {
		t.Fatal("invalid Scene3D work budget")
	}
	routes := wire.AppBudget{}
	for name, record := range budget.Cases {
		limits := map[string]int64{}
		var flatten func(string, any)
		flatten = func(path string, value any) {
			switch v := value.(type) {
			case map[string]any:
				for key, child := range v {
					flatten(strings.TrimPrefix(path+"."+key, "."), child)
				}
			case float64:
				if v < 0 || v != float64(int64(v)) {
					t.Fatalf("%s: invalid count %v", path, v)
				}
				limits[path] = int64(v)
			case bool:
				limits[path] = 0
				if v {
					limits[path] = 1
				}
			}
		}
		flatten("", record)
		if len(limits) == 0 {
			t.Fatalf("%s: no work limits", name)
		}
		routes[name] = wire.RouteBudget{Limits: limits}
	}
	return wire.Budget{Apps: map[string]wire.AppBudget{"scene3d": routes}}
}

func TestScene3DWorkRatchetRejectsRaisesAndRemoval(t *testing.T) {
	parse := func(cases string) wire.Budget {
		return sceneWorkLimits(t, []byte(`{"schema":"gosx.scene3d-work/v1","cases":`+cases+`}`))
	}
	base := parse(`{"hero":{"steady":{"draws":6,"bufferUploadBytes":32}}}`)
	for _, cases := range []string{
		`{"hero":{"steady":{"draws":7,"bufferUploadBytes":32}}}`,
		`{"hero":{"steady":{"draws":6}}}`,
		`{"other":{"steady":{"draws":6,"bufferUploadBytes":32}}}`,
	} {
		if len(wire.Ratchet(base, parse(cases))) == 0 {
			t.Fatalf("ratchet accepted %s", cases)
		}
	}
	if got := wire.Ratchet(base, parse(`{"hero":{"steady":{"draws":5,"bufferUploadBytes":16}}}`)); len(got) != 0 {
		t.Fatal(got)
	}
}

package earcut

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixtureExpectation mirrors an entry from upstream's test/expected.json
// (mapbox/earcut v2.2.4): the exact triangle count Triangulate must produce,
// and (when triangleCount > 0) the maximum acceptable Deviation.
type fixtureExpectation struct {
	triangles    int
	maxDeviation float64 // 0 means "must be exactly 0" per upstream's `expected.errors[id] || 0`
}

// fixtureExpectations are vendored verbatim from mapbox/earcut v2.2.4's
// test/expected.json (the "triangles" and "errors" maps), restricted to the
// fixtures vendored under testdata/.
var fixtureExpectations = map[string]fixtureExpectation{
	"building":            {triangles: 13},
	"dude":                {triangles: 106, maxDeviation: 2e-15},
	"water":               {triangles: 2482, maxDeviation: 0.0008},
	"water2":              {triangles: 1212},
	"water-huge":          {triangles: 5177, maxDeviation: 0.0011},
	"bad-hole":            {triangles: 42, maxDeviation: 0.019},
	"degenerate":          {triangles: 0},
	"hole-touching-outer": {triangles: 77},
	"touching-holes":      {triangles: 57},
	"steiner":             {triangles: 9},
	"empty-square":        {triangles: 0},
	"issue16":             {triangles: 12, maxDeviation: 4e-16},
	"issue17":             {triangles: 11, maxDeviation: 2e-16},
	"hourglass":           {triangles: 2},
}

// TestFixtures ports upstream's fixture-driven test loop (test/test.js
// iterating expected.json's "triangles" keys against test/fixtures/*.json),
// for the subset of fixtures vendored under testdata/.
func TestFixtures(t *testing.T) {
	for id, exp := range fixtureExpectations {
		id, exp := id, exp
		t.Run(id, func(t *testing.T) {
			rings := readFixture(t, id)
			vertices, holes, dims := Flatten(rings)

			indices := Triangulate(vertices, holes, dims)
			numTriangles := len(indices) / 3

			if numTriangles != exp.triangles {
				t.Errorf("%s: %d triangles, want %d", id, numTriangles, exp.triangles)
			}

			if exp.triangles > 0 {
				dev := Deviation(vertices, holes, dims, indices)
				if dev > exp.maxDeviation {
					t.Errorf("%s: deviation %v, want <= %v", id, dev, exp.maxDeviation)
				}
			}
		})
	}
}

func readFixture(t *testing.T, id string) [][][]float64 {
	t.Helper()
	path := filepath.Join("testdata", id+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	var rings [][][]float64
	if err := json.Unmarshal(raw, &rings); err != nil {
		t.Fatalf("parsing fixture %s: %v", path, err)
	}
	return rings
}

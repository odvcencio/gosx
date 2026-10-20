package budget

import "testing"

func TestMeasureSVGHandlerFetchFailsDeclaredFetches(t *testing.T) {
	const handler = `<svg onload="fetch(window.fixtureURL)"></svg>`
	for _, placement := range []string{"root", "fetched-child"} {
		t.Run(placement, func(t *testing.T) {
			root, child := handler, "<p>Unused child</p>"
			if placement == "fetched-child" {
				root, child = `<iframe src="/child/"></iframe>`, handler
			}
			gate := gateOptions(t)
			measured := measureCorpusDocuments(t, t.TempDir(), gate.Head.Info, root, child, executionCorpusEncoder)
			if measured.Coverage.Reachability != "unknown" {
				t.Fatal("dynamic handler fetch certified known", measured.Coverage)
			}
			if len(measured.Rows) == 0 {
				t.Fatal("missing measured rows")
			}
			for _, row := range measured.Rows {
				found := false
				for _, policy := range row.Policies {
					if policy.Name == "declared-fetches" {
						found = true
						if policy.Passed {
							t.Fatal("dynamic handler fetch passed declarations", placement, row.PageType)
						}
					}
				}
				if !found {
					t.Fatal("missing declared-fetches policy", row.PageType)
				}
			}
		})
	}
}

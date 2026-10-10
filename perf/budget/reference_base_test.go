package budget

import (
	"strings"
	"testing"
)

func TestMeasureFetchAndImportUseDifferentBases(t *testing.T) {
	for _, base := range []string{"", "/served/", "../served/"} {
		for _, calls := range []string{"fetch", "import", "both"} {
			t.Run(base+"/"+calls, func(t *testing.T) {
				g := closureGraph{redirects: map[string]string{}}
				root := `<script type="module" src="/assets/entry.js"></script>`
				docDir := "/counter/"
				if base != "" {
					root = `<base href="` + base + `"><base href="/ignored/">` + root
					docDir = "/served/"
				}
				code, deps := "", []string{}
				if calls != "import" {
					code += `fetch("./data.js");`
					deps = append(deps, "document-data")
				}
				if calls != "fetch" {
					code += `import("./data.js");`
					deps = append(deps, "module-data")
				}
				g.add("html", "/counter/", "html", "critical", root, []string{"/assets/entry.js"})
				var href *string
				if base != "" {
					href = &base
				}
				g.referenceContext("html", href, closureReference{"/assets/entry.js", "document", false})
				g.add("entry", "/assets/entry.js", "js", "dormant", code, nil, deps...)
				refs := []closureReference{}
				if calls != "import" {
					refs = append(refs, closureReference{"./data.js", "environment", false})
				}
				if calls != "fetch" {
					refs = append(refs, closureReference{"./data.js", "source", false})
				}
				g.referenceContext("entry", nil, refs...)
				g.add("document-data", docDir+"data.js", "js", "dormant", `const documentData=1;`, nil)
				g.add("module-data", "/assets/data.js", "js", "dormant", `const moduleData=1;`, nil)
				report, requests := measureClosureGraph(t, t.TempDir(), g)
				assertClosureModel(t, report, referenceClosure(g))
				for _, asset := range report.Assets {
					if !strings.HasSuffix(asset.ID, "-data") {
						continue
					}
					want := "startup"
					if calls == "import" && strings.HasSuffix(asset.ID, "document-data") || calls == "fetch" && strings.HasSuffix(asset.ID, "module-data") {
						want = "dormant"
					}
					if asset.Phase != want {
						t.Errorf("resource selected with the wrong URL base: %s phase=%s want=%s", asset.ID, asset.Phase, want)
					}
				}
				if calls != "import" && requests[docDir+"data.js"] == 0 || calls != "fetch" && requests["/assets/data.js"] == 0 {
					t.Fatal("required document or module request was omitted", requests)
				}
			})
		}
	}
}

func TestMeasureRedirectAliasRetainsPhysicalCriticalPhase(t *testing.T) {
	g := closureGraph{redirects: map[string]string{"/old/entry.js": "/assets/entry.js"}}
	g.add("html", "/counter/", "html", "critical", `<script src="/old/entry.js"></script>`, []string{"/old/entry.js"})
	g.add("alias", "/old/entry.js", "js", "startup", `const entry=1;`, nil)
	g.add("entry", "/assets/entry.js", "js", "critical", `const entry=1;`, nil)
	report, _ := measureClosureGraph(t, t.TempDir(), g)
	assertClosureModel(t, report, referenceClosure(g))
	for _, asset := range report.Assets {
		if asset.Phase != "critical" {
			t.Errorf("physical body phase=%s want=critical", asset.Phase)
		}
	}
	if report.Rows[0].PhaseBytes.Startup != int64(len("redirect fixture")) {
		t.Fatal("redirect cost lost its own request phase")
	}
}

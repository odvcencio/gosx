package budget

import (
	"fmt"
	"m31labs.dev/gosx/internal/pagecaps"
	"testing"
)

func TestCheckDocumentTreeEmbeddingRestrictions(t *testing.T) {
	const module = `<script type="module">run()</script>`
	for _, tc := range []struct {
		name, root, child   string
		fetched, executable bool
	}{
		{"srcdoc-fetched-child", srcdocWrap(`<iframe src="/child/"></iframe>`, 1, ""), module, true, true},
		{"inherited-srcdoc-sandbox", srcdocWrap(`<iframe src="/child/"></iframe>`, 1, "sandbox"), module, true, false},
		{"sandbox-fetched-child", `<iframe sandbox src="/child/"></iframe>`, module, true, false},
		{"sandbox-allows-scripts", `<iframe sandbox="allow-scripts" src="/child/"></iframe>`, module, true, true},
		{"srcdoc-overrides-src", `<iframe src="/child/" srcdoc="Static"></iframe>`, module, false, false},
		{"inert-srcdoc-overrides-src", `<iframe sandbox src="/child/" srcdoc="Static"></iframe>`, module, false, false},
		{"template-src", `<template><iframe src="/child/"></iframe></template>`, module, false, false},
		{"shared-restricted-first", `<iframe sandbox src="/child/"></iframe><iframe src="/child/"></iframe>`, module, true, true},
		{"shared-allowed-first", `<iframe src="/child/"></iframe><iframe sandbox src="/child/"></iframe>`, module, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := gateOptions(t)
			measured := measureCorpusDocuments(t, t.TempDir(), gate.Head.Info, tc.root, tc.child, executionCorpusEncoder)
			wantRequests := int64(1)
			if tc.fetched {
				wantRequests++
			}
			if measured.Rows[0].Requests != wantRequests || measured.Coverage.Reachability != "known" {
				t.Fatalf("document closure: requests=%d want=%d reachability=%s", measured.Rows[0].Requests, wantRequests, measured.Coverage.Reachability)
			}
			counts := measured.execution["/counter/"]
			wantSources := int64(0)
			if tc.executable {
				wantSources = 1
			}
			if counts.ExecutableSources != wantSources || counts.ExecutableScripts != wantSources {
				t.Fatalf("execution counts=%+v want sources/scripts=%d", counts, wantSources)
			}
			file, profile, coefficients := workedFile(t)
			page := file.PageTypes["static"]
			page.RequiredPolicies = []string{}
			file.PageTypes = map[string]PageType{"static": page}
			file.Routes[0].PageTypes = []string{"static"}
			derived, err := Derive(file, profile, coefficients)
			if err != nil {
				t.Fatal(err)
			}
			gate.File, gate.Profile, gate.Coefficients = derived, profile, coefficients
			gate.Head.Rows = nil
			for _, row := range measured.Rows {
				if row.PageType == "static" {
					gate.Head.Rows = append(gate.Head.Rows, row)
				}
			}
			gate.Head.Assets, gate.Head.Coverage = measured.Assets, measured.Coverage
			gate.Base.Rows[0].PageType = "static"
			gate.Base.Assets = append([]AssetReport{}, measured.Assets...)
			gate.Base.Coverage = measured.Coverage
			setGateBytes(gate.Base, 2048, 0)
			out, err := Check(gate)
			if err != nil {
				t.Fatal(err)
			}
			if out.Passed == tc.executable || tc.executable && !hasGateViolation(out, "policy") {
				t.Fatalf("static verdict=%v execution=%v violations=%v", out.Passed, tc.executable, out.Violations)
			}
		})
	}
}

func TestCheckDocumentTreeUnknownFailsClosed(t *testing.T) {
	for _, cause := range []string{"cycle", "unresolved-frame", "fetched-depth"} {
		t.Run(cause, func(t *testing.T) {
			root := &modelDocument{}
			docs := map[string]*modelDocument{"/counter/": root}
			switch cause {
			case "cycle":
				root.frames = []modelFrame{{src: "/child/"}}
				docs["/child/"] = &modelDocument{frames: []modelFrame{{src: "/counter/"}}}
			case "unresolved-frame":
				root.frames = []modelFrame{{src: "https://other.invalid/child/"}}
			case "fetched-depth":
				current := root
				for i := 1; i <= pagecaps.MaxSrcdocDepth+1; i++ {
					u := fmt.Sprintf("/d%02d/", i)
					next := &modelDocument{}
					docs[u] = next
					current.frames = []modelFrame{{src: u}}
					current = next
				}
			}
			gate := gateOptions(t)
			measured, _, _ := measureModelTree(t, docs)
			if measured.Coverage.Reachability != "unknown" {
				t.Fatal("unresolved tree certified", measured.Coverage)
			}
			gate.Head.Rows = nil
			for _, row := range measured.Rows {
				if row.PageType == "static" {
					gate.Head.Rows = append(gate.Head.Rows, row)
				}
			}
			gate.Head.Assets, gate.Head.Coverage = measured.Assets, measured.Coverage
			// Bind this independently produced evidence to the configured fixture set.
			for _, row := range gate.Head.Rows {
				for _, policy := range row.Policies {
					if policy.Name == "declared-fetches" && policy.Passed {
						t.Fatal("incomplete tree passed declaration policy")
					}
				}
			}
			gate.Base.Rows[0].PageType = "static"
			for _, reportOnly := range []bool{false, true} {
				gate.ReportOnly = reportOnly
				out, err := Check(gate)
				if err != nil {
					t.Fatal(err)
				}
				if out.Passed || !hasGateViolation(out, "unknown-reachability") {
					t.Fatalf("incomplete document tree passed: %+v", out)
				}
			}
		})
	}
}

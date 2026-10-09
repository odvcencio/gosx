package budget

import (
	"bytes"
	"errors"
	"html"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/pagecaps"
)

func srcdocWrap(body string, depth int, attributes string) string {
	for i := 0; i < depth; i++ {
		encoded := strings.ReplaceAll(html.EscapeString(body), "\r", "&#13;")
		body = `<iframe ` + attributes + ` srcdoc="` + encoded + `"></iframe>`
	}
	return body
}

func TestInlineMeasurementKeyedLiteral(t *testing.T) {
	// Existing callers construct measurements using these public fields.
	measurement := HTMLMeasurement{InlineAppScriptMax: 1025, ExecutableScripts: 1, SyncExecutableScripts: 1}
	for _, policy := range htmlGuardrailPolicies(measurement) {
		if policy.Passed {
			t.Fatal("constructed execution observations bypassed a guardrail", policy)
		}
	}
}

func TestInlineScriptSourceBytesAndFrameworkHash(t *testing.T) {
	for _, tc := range []struct{ prefix, code, suffix string }{
		{`<script>`, strings.Repeat("\r\n", 512) + "x", `</script>`},
		{`<svg><script>`, strings.Repeat("&#120;", 172), `</script></svg>`},
		{`<svg><script><![CDATA[`, strings.Repeat("x", 1025), `]]></script></svg>`},
		{`<math><script>`, strings.Repeat("&#120;", 172), `</script></math>`},
	} {
		body := tc.prefix + tc.code + tc.suffix
		root, err := measureHTML([]byte(body), HTMLMeasureOptions{}, testBodyNormalizer)
		if err != nil || root.InlineAppScriptMax < int64(len(tc.code)) || root.InlineAppScriptBytes < int64(len(tc.code)) {
			t.Errorf("raw script source bytes were lost: max=%d bytes=%d want at least %d: %v", root.InlineAppScriptMax, root.InlineAppScriptBytes, len(tc.code), err)
		}
		child, err := measureHTML([]byte(srcdocWrap(body, 1, "")), HTMLMeasureOptions{}, testBodyNormalizer)
		if err != nil || child.executionCounts() != root.executionCounts() {
			t.Errorf("srcdoc changed script byte observations: got=%+v want=%+v: %v", child.executionCounts(), root.executionCounts(), err)
		}
	}
	code := "const value=1;\r\n" + strings.Repeat("x", 1025)
	got, err := measureHTML([]byte(`<script>`+code+`</script>`), HTMLMeasureOptions{FrameworkScriptSHA256: []string{testMeasureHash([]byte(code))}}, testBodyNormalizer)
	if err != nil || got.InlineAppScriptMax != 0 || got.InlineAppScriptBytes != 0 || got.Framework.Raw != int64(len(code)) {
		t.Error("raw framework signature no longer binds inline ownership", err)
	}
}

func TestInlineSrcdocDepthBoundRejectsPartialEvidence(t *testing.T) {
	deep := srcdocWrap(`<p>Static</p>`, pagecaps.MaxSrcdocDepth+1, "")
	for _, body := range []string{deep, `<script>run()</script>` + deep, deep + `<button onclick="run()">Run</button>`} {
		_, detectErr := pagecaps.FromHTML([]byte(body))
		_, measureErr := measureHTML([]byte(body), HTMLMeasureOptions{}, testBodyNormalizer)
		var input *InputError
		if detectErr == nil || !errors.As(measureErr, &input) || input.Code != "capability" || CheckExitCode(nil, measureErr) != 2 {
			t.Fatal("over-depth document produced partial successful evidence", detectErr, measureErr)
		}
	}
	for _, body := range []string{"<template>" + deep + "</template>", srcdocWrap(deep, 1, "sandbox")} {
		got, err := measureHTML([]byte(body), HTMLMeasureOptions{}, testBodyNormalizer)
		if err != nil || got.ExecutableSources != 0 || got.capabilities.Runtime != "none" {
			t.Fatal("inert document crossed the active nesting bound", err)
		}
	}
}

func TestInlineActiveSrcdocGuardrails(t *testing.T) {
	for _, tc := range []struct {
		name, attributes, typ string
		depth, length         int
		inert                 bool
	}{
		{"classic", "", "", 1, 1025, false},
		{"module", "", "module", 1, 1025, false},
		{"nested", "", "", 32, 1025, false},
		{"sandbox-inert", `sandbox`, "", 1, 1025, true},
		{"sandbox-other-tokens", `sandbox="allow-same-origin allow-forms"`, "", 1, 1025, true},
		{"sandbox-non-ascii-space", "sandbox=\"allow-forms\u00a0allow-scripts\"", "", 1, 1025, true},
		{"sandbox-non-ascii-token", "sandbox=\"allow-\u017fcripts\"", "", 1, 1025, true},
		{"sandbox-script-token", `sandbox="ALLOW-SCRIPTS allow-same-origin"`, "", 1, 1025, false},
		{"template", "", "", 1, 1025, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(srcdocWrap(`<script type="`+tc.typ+`">`+strings.Repeat("x", tc.length)+`</script>`, tc.depth, tc.attributes))
			if tc.name == "template" {
				body = append(append([]byte("<template>"), body...), []byte("</template>")...)
			}
			got, err := measureHTML(body, HTMLMeasureOptions{}, testBodyNormalizer)
			if err != nil {
				t.Fatal(err)
			}
			scripts, inline, sync := int64(1), int64(tc.length), int64(1)
			if tc.typ == "module" {
				sync = 0
			}
			if tc.inert {
				scripts, inline, sync = 0, 0, 0
			}
			if got.ExecutableScripts != scripts || got.InlineAppScriptMax != inline || got.SyncExecutableScripts != sync {
				t.Errorf("srcdoc scripts/inline/sync = %d/%d/%d, want %d/%d/%d", got.ExecutableScripts, got.InlineAppScriptMax, got.SyncExecutableScripts, scripts, inline, sync)
			}
			caps, err := pagecaps.FromHTML(body)
			if err != nil || (caps.Runtime == "none") != tc.inert {
				t.Errorf("active document detection differs: runtime=%s error=%v", caps.Runtime, err)
			}
			whole, _ := testBodyNormalizer(body)
			if !bytes.Equal(got.full, body) || got.Sizes != whole || got.App.Raw != whole.Raw || got.Framework.Raw != 0 {
				t.Fatal("srcdoc compression must use the whole outer document")
			}
		})
	}
}

func TestCheckActiveSrcdocGuardrails(t *testing.T) {
	for _, tc := range []struct {
		name, typ, attributes string
		length                int
		passed                bool
	}{
		{"classic-small", "", "", 16, false},
		{"classic-large", "", "", 1025, false},
		{"module-at-limit", "module", "", 1024, true},
		{"module-over-limit", "module", "", 1025, false},
		{"sandbox-inert", "", `sandbox="allow-same-origin"`, 1025, true},
		{"sandbox-active", "", `sandbox="allow-scripts"`, 1025, false},
	} {
		for _, asChild := range []bool{false, true} {
			name := tc.name + "/root"
			if asChild {
				name = tc.name + "/child"
			}
			t.Run(name, func(t *testing.T) {
				gate := gateOptions(t)
				file, profile, coefficients := workedFile(t)
				page := file.PageTypes["enhanced"]
				page.RequiredPolicies = []string{"no-sync-script"}
				file.PageTypes = map[string]PageType{"enhanced": page}
				file.Routes[0].PageTypes = []string{"enhanced"}
				derived, err := Derive(file, profile, coefficients)
				if err != nil {
					t.Fatal(err)
				}
				gate.File, gate.Profile, gate.Coefficients = derived, profile, coefficients
				snippet := srcdocWrap(`<script type="`+tc.typ+`">`+strings.Repeat("x", tc.length)+`</script>`, 1, tc.attributes)
				measured := measureCorpusReport(t, t.TempDir(), gate.Head.Info, snippet, asChild, testBodyNormalizer)
				gate.Head.Rows = nil
				for _, row := range measured.Rows {
					if row.PageType == "enhanced" {
						gate.Head.Rows = append(gate.Head.Rows, row)
					}
				}
				gate.Head.Assets, gate.Head.Coverage = measured.Assets, measured.Coverage
				gate.Base.Rows[0].PageType = "enhanced"
				// Hold asset growth constant to isolate executable policy checks.
				gate.Base.Assets = append([]AssetReport{}, measured.Assets...)
				gate.Base.Coverage = measured.Coverage
				for _, reportOnly := range []bool{false, true} {
					gate.ReportOnly = reportOnly
					out, err := Check(gate)
					if err != nil {
						t.Fatal(err)
					}
					if out.Passed != tc.passed {
						t.Fatalf("srcdoc verdict=%t, want %t; violations=%v", out.Passed, tc.passed, out.Violations)
					}
					code := 0
					if !tc.passed && !reportOnly {
						code = 1
					}
					if CheckExitCode(out, err) != code || !tc.passed && !hasGateViolation(out, "policy") {
						t.Fatal("srcdoc policy failure or exit code missing", out.Violations)
					}
				}
			})
		}
	}
}

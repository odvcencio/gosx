package budget

import (
	"fmt"
	"html"
	"math/rand"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/pagecaps"
)

func executableByteMarkup(kind, code string) string {
	switch kind {
	case "script":
		return `<script type="module">` + code + `</script>`
	case "handler":
		return `<button onclick="` + html.EscapeString(code) + `">Run</button>`
	case "url":
		return `<a href="javascript:` + html.EscapeString(code) + `">Run</a>`
	case "refresh":
		return `<meta http-equiv="refresh" content="0; URL='javascript:` + html.EscapeString(code) + `'">`
	default:
		panic("missing executable byte witness: " + kind)
	}
}

func executableBytePlacement(markup string, placement policyPlacement) (root, child string) {
	root, child = markup, `<p>Unused child</p>`
	attributes := ""
	if placement.restricted {
		attributes = "sandbox"
	}
	if placement.child {
		root, child = `<iframe `+attributes+` src="/child/"></iframe>`, markup
	}
	if placement.depth > 0 {
		root = srcdocWrap(root, placement.depth, attributes)
	}
	if placement.template {
		root = `<template>` + root + `</template>`
	}
	return root, child
}

func TestCheckInlineExecutableByteBoundaries(t *testing.T) {
	dir := t.TempDir()
	for _, kind := range []string{"script", "handler", "url", "refresh"} {
		for _, placement := range policyPlacements() {
			for _, length := range []int{1024, 1025} {
				t.Run(fmt.Sprintf("%s/%s/%d", kind, placement.name, length), func(t *testing.T) {
					gate := policyPlacementGate(t, inlineExecutablePolicy)
					code := "/*" + strings.Repeat("x", length-4) + "*/"
					root, child := executableBytePlacement(executableByteMarkup(kind, code), placement)
					report := measureCorpusDocuments(t, dir, gate.Head.Info, root, child, executionCorpusEncoder)
					want := int64(length)
					if placement.template || placement.restricted {
						want = 0
					}
					counts := report.execution["/counter/"]
					if counts.InlineAppScriptMax != want || counts.InlineAppScriptBytes != want {
						t.Errorf("executing code bytes: max=%d total=%d want=%d", counts.InlineAppScriptMax, counts.InlineAppScriptBytes, want)
					}
					installPolicyMeasurement(t, &gate, report, gate.Head.Info)
					wantPass := want <= 1024
					for _, reportOnly := range []bool{false, true} {
						gate.ReportOnly = reportOnly
						out, err := Check(gate)
						if err != nil {
							t.Fatal(err)
						}
						if out.Passed != wantPass || hasGateViolation(out, "policy") == wantPass {
							t.Errorf("inline guardrail verdict: pass=%t want=%t violations=%v", out.Passed, wantPass, out.Violations)
						}
						wantExit := 0
						if !wantPass && !reportOnly {
							wantExit = 1
						}
						if got := CheckExitCode(out, err); got != wantExit {
							t.Errorf("exit=%d want=%d", got, wantExit)
						}
					}
				})
			}
		}
	}
}

func TestInlineExecutableAttributesCannotClaimFrameworkSignatures(t *testing.T) {
	code := "/*" + strings.Repeat("x", 1021) + "*/"
	for _, kind := range []string{"handler", "url", "refresh"} {
		for _, depth := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/depth=%d", kind, depth), func(t *testing.T) {
				markup := executableByteMarkup(kind, code)
				measured, err := measureHTML([]byte(srcdocWrap(markup, depth, "")), HTMLMeasureOptions{FrameworkScriptSHA256: []string{testMeasureHash([]byte(code))}}, executionCorpusEncoder)
				if err != nil {
					t.Fatal(err)
				}
				if measured.InlineAppScriptMax != 1025 || measured.InlineAppScriptBytes != 1025 || measured.inlineFramework {
					t.Errorf("attribute claimed framework ownership: %+v", measured.executionCounts())
				}
			})
		}
	}
}

func TestInlineExecutableSandboxHandlerAndExternalScript(t *testing.T) {
	code := "/*" + strings.Repeat("x", 1021) + "*/"
	for _, tc := range []struct {
		name, markup string
		bytes        int64
	}{
		{"parent-handler", `<iframe sandbox onload="` + code + `" srcdoc="&lt;p onclick=run()&gt;Child&lt;/p&gt;"></iframe>`, 1025},
		{"permitted-srcdoc", srcdocWrap(executableByteMarkup("handler", code), 1, `sandbox="allow-scripts"`), 1025},
		{"external-fallback", `<script src="/external.js">` + code + `</script>`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			measured, err := measureHTML([]byte(tc.markup), HTMLMeasureOptions{FrameworkScriptSHA256: []string{testMeasureHash([]byte(code))}}, executionCorpusEncoder)
			if err != nil {
				t.Fatal(err)
			}
			if measured.InlineAppScriptBytes != tc.bytes || measured.InlineAppScriptMax != tc.bytes || measured.inlineFramework {
				t.Errorf("inline code: %+v want bytes=%d", measured.executionCounts(), tc.bytes)
			}
		})
	}
}

func TestInlineExecutableGeneratedCodeByteAccounting(t *testing.T) {
	// Enumerate the classifier's kinds, requiring a witness for each. Adding a
	// kind to pagecaps fails here until its executing-code contract is covered.
	witnesses := map[pagecaps.ExecutableSourceKind]string{
		pagecaps.ScriptSource:        "script",
		pagecaps.EventHandlerSource:  "handler",
		pagecaps.JavascriptURLSource: "url",
		pagecaps.RefreshURLSource:    "refresh",
	}
	kinds := pagecaps.ExecutableSourceKinds()
	groups := [][]pagecaps.ExecutableSourceKind{}
	for _, kind := range kinds {
		if _, ok := witnesses[kind]; !ok {
			t.Fatalf("classifier kind %d has no code-byte accounting witness", kind)
		}
		groups = append(groups, []pagecaps.ExecutableSourceKind{kind})
	}
	groups = append(groups, kinds)
	rng := rand.New(rand.NewSource(55007))
	dir := t.TempDir()
	info := publicTestReport(t).Info
	measurements := 0
	for groupIndex, group := range groups {
		for _, placement := range policyPlacements() {
			for variant := 0; variant < 4; variant++ {
				t.Run(fmt.Sprintf("group-%d/%s/%d", groupIndex, placement.name, variant), func(t *testing.T) {
					var markup strings.Builder
					var codeBytes, codeMax int64
					for _, kind := range group {
						copies := 1 + rng.Intn(3)
						for copy := 0; copy < copies; copy++ {
							// Multibyte characters and HTML-sensitive code exercise
							// byte counts after attribute and srcdoc entity decoding.
							code := "/*é<&'\" " + strings.Repeat("x", rng.Intn(1400)) + "*/"
							fragment := executableByteMarkup(witnesses[kind], code)
							if variant%2 != 0 {
								fragment = strings.ReplaceAll(fragment, "javascript:", "JaVaScRiPt:")
							}
							// Check the classifier emits code, rather than URL or
							// refresh decoration, independently of budget accounting.
							sources := 0
							_, err := pagecaps.InspectHTML([]byte(fragment), func(source pagecaps.ExecutableSource) {
								sources++
								if source.Kind != kind || string(source.Body) != code {
									t.Errorf("classifier code differs: kind=%d want=%d bytes=%d want=%d", source.Kind, kind, len(source.Body), len(code))
								}
							})
							if err != nil || sources != 1 {
								t.Fatalf("classifier witness: sources=%d err=%v", sources, err)
							}
							markup.WriteString(fragment)
							codeBytes += int64(len(code))
							codeMax = max(codeMax, int64(len(code)))
						}
					}
					root, child := executableBytePlacement(markup.String(), placement)
					wrap := func(body string) []byte { return []byte("<!doctype html><html><body>" + body + "</body></html>") }
					tree, err := pagecaps.ParseDocumentTree(wrap(root), "/counter/", func(_, reference string) ([]byte, string, bool, error) {
						return wrap(child), reference, reference == "/child/", nil
					})
					if err != nil {
						t.Fatal(err)
					}
					var classifiedBytes, classifiedMax int64
					_, err = pagecaps.InspectDocumentTree(tree, func(_ *pagecaps.Document, source pagecaps.ExecutableSource) {
						if _, ok := witnesses[source.Kind]; !ok {
							t.Fatalf("unregistered emitted kind %d", source.Kind)
						}
						classifiedBytes += int64(len(source.Body))
						classifiedMax = max(classifiedMax, int64(len(source.Body)))
					})
					if err != nil {
						t.Fatal(err)
					}
					if placement.restricted || placement.template {
						codeBytes, codeMax = 0, 0
					}
					if classifiedBytes != codeBytes || classifiedMax != codeMax {
						t.Fatalf("permitted code differs: bytes/max=%d/%d want=%d/%d", classifiedBytes, classifiedMax, codeBytes, codeMax)
					}
					report := measureCorpusDocuments(t, dir, info, root, child, executionCorpusEncoder)
					counts := report.execution["/counter/"]
					if counts.InlineAppScriptBytes != classifiedBytes || counts.InlineAppScriptMax != classifiedMax {
						t.Errorf("guardrail accounting differs from classifier: bytes/max=%d/%d want=%d/%d", counts.InlineAppScriptBytes, counts.InlineAppScriptMax, classifiedBytes, classifiedMax)
					}
					for _, row := range report.Rows {
						if passed, exists := observedPolicy(row, inlineExecutablePolicy); !exists || passed != (classifiedMax <= 1024) {
							t.Errorf("guardrail policy=%t exists=%t max=%d", passed, exists, classifiedMax)
						}
					}
					measurements++
				})
			}
		}
	}
	t.Logf("seed=55007 classifier kinds=%d placements=%d generated measurements=%d", len(kinds), len(policyPlacements()), measurements)
}

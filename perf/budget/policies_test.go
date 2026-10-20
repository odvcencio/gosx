package budget

import (
	"bytes"
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"
)

func TestPolicyOnlyConfiguredRequirementsGateExceptFixedRules(t *testing.T) {
	page := PageType{RequiredPolicies: []string{"html-compressed"}}
	observed := []PolicyResult{{"html-compressed", true}, {inlineExecutablePolicy, true}, {"no-cookie", false}}
	if failures := requiredPolicyFailures("island", page, observed, nil); len(failures) != 0 {
		t.Fatal("unrequired policy forced a configuration edit", failures)
	}
	observed = append(observed, PolicyResult{"html-compressed", false})
	if failures := requiredPolicyFailures("island", page, observed, nil); !reflect.DeepEqual(failures, []string{"html-compressed"}) {
		t.Fatal("duplicate passing claim hid failure", failures)
	}
	if failures := requiredPolicyFailures("static", PageType{}, []PolicyResult{{inlineExecutablePolicy, true}}, []string{"zero-js"}); !reflect.DeepEqual(failures, []string{"zero-js"}) {
		t.Fatal("static zero-JS was waived", failures)
	}
	if failures := requiredPolicyFailures("island", PageType{}, nil, []string{inlineExecutablePolicy}); !reflect.DeepEqual(failures, []string{inlineExecutablePolicy}) {
		t.Fatal("fixed inline gate was waived", failures)
	}
}
func TestPolicyHTMLExecutableGuardrailAndSynchronousScripts(t *testing.T) {
	for _, tc := range []struct {
		body string
		sync int64
	}{
		{`<script src="/gosx/app.js"></script>`, 1},
		{`<script src="/gosx/app.js" defer></script>`, 0},
		{`<script src="/gosx/app.js" async></script>`, 0},
		{`<script type="module" src="/gosx/app.js"></script>`, 0},
		{`<script async>x()</script>`, 1},
		{`<script type="module">x()</script>`, 0},
		{`<template><script>x()</script></template><script type="application/json">{}</script>`, 0},
	} {
		html, err := measureHTML([]byte(tc.body), HTMLMeasureOptions{}, testBodyNormalizer)
		if err != nil || html.SyncExecutableScripts != tc.sync {
			t.Fatal("synchronous script classification incorrect", tc, html.SyncExecutableScripts, err)
		}
		policies := htmlGuardrailPolicies(html)
		if !policies[0].Passed || policies[1].Passed != (tc.sync == 0) {
			t.Fatal("HTML policy projection incorrect", policies)
		}
	}
	for _, size := range []int{1024, 1025} {
		html, err := measureHTML([]byte(`<script type="module">`+strings.Repeat("x", size)+`</script>`), HTMLMeasureOptions{}, testBodyNormalizer)
		if err != nil || htmlGuardrailPolicies(html)[0].Passed != (size == 1024) {
			t.Fatal("inline cap changed", size, err)
		}
	}
}
func TestPolicyPublicResultAllowlistIncludesFixedGuardrail(t *testing.T) {
	report := publicTestReport(t)
	report.Rows[0].Policies = append(report.Rows[0].Policies, PolicyResult{inlineExecutablePolicy, true})
	data, _ := json.Marshal(report)
	if err := testPublicValidator(t).Validate(bytes.NewReader(data), "json"); err != nil {
		t.Fatal("fixed guardrail absent from public allowlist", err)
	}
	report.Rows[0].Policies = append(report.Rows[0].Policies, PolicyResult{"other-policy", true})
	data, _ = json.Marshal(report)
	if err := testPublicValidator(t).Validate(bytes.NewReader(data), "json"); err == nil {
		t.Fatal("unknown policy admitted")
	}
}
func TestCheckModelRoundingRejectsOverflow(t *testing.T) {
	value, err := ceilMicros(big.NewRat(11, 10))
	if err != nil || value != 2 {
		t.Fatal("model prediction was rounded down", value, err)
	}
	huge := new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), 64))
	if _, err := ceilMicros(huge); err == nil {
		t.Fatal("model prediction overflowed into a pass")
	}
}

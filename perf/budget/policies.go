package budget

import "sort"

// Inline executable size is a fixed guardrail, independent of the configured
// policy list. Its closed result code carries no script text or source URL.
const inlineExecutablePolicy = "inline-app-executable"

func knownPolicyResult(name string) bool {
	props := inputDefinitions["PageType"].(map[string]any)["properties"].(map[string]any)
	return name == inlineExecutablePolicy || validateInput(name, props["requiredPolicies"].(map[string]any)["items"]) == nil
}

func htmlGuardrailPolicies(html HTMLMeasurement) []PolicyResult {
	return []PolicyResult{{Name: inlineExecutablePolicy, Passed: html.InlineAppScriptMax <= 1024}, {Name: "no-sync-script", Passed: html.SyncExecutableScripts == 0}}
}

func requiredPolicyFailures(pageType string, page PageType, observed []PolicyResult, waived []string) []string {
	required := map[string]bool{inlineExecutablePolicy: true}
	for _, name := range page.RequiredPolicies {
		required[name] = true
	}
	family, _, _ := pageTypeVariant(pageType)
	if family == "static" {
		required["zero-js"] = true
	}
	passed := map[string]bool{}
	for _, policy := range observed {
		old, exists := passed[policy.Name]
		passed[policy.Name] = policy.Passed && (!exists || old)
	}
	for _, name := range waived {
		if name != inlineExecutablePolicy && !(name == "zero-js" && family == "static") {
			passed[name] = true
		}
	}
	names := []string{}
	for name := range required {
		if !passed[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

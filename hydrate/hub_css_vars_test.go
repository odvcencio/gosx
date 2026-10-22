package hydrate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHubCSSVarsValidateAcceptsCustomPropertyNames(t *testing.T) {
	cfg := HubCSSVars{Vars: []HubCSSVar{{Name: "--accent-core", Path: "palette.core"}, {Name: "--hue", Path: "hue", Unit: "deg", Digits: 1}}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestHubCSSVarsValidateRejectsBadInput(t *testing.T) {
	cases := map[string]HubCSSVars{
		"no vars":          {},
		"name without --":  {Vars: []HubCSSVar{{Name: "accent", Path: "a"}}},
		"name with space":  {Vars: []HubCSSVar{{Name: "--a b", Path: "a"}}},
		"empty path":       {Vars: []HubCSSVar{{Name: "--a", Path: ""}}},
		"path with braces": {Vars: []HubCSSVar{{Name: "--a", Path: "a[0]"}}},
		"flash with space": {Vars: []HubCSSVar{{Name: "--a", Path: "a"}}, FlashClass: "x y"},
		"flash too short":  {Vars: []HubCSSVar{{Name: "--a", Path: "a"}}, FlashClass: "x", FlashMS: 5},
		"digits too big":   {Vars: []HubCSSVar{{Name: "--a", Path: "a", Digits: 11}}},
		"duplicate name":   {Vars: []HubCSSVar{{Name: "--a", Path: "a"}, {Name: "--a", Path: "b"}}},
	}
	for name, cfg := range cases {
		err := cfg.Validate()
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		if !strings.Contains(err.Error(), "cssVars") {
			t.Fatalf("%s: error must name cssVars so the author can find it: %v", name, err)
		}
	}
}

func TestHubBindingCSSVarsRoundTrip(t *testing.T) {
	b := HubBinding{Event: "palette", CSSVars: &HubCSSVars{Root: "state", Vars: []HubCSSVar{{Name: "--core", Path: "coreInner"}}, FlashClass: "palette-arrival", FlashMS: 7600, FlashWhen: "frame", LiveClass: "chrome-live"}}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"cssVars":{`, `"root":"state"`, `"flashMs":7600`, `"flashWhen":"frame"`, `"liveClass":"chrome-live"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s in %s", want, data)
		}
	}
	plain, _ := json.Marshal(HubBinding{Event: "tick", Signal: "$tick"})
	if strings.Contains(string(plain), "cssVars") {
		t.Fatalf("cssVars must be omitted when nil: %s", plain)
	}
}

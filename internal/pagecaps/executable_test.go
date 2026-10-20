package pagecaps

import (
	"html"
	"strings"
	"testing"
)

func TestPageCapsEmbeddedExecutableMarkup(t *testing.T) {
	for _, tc := range []struct {
		name, markup string
		active       bool
	}{
		{"legacy-type", `<script type="TEXT/ECMASCRIPT">run()</script>`, true},
		{"refresh", `<meta HTTP-EQUIV="ReFrEsH" content="0; URL = 'JaVaScRiPt:run()'">`, true},
		{"refresh-comma", `<meta http-equiv="refresh" content="0,javascript:run()">`, true},
		{"refresh-inert", `<meta http-equiv="refresh" content="0;url=/next/">`, false},
		{"srcdoc-handler", `<iframe srcdoc="&lt;p ONCLICK=run()&gt;Run&lt;/p&gt;"></iframe>`, true},
		{"srcdoc-inert", `<iframe srcdoc="&lt;script type=application/json&gt;{}&lt;/script&gt;"></iframe>`, false},
		{"srcdoc-plain-url", `<iframe srcdoc="javascript:run()"></iframe>`, false},
		{"srcdoc-overrides-src", `<iframe src="javascript:run()" srcdoc="&lt;p&gt;Static&lt;/p&gt;"></iframe>`, false},
		{"srcdoc-overrides-src-reversed", `<iframe srcdoc="&lt;p&gt;Static&lt;/p&gt;" src="javascript:run()"></iframe>`, false},
		{"sandbox-blocks-src", `<iframe sandbox src="javascript:run()"></iframe>`, false},
		{"sandbox-outer-handler", `<iframe sandbox onload="run()" srcdoc="&lt;script&gt;run()&lt;/script&gt;"></iframe>`, true},
		{"template", `<template><iframe srcdoc="&lt;script&gt;run()&lt;/script&gt;"></iframe></template>`, false},
		{"svg-template", `<svg><template><script>run()</script></template></svg>`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			caps, err := FromHTML([]byte(tc.markup))
			if err != nil || (caps.Runtime != "none") != tc.active {
				t.Fatalf("embedded executable classification differs: %v", err)
			}
		})
	}
}

func TestPageCapsBoundsNestedSrcdocParsing(t *testing.T) {
	body := `<p>Static</p>`
	for i := 0; i < 33; i++ {
		body = `<iframe srcdoc="` + html.EscapeString(body) + `"></iframe>`
	}
	if _, err := FromHTML([]byte(body)); err == nil {
		t.Fatal("unbounded nested document parsing accepted")
	}
	// Inert templates must not force parsing of embedded document contents.
	if caps, err := FromHTML([]byte("<template>" + body + "</template>")); err != nil || caps.Runtime != "none" {
		t.Fatal("inert embedded document was parsed", err)
	}
	if !ExecutableScriptType("text/javascript; broken") {
		t.Fatal("MIME essence with parameters became inert")
	}
	for _, typ := range []string{"application/json", "text/plain", "text/template; broken"} {
		if ExecutableScriptType(typ) || ExecutableScriptType(strings.ToUpper(typ)) {
			t.Fatal("data block treated as executable")
		}
	}
}

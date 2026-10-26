package htmlattr

import "testing"

func TestSafeSpreadNameRejectsSrcdoc(t *testing.T) {
	for _, name := range []string{"srcdoc", "SRCDOC", "srcDoc"} {
		if SafeSpreadName(name) {
			t.Errorf("executable HTML accepted from spread: %s", name)
		}
		if !SafeAuthoredName(name) {
			t.Errorf("explicit authored attribute refused: %s", name)
		}
	}
}

func TestSafeNamesAndURLs(t *testing.T) {
	for _, name := range []string{"", "x y", "x=y", "x\x00", "x<y", "x`y", "x\"y", "x/y", "x\u00a0y"} {
		if ValidName(name) {
			t.Errorf("accepted name %q", name)
		}
	}
	for _, name := range []string{"data-id", "aria-label", "xlink:href"} {
		if !ValidName(name) {
			t.Errorf("rejected %q", name)
		}
	}
	for _, name := range []string{"onload", "OnClick", "STYLE", "x y"} {
		if SafeSpreadName(name) {
			t.Errorf("unsafe spread %q", name)
		}
	}
	for _, tag := range []string{"img src=x", "1div", "div>", "div\x00"} {
		if ValidTag(tag) {
			t.Errorf("accepted tag %q", tag)
		}
	}
	for _, tag := range []string{"div", "my-element", "linearGradient", "svg:path"} {
		if !ValidTag(tag) {
			t.Errorf("rejected tag %q", tag)
		}
	}
	for _, name := range []string{"href", "SRC", "action", "formaction", "xlink:href", "poster"} {
		for _, value := range []string{"javascript:example", " JaVaScRiPt:example", "java\nscript:example", "data:text/html,example", "vbscript:example", "\x00javascript:example"} {
			if got := FilterURL(name, value); got != "#" {
				t.Errorf("%s accepted %q", name, value)
			}
		}
		for _, value := range []string{"/path", "../path", "#fragment", "?query", "//cdn.example/img", "https://app.example/", "http://app.example/", "mailto:inbox", "tel:123", "path?url=javascript:example", "java&#x73;cript:example"} {
			if got := FilterURL(name, value); got != value {
				t.Errorf("%s rejected %q", name, value)
			}
		}
	}
	if FilterURL("data-value", "javascript:example") != "javascript:example" {
		t.Fatal("changed non-URL data")
	}
}

func TestReservedDispatchName(t *testing.T) {
	for _, name := range []string{"data-gosx-handler", "DATA-GOSX-PATH", "data-gosx-on-click", "Data-Gosx-On-Input", "data-gosx-on-"} {
		if !ReservedDispatchName(name) || SafeAuthoredName(name) || SafeSpreadName(name) {
			t.Errorf("dispatch name allowed: %q", name)
		}
	}
	for _, name := range []string{"data-gosx-link", "data-gosx-region", "data-gosx-handlers", "data-gosx-pathology", "data-gosx-on"} {
		if ReservedDispatchName(name) || !SafeAuthoredName(name) || SafeSpreadName(name) {
			t.Errorf("ordinary name refused: %q", name)
		}
	}
}

func TestReservedDispatchNameUsesASCIIOnly(t *testing.T) {
	for _, name := range []string{"data-goſx-handler", "data-gosx-on-İnput", "data-goſx-path"} {
		want := name == "data-gosx-on-İnput" // the event suffix does not affect prefix ownership
		if got := ReservedDispatchName(name); got != want {
			t.Errorf("ReservedDispatchName(%q)=%v want %v", name, got, want)
		}
	}
}

func TestSafeSpreadNameRejectsFrameworkNamespace(t *testing.T) {
	for _, name := range []string{"data-gosx-", "data-gosx-bind-source", "DATA-GOSX-BIND-ATTR", "data-gosx-action", "data-gosx-action-target", "data-gosx-on-click", "data-gosx-live-bind-attr"} {
		if SafeSpreadName(name) {
			t.Errorf("framework attribute accepted from spread: %s", name)
		}
	}
	for _, name := range []string{"data-app-id", "data-gosx", "aria-label", "href"} {
		if !SafeSpreadName(name) {
			t.Errorf("ordinary attribute rejected: %s", name)
		}
	}
}

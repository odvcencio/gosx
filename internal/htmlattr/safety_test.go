package htmlattr

import "testing"

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

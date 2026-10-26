package gosx

import (
	"strings"
	"testing"
)

func TestDispatchAttrIsExplicitAndCannotComeFromSpread(t *testing.T) {
	for _, event := range []string{"click", "input", "change", "document-keydown", "document-keyup", "window-resize"} {
		got := RenderHTML(El("button", Attrs(DispatchAttr(event, `save"&`)), Spread(map[string]any{"data-gosx-on-" + event: "evil"})))
		if !strings.Contains(got, `data-gosx-on-`+event+`="save&#34;&amp;"`) || strings.Contains(got, "evil") {
			t.Fatalf("%s: %s", event, got)
		}
	}
	for _, event := range []string{"", "click x", `click"`, "click=evil", "onClick", "-click", "click-", "document-evil"} {
		if got := RenderHTML(El("button", Attrs(DispatchAttr(event, "evil")))); strings.Contains(got, "evil") {
			t.Fatalf("invalid event %q: %s", event, got)
		}
	}
}

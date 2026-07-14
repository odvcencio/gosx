package gosx

import (
	"strings"
	"testing"
)

func TestRuntimeSurfaceAttrs(t *testing.T) {
	html := RenderHTML(RuntimeSurface("form", RuntimeSurfaceOptions{
		Name:     "editor",
		Version:  "1",
		Fallback: "native-form",
	}, Text("fallback")))
	for _, want := range []string{
		`data-gosx-runtime-surface="editor"`,
		`data-gosx-runtime-surface-version="1"`,
		`data-gosx-fallback="native-form"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("surface markup missing %q: %s", want, html)
		}
	}
	if !strings.Contains(html, "fallback") {
		t.Fatalf("surface constructor dropped fallback children: %s", html)
	}
}

func TestActionAttrsEncodeTypedTransport(t *testing.T) {
	html := RenderHTML(Action("button", ActionOptions{
		Method: "post",
		URL:    "/notes/save",
		Event:  "saved",
		Signal: "$note.status",
		Target: "#status",
		Reset:  true,
	}, Text("Save")))
	for _, want := range []string{
		`data-gosx-action="POST /notes/save"`,
		`data-gosx-action-event="saved"`,
		`data-gosx-action-signal="$note.status"`,
		`data-gosx-action-target="#status"`,
		`data-gosx-reset`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("action markup missing %q: %s", want, html)
		}
	}
	if !strings.Contains(html, "Save") {
		t.Fatalf("action constructor dropped children: %s", html)
	}
}

func TestRegionAttrsNormalizeEvents(t *testing.T) {
	html := RenderHTML(Region("section", RegionOptions{
		URL:        "/notes/{value}",
		Signal:     "$note.id",
		Events:     []string{"change", "", "refresh"},
		Field:      "html",
		AllowEmpty: true,
	}, Text("server fallback")))
	for _, want := range []string{
		`data-gosx-region`,
		`data-gosx-region-url="/notes/{value}"`,
		`data-gosx-region-signal="$note.id"`,
		`data-gosx-region-on="change refresh"`,
		`data-gosx-region-field="html"`,
		`data-gosx-region-allow-empty`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("region markup missing %q: %s", want, html)
		}
	}
	if !strings.Contains(html, "server fallback") {
		t.Fatalf("region constructor dropped fallback children: %s", html)
	}
}

func TestProgressiveEnhancementAttrsDescribeFallback(t *testing.T) {
	html := RenderHTML(El("article", ProgressiveEnhancementAttrs(ProgressiveEnhancementOptions{
		Kind:     "motion",
		Layer:    "bootstrap",
		Fallback: "html",
	}), Text("server content")))
	for _, want := range []string{
		`data-gosx-enhance="motion"`,
		`data-gosx-enhance-layer="bootstrap"`,
		`data-gosx-fallback="html"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("enhancement markup missing %q: %s", want, html)
		}
	}
}

func TestManagedFormAttrsNormalizeMethodAndPreserveNativeFallback(t *testing.T) {
	html := RenderHTML(El("form", ManagedFormAttrs(ManagedFormOptions{
		Method:   "POST",
		State:    "pending",
		Layer:    "bootstrap",
		Fallback: "native-form",
	}), Text("Save")))
	for _, want := range []string{
		`data-gosx-form`,
		`data-gosx-form-mode="post"`,
		`data-gosx-form-state="pending"`,
		`data-gosx-enhance="form"`,
		`data-gosx-enhance-layer="bootstrap"`,
		`data-gosx-fallback="native-form"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("managed form markup missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, `data-gosx-form-mode="POST"`) {
		t.Fatalf("managed form method was not normalized: %s", html)
	}
}

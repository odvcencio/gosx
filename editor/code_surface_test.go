package editor

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestCodeSurfaceUsesSourceEditingContract(t *testing.T) {
	ed := New("code", Options{
		Surface:        SurfaceCode,
		Title:          "internal/api/api.go",
		Language:       Go,
		Content:        "package api\n",
		FormAction:     "/edit",
		DiagnosticsURL: "/diagnostics",
	})
	html := gosx.RenderHTML(ed.Render())
	for _, want := range []string{
		`editor-surface-code`,
		`data-editor-surface="code"`,
		`data-editor-language="go"`,
		`data-diagnostics-url="/diagnostics"`,
		`internal/api/api.go`,
		`name="content"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("code surface missing %q in %s", want, html)
		}
	}
	if strings.Contains(html, "Untitled field note") || strings.Contains(html, "Metadata") {
		t.Fatalf("code surface leaked publishing chrome: %s", html)
	}
	if !strings.Contains(html, `class="editor-native-card editor-panel editor-panel-diagnostics"`) {
		t.Fatalf("diagnostics panel must participate in panel selection: %s", html)
	}
}

func TestCodeSurfaceRendersDeclarativeCollaborationBinding(t *testing.T) {
	component := New("code", Options{Surface: SurfaceCode, Content: "x", Collaboration: &Collaboration{HubURL: "/gosx/hub/cells", CapabilityURL: "/api/cells/cell-1/capability", CellID: "cell-1", Path: "main.go"}})
	html := gosx.RenderHTML(component.Render())
	for _, want := range []string{"data-collaboration-hub=\"/gosx/hub/cells\"", "data-collaboration-capability-url=\"/api/cells/cell-1/capability\"", "data-collaboration-cell=\"cell-1\"", "data-collaboration-path=\"main.go\"", "/editor/collaborative-editor.js"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %s", want, html)
		}
	}
}

func TestCollaborationRuntimeProtectsUnacknowledgedLocalInput(t *testing.T) {
	asset, err := embeddedAssets.ReadFile("assets/collaborative-editor.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(asset)
	for _, want := range []string{"localDirty = true", "if (localDirty) return", "localDirty = false", "grant.expiresAt", "capability rotation"} {
		if !strings.Contains(source, want) {
			t.Fatalf("collaboration runtime missing %q", want)
		}
	}
}

func TestNativeEditorAssetProvidesMultiCursorEditing(t *testing.T) {
	asset, err := embeddedAssets.ReadFile("assets/native-editor.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(asset)
	for _, want := range []string{
		`dataset.multiCursorCount`,
		`beforeinput`,
		`deleteContentBackward`,
		`event.key === "ArrowDown"`,
		`event.key === "Escape"`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("native editor asset missing %q", want)
		}
	}
}

func TestCodeIntelligenceAssetProvidesStructuralNavigation(t *testing.T) {
	asset, err := embeddedAssets.ReadFile("assets/code-intelligence.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(asset)
	for _, want := range []string{
		`event.key === "F12"`,
		`event.altKey && event.shiftKey`,
		`definitionAtCursor`,
		`enclosingTag`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("code intelligence asset missing %q", want)
		}
	}
}

func TestCodeSurfaceRendersDeclarativeIntelligenceBinding(t *testing.T) {
	component := New("code", Options{
		Surface:  SurfaceCode,
		Language: Go,
		Content:  "package main\n",
		CodeIntelligence: &CodeIntelligence{
			Language:          "go",
			WasmExecURL:       "/intelligence/wasm_exec.js",
			RuntimeURL:        "/intelligence/gotreesitter.wasm",
			GrammarURL:        "/intelligence/go.bin",
			HighlightQueryURL: "/intelligence/go-highlights.scm",
			TagsQueryURL:      "/intelligence/go-tags.scm",
		},
	})
	html := gosx.RenderHTML(component.Render())
	for _, want := range []string{
		`data-code-intelligence-language="go"`,
		`data-code-intelligence-runtime="/intelligence/gotreesitter.wasm"`,
		`data-code-intelligence-grammar="/intelligence/go.bin"`,
		`data-code-intelligence-highlights="/intelligence/go-highlights.scm"`,
		`data-code-intelligence-tags="/intelligence/go-tags.scm"`,
		`/editor/code-intelligence.js`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %s", want, html)
		}
	}
}

func TestCodeSurfaceDefaults(t *testing.T) {
	ed := New("code", Options{Surface: SurfaceCode})
	if ed.Language != PlainText {
		t.Fatalf("language = %q, want %q", ed.Language, PlainText)
	}
	if len(ed.Options.Panels) != len(DefaultCodePanels) {
		t.Fatalf("panels = %#v, want %#v", ed.Options.Panels, DefaultCodePanels)
	}
	if len(ed.Options.Toolbar.Items) != 0 {
		t.Fatalf("code surface must not inherit Markdown toolbar: %#v", ed.Options.Toolbar.Items)
	}
}

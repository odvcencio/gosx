package editor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestNew_Defaults(t *testing.T) {
	ed := New("test-editor", Options{})
	if ed.Name != "test-editor" {
		t.Fatalf("name = %q, want %q", ed.Name, "test-editor")
	}
	if ed.Language != MarkdownPP {
		t.Fatalf("language = %q, want %q", ed.Language, MarkdownPP)
	}
	if ed.Theme != ThemeDark {
		t.Fatalf("theme = %q, want %q", ed.Theme, ThemeDark)
	}
	if ed.Document() == nil {
		t.Fatal("document should be initialized")
	}
}

func TestNew_WithContent(t *testing.T) {
	ed := New("test", Options{Content: "# Hello"})
	if ed.doc.Content() != "# Hello" {
		t.Fatalf("content = %q", ed.doc.Content())
	}
}

func TestNew_ProfileOptionsPopulateLegacyCompatibilityFields(t *testing.T) {
	ed := New("profiled", Options{
		Editor: EditorOptions{
			Content: "# Profiled",
			Label:   "Document",
			Prose:   ProseStyle{Size: "1.1rem"},
		},
		Metadata: MetadataOptions{
			Title:  "Profiled title",
			Status: StatusPublished,
		},
		Runtime: RuntimeOptions{
			PreviewURL:     "/preview",
			DiagnosticsURL: "/diagnostics",
		},
	})
	if ed.Options.Content != "# Profiled" || ed.Options.Editor.Content != "# Profiled" {
		t.Fatalf("profile content did not normalize: %#v", ed.Options)
	}
	if ed.Options.Title != "Profiled title" || ed.Options.Metadata.Title != "Profiled title" {
		t.Fatalf("metadata did not normalize: %#v", ed.Options)
	}
	if ed.Options.Prose.Size != "1.1rem" || ed.Options.Editor.Prose.Size != "1.1rem" {
		t.Fatalf("prose settings did not normalize: %#v", ed.Options)
	}
	if ed.Options.PreviewURL != "/preview" || ed.Options.Runtime.PreviewURL != "/preview" {
		t.Fatalf("runtime did not normalize: %#v", ed.Options)
	}
	if ed.Options.DiagnosticsURL != "/diagnostics" || ed.Options.Runtime.DiagnosticsURL != "/diagnostics" {
		t.Fatalf("diagnostics runtime did not normalize: %#v", ed.Options)
	}
	if ed.Options.Surface != SurfacePublishing {
		t.Fatalf("profile with metadata surface = %q, want %q", ed.Options.Surface, SurfacePublishing)
	}
}

func TestNew_EditorProfileDefaultsToDocumentSurface(t *testing.T) {
	ed := New("document", Options{Editor: EditorOptions{Content: "# Document"}})
	if ed.Options.Surface != SurfaceDocument {
		t.Fatalf("editor-only surface = %q, want %q", ed.Options.Surface, SurfaceDocument)
	}
	html := gosx.RenderHTML(ed.Render())
	if strings.Contains(html, `id="editor-panel-metadata"`) {
		t.Fatal("document surface should not render publishing metadata")
	}
}

func TestNew_LegacyOptionsKeepPublishingSurface(t *testing.T) {
	ed := New("legacy", Options{Content: "# Legacy"})
	if ed.Options.Surface != SurfacePublishing {
		t.Fatalf("legacy surface = %q, want %q", ed.Options.Surface, SurfacePublishing)
	}
	html := gosx.RenderHTML(ed.Render())
	if !strings.Contains(html, `id="editor-panel-metadata"`) {
		t.Fatal("legacy surface should retain publishing metadata")
	}
}

func TestNew_DefaultsCloneMutableOptions(t *testing.T) {
	ed := New("test", Options{})

	ed.Options.Keymap["Mod-B"] = CmdItalic
	ed.Options.Toolbar.Items[0].Label = "Changed"
	ed.Options.Panels[0] = PanelHistory

	if DefaultKeymap["Mod-B"] != CmdBold {
		t.Fatal("default keymap should not be mutated by editor options")
	}
	if DefaultToolbar.Items[0].Label != "Bold" {
		t.Fatal("default toolbar should not be mutated by editor options")
	}
	if DefaultPanels[0] != PanelPreview {
		t.Fatal("default panels should not be mutated by editor options")
	}
}

func TestRender_ProducesShell(t *testing.T) {
	ed := New("post-editor", Options{
		Content:        "# Hello",
		Label:          "Post Editor",
		Theme:          ThemeLight,
		DiagnosticsURL: "/diagnostics",
		ReadOnly:       true,
	})

	html := gosx.RenderHTML(ed.Render())

	for _, want := range []string{
		`class="editor-page editor-page-native"`,
		`href="/editor/prose.css"`,
		`src="/editor/prose-runtime.js"`,
		`id="post-editor"`,
		`href="/editor/editor.css"`,
		`src="/editor/mdpp-diagrams.js"`,
		`src="/editor/native-editor.js"`,
		`id="editor-native-form"`,
		`data-editor-native="true"`,
		`data-editor-surface="publishing"`,
		`data-diagnostics-url="/diagnostics"`,
		`data-editor-keymap=`,
		`data-gosx-runtime-surface="editor"`,
		`data-gosx-editor="true"`,
		`data-gosx-script="managed"`,
		`data-gosx-enhance="form"`,
		`id="editor-panel-preview"`,
		`class="editor-topbar editor-native-topbar"`,
		`id="editor-title"`,
		`name="title"`,
		`id="editor-toolbar"`,
		`role="toolbar"`,
		`data-command="emoji"`,
		`data-command="scene3d"`,
		`data-command="island"`,
		`data-command="diagram"`,
		`title="Emoji"`,
		`title="Scene3D"`,
		`aria-label="Post Editor"`,
		`id="editor-content"`,
		`id="editor-preview-content"`,
		`id="editor-gallery-grid"`,
		`id="editor-outline-headings"`,
		`id="editor-diagnostics-list"`,
		`id="editor-scratch"`,
		`Read only`,
		`id="editor-word-count">2</strong>`,
		`id="editor-reading-time">1</strong>`,
		`readonly`,
		`# Hello`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered html missing %q: %s", want, html)
		}
	}
}

func TestRender_ComputesContentStatsAndStatuses(t *testing.T) {
	ed := New("sync-editor", Options{
		Content: "alpha beta `code` gamma\n\n```go\nfmt.Println(\"ignored\")\n```\ndelta",
		OnSave:  func(doc Document) error { return nil },
		Hub:     nil,
	})

	html := gosx.RenderHTML(ed.Render())

	for _, want := range []string{
		`id="editor-save-status" class="editor-save-status editor-save-status-saved" aria-live="polite">Saved</span>`,
		`id="editor-word-count">4</strong>`,
		`id="editor-reading-time">1</strong>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered html missing %q: %s", want, html)
		}
	}
}

func TestRender_ProseContractAndExtensions(t *testing.T) {
	ed := New("prose-editor", Options{
		Content: "# Hello",
		Prose: ProseStyle{
			Size:    "clamp(1rem, 2vw, 1.25rem)",
			Leading: "1.7",
			Flow:    "1.1rem",
		},
		Extensions: []Extension{{
			ID:            "citations",
			StylesheetURL: "/extensions/citations.css",
			ScriptURL:     "/extensions/citations.js",
			Toolbar: Toolbar{Items: []ToolbarItem{{
				Command: Command("citation"),
				Label:   "Citation",
			}}},
		}},
	})

	html := gosx.RenderHTML(ed.Render())
	for _, want := range []string{
		`class="editor-preview-content gosx-prose"`,
		`data-gosx-prose-streaming="stable"`,
		`data-gosx-runtime-surface="mdpp-diagrams"`,
		`data-gosx-fallback="html"`,
		`--gosx-prose-size:clamp(1rem, 2vw, 1.25rem)`,
		`data-editor-extensions="citations"`,
		`href="/extensions/citations.css"`,
		`src="/extensions/citations.js"`,
		`data-command="citation"`,
		`data-gosx-extension="citations"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered html missing %q: %s", want, html)
		}
	}
}

func TestAssetHandlerServesNativeAssets(t *testing.T) {
	handler := AssetHandler()
	for _, path := range []string{"/editor.css", "/prose.css", "/prose-runtime.js", "/mdpp-diagrams.js", "/native-editor.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", path, w.Code, w.Body.String())
		}
		if strings.TrimSpace(w.Body.String()) == "" {
			t.Fatalf("%s returned an empty body", path)
		}
	}
}

func TestAssetHandlerServesProseRuntimeContract(t *testing.T) {
	handler := AssetHandler()
	req := httptest.NewRequest(http.MethodGet, "/prose-runtime.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("prose-runtime.js returned %d", w.Code)
	}
	for _, want := range []string{"GoSX standalone prose runtime", "reconcileHTML", "reconcileBlocks", "createBlockStream", "data-gosx-prose-key"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("prose-runtime.js missing %q", want)
		}
	}
}

func TestAssetHandlerServesDiagnosticsRuntimeContract(t *testing.T) {
	handler := AssetHandler()
	req := httptest.NewRequest(http.MethodGet, "/native-editor.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("native-editor.js returned %d", w.Code)
	}
	for _, want := range []string{"diagnosticsUrl", "requestDiagnostics", "editor-diagnostic", "reportOperationalFailure", "runtimeSurfaceAPI", "scheduler", "scheduleTask", "No Markdown++ diagnostics"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("native-editor.js missing %q", want)
		}
	}
}

func TestResolveTheme_BuiltinAndCustom(t *testing.T) {
	dark := ResolveTheme(ThemeDark)
	if dark.RootClass != "gosx-editor--theme-dark" {
		t.Fatalf("dark root class = %q", dark.RootClass)
	}

	custom := ResolveTheme(Theme("Solarized Night"))
	if custom.RootClass != "gosx-editor--theme-solarized-night" {
		t.Fatalf("custom root class = %q", custom.RootClass)
	}
	if custom.ColorScheme != "auto" {
		t.Fatalf("custom color scheme = %q", custom.ColorScheme)
	}
}

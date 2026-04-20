package editor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
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
		Content:  "# Hello",
		Label:    "Post Editor",
		Theme:    ThemeLight,
		ReadOnly: true,
	})

	html := gosx.RenderHTML(ed.Render())

	for _, want := range []string{
		`class="editor-page editor-page-native"`,
		`id="post-editor"`,
		`href="/editor/editor.css"`,
		`src="/editor/mdpp-diagrams.js"`,
		`src="/editor/native-editor.js"`,
		`id="editor-native-form"`,
		`data-editor-native="true"`,
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

func TestAssetHandlerServesNativeAssets(t *testing.T) {
	handler := AssetHandler()
	for _, path := range []string{"/editor.css", "/mdpp-diagrams.js", "/native-editor.js"} {
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

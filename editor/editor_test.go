package editor

import (
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
		`class="editor-page"`,
		`id="editor-form"`,
		`id="form-slug"`,
		`id="form-tags"`,
		`id="form-cover-image"`,
		`id="form-publish-at"`,
		`id="post-editor"`,
		`class="editor-app-shell gosx-editor gosx-editor--lang-markdown++ gosx-editor--theme-light gosx-editor--readonly"`,
		`data-editor-language="markdown++"`,
		`data-editor-theme="light"`,
		`data-color-scheme="light"`,
		`id="editor-title"`,
		`id="editor-toolbar"`,
		`role="toolbar"`,
		`aria-label="Post Editor"`,
		`data-panel="preview"`,
		`id="editor-metadata-panel"`,
		`id="editor-gallery-panel"`,
		`id="editor-history-panel"`,
		`role="complementary"`,
		`Read only`,
		`id="meta-word-count">2</span>`,
		`id="meta-reading-time">1</span>`,
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
		`id="save-status" class="save-status saved">Saved</span>`,
		`id="hub-status-dot" class="status-dot disconnected"`,
		`id="meta-word-count">4</span>`,
		`id="meta-reading-time">1</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered html missing %q: %s", want, html)
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

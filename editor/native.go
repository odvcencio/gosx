package editor

import (
	"fmt"
	"strings"

	"github.com/odvcencio/gosx"
)

func (e *Editor) renderNativeForm() gosx.Node {
	children := []gosx.Node{
		gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", "csrf_token"),
			gosx.Attr("value", e.Options.CSRFToken),
		)),
	}
	children = append(children, e.renderNativePanelRadios()...)
	for _, key := range sortedKeys(e.Options.ExtraFields) {
		children = append(children, gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", key),
			gosx.Attr("value", e.Options.ExtraFields[key]),
		)))
	}
	children = append(children, e.renderNativeTopbar(), e.renderNativeBody())

	attrs := gosx.Attrs(
		gosx.Attr("id", "editor-native-form"),
		gosx.Attr("class", "editor-native-form"),
		gosx.Attr("method", "POST"),
		gosx.Attr("action", e.Options.FormAction),
		gosx.Attr("data-editor-native", "true"),
		gosx.Attr("data-gosx-form", "true"),
		gosx.Attr("data-gosx-form-state", "idle"),
		gosx.Attr("data-gosx-enhance", "form"),
		gosx.Attr("data-gosx-enhance-layer", "bootstrap"),
		gosx.Attr("data-gosx-fallback", "native-form"),
		gosx.Attr("data-gosx-loading", e.Options.LoadingText),
	)
	attrs = appendStringAttr(attrs, "data-autosave-url", e.Options.AutoSaveURL)
	attrs = appendStringAttr(attrs, "data-preview-url", e.Options.PreviewURL)
	attrs = appendStringAttr(attrs, "data-upload-url", e.Options.UploadURL)
	attrs = appendStringAttr(attrs, "data-images-url", e.Options.ImagesURL)
	if e.Options.ReadOnly {
		attrs = append(attrs, gosx.BoolAttr("data-readonly"))
	}

	return gosx.El("form", attrs, gosx.Fragment(children...))
}

func (e *Editor) renderNativePanelRadios() []gosx.Node {
	nodes := []gosx.Node{
		gosx.El("input", gosx.Attrs(
			gosx.Attr("id", "editor-panel-none"),
			gosx.Attr("class", "editor-panel-radio"),
			gosx.Attr("type", "radio"),
			gosx.Attr("name", "editor_panel"),
			gosx.Attr("value", ""),
		)),
	}
	active := PanelPreview
	if len(e.Options.Panels) > 0 {
		active = e.Options.Panels[0]
	}
	for _, panel := range e.Options.Panels {
		attrs := gosx.Attrs(
			gosx.Attr("id", nativePanelRadioID(panel)),
			gosx.Attr("class", "editor-panel-radio"),
			gosx.Attr("type", "radio"),
			gosx.Attr("name", "editor_panel"),
			gosx.Attr("value", string(panel)),
		)
		if panel == active {
			attrs = append(attrs, gosx.BoolAttr("checked"))
		}
		nodes = append(nodes, gosx.El("input", attrs))
	}
	return nodes
}

func (e *Editor) renderNativeTopbar() gosx.Node {
	return gosx.El(
		"header",
		gosx.Attrs(gosx.Attr("class", "editor-topbar editor-native-topbar")),
		gosx.El(
			"a",
			gosx.Attrs(
				gosx.Attr("class", "editor-topbar-back"),
				gosx.Attr("href", e.Options.BackHref),
			),
			gosx.Text("Back"),
		),
		gosx.El(
			"label",
			gosx.Attrs(gosx.Attr("class", "editor-title-field")),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "editor-field-label")), gosx.Text("Title")),
			gosx.El("input", e.nativeInputAttrs("editor-title", "title", "text", "Untitled field note", e.titleValue(), true)),
		),
		gosx.El(
			"nav",
			gosx.Attrs(
				gosx.Attr("class", "editor-segments"),
				gosx.Attr("aria-label", "Editor panels"),
			),
			gosx.Fragment(e.renderNativePanelSegments()...),
		),
		gosx.El(
			"div",
			gosx.Attrs(gosx.Attr("class", "editor-actions editor-native-actions")),
			gosx.El(
				"span",
				gosx.Attrs(
					gosx.Attr("id", "editor-save-status"),
					gosx.Attr("class", "editor-save-status editor-save-status-saved"),
					gosx.Attr("aria-live", "polite"),
				),
				gosx.Text(e.saveStatusText()),
			),
			gosx.Fragment(e.renderNativeFormButtons(e.Options.Buttons)...),
		),
	)
}

func (e *Editor) renderNativePanelSegments() []gosx.Node {
	nodes := make([]gosx.Node, 0, len(e.Options.Panels))
	for _, panel := range e.Options.Panels {
		nodes = append(nodes, gosx.El(
			"label",
			gosx.Attrs(
				gosx.Attr("class", "editor-segment editor-segment-"+string(panel)),
				gosx.Attr("for", nativePanelRadioID(panel)),
			),
			gosx.Text(panelTitle(panel)),
		))
	}
	return nodes
}

func (e *Editor) renderNativeBody() gosx.Node {
	return gosx.El(
		"section",
		gosx.Attrs(
			gosx.Attr("class", "editor-native-body"),
			gosx.Attr("aria-label", e.ariaLabel()),
		),
		gosx.El(
			"div",
			gosx.Attrs(gosx.Attr("class", "editor-native-main")),
			e.renderNativeToolbar(),
			gosx.El(
				"div",
				gosx.Attrs(gosx.Attr("class", "editor-source-shell")),
				gosx.El("div", gosx.Attrs(
					gosx.Attr("class", "editor-line-numbers"),
					gosx.Attr("id", "editor-line-numbers"),
					gosx.Attr("aria-hidden", "true"),
				)),
				gosx.El(
					"pre",
					gosx.Attrs(
						gosx.Attr("class", "editor-highlight-layer"),
						gosx.Attr("aria-hidden", "true"),
					),
					gosx.El("code", gosx.Attrs(gosx.Attr("id", "editor-highlight-content"))),
				),
				gosx.El(
					"textarea",
					e.nativeTextareaAttrs("editor-content", "content", "editor-source", e.Options.Placeholder, 0),
					gosx.Text(e.doc.Content()),
				),
			),
		),
		gosx.El(
			"aside",
			gosx.Attrs(
				gosx.Attr("class", "editor-native-sidebar"),
				gosx.Attr("aria-label", "Post settings"),
			),
			gosx.Fragment(e.renderNativePanels()...),
		),
	)
}

func (e *Editor) renderNativeToolbar() gosx.Node {
	groups := [][]Command{
		{CmdBold, CmdItalic, CmdStrike, CmdCode, CmdLink, CmdImage, CmdEmoji},
		{CmdH1, CmdH2, CmdH3},
		{CmdList, CmdOrderedList, CmdTaskList, CmdBlockquote},
		{CmdNote, CmdWarning, CmdMath, CmdFootnote, CmdHR},
		{CmdScene3D, CmdIsland, CmdDiagram},
	}
	children := make([]gosx.Node, 0, len(groups)*2)
	for _, group := range groups {
		items := e.toolbarItems(group)
		if len(items) == 0 {
			continue
		}
		if len(children) > 0 {
			children = append(children, gosx.El("div", gosx.Attrs(gosx.Attr("class", "toolbar-sep"))))
		}
		children = append(children, gosx.El(
			"div",
			gosx.Attrs(gosx.Attr("class", nativeToolbarGroupClass(group))),
			gosx.Fragment(e.renderToolbarButtons(items)...),
		))
	}
	return gosx.El(
		"div",
		gosx.Attrs(
			gosx.Attr("class", "editor-toolbar"),
			gosx.Attr("id", "editor-toolbar"),
			gosx.Attr("role", "toolbar"),
			gosx.Attr("aria-label", "Formatting"),
		),
		gosx.Fragment(children...),
	)
}

func (e *Editor) renderNativePanels() []gosx.Node {
	nodes := make([]gosx.Node, 0, len(e.Options.Panels)+1)
	for _, panel := range e.Options.Panels {
		switch panel {
		case PanelPreview:
			nodes = append(nodes, e.renderNativePreviewPanel())
		case PanelMetadata:
			nodes = append(nodes, e.renderNativeMetadataPanel())
		case PanelImages:
			nodes = append(nodes, e.renderNativeImagesPanel())
		case PanelOutline:
			nodes = append(nodes, e.renderNativeOutlinePanel())
		case PanelScratch:
			nodes = append(nodes, e.renderNativeScratchPanel())
		case PanelHistory:
			nodes = append(nodes, e.renderNativeHistoryPanel())
		}
	}
	return nodes
}

func (e *Editor) renderNativePreviewPanel() gosx.Node {
	content := []gosx.Node{}
	switch {
	case strings.TrimSpace(e.Options.InitialPreviewHTML) != "":
		content = append(content, gosx.RawHTML(e.Options.InitialPreviewHTML))
	case strings.TrimSpace(e.doc.Content()) == "":
		content = append(content, gosx.El("p", gosx.Attrs(gosx.Attr("class", "editor-preview-empty")), gosx.Text(fallback(e.Options.InitialPreviewPlaceholder, "No content yet."))))
	default:
		content = append(content, gosx.El("p", gosx.Attrs(gosx.Attr("class", "editor-preview-empty")), gosx.Text(fallback(e.Options.InitialPreviewPlaceholder, "Preview updates after the next edit."))))
	}
	return gosx.El(
		"section",
		gosx.Attrs(gosx.Attr("class", "editor-native-card editor-panel editor-panel-preview")),
		gosx.El(
			"div",
			gosx.Attrs(gosx.Attr("class", "editor-native-heading-row")),
			gosx.El("h2", nil, gosx.Text("Preview")),
			gosx.El(
				"span",
				gosx.Attrs(gosx.Attr("class", "meta-status-badge meta-status-"+string(e.Options.Status))),
				gosx.Text(string(e.Options.Status)),
			),
		),
		gosx.El(
			"div",
			gosx.Attrs(
				gosx.Attr("class", "editor-preview-content"),
				gosx.Attr("id", "editor-preview-content"),
			),
			gosx.Fragment(content...),
		),
	)
}

func (e *Editor) renderNativeMetadataPanel() gosx.Node {
	return gosx.El(
		"section",
		gosx.Attrs(gosx.Attr("class", "editor-native-card editor-panel editor-panel-metadata")),
		gosx.El("h2", nil, gosx.Text("Metadata")),
		e.renderNativeStats(),
		e.renderNativeLabelInput("Slug", "slug", "text", e.Options.Slug, "optional-slug"),
		e.renderNativeLabelTextarea("Excerpt", "excerpt", e.Options.Excerpt, "Brief description...", 4),
		e.renderNativeLabelInput("Tags", "tags", "text", e.Options.Tags, "tools, notes"),
		e.renderNativeLabelInput("Cover image", "cover_image", "url", e.Options.CoverImage, "https://..."),
		e.renderNativeLabelInput("Mood", "mood", "text", e.Options.Mood, "caffeinated"),
		e.renderNativeLabelInput("Music", "music", "url", e.Options.Music, "https://youtube.com/watch?v=..."),
		e.renderNativeLabelInput("Publish at", "publish_at", "datetime-local", e.Options.PublishAt, ""),
		gosx.El(
			"div",
			gosx.Attrs(gosx.Attr("class", "editor-native-button-row")),
			gosx.Fragment(e.renderNativeFormButtons(e.Options.ScheduleButtons)...),
		),
	)
}

func (e *Editor) renderNativeStats() gosx.Node {
	words := e.wordCount()
	minutes := e.readingTime()
	return gosx.El(
		"div",
		gosx.Attrs(
			gosx.Attr("class", "editor-meta-stats"),
			gosx.Attr("aria-label", "Document statistics"),
		),
		gosx.El(
			"span",
			gosx.Attrs(gosx.Attr("class", "editor-meta-stat")),
			gosx.El("strong", gosx.Attrs(gosx.Attr("id", "editor-word-count")), gosx.Text(fmt.Sprintf("%d", words))),
			gosx.El("span", gosx.Attrs(gosx.Attr("id", "editor-word-label")), gosx.Text(wordLabel(words))),
		),
		gosx.El(
			"span",
			gosx.Attrs(gosx.Attr("class", "editor-meta-stat")),
			gosx.El("strong", gosx.Attrs(gosx.Attr("id", "editor-reading-time")), gosx.Text(fmt.Sprintf("%d", minutes))),
			gosx.El("span", gosx.Attrs(gosx.Attr("id", "editor-reading-label")), gosx.Text("min read")),
		),
	)
}

func (e *Editor) renderNativeImagesPanel() gosx.Node {
	uploadAttrs := gosx.Attrs(
		gosx.Attr("type", "button"),
		gosx.Attr("class", "editor-button editor-button-secondary editor-upload-button"),
		gosx.Attr("data-command", string(CmdImage)),
	)
	if e.Options.ReadOnly {
		uploadAttrs = append(uploadAttrs, gosx.BoolAttr("disabled"))
	}
	return gosx.El(
		"section",
		gosx.Attrs(gosx.Attr("class", "editor-native-card editor-panel editor-panel-images")),
		gosx.El(
			"div",
			gosx.Attrs(gosx.Attr("class", "editor-native-heading-row")),
			gosx.El("h2", nil, gosx.Text("Images")),
			gosx.El(
				"button",
				uploadAttrs,
				gosx.Text("Upload"),
			),
		),
		gosx.El(
			"div",
			gosx.Attrs(
				gosx.Attr("id", "editor-gallery-grid"),
				gosx.Attr("class", "editor-gallery-grid"),
			),
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "editor-preview-empty")), gosx.Text("Open this panel to load uploaded images.")),
		),
	)
}

func (e *Editor) renderNativeOutlinePanel() gosx.Node {
	return gosx.El(
		"section",
		gosx.Attrs(gosx.Attr("class", "editor-native-card editor-panel editor-panel-outline")),
		gosx.El("h2", nil, gosx.Text("Outline")),
		gosx.El(
			"nav",
			gosx.Attrs(
				gosx.Attr("id", "editor-outline-headings"),
				gosx.Attr("class", "editor-outline-headings"),
				gosx.Attr("aria-label", "Document outline"),
			),
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "editor-preview-empty")), gosx.Text("Start writing to see your outline.")),
		),
	)
}

func (e *Editor) renderNativeScratchPanel() gosx.Node {
	return gosx.El(
		"section",
		gosx.Attrs(gosx.Attr("class", "editor-native-card editor-panel editor-panel-scratch")),
		gosx.El("h2", nil, gosx.Text("Scratch")),
		e.renderNativeLabelTextarea("Private notes", "scratch", e.Options.Scratch, "Notes, links, ideas...", 12),
	)
}

func (e *Editor) renderNativeHistoryPanel() gosx.Node {
	return gosx.El(
		"section",
		gosx.Attrs(gosx.Attr("class", "editor-native-card editor-panel editor-panel-history")),
		gosx.El("h2", nil, gosx.Text("History")),
		gosx.El("p", gosx.Attrs(gosx.Attr("class", "editor-preview-empty")), gosx.Text("History starts after the first save.")),
	)
}

func (e *Editor) renderNativeLabelInput(label, name, typ, value, placeholder string) gosx.Node {
	return gosx.El(
		"label",
		nil,
		gosx.El("span", nil, gosx.Text(label)),
		gosx.El("input", e.nativeInputAttrs("editor-"+name, name, typ, placeholder, value, false)),
	)
}

func (e *Editor) renderNativeLabelTextarea(label, name, value, placeholder string, rows int) gosx.Node {
	return gosx.El(
		"label",
		nil,
		gosx.El("span", nil, gosx.Text(label)),
		gosx.El("textarea", e.nativeTextareaAttrs("editor-"+name, name, "", placeholder, rows), gosx.Text(value)),
	)
}

func (e *Editor) nativeInputAttrs(id, name, typ, placeholder, value string, required bool) gosx.AttrList {
	attrs := gosx.Attrs(
		gosx.Attr("id", id),
		gosx.Attr("name", name),
		gosx.Attr("type", typ),
		gosx.Attr("value", value),
	)
	attrs = appendStringAttr(attrs, "placeholder", placeholder)
	if required {
		attrs = append(attrs, gosx.BoolAttr("required"))
	}
	if e.Options.ReadOnly {
		attrs = append(attrs, gosx.BoolAttr("readonly"))
	}
	return attrs
}

func (e *Editor) nativeTextareaAttrs(id, name, className, placeholder string, rows int) gosx.AttrList {
	attrs := gosx.Attrs(
		gosx.Attr("id", id),
		gosx.Attr("name", name),
		gosx.Attr("wrap", "soft"),
		gosx.Attr("spellcheck", "false"),
		gosx.Attr("autocomplete", "off"),
		gosx.Attr("autocorrect", "off"),
		gosx.Attr("autocapitalize", "off"),
	)
	attrs = appendStringAttr(attrs, "class", className)
	attrs = appendStringAttr(attrs, "placeholder", placeholder)
	if rows > 0 {
		attrs = append(attrs, gosx.Attr("rows", fmt.Sprintf("%d", rows)))
	}
	if e.Options.ReadOnly {
		attrs = append(attrs, gosx.BoolAttr("readonly"))
	}
	return attrs
}

func (e *Editor) renderNativeFormButtons(buttons []FormButton) []gosx.Node {
	nodes := make([]gosx.Node, 0, len(buttons))
	for _, button := range buttons {
		if strings.TrimSpace(button.Label) == "" {
			continue
		}
		className := fallback(button.Class, "editor-button editor-button-secondary")
		attrs := gosx.Attrs(
			gosx.Attr("type", "submit"),
			gosx.Attr("class", className),
		)
		attrs = appendStringAttr(attrs, "name", button.Name)
		attrs = appendStringAttr(attrs, "value", button.Value)
		if e.Options.ReadOnly {
			attrs = append(attrs, gosx.BoolAttr("disabled"))
		}
		nodes = append(nodes, gosx.El("button", attrs, gosx.Text(button.Label)))
	}
	return nodes
}

func appendStringAttr(attrs gosx.AttrList, name, value string) gosx.AttrList {
	if strings.TrimSpace(value) == "" {
		return attrs
	}
	return append(attrs, gosx.Attr(name, value))
}

func nativePanelRadioID(panel Panel) string {
	return "editor-panel-" + string(panel)
}

func nativeToolbarGroupClass(commands []Command) string {
	for _, command := range commands {
		switch command {
		case CmdNote, CmdWarning, CmdMath, CmdFootnote, CmdHR, CmdScene3D, CmdIsland, CmdDiagram:
			return "toolbar-group toolbar-mdpp"
		}
	}
	return "toolbar-group"
}

func wordLabel(words int) string {
	if words == 1 {
		return "word"
	}
	return "words"
}

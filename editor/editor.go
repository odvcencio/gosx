package editor

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/editor/textmodel"
)

// Editor is the component descriptor returned by New.
type Editor struct {
	Name     string
	Language Lang
	Theme    Theme
	Options  Options
	doc      Document
}

// New creates an editor component with the given options.
func New(name string, opts Options) *Editor {
	opts.defaults()

	var doc Document = textmodel.NewDocument(opts.Content)

	return &Editor{
		Name:     name,
		Language: opts.Language,
		Theme:    opts.Theme,
		Options:  opts,
		doc:      doc,
	}
}

// Document returns the editor's active document implementation.
func (e *Editor) Document() Document {
	if e == nil {
		return nil
	}
	return e.doc
}

// Render returns the server-rendered editor shell.
func (e *Editor) Render() gosx.Node {
	if e == nil {
		return gosx.Fragment()
	}

	return gosx.Fragment(
		gosx.El(
			"div",
			gosx.Attrs(
				gosx.Attr("class", "editor-page"),
			),
			e.renderLoader(),
			e.renderForm(),
			e.renderAppShell(),
		),
		gosx.Fragment(e.renderAssetTags()...),
	)
}

func (e *Editor) renderLoader() gosx.Node {
	return gosx.El(
		"div",
		gosx.Attrs(
			gosx.Attr("class", "galaxy-loader"),
			gosx.Attr("id", "galaxy-loader"),
		),
		gosx.El(
			"div",
			gosx.Attrs(
				gosx.Attr("class", "galaxy-spinner"),
			),
		),
	)
}

func (e *Editor) renderForm() gosx.Node {
	children := []gosx.Node{
		gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", "csrf_token"),
			gosx.Attr("value", e.Options.CSRFToken),
		)),
		gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", "title"),
			gosx.Attr("id", "form-title"),
			gosx.Attr("value", e.titleValue()),
		)),
		gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", "slug"),
			gosx.Attr("id", "form-slug"),
			gosx.Attr("value", e.Options.Slug),
		)),
		gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", "excerpt"),
			gosx.Attr("id", "form-excerpt"),
			gosx.Attr("value", e.Options.Excerpt),
		)),
		gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", "tags"),
			gosx.Attr("id", "form-tags"),
			gosx.Attr("value", e.Options.Tags),
		)),
		gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", "cover_image"),
			gosx.Attr("id", "form-cover-image"),
			gosx.Attr("value", e.Options.CoverImage),
		)),
		gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", "publish_at"),
			gosx.Attr("id", "form-publish-at"),
			gosx.Attr("value", e.Options.PublishAt),
		)),
	}

	for _, key := range sortedKeys(e.Options.ExtraFields) {
		children = append(children, gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", key),
			gosx.Attr("value", e.Options.ExtraFields[key]),
		)))
	}

	textareaAttrs := gosx.Attrs(
		gosx.Attr("name", "content"),
		gosx.Attr("id", "editor-content"),
		gosx.Attr("placeholder", e.Options.Placeholder),
		gosx.Attr("aria-label", e.ariaLabel()),
	)
	if e.Options.ReadOnly {
		textareaAttrs = append(textareaAttrs, gosx.BoolAttr("readonly"))
	}
	children = append(children, gosx.El("textarea", textareaAttrs, gosx.Text(e.doc.Content())))

	return gosx.El(
		"form",
		gosx.Attrs(
			gosx.Attr("method", "POST"),
			gosx.Attr("action", e.Options.FormAction),
			gosx.Attr("id", "editor-form"),
			gosx.Attr("class", "editor-form"),
		),
		gosx.Fragment(children...),
	)
}

func (e *Editor) renderAppShell() gosx.Node {
	theme := ResolveTheme(e.Theme)
	classNames := []string{
		"editor-app-shell",
		"gosx-editor",
		"gosx-editor--lang-" + string(e.Language),
		theme.RootClass,
	}
	if e.Options.ReadOnly {
		classNames = append(classNames, "gosx-editor--readonly")
	}
	return gosx.El(
		"div",
		gosx.Attrs(
			gosx.Attr("id", e.Name),
			gosx.Attr("class", strings.Join(classNames, " ")),
			gosx.Attr("data-title", e.titleValue()),
			gosx.Attr("data-slug", e.Options.Slug),
			gosx.Attr("data-excerpt", e.Options.Excerpt),
			gosx.Attr("data-tags", e.Options.Tags),
			gosx.Attr("data-cover-image", e.Options.CoverImage),
			gosx.Attr("data-publish-at", e.Options.PublishAt),
			gosx.Attr("data-status", string(e.Options.Status)),
			gosx.Attr("data-csrf", e.Options.CSRFToken),
			gosx.Attr("data-upload-url", e.Options.UploadURL),
			gosx.Attr("data-post-id", e.Options.PostID),
			gosx.Attr("data-editor-language", string(e.Language)),
			gosx.Attr("data-editor-theme", string(e.Theme)),
			gosx.Attr("data-color-scheme", theme.ColorScheme),
			gosx.Attr("data-editor-label", e.ariaLabel()),
		),
		e.renderFullscreen(),
	)
}

func (e *Editor) renderFullscreen() gosx.Node {
	children := []gosx.Node{
		e.renderTopbar(),
		e.renderToolbar(),
		gosx.El(
			"div",
			gosx.Attrs(
				gosx.Attr("class", "editor-body"),
			),
			gosx.El(
				"div",
				gosx.Attrs(
					gosx.Attr("class", "editor-writing"),
					gosx.Attr("id", "editor-cm"),
					gosx.Attr("role", "textbox"),
					gosx.Attr("aria-multiline", "true"),
					gosx.Attr("aria-label", e.ariaLabel()),
				),
			),
			e.renderPreviewPanel(),
		),
	}

	if e.hasPanel(PanelMetadata) {
		children = append(children, e.renderMetadataPanel())
	}
	if e.hasPanel(PanelImages) {
		children = append(children, e.renderGalleryPanel())
	}
	if e.hasPanel(PanelHistory) {
		children = append(children, e.renderHistoryPanel())
	}

	return gosx.El(
		"div",
		gosx.Attrs(
			gosx.Attr("class", "editor-fullscreen"),
		),
		gosx.Fragment(children...),
	)
}

func (e *Editor) renderTopbar() gosx.Node {
	titleAttrs := gosx.Attrs(
		gosx.Attr("type", "text"),
		gosx.Attr("id", "editor-title"),
		gosx.Attr("class", "editor-title-inline"),
		gosx.Attr("value", e.titleValue()),
		gosx.Attr("placeholder", "Untitled"),
		gosx.Attr("aria-label", "Post title"),
	)
	if e.Options.ReadOnly {
		titleAttrs = append(titleAttrs, gosx.BoolAttr("readonly"))
	}

	return gosx.El(
		"div",
		gosx.Attrs(
			gosx.Attr("class", "editor-topbar"),
		),
		gosx.El(
			"a",
			gosx.Attrs(
				gosx.Attr("href", e.Options.BackHref),
				gosx.Attr("class", "editor-back"),
				gosx.Attr("title", "Back to admin"),
			),
			gosx.Text("← Back"),
		),
		gosx.El("input", titleAttrs),
		gosx.El(
			"div",
			gosx.Attrs(
				gosx.Attr("class", "editor-topbar-right"),
			),
			gosx.El(
				"span",
				gosx.Attrs(
					gosx.Attr("id", "hub-status-dot"),
					gosx.Attr("class", "status-dot disconnected"),
				),
			),
			gosx.El(
				"span",
				gosx.Attrs(
					gosx.Attr("id", "hub-status-label"),
					gosx.Attr("class", "status-label"),
				),
			),
			gosx.El(
				"span",
				gosx.Attrs(
					gosx.Attr("id", "save-status"),
					gosx.Attr("class", "save-status "+e.saveStatusClass()),
				),
				gosx.Text(e.saveStatusText()),
			),
			gosx.El(
				"div",
				gosx.Attrs(
					gosx.Attr("class", "editor-segments"),
				),
				gosx.Fragment(e.renderPanelButtons()...),
			),
		),
	)
}

func (e *Editor) renderToolbar() gosx.Node {
	children := make([]gosx.Node, 0, 8)
	for _, group := range [][]Command{
		{CmdBold, CmdItalic, CmdStrike, CmdCode, CmdLink, CmdImage},
		{CmdH1, CmdH2, CmdH3},
		{CmdList, CmdOrderedList, CmdTaskList, CmdBlockquote},
		{CmdNote, CmdWarning, CmdMath, CmdFootnote, CmdHR},
	} {
		groupNode := e.renderToolbarGroup(group)
		if groupNode.IsZero() {
			continue
		}
		if len(children) > 0 {
			children = append(children, gosx.El(
				"div",
				gosx.Attrs(
					gosx.Attr("class", "toolbar-sep"),
				),
			))
		}
		children = append(children, groupNode)
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

func (e *Editor) renderToolbarGroup(commands []Command) gosx.Node {
	items := e.toolbarItems(commands)
	if len(items) == 0 {
		return gosx.Node{}
	}

	return gosx.El(
		"div",
		gosx.Attrs(
			gosx.Attr("class", "toolbar-group"),
		),
		gosx.Fragment(e.renderToolbarButtons(items)...),
	)
}

func (e *Editor) renderToolbarButtons(items []ToolbarItem) []gosx.Node {
	nodes := make([]gosx.Node, 0, len(items))
	for _, item := range items {
		attrs := gosx.Attrs(
			gosx.Attr("type", "button"),
			gosx.Attr("data-command", string(item.Command)),
			gosx.Attr("title", item.Label),
			gosx.Attr("aria-label", item.Label),
		)
		if e.Options.ReadOnly {
			attrs = append(attrs, gosx.BoolAttr("disabled"))
		}
		nodes = append(nodes, gosx.El("button", attrs, gosx.Text(toolbarButtonLabel(item.Command, item.Label))))
	}
	return nodes
}

func (e *Editor) renderPanelButtons() []gosx.Node {
	nodes := make([]gosx.Node, 0, len(e.Options.Panels))
	for _, panel := range e.Options.Panels {
		attrs := gosx.Attrs(
			gosx.Attr("type", "button"),
			gosx.Attr("data-panel", string(panel)),
		)
		if e.Options.ReadOnly {
			attrs = append(attrs, gosx.BoolAttr("disabled"))
		}
		nodes = append(nodes, gosx.El(
			"button",
			attrs,
			gosx.Text(panelTitle(panel)),
		))
	}
	return nodes
}

func (e *Editor) renderPreviewPanel() gosx.Node {
	return gosx.El(
		"div",
		gosx.Attrs(
			gosx.Attr("class", "editor-preview-panel"),
			gosx.Attr("id", "editor-preview-panel"),
		),
		gosx.El(
			"div",
			gosx.Attrs(
				gosx.Attr("class", "editor-preview-content"),
				gosx.Attr("id", "editor-preview-content"),
			),
		),
	)
}

func (e *Editor) renderMetadataPanel() gosx.Node {
	return gosx.El(
		"div",
		gosx.Attrs(
			gosx.Attr("id", "editor-metadata-panel"),
			gosx.Attr("class", "editor-metadata-panel"),
			gosx.Attr("role", "complementary"),
			gosx.Attr("aria-label", "Metadata panel"),
		),
		e.renderPanelHeader("Post Settings", "btn-meta-close"),
		e.renderMetaField("Slug", "meta-slug", "meta-input", "post-url-slug", e.Options.Slug, false),
		e.renderMetaField("Excerpt", "meta-excerpt", "meta-textarea", "Brief description...", e.Options.Excerpt, true),
		gosx.El(
			"div",
			gosx.Attrs(gosx.Attr("class", "meta-field")),
			gosx.El("label", gosx.Attrs(gosx.Attr("class", "meta-label"), gosx.Attr("for", "meta-tags")), gosx.Text("Tags")),
			gosx.El("input", gosx.Attrs(
				gosx.Attr("type", "text"),
				gosx.Attr("id", "meta-tags"),
				gosx.Attr("class", "meta-input"),
				gosx.Attr("value", e.Options.Tags),
				gosx.Attr("placeholder", "go, web, tutorial"),
			)),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "meta-hint")), gosx.Text("Comma-separated")),
		),
		e.renderMetaField("Cover Image URL", "meta-cover-image", "meta-input", "https://...", e.Options.CoverImage, false),
		e.renderMoodField(),
		e.renderMetaField("Music", "meta-music", "meta-input", "https://youtube.com/watch?v=...", e.Options.Music, false),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "meta-divider"))),
		gosx.El(
			"div",
			gosx.Attrs(gosx.Attr("class", "meta-field")),
			gosx.El("label", gosx.Attrs(gosx.Attr("class", "meta-label")), gosx.Text("Status")),
			gosx.El(
				"div",
				gosx.Attrs(
					gosx.Attr("class", "meta-status"),
					gosx.Attr("id", "meta-status-display"),
				),
				gosx.El(
					"span",
					gosx.Attrs(
						gosx.Attr("class", "meta-status-badge meta-status-"+string(e.Options.Status)),
						gosx.Attr("id", "meta-status-badge"),
					),
					gosx.Text(humanizeLabel(string(e.Options.Status))),
				),
			),
		),
		gosx.El(
			"div",
			gosx.Attrs(
				gosx.Attr("class", "meta-field"),
				gosx.Attr("id", "meta-publish-controls"),
			),
			gosx.Fragment(e.renderPublishControls()...),
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "meta-divider"))),
		gosx.El(
			"div",
			gosx.Attrs(gosx.Attr("class", "meta-field")),
			gosx.El("label", gosx.Attrs(gosx.Attr("class", "meta-label")), gosx.Text("Stats")),
			gosx.El(
				"div",
				gosx.Attrs(gosx.Attr("class", "meta-stats")),
				gosx.El(
					"div",
					gosx.Attrs(gosx.Attr("class", "meta-stat")),
					gosx.El("span", gosx.Attrs(gosx.Attr("class", "meta-stat-value"), gosx.Attr("id", "meta-word-count")), gosx.Text(fmt.Sprintf("%d", e.wordCount()))),
					gosx.El("span", gosx.Attrs(gosx.Attr("class", "meta-stat-label")), gosx.Text("words")),
				),
				gosx.El(
					"div",
					gosx.Attrs(gosx.Attr("class", "meta-stat")),
					gosx.El("span", gosx.Attrs(gosx.Attr("class", "meta-stat-value"), gosx.Attr("id", "meta-reading-time")), gosx.Text(fmt.Sprintf("%d", readingTimeMinutes(e.wordCount())))),
					gosx.El("span", gosx.Attrs(gosx.Attr("class", "meta-stat-label")), gosx.Text("min read")),
				),
			),
		),
	)
}

func (e *Editor) renderGalleryPanel() gosx.Node {
	return gosx.El(
		"div",
		gosx.Attrs(
			gosx.Attr("id", "editor-gallery-panel"),
			gosx.Attr("class", "editor-gallery-panel"),
			gosx.Attr("role", "complementary"),
			gosx.Attr("aria-label", "Images panel"),
		),
		e.renderPanelHeader("Images", "btn-gallery-close"),
		gosx.El(
			"div",
			gosx.Attrs(
				gosx.Attr("id", "gallery-grid"),
				gosx.Attr("class", "gallery-grid"),
			),
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "gallery-loading")), gosx.Text("Loading...")),
		),
	)
}

func (e *Editor) renderHistoryPanel() gosx.Node {
	return gosx.El(
		"div",
		gosx.Attrs(
			gosx.Attr("id", "editor-history-panel"),
			gosx.Attr("class", "editor-history-panel"),
			gosx.Attr("role", "complementary"),
			gosx.Attr("aria-label", "History panel"),
		),
		e.renderPanelHeader("History", "btn-history-close"),
		gosx.El(
			"div",
			gosx.Attrs(
				gosx.Attr("id", "history-list"),
				gosx.Attr("class", "history-list"),
			),
			gosx.El("p", gosx.Attrs(gosx.Attr("class", "gallery-loading")), gosx.Text("No history yet.")),
		),
	)
}

func (e *Editor) renderPanelHeader(title, buttonID string) gosx.Node {
	return gosx.El(
		"div",
		gosx.Attrs(gosx.Attr("class", "meta-panel-header")),
		gosx.El("h3", gosx.Attrs(gosx.Attr("class", "meta-panel-title")), gosx.Text(title)),
		gosx.El(
			"button",
			gosx.Attrs(
				gosx.Attr("type", "button"),
				gosx.Attr("id", buttonID),
				gosx.Attr("class", "meta-close-btn"),
			),
			gosx.Text("×"),
		),
	)
}

func (e *Editor) renderMetaField(label, id, className, placeholder, value string, multiline bool) gosx.Node {
	children := []gosx.Node{
		gosx.El("label", gosx.Attrs(gosx.Attr("class", "meta-label"), gosx.Attr("for", id)), gosx.Text(label)),
	}

	if multiline {
		attrs := gosx.Attrs(
			gosx.Attr("id", id),
			gosx.Attr("class", className),
			gosx.Attr("rows", "3"),
			gosx.Attr("placeholder", placeholder),
		)
		if e.Options.ReadOnly {
			attrs = append(attrs, gosx.BoolAttr("readonly"))
		}
		children = append(children, gosx.El("textarea", attrs, gosx.Text(value)))
	} else {
		attrs := gosx.Attrs(
			gosx.Attr("type", "text"),
			gosx.Attr("id", id),
			gosx.Attr("class", className),
			gosx.Attr("value", value),
			gosx.Attr("placeholder", placeholder),
		)
		if e.Options.ReadOnly {
			attrs = append(attrs, gosx.BoolAttr("readonly"))
		}
		children = append(children, gosx.El("input", attrs))
	}

	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "meta-field")), gosx.Fragment(children...))
}

func (e *Editor) renderMoodField() gosx.Node {
	if len(e.Options.MoodChoices) == 0 {
		return gosx.Fragment()
	}

	// Build data-icon attributes on each option for JS preview updates
	options := []gosx.Node{
		gosx.El("option", gosx.Attrs(gosx.Attr("value", ""), gosx.Attr("data-icon", ""), gosx.Attr("data-anim", "")), gosx.Text("none")),
	}
	for _, mc := range e.Options.MoodChoices {
		attrs := gosx.Attrs(
			gosx.Attr("value", mc.Key),
			gosx.Attr("data-icon", mc.Icon),
			gosx.Attr("data-anim", mc.Anim),
		)
		if mc.Key == e.Options.Mood {
			attrs = append(attrs, gosx.BoolAttr("selected"))
		}
		options = append(options, gosx.El("option", attrs, gosx.Text(mc.Label)))
	}

	selectAttrs := gosx.Attrs(
		gosx.Attr("id", "meta-mood"),
		gosx.Attr("class", "meta-input"),
		gosx.Attr("onchange", "var o=this.options[this.selectedIndex];var p=document.getElementById('meta-mood-preview');if(o&&o.dataset.icon){p.src=o.dataset.icon;p.className='meta-mood-preview '+(o.dataset.anim||'');p.style.display=''}else{p.style.display='none';p.className='meta-mood-preview'}"),
	)
	if e.Options.ReadOnly {
		selectAttrs = append(selectAttrs, gosx.BoolAttr("disabled"))
	}

	// Initial preview icon
	previewSrc := ""
	previewDisplay := "none"
	previewAnim := ""
	for _, mc := range e.Options.MoodChoices {
		if mc.Key == e.Options.Mood && mc.Icon != "" {
			previewSrc = mc.Icon
			previewAnim = mc.Anim
			previewDisplay = ""
			break
		}
	}

	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "meta-field")),
		gosx.El("label", gosx.Attrs(gosx.Attr("class", "meta-label"), gosx.Attr("for", "meta-mood")),
			gosx.Text("Mood"),
		),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "meta-mood-row")),
			gosx.El("select", selectAttrs, gosx.Fragment(options...)),
			gosx.El("img", gosx.Attrs(
				gosx.Attr("id", "meta-mood-preview"),
				gosx.Attr("class", "meta-mood-preview "+previewAnim),
				gosx.Attr("src", previewSrc),
				gosx.Attr("alt", ""),
				gosx.Attr("width", "48"),
				gosx.Attr("height", "48"),
				gosx.Attr("style", "display:"+previewDisplay),
			)),
		),
	)
}

func (e *Editor) renderPublishControls() []gosx.Node {
	buttonAttrs := func(id, className string) gosx.AttrList {
		attrs := gosx.Attrs(
			gosx.Attr("type", "button"),
			gosx.Attr("id", id),
			gosx.Attr("class", className),
		)
		if e.Options.ReadOnly {
			attrs = append(attrs, gosx.BoolAttr("disabled"))
		}
		return attrs
	}

	scheduleInputAttrs := gosx.Attrs(
		gosx.Attr("type", "datetime-local"),
		gosx.Attr("id", "meta-publish-at"),
		gosx.Attr("class", "meta-input"),
		gosx.Attr("value", e.Options.PublishAt),
	)
	if e.Options.ReadOnly {
		scheduleInputAttrs = append(scheduleInputAttrs, gosx.BoolAttr("readonly"))
	}

	switch e.Options.Status {
	case StatusPublished:
		return []gosx.Node{
			gosx.El("button", buttonAttrs("btn-unpublish", "meta-btn meta-btn-danger"), gosx.Text("Unpublish")),
		}
	case StatusScheduled:
		return []gosx.Node{
			gosx.El(
				"div",
				gosx.Attrs(gosx.Attr("class", "meta-schedule-info")),
				gosx.El("span", gosx.Attrs(gosx.Attr("class", "meta-hint")), gosx.Text("Scheduled for: "+fallback(e.Options.PublishAt, "unknown"))),
			),
			gosx.El(
				"div",
				gosx.Attrs(gosx.Attr("class", "meta-btn-row")),
				gosx.El("button", buttonAttrs("btn-unschedule", "meta-btn meta-btn-ghost"), gosx.Text("Unschedule")),
				gosx.El("button", buttonAttrs("btn-reschedule", "meta-btn meta-btn-ghost"), gosx.Text("Reschedule")),
			),
			gosx.El(
				"div",
				gosx.Attrs(gosx.Attr("id", "schedule-picker-row"), gosx.Attr("class", "meta-schedule-row"), gosx.Attr("style", "display:none")),
				gosx.El("input", scheduleInputAttrs),
				gosx.El("button", buttonAttrs("btn-do-schedule", "meta-btn meta-btn-primary"), gosx.Text("Schedule")),
			),
		}
	default:
		return []gosx.Node{
			gosx.El("button", buttonAttrs("btn-publish", "meta-btn meta-btn-primary"), gosx.Text("Publish")),
			gosx.El("div", gosx.Attrs(gosx.Attr("class", "meta-or")), gosx.Text("or")),
			gosx.El(
				"div",
				gosx.Attrs(gosx.Attr("class", "meta-schedule-row")),
				gosx.El("input", scheduleInputAttrs),
				gosx.El("button", buttonAttrs("btn-do-schedule", "meta-btn meta-btn-ghost"), gosx.Text("Schedule")),
			),
		}
	}
}

func (e *Editor) renderAssetTags() []gosx.Node {
	var nodes []gosx.Node
	if strings.TrimSpace(e.Options.StylesheetURL) != "" {
		nodes = append(nodes, gosx.El("link", gosx.Attrs(
			gosx.Attr("rel", "stylesheet"),
			gosx.Attr("href", e.Options.StylesheetURL),
		)))
	}
	if strings.TrimSpace(e.Options.ScriptURL) != "" {
		nodes = append(nodes, gosx.El("script", gosx.Attrs(
			gosx.Attr("src", e.Options.ScriptURL),
			gosx.Attr("defer", "defer"),
		)))
	}
	return nodes
}

func (e *Editor) titleValue() string {
	if title := strings.TrimSpace(e.Options.Title); title != "" {
		return title
	}
	return ""
}

func (e *Editor) ariaLabel() string {
	if label := strings.TrimSpace(e.Options.Label); label != "" {
		return label
	}
	if title := strings.TrimSpace(e.Options.Title); title != "" {
		return title
	}
	return "Editor"
}

func (e *Editor) saveStatusText() string {
	if e.Options.ReadOnly {
		return "Read only"
	}
	return "Saved"
}

func (e *Editor) saveStatusClass() string {
	if e.Options.ReadOnly {
		return "saved"
	}
	return "saved"
}

func (e *Editor) hasPanel(panel Panel) bool {
	for _, candidate := range e.Options.Panels {
		if candidate == panel {
			return true
		}
	}
	return false
}

func (e *Editor) toolbarItems(commands []Command) []ToolbarItem {
	index := make(map[Command]ToolbarItem, len(e.Options.Toolbar.Items))
	for _, item := range e.Options.Toolbar.Items {
		index[item.Command] = item
	}

	items := make([]ToolbarItem, 0, len(commands))
	for _, command := range commands {
		item, ok := index[command]
		if ok {
			items = append(items, item)
		}
	}
	return items
}

func toolbarButtonLabel(command Command, fallbackLabel string) string {
	switch command {
	case CmdBold:
		return "B"
	case CmdItalic:
		return "I"
	case CmdStrike:
		return "S"
	case CmdCode:
		return "Code"
	case CmdLink:
		return "Link"
	case CmdImage:
		return "Img"
	case CmdH1:
		return "H1"
	case CmdH2:
		return "H2"
	case CmdH3:
		return "H3"
	case CmdList:
		return "• List"
	case CmdOrderedList:
		return "1. List"
	case CmdTaskList:
		return "Task"
	case CmdBlockquote:
		return "> Quote"
	case CmdNote:
		return "Note"
	case CmdWarning:
		return "Warn"
	case CmdMath:
		return "Math"
	case CmdFootnote:
		return "[^]"
	case CmdHR:
		return "—"
	default:
		return fallbackLabel
	}
}

func panelTitle(panel Panel) string {
	switch panel {
	case PanelPreview:
		return "Preview"
	case PanelMetadata:
		return "Metadata"
	case PanelImages:
		return "Images"
	case PanelHistory:
		return "History"
	default:
		return humanizeLabel(string(panel))
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

var (
	fencedCodeRE = regexp.MustCompile("(?s)```.*?```")
	inlineCodeRE = regexp.MustCompile("`[^`]+`")
)

func (e *Editor) wordCount() int {
	content := fencedCodeRE.ReplaceAllString(e.doc.Content(), " ")
	content = inlineCodeRE.ReplaceAllString(content, " ")
	return len(strings.Fields(content))
}

func readingTimeMinutes(words int) int {
	if words <= 0 {
		return 0
	}
	return (words + 199) / 200
}

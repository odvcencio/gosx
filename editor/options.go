package editor

import (
	"github.com/odvcencio/gosx/editor/input"
	"github.com/odvcencio/gosx/editor/textmodel"
	"github.com/odvcencio/gosx/editor/toolbar"
	"github.com/odvcencio/gosx/hub"
)

// Document aliases the editor text model contract at the top-level package.
type Document = textmodel.Document

// Position aliases a line+column document position.
type Position = textmodel.Position

// Range aliases a document span.
type Range = textmodel.Range

// OpKind aliases a document operation kind.
type OpKind = textmodel.OpKind

const (
	OpInsert  OpKind = textmodel.OpInsert
	OpDelete  OpKind = textmodel.OpDelete
	OpReplace OpKind = textmodel.OpReplace
)

// Operation aliases a single document edit.
type Operation = textmodel.Operation

// Command aliases a semantic editor action.
type Command = input.Command

const (
	CmdBold        Command = input.CmdBold
	CmdItalic      Command = input.CmdItalic
	CmdStrike      Command = input.CmdStrike
	CmdCode        Command = input.CmdCode
	CmdLink        Command = input.CmdLink
	CmdImage       Command = input.CmdImage
	CmdH1          Command = input.CmdH1
	CmdH2          Command = input.CmdH2
	CmdH3          Command = input.CmdH3
	CmdList        Command = input.CmdList
	CmdOrderedList Command = input.CmdOrderedList
	CmdTaskList    Command = input.CmdTaskList
	CmdBlockquote  Command = input.CmdBlockquote
	CmdNote        Command = input.CmdNote
	CmdWarning     Command = input.CmdWarning
	CmdMath        Command = input.CmdMath
	CmdFootnote    Command = input.CmdFootnote
	CmdHR          Command = input.CmdHR
	CmdUndo        Command = input.CmdUndo
	CmdRedo        Command = input.CmdRedo
	CmdSave        Command = input.CmdSave
	CmdIndent      Command = input.CmdIndent
	CmdDedent      Command = input.CmdDedent
	CmdNewline     Command = input.CmdNewline
	CmdCopy        Command = input.CmdCopy
	CmdCut         Command = input.CmdCut
	CmdSelectAll   Command = input.CmdSelectAll
	CmdEscape      Command = input.CmdEscape
)

// Keymap aliases the editor keybinding map.
type Keymap = input.Keymap

// DefaultKeymap provides standard editor keybindings.
var DefaultKeymap = cloneKeymap(input.DefaultKeymap)

// ToolbarItem aliases a toolbar button descriptor.
type ToolbarItem = toolbar.Item

// Toolbar aliases toolbar configuration at the root package.
type Toolbar = toolbar.Toolbar

// ToolbarAction aliases a toolbar command payload.
type ToolbarAction = toolbar.Action

// DefaultToolbar is the standard markdown++ toolbar.
var DefaultToolbar = cloneToolbar(toolbar.DefaultToolbar)

// Lang selects the editor language/grammar.
type Lang string

const (
	MarkdownPP Lang = "markdown++"
	Markdown   Lang = "markdown"
)

// Theme selects the editor color theme.
type Theme string

const (
	ThemeDark  Theme = "dark"
	ThemeLight Theme = "light"
)

// Panel identifies a side panel.
type Panel string

const (
	PanelPreview  Panel = "preview"
	PanelMetadata Panel = "metadata"
	PanelImages   Panel = "images"
	PanelHistory  Panel = "history"
)

// Status identifies the publication state shown in the editor chrome.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusScheduled Status = "scheduled"
	StatusPublished Status = "published"
)

// DefaultPanels is the standard panel set exposed by the editor shell.
var DefaultPanels = []Panel{
	PanelPreview,
	PanelMetadata,
	PanelImages,
	PanelHistory,
}

// Options configures the editor.
type Options struct {
	Content       string
	Label         string
	Title         string
	Slug          string
	Excerpt       string
	Tags          string
	CoverImage    string
	PublishAt     string
	Status        Status
	Placeholder   string
	BackHref      string
	FormAction    string
	UploadURL     string
	StylesheetURL string
	ScriptURL     string
	CSRFToken     string
	ExtraFields   map[string]string
	Language      Lang
	Theme         Theme
	Toolbar       Toolbar
	Keymap        Keymap
	Panels        []Panel
	OnSave        func(doc Document) error
	OnUpload      func(name string, data []byte) (url string, err error)
	Hub           *hub.Hub
	PostID        string
	ReadOnly      bool
}

func (o *Options) defaults() {
	if o.Language == "" {
		o.Language = MarkdownPP
	}
	if o.Theme == "" {
		o.Theme = ThemeDark
	}
	if o.Status == "" {
		o.Status = StatusDraft
	}
	if o.Placeholder == "" {
		o.Placeholder = "Write your post in markdown..."
	}
	if o.BackHref == "" {
		o.BackHref = "/"
	}
	if o.ExtraFields == nil {
		o.ExtraFields = map[string]string{}
	}
	if len(o.Toolbar.Items) == 0 {
		o.Toolbar = cloneToolbar(DefaultToolbar)
	}
	if o.Keymap == nil {
		o.Keymap = cloneKeymap(DefaultKeymap)
	}
	if o.Panels == nil {
		o.Panels = clonePanels(DefaultPanels)
	}
}

func cloneKeymap(src Keymap) Keymap {
	if src == nil {
		return nil
	}
	dst := make(Keymap, len(src))
	for key, cmd := range src {
		dst[key] = cmd
	}
	return dst
}

func cloneToolbar(src Toolbar) Toolbar {
	items := append([]ToolbarItem(nil), src.Items...)
	return Toolbar{Items: items}
}

func clonePanels(src []Panel) []Panel {
	return append([]Panel(nil), src...)
}

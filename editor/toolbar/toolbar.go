package toolbar

import "github.com/odvcencio/gosx/editor/input"

// Item is a single toolbar button.
type Item struct {
	Command input.Command
	Label   string
	Icon    string
}

// Toolbar is an ordered list of toolbar items.
type Toolbar struct {
	Items []Item
}

// Without returns a new toolbar with the given command removed.
func (t Toolbar) Without(cmd input.Command) Toolbar {
	items := make([]Item, 0, len(t.Items))
	for _, item := range t.Items {
		if item.Command != cmd {
			items = append(items, item)
		}
	}
	return Toolbar{Items: items}
}

// DefaultToolbar is the standard markdown++ toolbar.
var DefaultToolbar = Toolbar{
	Items: []Item{
		{input.CmdBold, "Bold", "icon-bold"},
		{input.CmdItalic, "Italic", "icon-italic"},
		{input.CmdStrike, "Strikethrough", "icon-strike"},
		{input.CmdCode, "Code", "icon-code"},
		{input.CmdLink, "Link", "icon-link"},
		{input.CmdImage, "Image", "icon-image"},
		{input.CmdH1, "H1", "icon-h1"},
		{input.CmdH2, "H2", "icon-h2"},
		{input.CmdH3, "H3", "icon-h3"},
		{input.CmdList, "List", "icon-list"},
		{input.CmdOrderedList, "Ordered List", "icon-ol"},
		{input.CmdTaskList, "Task List", "icon-task"},
		{input.CmdBlockquote, "Quote", "icon-quote"},
		{input.CmdNote, "Note", "icon-note"},
		{input.CmdWarning, "Warning", "icon-warning"},
		{input.CmdMath, "Math", "icon-math"},
		{input.CmdFootnote, "Footnote", "icon-footnote"},
		{input.CmdHR, "Divider", "icon-hr"},
	},
}

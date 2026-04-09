package toolbar

import (
	"strings"

	"github.com/odvcencio/gosx/editor/input"
	"github.com/odvcencio/gosx/editor/textmodel"
)

// Action maps a toolbar command to a concrete text operation.
// Value carries optional payload such as a URL for links or images.
type Action struct {
	Command input.Command
	Value   string
}

// Snippet returns the markdown inserted for this toolbar action.
func (a Action) Snippet(selection string) (string, bool) {
	switch a.Command {
	case input.CmdBold:
		return "**" + placeholder(selection, "bold") + "**", true
	case input.CmdItalic:
		return "*" + placeholder(selection, "italic") + "*", true
	case input.CmdStrike:
		return "~~" + placeholder(selection, "text") + "~~", true
	case input.CmdCode:
		return "\n```\n" + placeholder(selection, "code") + "\n```\n", true
	case input.CmdLink:
		return "[" + placeholder(selection, "text") + "](" + placeholder(a.Value, "url") + ")", true
	case input.CmdImage:
		return "![" + placeholder(selection, "alt") + "](" + placeholder(a.Value, "url") + ")", true
	case input.CmdH1:
		return "\n# " + placeholder(selection, "Heading") + "\n", true
	case input.CmdH2:
		return "\n## " + placeholder(selection, "Heading") + "\n", true
	case input.CmdH3:
		return "\n### " + placeholder(selection, "Heading") + "\n", true
	case input.CmdList:
		return "\n- " + placeholder(selection, "item") + "\n", true
	case input.CmdOrderedList:
		return "\n1. " + placeholder(selection, "item") + "\n", true
	case input.CmdTaskList:
		return "\n- [ ] " + placeholder(selection, "todo") + "\n", true
	case input.CmdBlockquote:
		return "\n> " + placeholder(selection, "quote") + "\n", true
	case input.CmdNote:
		return "\n> [!NOTE]\n> " + placeholder(selection, "Note content") + "\n", true
	case input.CmdWarning:
		return "\n> [!WARNING]\n> " + placeholder(selection, "Warning content") + "\n", true
	case input.CmdMath:
		return "\n$$\n" + placeholder(selection, "E = mc^2") + "\n$$\n", true
	case input.CmdFootnote:
		id := placeholder(a.Value, "")
		if id == "" {
			id = placeholder(selection, "1")
		}
		return "[^" + id + "]", true
	case input.CmdHR:
		return "\n---\n", true
	default:
		return "", false
	}
}

// Operation returns the document operation produced by this toolbar action.
func (a Action) Operation(rng textmodel.Range, selection string) (textmodel.Operation, bool) {
	snippet, ok := a.Snippet(selection)
	if !ok {
		return textmodel.Operation{}, false
	}

	kind := textmodel.OpReplace
	if rng.Empty() {
		kind = textmodel.OpInsert
	}

	return textmodel.Operation{
		Kind:    kind,
		Range:   rng,
		Content: []byte(snippet),
		Origin:  "toolbar",
	}, true
}

func placeholder(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed
	}
	return fallback
}

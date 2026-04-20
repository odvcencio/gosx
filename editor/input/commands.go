package input

// Command is a semantic editor action.
type Command string

const (
	CmdBold        Command = "bold"
	CmdItalic      Command = "italic"
	CmdStrike      Command = "strike"
	CmdCode        Command = "code"
	CmdLink        Command = "link"
	CmdImage       Command = "image"
	CmdEmoji       Command = "emoji"
	CmdH1          Command = "h1"
	CmdH2          Command = "h2"
	CmdH3          Command = "h3"
	CmdList        Command = "list"
	CmdOrderedList Command = "ordered_list"
	CmdTaskList    Command = "task_list"
	CmdBlockquote  Command = "blockquote"
	CmdNote        Command = "note"
	CmdWarning     Command = "warning"
	CmdMath        Command = "math"
	CmdFootnote    Command = "footnote"
	CmdHR          Command = "hr"
	CmdScene3D     Command = "scene3d"
	CmdIsland      Command = "island"
	CmdDiagram     Command = "diagram"
	CmdUndo        Command = "undo"
	CmdRedo        Command = "redo"
	CmdSave        Command = "save"
	CmdIndent      Command = "indent"
	CmdDedent      Command = "dedent"
	CmdNewline     Command = "newline"
	CmdCopy        Command = "copy"
	CmdCut         Command = "cut"
	CmdSelectAll   Command = "select_all"
	CmdEscape      Command = "escape"
)

package input

// Keymap maps key chords to commands.
// "Mod" is platform-neutral: Ctrl on Linux/Windows, Cmd on Mac.
type Keymap map[string]Command

// Merge returns a new Keymap with custom bindings layered on top.
func (k Keymap) Merge(other Keymap) Keymap {
	merged := make(Keymap, len(k)+len(other))
	for key, cmd := range k {
		merged[key] = cmd
	}
	for key, cmd := range other {
		merged[key] = cmd
	}
	return merged
}

// DefaultKeymap provides standard editor keybindings.
var DefaultKeymap = Keymap{
	"Mod-B":       CmdBold,
	"Mod-I":       CmdItalic,
	"Mod-Z":       CmdUndo,
	"Mod-Shift-Z": CmdRedo,
	"Mod-S":       CmdSave,
	"Mod-A":       CmdSelectAll,
	"Mod-C":       CmdCopy,
	"Mod-X":       CmdCut,
	"Tab":         CmdIndent,
	"Shift-Tab":   CmdDedent,
	"Enter":       CmdNewline,
	"Escape":      CmdEscape,
}

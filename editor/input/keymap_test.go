package input

import "testing"

func TestKeymap_Merge(t *testing.T) {
	base := DefaultKeymap
	custom := Keymap{"Mod-K": CmdLink}
	merged := base.Merge(custom)

	if merged["Mod-K"] != CmdLink {
		t.Fatal("custom binding should appear in merged")
	}
	if merged["Mod-B"] != CmdBold {
		t.Fatal("base binding should survive merge")
	}
}

func TestKeymap_MergeOverride(t *testing.T) {
	base := DefaultKeymap
	custom := Keymap{"Mod-B": CmdStrike}
	merged := base.Merge(custom)

	if merged["Mod-B"] != CmdStrike {
		t.Fatal("custom should override base")
	}
}

func TestDefaultKeymapIncludesCodeCommands(t *testing.T) {
	want := map[string]Command{
		"Tab": CmdIndent, "Shift-Tab": CmdDedent, "Mod-/": CmdToggleComment,
		"Mod-Shift-\\": CmdMatchBracket, "Mod-Alt-ArrowUp": CmdAddCursorUp,
		"Mod-Alt-ArrowDown": CmdAddCursorDown, "Alt-Shift-ArrowUp": CmdBlockSelectUp,
		"Alt-Shift-ArrowDown": CmdBlockSelectDown, "Mod-F": CmdFind, "Mod-H": CmdReplace,
	}
	for chord, command := range want {
		if got := DefaultKeymap[chord]; got != command {
			t.Errorf("DefaultKeymap[%q] = %q, want %q", chord, got, command)
		}
	}
}

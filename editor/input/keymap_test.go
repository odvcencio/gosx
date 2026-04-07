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

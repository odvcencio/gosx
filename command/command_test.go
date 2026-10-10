package command

import (
	"strings"
	"testing"
)

func TestParseChord(t *testing.T) {
	tests := map[string]Chord{
		"Mod+Z":            {Mod: true, Key: "z"},
		"mod+shift+z":      {Mod: true, Shift: true, Key: "z"},
		"Ctrl+Alt+Delete":  {Ctrl: true, Alt: true, Key: "Delete"},
		"Shift+Space":      {Shift: true, Key: " "},
		"F9":               {Key: "F9"},
		"ArrowLeft":        {Key: "ArrowLeft"},
		"Mod+1":            {Mod: true, Key: "1"},
		"Cmd+K":            {Meta: true, Key: "k"},
		"Plus":             {Key: "+"},
		"Option+Super+Esc": {Alt: true, Meta: true, Key: "Escape"},
		"Control+Up":       {Ctrl: true, Key: "ArrowUp"},
		"Shift+É":          {Shift: true, Key: "é"},
	}
	for text, want := range tests {
		got, err := ParseChord(text)
		if err != nil || got != want {
			t.Errorf("ParseChord(%q) = %+v, %v; want %+v", text, got, err, want)
		}
	}
	for _, bad := range []string{"", "Mod", "Mod+", "Ctrl+Mod+Z", "Meta+Mod+Z", "Laser+Z", "Z+Mod", "Ctrl++", "Fno", "Shift+word"} {
		if _, err := ParseChord(bad); err == nil {
			t.Errorf("ParseChord(%q) accepted", bad)
		}
	}
}

func TestKeyShortcutsRendersARIAWithModAlternatives(t *testing.T) {
	for keys, want := range map[string]string{
		"Mod+Z, F9":        "Control+Z Meta+Z F9",
		"Shift+Space":      "Shift+Space",
		"Ctrl+Alt+ArrowUp": "Control+Alt+ArrowUp",
		"Mod+Shift+Plus":   "Control+Shift+Plus Meta+Shift+Plus",
		"bad, Enter":       "Enter",
	} {
		if got := KeyShortcuts(strings.Split(keys, ", ")...); got != want {
			t.Errorf("KeyShortcuts(%q) = %q, want %q", keys, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	good := []Command{
		{ID: "edit.undo", Title: "Undo", Keys: []string{"Mod+Z"}, Action: Action{Signal: "$edit", Value: "undo"}},
		{ID: "edit.redo", Title: "Redo", Keys: []string{"Mod+Shift+Z"}, Action: Action{Signal: "$edit", Value: "redo"}},
		{ID: "clip.consolidate", Title: "Consolidate", Keys: []string{"Mod+J"}, Reserved: true},
		{ID: "help-open", Title: "Help", Action: Action{Open: "#help"}},
		{ID: "help.close", Title: "Close", Action: Action{Close: "#help"}},
	}
	if err := Validate(good); err != nil {
		t.Fatal(err)
	}
	for name, cmds := range map[string][]Command{
		"duplicate id": {good[0], good[0]},
		"empty title":  {{ID: "x", Action: Action{Open: "#p"}}},
		"blank title":  {{ID: "x", Title: " ", Action: Action{Open: "#p"}}},
		"bad chord":    {{ID: "x", Title: "X", Keys: []string{"Mod+"}, Action: Action{Open: "#p"}}},
		"bad id":       {{ID: "has space", Title: "X", Action: Action{Open: "#p"}}},
		"no action":    {{ID: "x", Title: "X", Keys: []string{"F2"}}},
	} {
		if err := Validate(cmds); err == nil || !strings.Contains(err.Error(), cmds[len(cmds)-1].ID) {
			t.Errorf("%s: error = %v; must name the command", name, err)
		}
	}
}

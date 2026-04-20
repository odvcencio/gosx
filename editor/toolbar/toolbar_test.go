package toolbar

import (
	"testing"

	"github.com/odvcencio/gosx/editor/input"
)

func TestToolbar_Without(t *testing.T) {
	tb := DefaultToolbar
	reduced := tb.Without(input.CmdMath)

	for _, item := range reduced.Items {
		if item.Command == input.CmdMath {
			t.Fatal("CmdMath should have been removed")
		}
	}

	found := false
	for _, item := range DefaultToolbar.Items {
		if item.Command == input.CmdMath {
			found = true
		}
	}
	if !found {
		t.Fatal("original toolbar should still have CmdMath")
	}
}

func TestDefaultToolbar_IncludesEmoji(t *testing.T) {
	for _, item := range DefaultToolbar.Items {
		if item.Command == input.CmdEmoji {
			if item.Label != "Emoji" {
				t.Fatalf("emoji label = %q", item.Label)
			}
			return
		}
	}
	t.Fatal("DefaultToolbar should include CmdEmoji")
}

func TestDefaultToolbar_IncludesMdppEmbeds(t *testing.T) {
	want := map[input.Command]string{
		input.CmdScene3D: "Scene3D",
		input.CmdIsland:  "Island",
		input.CmdDiagram: "Diagram",
	}
	for _, item := range DefaultToolbar.Items {
		if label, ok := want[item.Command]; ok {
			if item.Label != label {
				t.Fatalf("%s label = %q, want %q", item.Command, item.Label, label)
			}
			delete(want, item.Command)
		}
	}
	if len(want) != 0 {
		t.Fatalf("DefaultToolbar missing markdown++ embeds: %#v", want)
	}
}

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

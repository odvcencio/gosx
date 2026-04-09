package toolbar

import (
	"testing"

	"github.com/odvcencio/gosx/editor/input"
	"github.com/odvcencio/gosx/editor/textmodel"
)

func TestActionSnippet_Defaults(t *testing.T) {
	action := Action{Command: input.CmdBold}
	got, ok := action.Snippet("")
	if !ok {
		t.Fatal("expected snippet")
	}
	if got != "**bold**" {
		t.Fatalf("snippet = %q", got)
	}
}

func TestActionSnippet_UsesPayloadForImage(t *testing.T) {
	action := Action{Command: input.CmdImage, Value: "https://cdn.test/image.png"}
	got, ok := action.Snippet("hero")
	if !ok {
		t.Fatal("expected snippet")
	}
	if got != "![hero](https://cdn.test/image.png)" {
		t.Fatalf("snippet = %q", got)
	}
}

func TestActionSnippet_FootnotePrefersExplicitValue(t *testing.T) {
	action := Action{Command: input.CmdFootnote, Value: "note-1"}
	got, ok := action.Snippet("ignored")
	if !ok {
		t.Fatal("expected snippet")
	}
	if got != "[^note-1]" {
		t.Fatalf("snippet = %q", got)
	}
}

func TestActionOperation_UsesToolbarOrigin(t *testing.T) {
	action := Action{Command: input.CmdWarning}
	rng := textmodel.Range{
		Start: textmodel.Position{Line: 2, Col: 3},
		End:   textmodel.Position{Line: 2, Col: 7},
	}

	op, ok := action.Operation(rng, "Watch out")
	if !ok {
		t.Fatal("expected operation")
	}
	if op.Kind != textmodel.OpReplace {
		t.Fatalf("kind = %v", op.Kind)
	}
	if op.Origin != "toolbar" {
		t.Fatalf("origin = %q", op.Origin)
	}
	if string(op.Content) != "\n> [!WARNING]\n> Watch out\n" {
		t.Fatalf("content = %q", string(op.Content))
	}
}

func TestActionOperation_InsertForEmptyRange(t *testing.T) {
	action := Action{Command: input.CmdHR}
	rng := textmodel.Range{
		Start: textmodel.Position{Line: 0, Col: 0},
		End:   textmodel.Position{Line: 0, Col: 0},
	}

	op, ok := action.Operation(rng, "")
	if !ok {
		t.Fatal("expected operation")
	}
	if op.Kind != textmodel.OpInsert {
		t.Fatalf("kind = %v", op.Kind)
	}
}

func TestActionSnippet_UnknownCommand(t *testing.T) {
	_, ok := Action{Command: input.CmdSave}.Snippet("")
	if ok {
		t.Fatal("expected unknown toolbar command to be unsupported")
	}
}

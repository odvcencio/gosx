package toolbar

import (
	"strings"
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

func TestActionSnippet_EmojiUsesStandardShortcodePayload(t *testing.T) {
	action := Action{Command: input.CmdEmoji, Value: ":t-rex:"}
	got, ok := action.Snippet("")
	if !ok {
		t.Fatal("expected snippet")
	}
	if got != ":t-rex:" {
		t.Fatalf("snippet = %q", got)
	}
}

func TestActionSnippet_EmojiUsesSelectionAsShortcode(t *testing.T) {
	action := Action{Command: input.CmdEmoji}
	got, ok := action.Snippet("Face With Spiral Eyes")
	if !ok {
		t.Fatal("expected snippet")
	}
	if got != ":face_with_spiral_eyes:" {
		t.Fatalf("snippet = %q", got)
	}
}

func TestActionSnippet_EmojiDefaultsToSmile(t *testing.T) {
	action := Action{Command: input.CmdEmoji}
	got, ok := action.Snippet("")
	if !ok {
		t.Fatal("expected snippet")
	}
	if got != ":smile:" {
		t.Fatalf("snippet = %q", got)
	}
}

func TestActionSnippet_Scene3D(t *testing.T) {
	got, ok := Action{Command: input.CmdScene3D}.Snippet("title: Mini scene\nshape: sphere")
	if !ok {
		t.Fatal("expected snippet")
	}
	want := "\n```gosx-scene\ntitle: Mini scene\nshape: sphere\n```\n"
	if got != want {
		t.Fatalf("snippet = %q, want %q", got, want)
	}
}

func TestActionSnippet_Island(t *testing.T) {
	got, ok := Action{Command: input.CmdIsland}.Snippet("")
	if !ok {
		t.Fatal("expected snippet")
	}
	for _, want := range []string{"```gosx-island", "component: counter", "count: 0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("snippet missing %q: %q", want, got)
		}
	}
}

func TestActionSnippet_DiagramIsEmptyMermaidFenceByDefault(t *testing.T) {
	got, ok := Action{Command: input.CmdDiagram}.Snippet("")
	if !ok {
		t.Fatal("expected snippet")
	}
	want := "\n```mermaid\n\n```\n"
	if got != want {
		t.Fatalf("snippet = %q, want %q", got, want)
	}

	got, ok = Action{Command: input.CmdDiagram}.Snippet("flowchart TD\n  A --> B")
	if !ok {
		t.Fatal("expected snippet")
	}
	want = "\n```mermaid\nflowchart TD\n  A --> B\n```\n"
	if got != want {
		t.Fatalf("snippet = %q, want %q", got, want)
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

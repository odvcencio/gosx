package ir_test

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

// TestIslandEventsDocExampleCompiles extracts the ```gsx block from
// docs/island-events.md and lowers it, so the documented Fader keeps compiling.
func TestIslandEventsDocExampleCompiles(t *testing.T) {
	doc, err := os.ReadFile("../docs/island-events.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	start := strings.Index(text, "```gsx\n")
	if start < 0 {
		t.Fatal("docs/island-events.md has no gsx example")
	}
	start += len("```gsx\n")
	end := strings.Index(text[start:], "```")
	if end < 0 {
		t.Fatal("unterminated gsx example")
	}
	prog, err := parse(t, []byte(text[start:start+end]))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := ir.Validate(prog); len(got) != 0 {
		t.Fatalf("validate: %+v", got)
	}
	if got := ir.ValidateWarnings(prog); len(got) != 0 {
		t.Fatalf("warnings: %+v", got)
	}
	island, err := ir.LowerIsland(prog, 0)
	if err != nil {
		t.Fatalf("LowerIsland: %v", err)
	}
	if len(island.Handlers) != 4 {
		t.Fatalf("handlers = %d, want 4", len(island.Handlers))
	}
}

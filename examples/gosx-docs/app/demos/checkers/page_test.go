package checkers

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestCheckersPageCompilesWithSemanticFallback(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	program, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile page.gsx: %v", err)
	}
	if len(program.Components) == 0 {
		t.Fatal("page.gsx has no components")
	}
	text := string(source)
	for _, required := range []string{
		"<Scene3D",
		"Keyboard board · 121 holes",
		"data-checkers-hole",
		"checkers-status",
		"checkers-restart",
		"checkers-material",
		`role="grid"`,
		"data-x",
		"Prototype limitations",
		"<noscript>",
		"/checkers-client.js",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("page.gsx missing %q", required)
		}
	}
}

func TestCheckersClientProvidesRovingKeyboardAndMaterialSelection(t *testing.T) {
	source, err := os.ReadFile("../../../public/checkers-client.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{"onBoardKeydown", "ArrowRight", "moveBoardFocus", "onMaterialChange", `searchParams.set("material"`} {
		if !strings.Contains(text, required) {
			t.Errorf("checkers-client.js missing %q", required)
		}
	}
}

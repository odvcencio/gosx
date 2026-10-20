package ir

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestIslandEventTypesMatchRuntimeDelegatedEvents ties islandEventTypes to the
// event lists in client/runtime/host/events.ts, so the compiler and the
// runtime cannot drift apart.
func TestIslandEventTypesMatchRuntimeDelegatedEvents(t *testing.T) {
	raw, err := os.ReadFile("../client/runtime/host/events.ts")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)

	list := regexp.MustCompile(`(?s)const DELEGATED_EVENTS = \[(.*?)\];`).FindStringSubmatch(source)
	if list == nil {
		t.Fatal("DELEGATED_EVENTS not found in events.ts")
	}
	// Drop line comments before collecting string literals.
	body := regexp.MustCompile(`//[^\n]*`).ReplaceAllString(list[1], "")
	var runtime []string
	for _, m := range regexp.MustCompile(`"([a-z-]+)"`).FindAllStringSubmatch(body, -1) {
		runtime = append(runtime, m[1])
	}
	globals := regexp.MustCompile(`(?s)const GLOBAL_DELEGATED_EVENTS = \[(.*?)\];`).FindStringSubmatch(source)
	if globals == nil {
		t.Fatal("GLOBAL_DELEGATED_EVENTS not found in events.ts")
	}
	for _, m := range regexp.MustCompile(`marker: "([a-z-]+)"`).FindAllStringSubmatch(globals[1], -1) {
		runtime = append(runtime, m[1])
	}

	var compiler []string
	for _, eventType := range islandEventTypes {
		compiler = append(compiler, eventType)
	}
	sort.Strings(runtime)
	sort.Strings(compiler)
	if strings.Join(runtime, ",") != strings.Join(compiler, ",") {
		t.Fatalf("islandEventTypes and events.ts disagree\ncompiler: %v\nruntime:  %v", compiler, runtime)
	}
}

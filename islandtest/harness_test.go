package islandtest

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx/island/program"
)

func TestHarnessClickUpdatesRenderedHTML(t *testing.T) {
	h, err := New(program.CounterProgram(), nil)
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}

	if html := h.HTML(); !strings.Contains(html, ">0<") {
		t.Fatalf("expected initial count render, got %s", html)
	}

	patches, err := h.Click("increment")
	if err != nil {
		t.Fatalf("click increment: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("expected patches after increment")
	}

	if html := h.HTML(); !strings.Contains(html, ">1<") {
		t.Fatalf("expected updated count render, got %s", html)
	}
}

func TestHarnessInputCarriesEventValue(t *testing.T) {
	h, err := New(program.EditorProgram(), nil)
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}

	patches, err := h.Input("onInput", "abc")
	if err != nil {
		t.Fatalf("input onInput: %v", err)
	}
	if len(patches) == 0 {
		t.Fatal("expected patches after input")
	}

	html := h.HTML()
	if !strings.Contains(html, "abc") {
		t.Fatalf("expected editor contents in html, got %s", html)
	}
	if !strings.Contains(html, "3 chars") {
		t.Fatalf("expected updated character count, got %s", html)
	}
}

func TestHarnessDispatchErrorsForUnknownHandler(t *testing.T) {
	h, err := New(program.CounterProgram(), nil)
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}

	if _, err := h.Click("missing"); err == nil {
		t.Fatal("expected missing-handler error")
	}
}

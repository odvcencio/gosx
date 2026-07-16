package prose

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetHandlerServesStylesheet(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, DefaultStylesheetPath, nil)
	w := httptest.NewRecorder()
	AssetHandler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("prose.css returned %d", w.Code)
	}
	for _, want := range []string{".gosx-prose", "--gosx-prose-size", "--gosx-prose-flow"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("prose.css missing %q", want)
		}
	}
}

func TestAssetHandlerServesStandaloneRuntime(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, DefaultRuntimeScriptPath, nil)
	w := httptest.NewRecorder()
	AssetHandler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("prose-runtime.js returned %d", w.Code)
	}
	for _, want := range []string{"GoSX standalone prose runtime", "reconcileHTML", "reconcileBlocks", "createBlockStream", "window.GosxProse"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("prose-runtime.js missing %q", want)
		}
	}
	if RuntimeScript() == "" {
		t.Fatal("RuntimeScript() returned empty source")
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

// TestDocumentContractNamesEveryFeatureTheLoaderReads fails when the client
// feature loader reads a runtime asset key that the server's document
// contract never writes. That mismatch caused the black screen of 2026-09-29:
// the loader read bootstrapFeatureTextLayoutPath, found nothing, and fell back
// to an unhashed /gosx/ URL.
func TestDocumentContractNamesEveryFeatureTheLoaderReads(t *testing.T) {
	declared := map[string]bool{}
	typ := reflect.TypeOf(documentContractAssets{})
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		declared[name] = true
	}

	keyPattern := regexp.MustCompile(`assets\.(bootstrapFeature[A-Za-z0-9]+Path)`)
	read := map[string][]string{}
	for _, file := range []string{
		"../client/js/bootstrap-src/00-textlayout.ts",
		"../client/js/bootstrap-src/26-runtime-tail.ts",
	} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read loader source: %v", err)
		}
		for _, match := range keyPattern.FindAllStringSubmatch(string(src), -1) {
			read[match[1]] = append(read[match[1]], filepath.Base(file))
		}
	}
	if len(read) == 0 {
		t.Fatal("found no runtime asset keys in the feature loader; the pattern is stale")
	}
	var missing []string
	for key, files := range read {
		if !declared[key] {
			missing = append(missing, key+" (read by "+strings.Join(files, ", ")+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("the feature loader reads runtime asset keys the document contract never writes:\n%s", strings.Join(missing, "\n"))
	}

	// The client normalizes the contract before the loader sees it, so every
	// key must also survive gosxDocumentRuntimeAssets in 05-document-env.ts.
	env, err := os.ReadFile("../client/js/bootstrap-src/05-document-env.ts")
	if err != nil {
		t.Fatalf("read document env source: %v", err)
	}
	start := strings.Index(string(env), "function gosxDocumentRuntimeAssets(")
	if start < 0 {
		t.Fatal("gosxDocumentRuntimeAssets not found in 05-document-env.ts")
	}
	body := string(env[start:])
	if end := strings.Index(body, "\n  }\n"); end > 0 {
		body = body[:end]
	}
	// The normalizer copies keys by pattern; each key must match it.
	copyPattern := regexp.MustCompile(`/\^bootstrapFeature/`)
	var dropped []string
	for key := range read {
		if !strings.Contains(body, key+":") && !(copyPattern.MatchString(body) && strings.HasPrefix(key, "bootstrapFeature")) {
			dropped = append(dropped, key)
		}
	}
	sort.Strings(dropped)
	if len(dropped) > 0 {
		t.Fatalf("gosxDocumentRuntimeAssets drops runtime asset keys the feature loader reads:\n%s", strings.Join(dropped, "\n"))
	}
}

// TestAppServesUnhashedFeatureChunksFromDistBuild covers an app served from
// its project root with the production build in dist/. The island renderer
// reads dist/build.json there, so the compat /gosx/ route must read the same
// manifest: otherwise every unhashed feature URL returns 404.
func TestAppServesUnhashedFeatureChunksFromDistBuild(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	runtimeDir := filepath.Join(dist, "assets", "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"bootstrap-feature-textlayout.c072.js": "window.__gosx_textlayout_chunk = 1;",
		"bootstrap-feature-engines.7b56.js":    "window.__gosx_engines_chunk = 1;",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(runtimeDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		BootstrapFeatureTextlayout: buildmanifest.HashedAsset{File: "bootstrap-feature-textlayout.c072.js", Hash: "c072"},
		BootstrapFeatureEngines:    buildmanifest.HashedAsset{File: "bootstrap-feature-engines.7b56.js", Hash: "7b56"},
	}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "build.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	app := New()
	app.SetRuntimeRoot(root)
	handler := app.Build()
	for path, want := range map[string]string{
		"/gosx/bootstrap-feature-textlayout.js":        "__gosx_textlayout_chunk",
		"/gosx/bootstrap-feature-engines.js":           "__gosx_engines_chunk",
		"/gosx/bootstrap-feature-textlayout.js?v=c072": "__gosx_textlayout_chunk",
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, w.Code)
		}
		if body := w.Body.String(); !strings.Contains(body, want) {
			t.Fatalf("GET %s served %q, want the hashed chunk", path, body)
		}
	}
}

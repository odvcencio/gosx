package server

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSceneAssetURLRewriteTouchesOnlyStringValues(t *testing.T) {
	paths := map[string]string{"/table-assets/a.png": "/table-assets/hash/a.png"}
	raw := []byte("{\n  \"/table-assets/a.png\" : [\"/table-assets/a.png\",9007199254740993,-0.000e+1,\"prefix /table-assets/a.png\",\"\\\"/table-assets/a.png\\\"\"]\n}")
	want := bytes.Replace(raw, []byte(`["/table-assets/a.png",`), []byte(`["/table-assets/hash/a.png",`), 1)
	if got := rewriteManagedAssetURLs(raw, paths); !bytes.Equal(got, want) {
		t.Fatalf("changed unrelated JSON bytes:\n%s", got)
	}
	if !bytes.Equal(rewriteManagedAssetURLs(raw, map[string]string{"/missing": "/new"}), raw) {
		t.Fatal("no-match rewrite changed JSON")
	}
}

func TestManagedAssetsValidatorsAndPaths(t *testing.T) {
	a, err := NewManagedAssets("/assets/", fstest.MapFS{"tile.svg": &fstest.MapFile{Data: []byte("<svg/>")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, versioned := range []bool{false, true} {
		uri := "/assets/tile.svg"
		if versioned {
			uri = a.URL(uri)
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", uri, nil)
		a.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.String() != "<svg/>" || strings.Contains(w.Header().Get("Cache-Control"), "immutable") != versioned {
			t.Fatal(w.Code, w.Header(), w.Body.String())
		}
		r.Header.Set("If-None-Match", w.Header().Get("ETag"))
		w = httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != 304 {
			t.Fatal(w.Code)
		}
	}
	for _, uri := range []string{"/assets/invalid/tile.svg", "/assets/../tile.svg", "/elsewhere/tile.svg", "/assets/"} {
		w := httptest.NewRecorder()
		a.ServeHTTP(w, httptest.NewRequest("GET", uri, nil))
		if w.Code != 404 {
			t.Fatal(uri, w.Code)
		}
	}
	raw, err := a.Marshal(map[string]any{"url": "/assets/tile.svg", "revision": uint64(18446744073709551615)})
	if err != nil || !bytes.Contains(raw, []byte("18446744073709551615")) || !bytes.Contains(raw, []byte(a.URL("/assets/tile.svg"))) {
		t.Fatal(string(raw), err)
	}
}

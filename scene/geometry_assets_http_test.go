//go:build !js || !wasm

package scene

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestGeometryAssetsBudgetAttributesAndETag(t *testing.T) {
	a := NewGeometryAssets("/assets/", 4096)
	v := &MeshVertices{Count: 3, Positions: []float64{0, 0, 0, 1, 0, 0, 0, 1, 0}, Indices: []uint32{0, 1, 2}, Attributes: map[string]MeshAttribute{"heat": {Data: []float64{1, 2, 3}, ItemSize: 1}}}
	uri, shared := a.Intern(v)
	if uri == "" || shared != v {
		t.Fatal("geometry not interned")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", uri, nil)
	a.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatal(w.Code, w.Header())
	}
	var got MeshVertices
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Attributes["heat"].Data[2] != 3 {
		t.Fatal(err, got)
	}
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 304 {
		t.Fatal(w.Code)
	}
	copy := *v
	copy.Attributes = map[string]MeshAttribute{"heat": {Data: []float64{3, 2, 1}, ItemSize: 1}}
	if GeometryHash(v) == GeometryHash(&copy) {
		t.Fatal("custom stream missing from content identity")
	}
	small := NewGeometryAssets("/assets/", 1)
	if uri, _ := small.Intern(v); uri != "" {
		t.Fatal("over budget geometry published")
	}
	if _, shared := a.Intern(v); shared != v {
		t.Fatal("interned stream not reused")
	}
}

package intelligenceassets

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedBundleMatchesManifestAndServesWASM(t *testing.T) {
	var manifest struct {
		Language string            `json:"language"`
		Assets   map[string]string `json:"assets"`
	}
	data, err := assetsFS.ReadFile("assets/manifest.json")
	if err != nil || json.Unmarshal(data, &manifest) != nil {
		t.Fatalf("manifest: %v", err)
	}
	if manifest.Language != "go" || len(manifest.Assets) != 5 {
		t.Fatalf("manifest=%+v", manifest)
	}
	for name, expected := range manifest.Assets {
		asset, err := assetsFS.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(asset)
		if actual := "sha256:" + hex.EncodeToString(digest[:]); actual != expected {
			t.Fatalf("%s digest=%s want=%s", name, actual, expected)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/gotreesitter.wasm", nil)
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	body, readErr := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/wasm" || !strings.Contains(response.Header.Get("Cache-Control"), "immutable") || readErr != nil || len(body) < 1_000_000 {
		t.Fatalf("status=%d contentType=%q cache=%q bytes=%d err=%v", response.StatusCode, response.Header.Get("Content-Type"), response.Header.Get("Cache-Control"), len(body), readErr)
	}
}

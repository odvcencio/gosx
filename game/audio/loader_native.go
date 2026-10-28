//go:build !js || !wasm

package audio

import (
	"fmt"
	"io"
	"net/http"
)

// HTTPLoader loads clip bytes with net/http.Get.
//
// HTTPLoader{} is valid in every build. The Client field exists only in native
// builds. In a js/wasm build HTTPLoader has no fields and fetches with the
// browser's fetch API (see loader_js.go), so this package does not import
// net/http there.
type HTTPLoader struct {
	// Client selects the HTTP client. A nil Client uses http.DefaultClient.
	// This field exists only in native builds; code that sets it does not
	// compile for GOOS=js GOARCH=wasm.
	Client *http.Client
}

// Load implements ClipLoader.
func (l HTTPLoader) Load(uri string) ([]byte, error) {
	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(uri)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("audio: GET %s: status %d", uri, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

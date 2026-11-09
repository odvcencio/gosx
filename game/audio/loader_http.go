//go:build !js || !wasm

package audio

import (
	"fmt"
	"io"
	"net/http"
)

// HTTPLoader loads clip bytes with net/http.Get. It exists on native builds only;
// GOOS=js GOARCH=wasm builds get a fetch-based HTTPLoader (loader_js.go) that
// links no net/http.
type HTTPLoader struct {
	// Client selects the HTTP client. A nil Client uses http.DefaultClient.
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

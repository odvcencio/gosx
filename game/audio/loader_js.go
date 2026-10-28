//go:build js && wasm

package audio

import (
	"context"
	"fmt"

	"m31labs.dev/gosx/client/browser"
)

// statusOK is net/http.StatusOK. This file does not import net/http, which
// would add crypto/tls and crypto/x509 to every browser module that links it.
const statusOK = 200

// HTTPLoader loads clip bytes with the browser's fetch API, through
// client/browser.Fetch. It sends a plain GET for the clip URI. As with fetch,
// a relative URI resolves against the page and redirects are followed.
//
// HTTPLoader{} is valid in every build. This type has no fields: the Client
// field, which selects an *http.Client, exists only in native builds, so code
// that sets Client does not compile for GOOS=js GOARCH=wasm.
//
// A status other than 200 returns "audio: GET <uri>: status N", the same text
// the native HTTPLoader returns. A transport failure returns
// "audio: GET <uri>: " followed by client/browser's error, which errors.Is can
// match (browser.ErrFetchUnavailable when the page has no fetch). The native
// loader returns a *url.Error here; this one does not.
//
// Load blocks until the whole body has arrived, even for a status other than
// 200. Call it, and the Player methods that call it (LoadManifest, and Play or
// ScheduleAt for a clip that is not decoded yet), from a goroutine, never from
// inside a syscall/js event callback.
type HTTPLoader struct{}

// Load implements ClipLoader.
func (HTTPLoader) Load(uri string) ([]byte, error) {
	response, err := browser.Fetch(context.Background(), browser.FetchRequest{URL: uri})
	// Fetch can return a status and an error together: the headers arrived and
	// reading the body failed. The native loader checks the status before it
	// reads the body, so a 404 reports "status 404" whatever the body does.
	// Only a failure before any status arrived (status 0) is a transport error.
	if err != nil && response.Status == 0 {
		return nil, fmt.Errorf("audio: GET %s: %w", uri, err)
	}
	// Status 0 without an error (an opaque response) lands here too, so it is
	// never returned as an empty clip.
	if response.Status != statusOK {
		return nil, fmt.Errorf("audio: GET %s: status %d", uri, response.Status)
	}
	if err != nil {
		return nil, fmt.Errorf("audio: GET %s: %w", uri, err)
	}
	return response.Body, nil
}

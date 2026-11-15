//go:build js && wasm

package rooms

import (
	"errors"
	"net/http"
)

// The docs server never runs in a browser; these keep main.go compiling in a
// js/wasm build of ./... .

// Server is the room server type, unavailable in a js/wasm build.
type Server struct{}

func (*Server) ServeHTTP(http.ResponseWriter, *http.Request) {}

// NewServer is unavailable in a js/wasm build.
func NewServer(string) (*Server, error) {
	return nil, errors.New("rooms: server builds only for the host")
}

// AssetHandler is unavailable in a js/wasm build.
func AssetHandler(string) http.Handler { return http.NotFoundHandler() }

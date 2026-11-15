package server

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

func TestPageRuntimeHeadPropagatesPreloadNonce(t *testing.T) {
	r := NewPageRuntime()
	r.Engine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(`{"forceWebGL":true}`)}, gosx.Text(""))
	hints := gosx.RenderHTML(r.PreloadHintsWithNonce("request-nonce"))
	head := gosx.RenderHTML(r.HeadWithNonce("request-nonce"))
	found := false
	for _, line := range strings.Split(hints, "\n") {
		if strings.Contains(line, `as="script"`) {
			found = true
			if !strings.Contains(line, `nonce="request-nonce"`) || !strings.Contains(head, line) {
				t.Fatalf("nonce lost from head: %s", head)
			}
		}
	}
	if !found {
		t.Fatalf("fixture emitted no script hints: %s", hints)
	}
}

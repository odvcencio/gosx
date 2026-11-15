package route

import (
	"encoding/json"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

func TestFileRenderEnvPreloadHintsUseRequestNonce(t *testing.T) {
	ctx := &RouteContext{}
	ctx.SetNonce("route-nonce")
	ctx.Engine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: json.RawMessage(`{"forceWebGL":true}`)}, gosx.Text(""))
	env := newFileRenderEnv(ctx, FilePage{})
	hints := gosx.RenderHTML(env.islandPreloadHints())
	found := false
	for _, line := range strings.Split(hints, "\n") {
		if strings.Contains(line, `as="script"`) {
			found = true
			if !strings.Contains(line, `nonce="route-nonce"`) {
				t.Fatalf("nonce missing from hint: %s", line)
			}
		}
	}
	if !found {
		t.Fatalf("fixture emitted no script hints: %s", hints)
	}
}

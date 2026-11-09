package scene

import (
	"os"
	"strings"
	"testing"
)

// The browser mount dispatches MountGraphicsResetEvent with the phase names
// GraphicsReset decodes. Both sides must spell them the same way.
func TestGraphicsResetEventMatchesBrowserMount(t *testing.T) {
	source, err := os.ReadFile("../client/runtime/scene3d/mount-quality.ts")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, want := range []string{`"` + MountGraphicsResetEvent + `"`, `phase === "` + GraphicsResetLost + `"`, `"` + GraphicsResetRestored + `"`} {
		if !strings.Contains(text, want) {
			t.Errorf("mount-quality.ts does not contain %s", want)
		}
	}
}

func TestGraphicsResetRequiresBrowserSurface(t *testing.T) {
	cancel, err := NewSurface("scene").OnGraphicsReset(func(GraphicsReset) {})
	if err == nil || cancel != nil {
		t.Fatalf("host build must report that graphics resets need js/wasm, got cancel=%v err=%v", cancel != nil, err)
	}
}

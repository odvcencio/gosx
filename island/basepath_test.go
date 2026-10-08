package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

func TestSceneWebGPULoaderUsesBasePath(t *testing.T) {
	r := NewRenderer("main")
	r.SetBasePath("/.proxy/game")
	r.SetBootstrapFeatureScene3DWebGPUPath("/gosx/webgpu.hashed.js")
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.Text(""))
	markup := gosx.RenderHTML(r.BootstrapScript())
	if !strings.Contains(markup, `s.src="/.proxy/game/gosx/webgpu.hashed.js"`) {
		t.Fatal("inline scene loader lost prefix", markup)
	}
	if strings.Contains(markup, `s.src="/gosx/`) {
		t.Fatal("inline loader generated an unprefixed URL")
	}
}

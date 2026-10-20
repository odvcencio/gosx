package island

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/internal/basepath"
)

func TestSceneWebGPULoaderUsesBasePath(t *testing.T) {
	r := NewRenderer("main")
	r.SetBasePath("/.proxy/game")
	r.SetBootstrapFeatureScene3DWebGPUPath("/gosx/webgpu.hashed.js")
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.Text(""))
	markup := basepath.HTML("/.proxy/game", gosx.RenderHTML(r.BootstrapScript()))
	if !strings.Contains(markup, `data-gosx-scene3d-webgpu-url="/.proxy/game/gosx/webgpu.hashed.js"`) {
		t.Fatal("lazy scene loader lost prefix", markup)
	}
	if strings.Contains(markup, `data-gosx-scene3d-webgpu-url="/gosx/`) {
		t.Fatal("lazy loader generated an unprefixed URL")
	}
}

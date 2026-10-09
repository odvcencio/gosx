package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
)

func renderDocumentContract(t *testing.T, handler http.Handler, target string) string {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	body := w.Body.String()
	start := strings.Index(body, `<script id="gosx-document"`)
	if start < 0 {
		t.Fatalf("no document contract in %s", body)
	}
	end := strings.Index(body[start:], "</script>")
	return body[start : start+end]
}

func TestDocumentFeaturePathsPublishFlatKeys(t *testing.T) {
	app := New()
	app.Page("GET /bridge", func(ctx *Context) gosx.Node {
		if err := ctx.Runtime().RequireFeature("engine-bridge"); err != nil {
			t.Error(err)
		}
		return ctx.Engine(engine.Config{
			Name:     "GoWASMFixture",
			Kind:     engine.KindSurface,
			Runtime:  engine.RuntimeGoWASM,
			WASMPath: "/engines/fixture.wasm",
		}, gosx.El("span", gosx.Text("fallback")))
	})
	app.Page("GET /scene", func(ctx *Context) gosx.Node {
		return ctx.Engine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.El("span", gosx.Text("fallback")))
	})
	handler := app.Build()

	contract := renderDocumentContract(t, handler, "/bridge")
	for _, want := range []string{
		`"bootstrapFeatureEngineBridgePath":"/gosx/bootstrap-feature-engine-bridge.js"`,
		`"bootstrapFeatureEnginesPath":"/gosx/bootstrap-feature-engines.js"`,
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("contract lacks %s: %s", want, contract)
		}
	}

	scene := renderDocumentContract(t, handler, "/scene")
	if !strings.Contains(scene, `"bootstrapFeatureTextLayoutPath"`) {
		t.Fatalf("legacy textlayout key must stay: %s", scene)
	}
	if strings.Contains(scene, "bootstrapFeatureEngineBridgePath") {
		t.Fatalf("a page that does not require the bridge must not advertise it: %s", scene)
	}
}

func TestDocumentContractLegacyFeatureKeysAreNotDuplicated(t *testing.T) {
	app := New()
	app.Page("GET /bridge", func(ctx *Context) gosx.Node {
		_ = ctx.Runtime().RequireFeature("engine-bridge")
		return ctx.Engine(engine.Config{Name: "GoWASMFixture", Kind: engine.KindSurface, Runtime: engine.RuntimeGoWASM, WASMPath: "/e.wasm"}, gosx.Text("x"))
	})
	contract := renderDocumentContract(t, app.Build(), "/bridge")
	if n := strings.Count(contract, `"bootstrapFeatureEnginesPath"`); n != 1 {
		t.Fatalf("bootstrapFeatureEnginesPath appears %d times: %s", n, contract)
	}
}

func TestPageRuntimeSummaryStaysComparable(t *testing.T) {
	a, b := NewPageRuntime().Summary(), NewPageRuntime().Summary()
	if a != b {
		t.Fatal("PageRuntimeSummary must stay comparable with ==")
	}
}

func TestRequireFeatureCollisionNeverReachesTheContract(t *testing.T) {
	app := New()
	app.Page("GET /collide", func(ctx *Context) gosx.Node {
		rt := ctx.Runtime()
		if err := rt.RequireFeature("a1"); err != nil {
			t.Error(err)
		}
		if err := rt.RequireFeature("a-1"); err == nil {
			t.Error("a-1 collides with a1 and must be rejected")
		}
		if err := rt.RequireFeature("text-layout"); err == nil {
			t.Error("text-layout collides with the legacy textlayout key and must be rejected")
		}
		return ctx.Engine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface}, gosx.Text("x"))
	})
	contract := renderDocumentContract(t, app.Build(), "/collide")
	if n := strings.Count(contract, `"bootstrapFeatureA1Path"`); n != 1 {
		t.Fatalf("bootstrapFeatureA1Path appears %d times: %s", n, contract)
	}
	if n := strings.Count(contract, `"bootstrapFeatureTextLayoutPath"`); n != 1 {
		t.Fatalf("bootstrapFeatureTextLayoutPath appears %d times: %s", n, contract)
	}
	if !strings.Contains(contract, `"bootstrapFeatureTextLayoutPath":"/gosx/bootstrap-feature-textlayout`) {
		t.Fatalf("the legacy key must keep the textlayout chunk URL: %s", contract)
	}
}

func TestRequireSceneFeatureOnPlainEnginePageIsRejected(t *testing.T) {
	app := New()
	app.Page("GET /plain", func(ctx *Context) gosx.Node {
		if err := ctx.Runtime().RequireFeature("scene3d"); err == nil {
			t.Error("scene3d needs a GoSXScene3D engine and must be rejected")
		}
		return ctx.Engine(engine.Config{Name: "Plain", Kind: engine.KindSurface}, gosx.Text("x"))
	})
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/plain", nil))
	if strings.Contains(w.Body.String(), `"features"`) {
		t.Fatalf("a rejected feature must not reach the manifest: %s", w.Body.String())
	}
}

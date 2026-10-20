package budget

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func opaqueReferenceGraph(document, target string) ReachabilityOptions {
	bodies := map[string][]byte{
		"app/fixture/html":   []byte(document),
		"app/fixture/entry":  []byte(`import "/js/hidden.js";`),
		"app/fixture/hidden": []byte(`export const hidden = 1;`),
	}
	return ReachabilityOptions{
		Graph: &buildmanifest.PerfAssetUses{Version: 1, Assets: []buildmanifest.PerfAssetUse{
			graphAsset("app/fixture/html", "/counter/", "html", "critical", "always", bodies["app/fixture/html"]),
			graphAsset("app/fixture/entry", target, "other", "dormant", "always", bodies["app/fixture/entry"]),
			graphAsset("app/fixture/hidden", "/js/hidden.js", "js", "dormant", "always", bodies["app/fixture/hidden"]),
		}},
		Bodies:  bodies,
		Route:   FixtureRoute{RouteTemplate: "/counter/", CriticalAssetIDs: []string{"app/fixture/html"}},
		Backend: "none",
	}
}

func TestReachabilityRejectsSemanticReferencesToOther(t *testing.T) {
	for _, tc := range []struct{ name, document, target string }{
		{"module", `<script type="module" src="/js/x.js"></script>`, "/js/x.js"},
		{"stylesheet", `<link rel="stylesheet" href="/style.css">`, "/style.css"},
		{"document", `<iframe src="/child/"></iframe>`, "/child/"},
		{"wasm", `<script id="gosx-manifest" type="application/json">{"version":"0.1.0","runtime":{"path":"/runtime.wasm"},"islands":[{}]}</script>`, "/runtime.wasm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveReachability(opaqueReferenceGraph(tc.document, tc.target))
			var input *InputError
			if !errors.As(err, &input) || input.Code != "wrong-fixture" || input.Pointer != "/assets/1/kind" {
				t.Fatalf("semantic reference certified as an opaque body: %v", err)
			}
		})
	}
}

func TestMeasureOpaqueModuleCannotHideImports(t *testing.T) {
	for _, media := range []string{"text/javascript", "application/octet-stream"} {
		t.Run(media, func(t *testing.T) {
			graph := opaqueReferenceGraph(`<script type="module" src="/js/x.js"></script>`, "/js/x.js")
			opts, requests := testMeasuredResourceGraph(t, graph, nil)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, asset := range graph.Graph.Assets {
					if asset.URL != r.URL.Path {
						continue
					}
					requests[asset.URL].Add(1)
					contentType := media
					if asset.Kind == "html" {
						contentType = "text/html"
					}
					w.Header().Set("Content-Type", contentType)
					w.Write(graph.Bodies[asset.ID])
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			opts.BaseURL, opts.Client = server.URL, server.Client()
			// An opaque served type must not bypass the loader-context check.
			_, err := measureApp(context.Background(), opts, testBodyNormalizer)
			var input *InputError
			if !errors.As(err, &input) || input.Code != "wrong-fixture" || input.Pointer != "/assets/1/kind" {
				t.Fatalf("wrong-kind module hid its import closure: %v", err)
			}
			if requests["/js/hidden.js"].Load() != 0 {
				t.Fatal("invalid declaration was traversed before rejection")
			}
		})
	}
}

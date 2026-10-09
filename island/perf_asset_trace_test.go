package island

import (
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/game"
	"m31labs.dev/gosx/scene"
)

func TestPerfAssetRendererObservedBootstrap(t *testing.T) {
	for _, shape := range []string{"enhanced", "island", "monolith"} {
		t.Run(shape, func(t *testing.T) {
			r, opts := perfTracePage(t, shape, "/gosx/assets")
			uses, err := r.PerfAssetUses(opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range uses.Assets {
				if a.URL != r.Summary().BootstrapPath {
					continue
				}
				opts.RuntimeFetches = []PerfRuntimeFetch{{URL: a.URL, Phase: "startup"}}
				observed, err := r.PerfAssetUses(opts)
				if err != nil {
					t.Fatalf("observed bootstrap: %v", err)
				}
				if slices.Contains(perfAssetByID(t, observed, a.ID).Dependencies, a.ID) {
					t.Fatal("bootstrap depends on itself")
				}
				return
			}
			t.Fatal("selected bootstrap absent from graph")
		})
	}
}

func TestPerfAssetRendererObservedPublicURL(t *testing.T) {
	for _, base := range []string{"/demo", "/a/b"} {
		t.Run(base, func(t *testing.T) {
			r, opts := perfTracePage(t, "enhanced", "/gosx/assets")
			r.SetBasePath(base)
			uses, err := r.PerfAssetUses(opts)
			if err != nil {
				t.Fatal(err)
			}
			url := perfAssetByID(t, uses, "framework/runtime/navigation.js").URL
			opts.RuntimeFetches = []PerfRuntimeFetch{{URL: url, Phase: "after-ready"}}
			observed, err := r.PerfAssetUses(opts)
			if err != nil {
				t.Fatalf("emitted public URL: %v", err)
			}
			if a := perfAssetByID(t, observed, "framework/runtime/navigation.js"); a.URL != url || a.Phase != "after-ready" {
				t.Fatalf("public identity changed: %+v", a)
			}
			opts.RuntimeFetches[0].URL = strings.TrimPrefix(url, base)
			if _, err := r.PerfAssetUses(opts); err == nil {
				t.Fatal("unprefixed URL accepted as a public identity")
			}
		})
	}
}

// The fixture pages exercise each runtime selection, including both rendering
// backends, shared programs, lite/monolith fallbacks and preview compatibility.
func perfTracePage(t *testing.T, shape, assetBase string) (*Renderer, PerfAssetOptions) {
	t.Helper()
	r, m := perfAssetRendererFixture(t)
	if err := r.ApplyBuildManifest(m, assetBase); err != nil {
		t.Fatal(err)
	}
	opts := PerfAssetOptions{RuntimeFetches: []PerfRuntimeFetch{}}
	shared := strings.HasSuffix(shape, "-shared")
	switch shape {
	case "static":
	case "enhanced":
		r.EnableBootstrap()
	case "island", "monolith":
		r.RenderIsland("Counter", nil, gosx.Text(""))
		if shape == "monolith" {
			r.bootstrapRuntimePath = ""
		}
	case "compute":
		if _, err := r.RegisterComputeIsland(ComputeIslandConfig{Name: "Counter"}); err != nil {
			t.Fatal(err)
		}
	case "engine-js", "engine-shared", "media-js", "media-shared":
		cfg := engine.Config{Name: "Example", Kind: engine.KindWorker}
		if strings.HasPrefix(shape, "media-") {
			cfg.Kind = engine.KindVideo
		}
		if shared {
			cfg.Runtime = engine.Runtime("shared")
		}
		r.RenderEngine(cfg, gosx.Text(""))
	case "scene-js", "scene-shared", "webgl-js", "webgl-shared", "game-js", "game-shared":
		cfg := (scene.Props{}).EngineConfig()
		if strings.HasPrefix(shape, "game-") {
			cfg = game.New(game.Config{Profile: game.InteractiveProfile(), Scene: func(*game.Context) scene.Props {
				return scene.Props{}
			}}).EngineConfig()
		}
		if shared {
			cfg.Runtime = engine.Runtime("shared")
		}
		r.RenderEngine(cfg, gosx.Text(""))
		opts.Backend = "webgpu"
		if strings.HasPrefix(shape, "webgl-") {
			opts.Backend = "webgl2"
		}
	case "controller":
		r.RegisterController(controller.Config{Storage: &controller.Storage{}})
	case "preview":
		EnablePreviewBootstrap()
	default:
		t.Fatalf("unknown fixture shape %q", shape)
	}
	return r, opts
}

func TestPerfAssetRendererObservedGraphRoundTrip(t *testing.T) {
	const seed = 52903
	shapes := []string{"static", "enhanced", "island", "compute", "engine-js", "engine-shared", "media-js", "media-shared",
		"scene-js", "scene-shared", "webgl-js", "webgl-shared", "game-js", "game-shared", "controller", "monolith", "preview"}
	rng := rand.New(rand.NewSource(seed))
	cells, urls, traces := 0, 0, 0
	for _, shape := range shapes {
		for _, base := range []string{"", "/demo", "/a/b"} {
			for _, assetBase := range []string{"/gosx/assets", "/assets"} {
				t.Run(shape+base+assetBase, func(t *testing.T) {
					r, opts := perfTracePage(t, shape, assetBase)
					r.SetBasePath(base)
					baseline, err := r.PerfAssetUses(opts)
					if err != nil {
						t.Fatal(err)
					}
					cells++
					urls += len(baseline.Assets)
					bootstrap, sceneBase := "", ""
					for _, a := range baseline.Assets {
						if a.URL == base+r.Summary().BootstrapPath {
							bootstrap = a.ID
						}
						if r.Summary().BootstrapFeatureScene3DPath != "" && a.URL == base+r.Summary().BootstrapFeatureScene3DPath {
							sceneBase = a.ID
						}
					}
					if bootstrap == "" {
						t.Fatal("fixture bootstrap absent")
					}
					check := func(trace []PerfRuntimeFetch) *buildmanifest.PerfAssetUses {
						t.Helper()
						traces++
						observedOpts := opts
						observedOpts.RuntimeFetches = trace
						got, err := r.PerfAssetUses(observedOpts)
						if err != nil {
							t.Fatalf("trace %+v: %v", trace, err)
						}
						assertPerfTraceClosure(t, baseline, got, trace, bootstrap, sceneBase)
						return got
					}
					for _, phase := range []string{"startup", "after-ready"} {
						all := make([]PerfRuntimeFetch, 0, len(baseline.Assets))
						for _, a := range baseline.Assets {
							fetch := PerfRuntimeFetch{URL: a.URL, Phase: phase}
							check([]PerfRuntimeFetch{fetch})
							all = append(all, fetch)
						}
						want := check(all)
						for shuffle := 0; shuffle < 3; shuffle++ {
							rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
							if got := check(all); !reflect.DeepEqual(got, want) {
								t.Fatal("uniform trace depends on order")
							}
						}
					}
					mixed := make([]PerfRuntimeFetch, 0, len(baseline.Assets)*2)
					for i, a := range baseline.Assets {
						phase := "after-ready"
						if i%2 == 0 {
							phase = "startup"
						}
						mixed = append(mixed, PerfRuntimeFetch{URL: a.URL, Phase: phase})
						if i%3 == 0 {
							mixed = append(mixed, PerfRuntimeFetch{URL: a.URL, Phase: "after-ready"})
						}
					}
					want := check(mixed)
					for shuffle := 0; shuffle < 5; shuffle++ {
						rng.Shuffle(len(mixed), func(i, j int) { mixed[i], mixed[j] = mixed[j], mixed[i] })
						if got := check(mixed); !reflect.DeepEqual(got, want) {
							t.Fatal("mixed trace depends on order")
						}
					}
					again, err := r.PerfAssetUses(opts)
					if err != nil || !reflect.DeepEqual(again, baseline) {
						t.Fatalf("trace mutated renderer: %v", err)
					}
				})
			}
		}
	}
	t.Logf("seed=%d cells=%d emitted URLs=%d traces=%d", seed, cells, urls, traces)
}

func assertPerfTraceClosure(t *testing.T, baseline, got *buildmanifest.PerfAssetUses, trace []PerfRuntimeFetch, bootstrap, sceneBase string) {
	t.Helper()
	if err := (&buildmanifest.Manifest{PerfAssetUses: got}).ValidatePerfAssetUses(); err != nil {
		t.Fatalf("invalid observed graph: %v", err)
	}
	want := clonePerfAssetUses(baseline)
	byID, byURL := map[string]int{}, map[string]int{}
	for i, a := range want.Assets {
		byID[a.ID], byURL[a.URL] = i, i
	}
	earlier := func(a, b string) string {
		if a == "startup" || b == "startup" {
			return "startup"
		}
		if a == "after-ready" || b == "after-ready" {
			return "after-ready"
		}
		return "dormant"
	}
	for _, fetch := range trace {
		a := &want.Assets[byURL[fetch.URL]]
		a.Phase = earlier(a.Phase, fetch.Phase)
		if !strings.HasPrefix(a.ID, "framework/runtime/") {
			continue
		}
		deps := []string{bootstrap}
		if strings.HasPrefix(a.ID, "framework/runtime/bootstrap-feature-scene3d-") {
			if sceneBase != "" {
				deps[0] = sceneBase
			}
			if strings.HasSuffix(a.ID, "-timeline.js") || strings.HasSuffix(a.ID, "-particle-burst.js") {
				deps = append(deps, "framework/runtime/bootstrap-feature-scene3d-command.js")
			}
			if strings.HasSuffix(a.ID, "-particle-burst.js") {
				deps = append(deps, "framework/runtime/bootstrap-feature-scene3d-compute.js")
			}
		}
		for _, id := range deps {
			if id != a.ID && !slices.Contains(a.Dependencies, id) {
				a.Dependencies = append(a.Dependencies, id)
			}
		}
	}
	// Solve the prerequisite closure to a fixed point, independently of the
	// producer's recursive activation and observed-request order.
	for changed := true; changed; {
		changed = false
		for _, a := range want.Assets {
			for _, id := range a.Dependencies {
				dep := &want.Assets[byID[id]]
				if phase := earlier(dep.Phase, a.Phase); phase != dep.Phase {
					dep.Phase, changed = phase, true
				}
			}
		}
	}
	if len(got.Assets) != len(want.Assets) {
		t.Fatal("trace changed inventory size")
	}
	for i, a := range got.Assets {
		w := want.Assets[i]
		if a.ID != w.ID || a.URL != w.URL || a.SHA256 != w.SHA256 || a.Owner != w.Owner || a.Kind != w.Kind || a.Phase != w.Phase {
			t.Fatalf("identity/phase: got %+v, want %+v", a, w)
		}
		slices.Sort(w.Dependencies)
		if slices.Contains(a.Dependencies, a.ID) || !reflect.DeepEqual(a.Dependencies, w.Dependencies) {
			t.Fatalf("dependencies for %s: got %v, want %v", a.ID, a.Dependencies, w.Dependencies)
		}
	}
}

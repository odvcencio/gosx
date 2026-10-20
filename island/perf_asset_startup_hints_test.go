package island

import (
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/hydrate"
)

var updateStartupHints = flag.Bool("update-startup-hints", false, "record exact startup asset hint violations; the base ratchet still forbids additions")

//go:generate go test -run ^TestPerfAssetStartupHeadHints$ -args -update-startup-hints

const startupHintAllowlistPath = "../perf/budgets/startup-assets-allowlist.json"

type startupHintAllowlist struct {
	Schema     string   `json:"schema"`
	Note       string   `json:"note"`
	Violations []string `json:"violations"`
}

// These are discovery edges, not load durations. A startup asset that waits
// for a JavaScript loader needs its own preload/modulepreload in the head.
// Use stable asset IDs and dependency IDs so bundle hashes cannot hide a new
// waterfall. Direct head scripts are deliberately not substituted for hints.
func startupHintViolations(uses *buildmanifest.PerfAssetUses, refs []PerfHeadRef) []string {
	hinted := map[string]bool{}
	for _, ref := range refs {
		if ref.Tag != "link" {
			continue
		}
		for _, rel := range strings.Fields(strings.ToLower(ref.Rel)) {
			if rel == "preload" || rel == "modulepreload" {
				hinted[ref.URL] = true
			}
		}
	}
	byID := map[string]buildmanifest.PerfAssetUse{}
	for _, asset := range uses.Assets {
		byID[asset.ID] = asset
	}
	found := map[string]bool{}
	for _, asset := range uses.Assets {
		if asset.Phase != "startup" || hinted[asset.URL] {
			continue
		}
		for _, dependency := range asset.Dependencies {
			if byID[dependency].Kind == "js" {
				found[asset.ID+" <- "+dependency] = true
			}
		}
	}
	out := make([]string, 0, len(found))
	for edge := range found {
		out = append(out, edge)
	}
	sort.Strings(out)
	return out
}

func TestPerfAssetStartupHeadHints(t *testing.T) {
	// Cover disabled gates as well as individual and combined mount gates. The
	// fixture supplies verified asset identities; Renderer supplies the actual
	// head, backend admission, startup phases and JavaScript dependencies.
	scenes := []struct{ name, props string }{
		{"default", `{}`},
		{"zoom", `{"controlZoom":true}`},
		{"walk", `{"controls":"first-person","walk":{}}`},
		{"vessel", `{"vessel":{"nodeId":"boat"}}`},
		{"particles", `{"scene":{"computeParticles":[{"id":"dust","count":1}]}}`},
		{"combined", `{"controlZoom":true,"controls":"first-person","walk":{},"vessel":{"nodeId":"boat"},"scene":{"computeParticles":[{"id":"dust","count":1}],"labels":[{"id":"caption","text":"Boat"}]}}`},
	}
	backends := []struct {
		name, backend, cap string
		gpu                bool
	}{
		{"both-webgpu", "webgpu", "", true},
		{"both-webgl-api-absent", "webgl2", "", false},
		{"both-webgl-api-present", "webgl2", "", true},
		{"webgpu-only", "webgpu", "webgpu", true},
		{"webgl-only", "webgl2", "webgl", false},
	}
	violations := []string{}
	for _, variant := range []string{"selective", "lite", "monolith", "scene-monolith"} {
		for _, scene := range scenes {
			for _, backend := range backends {
				name := variant + "/" + scene.name + "/" + backend.name
				t.Run(name, func(t *testing.T) {
					for _, base := range []string{"", "/demo"} {
						r, _ := perfAssetRendererFixture(t)
						switch variant {
						case "lite":
							r.bootstrapRuntimePath = ""
						case "monolith":
							r.bootstrapRuntimePath, r.bootstrapLitePath = "", ""
						case "scene-monolith":
							r.bootstrapRuntimePath, r.bootstrapLitePath, r.bootstrapFeatureScene3dPath = "", "", ""
						}
						r.SetBasePath(base)
						props := map[string]any{}
						if err := json.Unmarshal([]byte(scene.props), &props); err != nil {
							t.Fatal(err)
						}
						if backend.cap != "" {
							props["backendCaps"] = map[string]any{"capable": []string{backend.cap}}
						}
						data, err := json.Marshal(props)
						if err != nil {
							t.Fatal(err)
						}
						r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: data}, gosx.Text(""))
						got := collectStartupHintViolations(t, r, PerfAssetOptions{Backend: backend.backend, NavigatorGPU: &backend.gpu, RuntimeFetches: []PerfRuntimeFetch{}})
						pathCase := "root"
						if base != "" {
							pathCase = "prefixed"
						}
						for _, edge := range got {
							violations = append(violations, name+"/"+pathCase+"/"+edge)
						}
					}

				})
			}
		}
	}
	// Non-scene startup owners must obey the same discovery rule.
	for _, feature := range []string{"island", "shared-engine", "controller-input", "hub", "text"} {
		r, _ := perfAssetRendererFixture(t)
		switch feature {
		case "island":
			r.RenderIsland("Counter", nil, gosx.Text("0"))
		case "shared-engine":
			r.RenderEngine(engine.Config{Name: "Example", Kind: engine.KindWorker, Runtime: engine.RuntimeShared}, gosx.Text(""))
		case "controller-input":
			r.RegisterController(controller.Config{Name: "fixture", Storage: &controller.Storage{}})
		case "hub":
			r.BindHub("updates", "/gosx/hub/updates", []hydrate.HubBinding{})
		case "text":
			r.EnableBootstrap()
			// The lite forwarder requests its verified compatibility body.
			for i := range r.perfAssets.Assets {
				if r.perfAssets.Assets[i].ID == "framework/runtime/bootstrap-feature-textlayout.js" {
					r.perfAssets.Assets[i].URL = "/gosx/bootstrap-feature-textlayout.js"
				}
			}
		}
		for _, edge := range collectStartupHintViolations(t, r, PerfAssetOptions{TextLayout: feature == "text", RuntimeFetches: []PerfRuntimeFetch{}}) {
			violations = append(violations, "selective/"+feature+"/"+edge)
		}
	}
	sort.Strings(violations)
	allowlist := startupHintAllowlist{Schema: "gosx.startup-asset-hints/v1", Note: "Existing startup assets without preload/modulepreload despite a JavaScript dependency. Keys include scenario, asset ID and dependency ID. Remove resolved violations; additions are forbidden by the base-branch ratchet.", Violations: violations}
	if *updateStartupHints {
		data, err := json.MarshalIndent(allowlist, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(startupHintAllowlistPath, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	allowed := readStartupHintAllowlist(t, startupHintAllowlistPath)
	added, stale := startupHintSetChanges(allowed.Violations, violations)
	for _, edge := range added {
		t.Errorf("late-discovered startup asset: %s; add a head preload/modulepreload", edge)
	}
	for _, edge := range stale {
		t.Errorf("resolved startup asset: %s; remove it from the allowlist", edge)
	}
	t.Logf("%d current startup discovery violations (covering root and prefixed base paths)", len(violations))
}

func collectStartupHintViolations(t *testing.T, r *Renderer, opts PerfAssetOptions) []string {
	t.Helper()
	uses, err := r.PerfAssetUses(opts)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := DefaultPerfHeadScanner(gosx.RenderHTML(gosx.Fragment(r.PreloadHints(), r.PageHead())))
	if err != nil {
		t.Fatal(err)
	}
	return startupHintViolations(uses, refs)
}

func readStartupHintAllowlist(t *testing.T, path string) startupHintAllowlist {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return parseStartupHintAllowlist(t, data)
}

func parseStartupHintAllowlist(t *testing.T, data []byte) startupHintAllowlist {
	t.Helper()
	var list startupHintAllowlist
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if list.Schema != "gosx.startup-asset-hints/v1" || list.Violations == nil || !sort.StringsAreSorted(list.Violations) {
		t.Fatal("invalid startup asset hint allowlist")
	}
	for i, edge := range list.Violations {
		if strings.TrimSpace(edge) == "" || i > 0 && edge == list.Violations[i-1] {
			t.Fatal("empty or duplicate startup hint allowlist entry")
		}
	}
	return list
}

func startupHintSetChanges(base, head []string) (added, removed []string) {
	b, h := map[string]bool{}, map[string]bool{}
	for _, edge := range base {
		b[edge] = true
	}
	for _, edge := range head {
		h[edge] = true
		if !b[edge] {
			added = append(added, edge)
		}
	}
	for _, edge := range base {
		if !h[edge] {
			removed = append(removed, edge)
		}
	}
	return added, removed
}

func TestPerfAssetStartupHintAllowlistOnlyShrinks(t *testing.T) {
	ref := os.Getenv("PERF_STARTUP_BASE_REF")
	if ref == "" {
		t.Skip("base comparison runs in js-tests with PERF_STARTUP_BASE_REF")
	}
	path := "perf/budgets/startup-assets-allowlist.json"
	tree, err := exec.Command("git", "ls-tree", ref, "--", path).CombinedOutput()
	if err != nil {
		t.Fatalf("verify base tree: %s: %v", tree, err)
	}
	if len(tree) == 0 {
		return
	} // Introduction is allowed only after verifying the base tree.
	data, err := exec.Command("git", "show", ref+":"+path).Output()
	if err != nil {
		t.Fatal(err)
	}
	base := parseStartupHintAllowlist(t, data)
	head := readStartupHintAllowlist(t, startupHintAllowlistPath)
	added, _ := startupHintSetChanges(base.Violations, head.Violations)
	for _, edge := range added {
		t.Errorf("startup hint allowlist grew: %s", edge)
	}
}

func TestPerfAssetStartupHintRule(t *testing.T) {
	uses := &buildmanifest.PerfAssetUses{Assets: []buildmanifest.PerfAssetUse{
		{ID: "loader", URL: "/loader.js", Kind: "js", Phase: "startup"},
		{ID: "late", URL: "/late.js", Kind: "js", Phase: "startup", Dependencies: []string{"loader"}},
		{ID: "lazy", URL: "/lazy.js", Kind: "js", Phase: "after-ready", Dependencies: []string{"loader"}},
		{ID: "data", URL: "/data.wasm", Kind: "wasm", Phase: "startup", Dependencies: []string{"loader"}},
	}}
	if got := startupHintViolations(uses, nil); !reflect.DeepEqual(got, []string{"data <- loader", "late <- loader"}) {
		t.Fatal(got)
	}
	for _, rel := range []string{"preload", "modulepreload"} {
		refs := []PerfHeadRef{{Tag: "link", URL: "/late.js", Rel: rel}, {Tag: "link", URL: "/data.wasm", Rel: "preload", As: "fetch"}}
		if got := startupHintViolations(uses, refs); len(got) != 0 {
			t.Fatal(got)
		}
	}
	for _, refs := range [][]PerfHeadRef{{{Tag: "link", URL: "/late.js", Rel: "prefetch"}}, {{Tag: "script", URL: "/late.js"}}, {{Tag: "link", URL: "/wrong.js", Rel: "preload"}}} {
		if got := startupHintViolations(uses, refs); len(got) != 2 {
			t.Fatal(got)
		}
	}
	added, removed := startupHintSetChanges([]string{"old"}, []string{"new"})
	if !reflect.DeepEqual(added, []string{"new"}) || !reflect.DeepEqual(removed, []string{"old"}) {
		t.Fatal(added, removed)
	}
	added, _ = startupHintSetChanges([]string{"old", "new"}, []string{"old"})
	if len(added) != 0 {
		t.Fatal("ratchet rejected a shrinking allowlist")
	}
}

package docs

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	gosxformat "m31labs.dev/gosx/format"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
)

func TestDocSceneFeatureRouteSelection(t *testing.T) {
	expected := []string{
		"/docs/getting-started",
		"/docs/compiler",
		"/docs/routing",
		"/docs/runtime",
		"/docs/streaming",
		"/docs/islands",
		"/docs/signals",
		"/docs/hubs",
		"/docs/engines",
		"/docs/motion",
		"/docs/deployment",
	}
	if len(docSceneSpecs) != len(expected) {
		t.Fatalf("selected route count = %d, want %d", len(docSceneSpecs), len(expected))
	}
	for index, routePath := range expected {
		if docSceneSpecs[index].Route != routePath {
			t.Fatalf("selected route %d = %q, want %q", index, docSceneSpecs[index].Route, routePath)
		}
		feature, ok := DocSceneFeatureForRoute(routePath + "/?preview=1")
		if !ok {
			t.Fatalf("selected route %q has no scene feature", routePath)
		}
		if feature.Route != routePath || feature.Slug == "" || feature.SurfaceID == "" || feature.HeadingID == "" {
			t.Fatalf("selected route %q returned incomplete feature %#v", routePath, feature)
		}
		if !strings.HasPrefix(feature.InteractionHint, "Pointer interaction only:") ||
			!strings.Contains(feature.InteractionHint, "drag") ||
			!strings.Contains(feature.InteractionHint, "wheel or pinch") {
			t.Errorf("selected route %q does not disclose its pointer-only controls: %q", routePath, feature.InteractionHint)
		}
	}
	for _, routePath := range []string{"/docs/auth", "/docs/forms", "/docs/images", "/docs/scene3d", "/demos"} {
		if _, ok := DocSceneFeatureForRoute(routePath); ok {
			t.Fatalf("non-conceptual route %q unexpectedly selected a docs scene", routePath)
		}
	}
}

func TestDocSceneFactoriesStayDeterministicAndWithinBudget(t *testing.T) {
	surfaceIDs := make(map[string]struct{}, len(docSceneSpecs))
	for _, spec := range docSceneSpecs {
		first, ok := DocSceneFeatureForRoute(spec.Route)
		if !ok {
			t.Fatalf("route %q has no feature", spec.Route)
		}
		second, _ := DocSceneFeatureForRoute(spec.Route)
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("route %q scene factory is not deterministic", spec.Route)
		}
		if _, exists := surfaceIDs[first.SurfaceID]; exists {
			t.Fatalf("route %q reuses surface id %q", spec.Route, first.SurfaceID)
		}
		surfaceIDs[first.SurfaceID] = struct{}{}

		props := first.Scene
		if props.Responsive == nil || !*props.Responsive {
			t.Errorf("route %q scene is not responsive", spec.Route)
		}
		if props.PreferWebGPU == nil || !*props.PreferWebGPU {
			t.Errorf("route %q scene does not prefer WebGPU", spec.Route)
		}
		if props.AutoRotate == nil || *props.AutoRotate {
			t.Errorf("route %q scene must not auto-rotate", spec.Route)
		}
		if props.Controls != scene.ControlOrbit || props.UnsupportedMessage == "" {
			t.Errorf("route %q scene has incomplete interaction/fallback semantics", spec.Route)
		}
		if props.Label != spec.Title || props.AriaLabel != spec.Title {
			t.Errorf("route %q canvas labels = (%q, %q), want %q", spec.Route, props.Label, props.AriaLabel, spec.Title)
		}
		if props.MaxFrameRate > 30 || props.MaxDevicePixelRatio > 1.5 || props.MaxPixels > 384000 {
			t.Errorf("route %q scene exceeds render budget: fps=%v dpr=%v pixels=%d", spec.Route, props.MaxFrameRate, props.MaxDevicePixelRatio, props.MaxPixels)
		}
		if len(props.Graph.Nodes) > docSceneMaxNodes {
			t.Errorf("route %q graph has %d nodes, budget is %d", spec.Route, len(props.Graph.Nodes), docSceneMaxNodes)
		}
		if required := props.EngineRequiredCapabilities(); len(required) != 0 {
			t.Errorf("route %q unexpectedly hard-requires capabilities %v", spec.Route, required)
		}
		for _, capability := range []string{"canvas", "webgpu", "webgl", "animation"} {
			if !slices.Contains(props.EngineCapabilities(), capability) {
				t.Errorf("route %q capability set lacks %q: %v", spec.Route, capability, props.EngineCapabilities())
			}
		}
		canonical := props.CanonicalIR()
		if err := canonical.Validate(); err != nil {
			t.Errorf("route %q emits invalid SceneIR: %v", spec.Route, err)
		}

		seenIDs := make(map[string]struct{}, len(props.Graph.Nodes))
		movingNodes := 0
		for _, node := range props.Graph.Nodes {
			id, moving := boundedDocSceneNode(t, spec.Route, node)
			if id == "" {
				t.Errorf("route %q contains a node without a stable id", spec.Route)
			}
			if !strings.HasPrefix(id, "doc-"+spec.Slug+"-") {
				t.Errorf("route %q node id %q has wrong stable prefix", spec.Route, id)
			}
			if _, exists := seenIDs[id]; exists {
				t.Errorf("route %q duplicates node id %q", spec.Route, id)
			}
			seenIDs[id] = struct{}{}
			if moving {
				movingNodes++
			}
		}
		if spec.Route == "/docs/motion" {
			if movingNodes != 1 {
				t.Errorf("motion route moving node count = %d, want 1", movingNodes)
			}
		} else if movingNodes != 0 {
			t.Errorf("route %q has %d decorative moving nodes", spec.Route, movingNodes)
		}
	}
}

func TestWithDocSceneFeaturePreservesMapLoaderDataAndBindsOnce(t *testing.T) {
	original := map[string]any{"title": "Runtime", "count": 3}
	bindingCalls := 0
	wrapped := withDocSceneFeature(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return original, nil
		},
		Bindings: func(ctx *route.RouteContext, page route.FilePage, data any) route.FileTemplateBindings {
			bindingCalls++
			return route.FileTemplateBindings{
				Values: map[string]any{"existing": "kept"},
			}
		},
	})
	page := route.FilePage{RoutePath: "/docs/runtime"}
	data, err := wrapped.Load(&route.RouteContext{}, page)
	if err != nil {
		t.Fatal(err)
	}
	values, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("loader data type changed: got %T", data)
	}
	values["identity-probe"] = true
	if original["identity-probe"] != true {
		t.Fatal("loader-owned map identity changed")
	}
	delete(values, "identity-probe")
	if _, mutated := original[docSceneBindingKey]; mutated {
		t.Fatal("scene binding leaked into loader-owned data")
	}
	bindings := wrapped.Bindings(&route.RouteContext{}, page, data)
	if bindingCalls != 1 {
		t.Fatalf("original bindings called %d times, want 1", bindingCalls)
	}
	if bindings.Values["existing"] != "kept" {
		t.Fatalf("scene bindings replaced existing bindings: %#v", bindings.Values)
	}
	if bound, ok := bindings.Values[docSceneBindingKey].(DocSceneFeature); !ok || bound.Route != page.RoutePath {
		t.Fatalf("scene binding = %#v", bindings.Values[docSceneBindingKey])
	}
}

func TestWithDocSceneFeaturePreservesNonMapAndUnselectedData(t *testing.T) {
	type payload struct {
		Title string
	}
	original := &payload{Title: "typed"}
	wrapped := withDocSceneFeature(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return original, nil
		},
	})

	selected := route.FilePage{RoutePath: "/docs/runtime"}
	data, err := wrapped.Load(&route.RouteContext{}, selected)
	if err != nil {
		t.Fatal(err)
	}
	if data != original {
		t.Fatalf("selected non-map loader data changed identity: got %T %#v", data, data)
	}
	if _, ok := wrapped.Bindings(&route.RouteContext{}, selected, data).Values[docSceneBindingKey].(DocSceneFeature); !ok {
		t.Fatal("selected non-map data did not receive the independent scene binding")
	}

	unselected := route.FilePage{RoutePath: "/docs/forms"}
	data, err = wrapped.Load(&route.RouteContext{}, unselected)
	if err != nil {
		t.Fatal(err)
	}
	if data != original {
		t.Fatalf("unselected loader data changed identity: got %T %#v", data, data)
	}
	if _, exists := wrapped.Bindings(&route.RouteContext{}, unselected, data).Values[docSceneBindingKey]; exists {
		t.Fatal("unselected route received a scene binding")
	}
}

func TestWithDocSceneFeaturePreservesLoaderErrors(t *testing.T) {
	want := errors.New("load failed")
	wrapped := withDocSceneFeature(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return nil, want
		},
	})
	if _, err := wrapped.Load(&route.RouteContext{}, route.FilePage{RoutePath: "/docs/runtime"}); !errors.Is(err, want) {
		t.Fatalf("wrapped loader error = %v, want %v", err, want)
	}
}

func TestDocSceneRenderColorsAreRuntimeParserValid(t *testing.T) {
	validColor := regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	for name, color := range map[string]string{
		"canvas":      docSceneCanvas,
		"text":        docSceneText,
		"secondary":   docSceneSecondary,
		"accent":      docSceneAccent,
		"accent-line": docSceneAccentLine,
	} {
		if !validColor.MatchString(color) {
			t.Errorf("%s color %q is not accepted by the Scene3D runtime parser", name, color)
		}
	}
	for _, spec := range docSceneSpecs {
		feature, _ := DocSceneFeatureForRoute(spec.Route)
		encoded, err := json.Marshal(feature.Scene)
		if err != nil {
			t.Fatalf("marshal %q scene: %v", spec.Route, err)
		}
		if bytes.Contains(encoded, []byte("var(--")) {
			t.Errorf("route %q scene payload contains unsupported CSS color variable: %s", spec.Route, encoded)
		}
	}
}

func TestDocSceneRouteCapabilitiesStayLocal(t *testing.T) {
	for _, spec := range docSceneSpecs {
		path := filepath.Join("docs", spec.Slug, "page.gsx")
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read selected route %q: %v", spec.Route, err)
		}
		for _, contract := range []string{
			`class="doc-scene"`,
			`respectReducedMotion={true}`,
			`data-gosx-link="true"`,
			`aria-labelledby={docScene.HeadingID}`,
		} {
			if !bytes.Contains(source, []byte(contract)) {
				t.Errorf("selected route %q is missing %q", spec.Route, contract)
			}
		}
		program := compileDocSceneSource(t, path)
		if count := countIRTag(program, "Scene3D"); count != 1 {
			t.Errorf("selected route %q Scene3D node count = %d, want 1", spec.Route, count)
		}
	}
	for _, path := range []string{
		filepath.Join("docs", "layout.gsx"),
		filepath.Join("docs", "auth", "page.gsx"),
		filepath.Join("docs", "forms", "page.gsx"),
	} {
		program := compileDocSceneSource(t, path)
		if count := countIRTag(program, "Scene3D"); count != 0 {
			t.Errorf("payload-free source %q Scene3D node count = %d, want 0", path, count)
		}
	}

	layout, err := os.ReadFile(filepath.Join("docs", "layout.css"))
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{
		`.doc-scene__mount[data-gosx-scene3d-renderer]::after`,
		`attr(data-gosx-scene3d-renderer)`,
		`attr(data-gosx-scene3d-renderer-fallback)`,
		`@media (max-width: 767px)`,
	} {
		if !bytes.Contains(layout, []byte(contract)) {
			t.Errorf("docs Scene3D layout is missing %q", contract)
		}
	}
}

func TestDocsLayoutAndSceneStagesCompileAndStayFormatted(t *testing.T) {
	paths := []string{filepath.Join("docs", "layout.gsx")}
	for _, spec := range docSceneSpecs {
		paths = append(paths, filepath.Join("docs", spec.Slug, "page.gsx"))
	}
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if _, err := gosx.Compile(source); err != nil {
			t.Errorf("compile %s: %v", path, err)
		}
		formatted, err := gosxformat.Source(source)
		if err != nil {
			t.Errorf("format %s: %v", path, err)
			continue
		}
		if !bytes.Equal(formatted, source) {
			t.Errorf("%s is not canonical gosx fmt output", path)
		}
	}
}

func compileDocSceneSource(t *testing.T, path string) *ir.Program {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	program, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile %s: %v", path, err)
	}
	return program
}

func countIRTag(program *ir.Program, tag string) int {
	count := 0
	for _, node := range program.Nodes {
		if node.Tag == tag {
			count++
		}
	}
	return count
}

func boundedDocSceneNode(t *testing.T, routePath string, node scene.Node) (string, bool) {
	t.Helper()
	switch value := node.(type) {
	case scene.AmbientLight:
		return value.ID, false
	case scene.DirectionalLight:
		return value.ID, false
	case scene.Mesh:
		if material, ok := value.Material.(scene.StandardMaterial); ok {
			if material.Texture != "" || material.NormalMap != "" || material.RoughnessMap != "" || material.MetalnessMap != "" || material.EmissiveMap != "" {
				t.Errorf("route %q mesh %q depends on external texture assets", routePath, value.ID)
			}
		}
		moving := value.Spin != (scene.Euler{}) || value.Drift != (scene.Vector3{})
		return value.ID, moving
	default:
		t.Fatalf("route %q uses unbounded or unsupported docs node type %T", routePath, node)
		return "", false
	}
}

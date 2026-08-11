package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

func TestShippedSceneBase64LeafIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "bootstrap-src/11-scene-base64.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped Scene3D base64 leaf: %v", err)
	}
	if !strings.Contains(string(body), "@returns {Uint8Array}") || !strings.Contains(string(body), "@param {string} str") {
		t.Fatalf("%s does not carry the typed sceneBase64Decode JSDoc contract", rel)
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-feature-scene3d.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed base64 leaf: %v", name, err)
		}
		if strings.Contains(built.code, "@returns {Uint8Array}") || strings.Contains(built.code, "@param {string} str") {
			t.Errorf("%s retained the source-only TypeScript JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedSceneIRSchemaLeafIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "bootstrap-src/15-scene-ir-schema.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped SceneIR schema leaf: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} SceneIR",
		"@property {number} version",
		"@typedef {object} SceneRenderBundle",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry schema contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-feature-scene3d.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed SceneIR schema leaf: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {object} SceneIR") || strings.Contains(built.code, "@property {number} version") {
			t.Errorf("%s retained the source-only SceneIR JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedSceneMathLeafIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "bootstrap-src/11-scene-math.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped Scene3D math leaf: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {{x: number, y: number, z: number}} ScenePoint",
		"@typedef {{origin: ScenePoint, dir: ScenePoint}} SceneRay",
		"function sceneProjectPoint",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry math contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-feature-scene3d.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed Scene3D math leaf: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {{x: number, y: number, z: number}} ScenePoint") {
			t.Errorf("%s retained the source-only ScenePoint JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedSceneDecompressCodecIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "bootstrap-src/11a-scene-decompress.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped Scene3D decompress codec: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} SceneCompressedChunk",
		"@property {number} bitWidth",
		"function sceneDecompressArray",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry codec contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-feature-scene3d-decompress.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed Scene3D decompress codec: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {object} SceneCompressedChunk") {
			t.Errorf("%s retained the source-only SceneCompressedChunk JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedSceneDrawPlanLeafIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "bootstrap-src/15-scene-draw-plan.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped Scene3D draw plan leaf: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} SceneWorldDrawPlan",
		"@property {Float32Array} staticOpaquePositions",
		"function buildSceneWorldDrawPlan",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry draw-plan contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-feature-scene3d.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed Scene3D draw-plan leaf: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {object} SceneWorldDrawPlan") {
			t.Errorf("%s retained the source-only SceneWorldDrawPlan JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedScenePlannerIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "bootstrap-src/15b-scene-planner.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped Scene3D planner: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} ScenePlannerTelemetry",
		"@typedef {object} PreparedScene",
		"function prepareScene",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry planner contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-feature-scene3d.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed Scene3D planner: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {object} ScenePlannerTelemetry") {
			t.Errorf("%s retained the source-only ScenePlannerTelemetry JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedSceneMaterialNormalizerIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "bootstrap-src/13-scene-material.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped Scene3D material normalizer: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} SceneMaterialProfile",
		"@property {string} renderPass",
		"function sceneObjectMaterialProfile",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry material normalization contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-feature-scene3d.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed Scene3D material normalizer: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {object} SceneMaterialProfile") {
			t.Errorf("%s retained the source-only SceneMaterialProfile JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedBrowserActionHostIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "../runtime/host/actions.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped browser action host: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} GoSXActionResult",
		"@typedef {object} GoSXActionEventDetail",
		"function actionFetch",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry browser action-host contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-lite.js", "bootstrap-runtime.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed browser action host: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {object} GoSXActionResult") {
			t.Errorf("%s retained the source-only GoSXActionResult JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedBrowserRegionHostIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "../runtime/host/regions.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped browser region host: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} GoSXRegionRecord",
		"@property {string[]} onEvents",
		"function fetchRegion",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry browser region-host contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-lite.js", "bootstrap-runtime.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed browser region host: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {object} GoSXRegionRecord") {
			t.Errorf("%s retained the source-only GoSXRegionRecord JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedBrowserControllerHostIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "../runtime/host/controllers.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped browser controller host: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} GoSXControllerRecord",
		"@property {Record<string, string>} outputs",
		"function mountController",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry browser controller-host contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-feature-controllers.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed browser controller host: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {object} GoSXControllerRecord") {
			t.Errorf("%s retained the source-only GoSXControllerRecord JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedBrowserDOMHostIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "../runtime/host/dom.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped browser DOM host: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} GoSXDOMScope",
		"@typedef {object} GoSXDOMLifecycleResult",
		"function replaceRuntimeContent",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry browser DOM-host contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-lite.js", "bootstrap-runtime.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed browser DOM host: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {object} GoSXDOMScope") {
			t.Errorf("%s retained the source-only GoSXDOMScope JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedBrowserFacadeHostIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "../runtime/host/facade.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped browser facade host: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} GoSXTransportScope",
		"@typedef {object} GoSXRuntimeSurfaceContext",
		"function gosxRequest",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry browser facade-host contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-lite.js", "bootstrap-runtime.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed browser facade host: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {object} GoSXTransportScope") {
			t.Errorf("%s retained the source-only GoSXTransportScope JSDoc contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedBrowserStreamHostIsTypedAndBuildsInItsOwners(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "../runtime/host/stream.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped browser stream host: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} GoSXStreamTemplate",
		"function consume",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry browser stream-host contract %q", rel, contract)
		}
	}

	for _, name := range []string{"bootstrap.js", "bootstrap-lite.js", "bootstrap-runtime.js"} {
		var entry output
		for _, candidate := range outputs {
			if candidate.name == name {
				entry = candidate
				break
			}
		}
		if entry.name == "" {
			t.Fatalf("chunk table has no %s", name)
		}
		built, err := buildBundle(dir, entry, "esbuild", false)
		if err != nil {
			t.Fatalf("build %s with typed browser stream host: %v", name, err)
		}
		if strings.Contains(built.code, "@typedef {object} GoSXStreamTemplate") {
			t.Errorf("%s retained the source-only GoSXStreamTemplate contract", name)
		}
		var parsed struct {
			Sources []string `json:"sources"`
		}
		if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
			t.Fatalf("decode %s source map: %v", name, err)
		}
		found := false
		for _, source := range parsed.Sources {
			if source == rel {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s source map does not retain %s: %v", name, rel, parsed.Sources)
		}
	}
}

func TestShippedBrowserNavigationHostIsTypedAndParses(t *testing.T) {
	dir := shippedClientJS(t)
	rel := "../runtime/host/navigation.ts"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read shipped browser navigation host: %v", err)
	}
	textBody := string(body)
	for _, contract := range []string{
		"@typedef {object} GoSXNavigationState",
		"@typedef {object} GoSXNavigationAPI",
		"const navigationAPI",
	} {
		if !strings.Contains(textBody, contract) {
			t.Errorf("%s does not carry browser navigation-host contract %q", rel, contract)
		}
	}
	if err := validateTypedSource(sourceFile(rel), body); err != nil {
		t.Fatalf("parse typed browser navigation host: %v", err)
	}
}

func TestShippedSceneCommandHostsAreTypedAndBuildInTheirOwners(t *testing.T) {
	dir := shippedClientJS(t)
	leaves := []struct {
		rel       string
		owners    []string
		contracts []string
	}{
		{
			rel:       "../runtime/scene3d/command-bridge.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d.js"},
			contracts: []string{"@typedef {object} GoSXScene3DCommandBridge"},
		},
		{
			rel:       "../runtime/scene3d/command-runtime.ts",
			owners:    []string{"bootstrap-feature-scene3d-command.js"},
			contracts: []string{"@typedef {object} GoSXScene3DCommandRecord"},
		},
	}

	for _, leaf := range leaves {
		leaf := leaf
		t.Run(filepath.Base(leaf.rel), func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(leaf.rel)))
			if err != nil {
				t.Fatalf("read shipped Scene3D command host %s: %v", leaf.rel, err)
			}
			textBody := string(body)
			for _, contract := range leaf.contracts {
				if !strings.Contains(textBody, contract) {
					t.Errorf("%s does not carry command-host contract %q", leaf.rel, contract)
				}
			}
			for _, name := range leaf.owners {
				var entry output
				for _, candidate := range outputs {
					if candidate.name == name {
						entry = candidate
						break
					}
				}
				if entry.name == "" {
					t.Fatalf("chunk table has no %s", name)
				}
				built, err := buildBundle(dir, entry, "esbuild", false)
				if err != nil {
					t.Fatalf("build %s with typed command host %s: %v", name, leaf.rel, err)
				}
				for _, contract := range leaf.contracts {
					if strings.Contains(built.code, contract) {
						t.Errorf("%s retained source-only command contract %q", name, contract)
					}
				}
				var parsed struct {
					Sources []string `json:"sources"`
				}
				if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
					t.Fatalf("decode %s source map: %v", name, err)
				}
				found := false
				for _, source := range parsed.Sources {
					if source == leaf.rel {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s source map does not retain %s: %v", name, leaf.rel, parsed.Sources)
				}
			}
		})
	}
}

func TestShippedSceneHostLeavesAreTypedAndBuildInTheirOwners(t *testing.T) {
	dir := shippedClientJS(t)
	leaves := []struct {
		rel       string
		owners    []string
		contracts []string
	}{
		{
			rel:       "../runtime/scene3d/dom-regions.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d.js"},
			contracts: []string{"@typedef {object} GoSXSceneDOMRegionConfig", "function sceneDOMRegionMeasure"},
		},
		{
			rel:       "../runtime/scene3d/compute.ts",
			owners:    []string{"bootstrap-feature-scene3d-compute.js"},
			contracts: []string{"@typedef {object} GoSXSceneComputeSystem", "function createSceneParticleSystem"},
		},
		{
			rel:       "../runtime/scene3d/gltf.ts",
			owners:    []string{"bootstrap-feature-scene3d-gltf.js"},
			contracts: []string{"@typedef {object} GoSXSceneModelAsset", "async function sceneLoadGLTFModel"},
		},
		{
			rel:       "../runtime/scene3d/animation.ts",
			owners:    []string{"bootstrap-feature-scene3d-animation.js"},
			contracts: []string{"@typedef {object} GoSXSceneAnimationClip", "function createSceneAnimationMixer"},
		},
		{
			rel:       "../runtime/scene3d/overlays.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d.js"},
			contracts: []string{"@typedef {object} GoSXSceneOverlayController", "function createSceneStatsOverlay"},
		},
		{
			rel:       "../runtime/scene3d/overlay-dom.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d.js"},
			contracts: []string{"@typedef {object} GoSXSceneDOMOverlayController", "function sceneLabelLayoutKey"},
		},
		{
			rel:       "../runtime/scene3d/mount-backend.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d.js"},
			contracts: []string{"@typedef {object} GoSXSceneBackendSelection", "function chooseSceneBackend"},
		},
		{
			rel:       "../runtime/scene3d/mount-quality.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d.js"},
			contracts: []string{"@typedef {object} GoSXSceneQualityState", "function sceneUpdateQualityLadder"},
		},
		{
			rel:       "../runtime/scene3d/mount-viewport.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d.js"},
			contracts: []string{"@typedef {object} GoSXSceneViewportState", "function sceneViewportFromMount"},
		},
		{
			rel:       "../runtime/scene3d/mount-controls.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d.js"},
			contracts: []string{"@typedef {object} GoSXSceneControlState", "function setupSceneBuiltInControls"},
		},
		{
			rel:       "../runtime/scene3d/mount-telemetry.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d.js"},
			contracts: []string{"@typedef {object} GoSXSceneMountTelemetry"},
		},
		{
			rel:       "../runtime/scene3d/mount.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d.js"},
			contracts: []string{"@typedef {object} GoSXSceneEngineMountContext"},
		},
		{
			rel:       "../runtime/scene3d/webgl.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d-webgl.js"},
			contracts: []string{"@typedef {object} GoSXSceneWebGLRenderer", "function createScenePBRRendererOrFallback"},
		},
		{
			rel:       "../runtime/scene3d/webgpu.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d-webgpu.js"},
			contracts: []string{"@typedef {object} GoSXSceneWebGPURenderer", "function createSceneWebGPURenderer"},
		},
		{
			rel:       "../runtime/scene3d/mount-webgl.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-scene3d.js"},
			contracts: []string{"@typedef {object} GoSXSceneWebGLMountHost", "function createSceneWebGLResult"},
		},
	}

	for _, leaf := range leaves {
		leaf := leaf
		t.Run(filepath.Base(leaf.rel), func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(leaf.rel)))
			if err != nil {
				t.Fatalf("read shipped Scene3D host leaf %s: %v", leaf.rel, err)
			}
			textBody := string(body)
			for _, contract := range leaf.contracts {
				if !strings.Contains(textBody, contract) {
					t.Errorf("%s does not carry Scene3D host contract %q", leaf.rel, contract)
				}
			}
			for _, name := range leaf.owners {
				var entry output
				for _, candidate := range outputs {
					if candidate.name == name {
						entry = candidate
						break
					}
				}
				if entry.name == "" {
					t.Fatalf("chunk table has no %s", name)
				}
				built, err := buildBundle(dir, entry, "esbuild", false)
				if err != nil {
					t.Fatalf("build %s with typed Scene3D host %s: %v", name, leaf.rel, err)
				}
				for _, contract := range leaf.contracts {
					if strings.Contains(built.code, contract) {
						t.Errorf("%s retained source-only Scene3D contract %q", name, contract)
					}
				}
				var parsed struct {
					Sources []string `json:"sources"`
				}
				if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
					t.Fatalf("decode %s source map: %v", name, err)
				}
				found := false
				for _, source := range parsed.Sources {
					if source == leaf.rel {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s source map does not retain %s: %v", name, leaf.rel, parsed.Sources)
				}
			}
		})
	}
}

func TestShippedBrowserLifecycleTailHostsAreTypedAndBuildInTheirOwners(t *testing.T) {
	dir := shippedClientJS(t)
	leaves := []struct {
		rel       string
		owners    []string
		contracts []string
		symbol    string
	}{
		{
			rel:       "../runtime/host/events.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-islands.js"},
			contracts: []string{"@typedef {object} GoSXDelegatedListener"},
			symbol:    "function setupEventDelegation",
		},
		{
			rel:       "../runtime/host/hydration.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-islands.js"},
			contracts: []string{"@typedef {object} GoSXIslandEntry"},
			symbol:    "async function hydrateIsland",
		},
		{
			rel:       "../runtime/host/disposal.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-islands.js"},
			contracts: []string{"@typedef {object} GoSXIslandRecord"},
			symbol:    "window.__gosx_dispose_island",
		},
		{
			rel:       "../runtime/host/hubs.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-hubs.js"},
			contracts: []string{"@typedef {object} GoSXHubRecord"},
			symbol:    "function connectHub",
		},
		{
			rel:       "../runtime/host/hub-disposal.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-hubs.js"},
			contracts: []string{"@typedef {object} GoSXHubRecord"},
			symbol:    "window.__gosx_disconnect_hub",
		},
		{
			rel:       "../runtime/host/engine-disposal.ts",
			owners:    []string{"bootstrap.js", "bootstrap-feature-engines.js"},
			contracts: []string{"@typedef {object} GoSXEngineRecord"},
			symbol:    "window.__gosx_dispose_engine",
		},
		{
			rel:       "../runtime/host/page-disposal.ts",
			owners:    []string{"bootstrap.js"},
			contracts: []string{"@typedef {object} GoSXPageManifest"},
			symbol:    "async function disposePage",
		},
	}

	for _, leaf := range leaves {
		leaf := leaf
		t.Run(filepath.Base(leaf.rel), func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(leaf.rel)))
			if err != nil {
				t.Fatalf("read shipped browser lifecycle host %s: %v", leaf.rel, err)
			}
			textBody := string(body)
			for _, contract := range leaf.contracts {
				if !strings.Contains(textBody, contract) {
					t.Errorf("%s does not carry lifecycle-host contract %q", leaf.rel, contract)
				}
			}
			if !strings.Contains(textBody, leaf.symbol) {
				t.Errorf("%s does not carry lifecycle-host symbol %q", leaf.rel, leaf.symbol)
			}

			for _, name := range leaf.owners {
				var entry output
				for _, candidate := range outputs {
					if candidate.name == name {
						entry = candidate
						break
					}
				}
				if entry.name == "" {
					t.Fatalf("chunk table has no %s", name)
				}
				built, err := buildBundle(dir, entry, "esbuild", false)
				if err != nil {
					t.Fatalf("build %s with typed lifecycle host %s: %v", name, leaf.rel, err)
				}
				for _, contract := range leaf.contracts {
					if strings.Contains(built.code, contract) {
						t.Errorf("%s retained source-only lifecycle contract %q", name, contract)
					}
				}
				var parsed struct {
					Sources []string `json:"sources"`
				}
				if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
					t.Fatalf("decode %s source map: %v", name, err)
				}
				found := false
				for _, source := range parsed.Sources {
					if source == leaf.rel {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s source map does not retain %s: %v", name, leaf.rel, parsed.Sources)
				}
			}
		})
	}
}

func TestESBuildLoaderFollowsSourceExtensions(t *testing.T) {
	tests := []struct {
		name  string
		entry output
		want  esbuild.Loader
	}{
		{name: "javascript", entry: chunk("runtime.js", "bootstrap-src/runtime.js"), want: esbuild.LoaderJS},
		{name: "typescript", entry: chunk("runtime.js", "bootstrap-src/runtime.ts"), want: esbuild.LoaderTS},
		{name: "mixed", entry: chunk("runtime.js", "bootstrap-src/head.js", "bootstrap-src/runtime.ts"), want: esbuild.LoaderTS},
		{name: "tsx", entry: chunk("runtime.js", "bootstrap-src/view.tsx"), want: esbuild.LoaderTSX},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := esbuildLoaderForOutput(test.entry)
			if err != nil {
				t.Fatalf("esbuildLoaderForOutput: %v", err)
			}
			if got != test.want {
				t.Fatalf("loader = %v, want %v", got, test.want)
			}
		})
	}

	if _, err := esbuildLoaderForOutput(chunk("runtime.js", "bootstrap-src/runtime.jsx")); err == nil {
		t.Fatal("esbuildLoaderForOutput accepted an unsupported extension")
	}
}

func TestBuildBundleAcceptsTypeScriptAndKeepsOriginalMapIdentity(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("10-runtime-contract.ts", `import type { ExternalContract } from "./external-contract";
export interface RuntimeContract extends ExternalContract {
  version: number;
}
function contractVersion(contract: RuntimeContract): number {
  return contract.version;
}
const contract = { version: 1 } as RuntimeContract;
globalThis.__gosx_contract_version = contractVersion(contract);
`)

	built, err := buildBundle(f.dir, chunk("typed-runtime.js", rel), "esbuild", false)
	if err != nil {
		t.Fatalf("buildBundle: %v", err)
	}
	for _, erased := range []string{"interface RuntimeContract", "ExternalContract", "import type"} {
		if strings.Contains(built.code, erased) {
			t.Errorf("built JavaScript kept TypeScript-only syntax %q", erased)
		}
	}
	if !strings.Contains(built.code, "__gosx_contract_version") {
		t.Fatal("built JavaScript lost the runtime contract assignment")
	}

	var parsed struct {
		Sources        []string `json:"sources"`
		SourcesContent []string `json:"sourcesContent"`
	}
	if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
		t.Fatalf("decode source map: %v", err)
	}
	if len(parsed.Sources) != 1 || parsed.Sources[0] != rel {
		t.Fatalf("source map sources = %v, want [%s]", parsed.Sources, rel)
	}
	if len(parsed.SourcesContent) != 1 || !strings.Contains(parsed.SourcesContent[0], "interface RuntimeContract") {
		t.Fatalf("source map lost the original TypeScript source: %v", parsed.SourcesContent)
	}
}

func TestChunkClosureErasesTypesButStillFindsMissingRuntimeValues(t *testing.T) {
	f := newFixture(t)
	closed := f.writeSource("10-closed.ts", `interface RuntimeInput { value: number }
function normalize(input: RuntimeInput): number { return input.value + 1; }
const input: RuntimeInput = { value: 41 };
globalThis.__gosx_typed_result = normalize(input);
`)
	free, err := chunkFreeIdentifiers(f.dir, chunk("closed.js", closed))
	if err != nil {
		t.Fatalf("closed TypeScript chunk: %v", err)
	}
	if len(free) != 0 {
		t.Fatalf("closed TypeScript chunk reports free identifiers %v", free)
	}

	broken := f.writeSource("20-broken.ts", `const result: number = missingRuntimeValue();
globalThis.__gosx_typed_result = result;
`)
	free, err = chunkFreeIdentifiers(f.dir, chunk("broken.js", broken))
	if err != nil {
		t.Fatalf("broken TypeScript chunk: %v", err)
	}
	if len(free) != 1 || free[0] != "missingRuntimeValue" {
		t.Fatalf("broken TypeScript chunk reports %v, want [missingRuntimeValue]", free)
	}
}

func TestChunkClosureUnderstandsTypeOnlyImportsAndValueExports(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("10-module.ts", `import type { ExternalContract } from "./external-contract";
export interface RuntimeContract extends ExternalContract { version: number }
export function contractVersion(contract: RuntimeContract): number {
  return contract.version;
}
`)

	free, err := chunkFreeIdentifiers(f.dir, chunk("module.js", rel))
	if err != nil {
		t.Fatalf("module-shaped TypeScript chunk: %v", err)
	}
	if len(free) != 0 {
		t.Fatalf("module-shaped TypeScript chunk reports free identifiers %v", free)
	}
}

func TestGotreesitterTypeScriptDiagnosticCarriesSourceRange(t *testing.T) {
	err := validateTypedSource(
		sourceFile("bootstrap-src/broken.ts"),
		[]byte("const value: number = @;\n"),
	)
	if err == nil {
		t.Fatal("validateTypedSource accepted invalid TypeScript")
	}
	for _, want := range []string{
		"bootstrap-src/broken.ts:1:",
		"-1:",
		"TypeScript syntax error",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("diagnostic does not contain %q: %v", want, err)
		}
	}
}

func TestTdewolffPathErasesTypeScriptBeforeMinifying(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("10-typed.ts", `interface RuntimeValue { count: number }
const value: RuntimeValue = { count: 2 };
globalThis.__gosx_typed_count = value.count;
`)

	built, err := buildBundle(f.dir, chunk("typed.js", rel), "tdewolff", false)
	if err != nil {
		t.Fatalf("buildBundle with tdewolff: %v", err)
	}
	if strings.Contains(built.code, "interface RuntimeValue") || strings.Contains(built.code, ": RuntimeValue") {
		t.Fatalf("tdewolff bundle kept TypeScript syntax: %s", built.code)
	}
}

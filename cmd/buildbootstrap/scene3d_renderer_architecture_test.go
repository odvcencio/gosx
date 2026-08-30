package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type rendererArchitectureBaseline struct {
	SourceSets         map[string]rendererSourceSetBaseline `json:"sourceSets"`
	FunctionDefaults   rendererSymbolMetric                 `json:"functionDefaults"`
	FunctionExceptions []rendererSymbolMetric               `json:"functionExceptions"`
	Symbols            []rendererSymbolMetric               `json:"symbols"`
}

type rendererSourceSetBaseline struct {
	Chunk            string            `json:"chunk"`
	ChunkSources     []string          `json:"chunkSources"`
	ChunkSourceRoles map[string]string `json:"chunkSourceRoles"`
	Sources          []string          `json:"sources"`
}

type rendererSymbolMetric struct {
	Source     string `json:"source"`
	Name       string `json:"name"`
	Lines      int    `json:"lines"`
	Cyclomatic int    `json:"cyclomatic"`
	Cognitive  int    `json:"cognitive"`
	MaxNesting int    `json:"maxNesting"`
	Parameters int    `json:"parameters"`
}

func loadRendererArchitectureBaseline(t *testing.T) (string, rendererArchitectureBaseline) {
	t.Helper()
	clientJS := shippedClientJS(t)
	raw, err := os.ReadFile(filepath.Join(clientJS, "testdata", "scene3d-renderer-architecture.json"))
	if err != nil {
		t.Fatalf("read renderer architecture baseline: %v", err)
	}
	var baseline rendererArchitectureBaseline
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("parse renderer architecture baseline: %v", err)
	}
	return clientJS, baseline
}

func TestRendererSourceOrderMatchesMonolithAndLazyChunks(t *testing.T) {
	_, baseline := loadRendererArchitectureBaseline(t)
	for backend, sourceSet := range baseline.SourceSets {
		var lazyRoster, lazy, monolith []string
		for _, entry := range outputs {
			var selected []string
			for _, source := range entry.sources {
				for _, expected := range sourceSet.Sources {
					if source.rel == expected {
						selected = append(selected, source.rel)
					}
				}
			}
			switch entry.name {
			case sourceSet.Chunk:
				lazy = selected
				for _, source := range entry.sources {
					lazyRoster = append(lazyRoster, source.rel)
				}
			case "bootstrap.js":
				monolith = selected
			}
		}
		if got, want := strings.Join(lazyRoster, "\x00"), strings.Join(sourceSet.ChunkSources, "\x00"); got != want {
			t.Errorf("%s lazy renderer chunk roster = %q, want %q", backend, lazyRoster, sourceSet.ChunkSources)
		}
		if got, want := strings.Join(lazy, "\x00"), strings.Join(sourceSet.Sources, "\x00"); got != want {
			t.Errorf("%s lazy renderer order = %q, want %q", backend, lazy, sourceSet.Sources)
		}
		if got, want := strings.Join(monolith, "\x00"), strings.Join(sourceSet.Sources, "\x00"); got != want {
			t.Errorf("%s monolith renderer order = %q, want %q", backend, monolith, sourceSet.Sources)
		}
		if err := validateRendererChunkRoles(backend, sourceSet); err != nil {
			t.Errorf("%s renderer chunk role contract failed: %v", backend, err)
		}
	}
}

func TestRendererChunkRoleContractRejectsSpoofedRoster(t *testing.T) {
	_, baseline := loadRendererArchitectureBaseline(t)
	sourceSet := baseline.SourceSets["webgpu"]

	invented := cloneRendererSourceSet(sourceSet)
	invented.ChunkSourceRoles["bootstrap-src/26e-feature-scene3d-webgpu-prefix.ts"] = "invented-role"
	if err := validateRendererChunkRoles("webgpu", invented); err == nil || !strings.Contains(err.Error(), "unknown governed role") {
		t.Fatalf("invented role was not rejected correctly: %v", err)
	}

	swapped := cloneRendererSourceSet(sourceSet)
	swapped.ChunkSourceRoles["bootstrap-src/26e-feature-scene3d-webgpu-prefix.ts"] = "backend-support"
	if err := validateRendererChunkRoles("webgpu", swapped); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("swapped role was not rejected correctly: %v", err)
	}

	stale := cloneRendererSourceSet(sourceSet)
	stale.ChunkSourceRoles["bootstrap-src/not-in-roster.ts"] = "governed-renderer"
	if err := validateRendererChunkRoles("webgpu", stale); err == nil || !strings.Contains(err.Error(), "must exactly match") {
		t.Fatalf("stale role key was not rejected correctly: %v", err)
	}

	camouflaged := cloneRendererSourceSet(sourceSet)
	extra := "bootstrap-src/26e2-feature-scene3d-webgpu-core.ts"
	camouflaged.ChunkSources = append(camouflaged.ChunkSources, extra)
	camouflaged.ChunkSourceRoles[extra] = "backend-support"
	if err := validateRendererChunkRoles("webgpu", camouflaged); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("coordinated output/manifest/contract camouflage was not rejected correctly: %v", err)
	}
}

func cloneRendererSourceSet(sourceSet rendererSourceSetBaseline) rendererSourceSetBaseline {
	clone := rendererSourceSetBaseline{
		Chunk:            sourceSet.Chunk,
		ChunkSources:     append([]string(nil), sourceSet.ChunkSources...),
		ChunkSourceRoles: map[string]string{},
		Sources:          append([]string(nil), sourceSet.Sources...),
	}
	for source, role := range sourceSet.ChunkSourceRoles {
		clone.ChunkSourceRoles[source] = role
	}
	return clone
}

func validateRendererChunkRoles(backend string, sourceSet rendererSourceSetBaseline) error {
	authorities := rendererChunkRoleAuthorities(backend, sourceSet.Sources)
	roster := map[string]bool{}
	for _, source := range sourceSet.ChunkSources {
		if roster[source] {
			return fmt.Errorf("%s registers duplicate source %s", sourceSet.Chunk, source)
		}
		roster[source] = true
	}
	if len(sourceSet.ChunkSourceRoles) != len(roster) {
		return fmt.Errorf("%s chunkSourceRoles must exactly match chunkSources", sourceSet.Chunk)
	}
	for source := range sourceSet.ChunkSourceRoles {
		if !roster[source] {
			return fmt.Errorf("%s chunkSourceRoles must exactly match chunkSources; stale key %s", sourceSet.Chunk, source)
		}
	}
	governed := map[string]bool{}
	for _, source := range sourceSet.Sources {
		governed[source] = true
	}
	for _, source := range sourceSet.ChunkSources {
		role := sourceSet.ChunkSourceRoles[source]
		allowed, ok := authorities[role]
		if !ok {
			return fmt.Errorf("%s source %s has unknown governed role %q", sourceSet.Chunk, source, role)
		}
		if !allowed[source] {
			return fmt.Errorf("%s source %s has role %s, but that role is not authorized for this path", sourceSet.Chunk, source, role)
		}
		if governed[source] && role != "governed-renderer" {
			return fmt.Errorf("%s governed renderer source %s has role %s", sourceSet.Chunk, source, role)
		}
		if role == "governed-renderer" && !governed[source] {
			return fmt.Errorf("%s source %s is marked governed-renderer but is missing from sourceSets.%s.sources", sourceSet.Chunk, source, backend)
		}
	}
	return nil
}

func rendererChunkRoleAuthorities(backend string, governedSources []string) map[string]map[string]bool {
	authorities := map[string]map[string]bool{"governed-renderer": {}}
	for _, source := range governedSources {
		authorities["governed-renderer"][source] = true
	}
	exact := map[string]map[string][]string{
		"webgl": {
			"chunk-wrapper": {
				"bootstrap-src/26j-feature-scene3d-webgl-prefix.ts",
				"bootstrap-src/26j-feature-scene3d-webgl-suffix.ts",
			},
			"shared-scene-support": {
				"bootstrap-src/15a1-scene-texture-budget.ts",
				"bootstrap-src/16b-scene-hdr.ts",
			},
			"backend-support": {
				"bootstrap-src/16e-scene-webgl-legacy.ts",
			},
		},
		"webgpu": {
			"chunk-wrapper": {
				"bootstrap-src/26e-feature-scene3d-webgpu-prefix.ts",
				"bootstrap-src/26e-feature-scene3d-webgpu-suffix.ts",
			},
			"backend-support": {
				"bootstrap-src/26e1-feature-scene3d-webgpu-compute-bridge.ts",
			},
		},
	}
	for role, sources := range exact[backend] {
		if authorities[role] == nil {
			authorities[role] = map[string]bool{}
		}
		for _, source := range sources {
			authorities[role][source] = true
		}
	}
	return authorities
}

func TestRendererCompleteFunctionInventoryRatchetsOffline(t *testing.T) {
	clientJS, baseline := loadRendererArchitectureBaseline(t)
	defaults := baseline.FunctionDefaults
	exceptions := map[string]rendererSymbolMetric{}
	for _, exception := range baseline.FunctionExceptions {
		key := exception.Source + "\x00" + exception.Name
		if _, ok := exceptions[key]; ok {
			t.Fatalf("duplicate renderer function exception %s:%s", exception.Source, exception.Name)
		}
		exceptions[key] = exception
	}
	seen := map[string]bool{}
	neededExceptions := map[string]bool{}
	for _, sourceSet := range baseline.SourceSets {
		for _, source := range sourceSet.Sources {
			raw, err := os.ReadFile(filepath.Join(clientJS, filepath.FromSlash(source)))
			if err != nil {
				t.Fatalf("read %s: %v", source, err)
			}
			metrics, err := rendererAllFunctionMetrics(raw, source)
			if err != nil {
				t.Fatalf("measure function inventory for %s: %v", source, err)
			}
			for _, metric := range metrics {
				key := metric.Source + "\x00" + metric.Name
				seen[key] = true
				if regressions := rendererMetricRegressions(metric, defaults); len(regressions) == 0 {
					continue
				}
				exception, ok := exceptions[key]
				if !ok {
					t.Errorf("%s:%s exceeds default renderer function budget without an explicit exception: %s", metric.Source, metric.Name, strings.Join(rendererMetricRegressions(metric, defaults), ", "))
					continue
				}
				neededExceptions[key] = true
				if regressions := rendererMetricRegressions(metric, exception); len(regressions) != 0 {
					t.Errorf("%s:%s complexity regressed beyond exception: %s", metric.Source, metric.Name, strings.Join(regressions, ", "))
				}
			}
		}
	}
	for key, exception := range exceptions {
		if !seen[key] {
			t.Errorf("renderer function exception no longer matches a governed function: %s:%s", exception.Source, exception.Name)
			continue
		}
		if !neededExceptions[key] {
			t.Errorf("renderer function exception no longer exceeds default budgets and must be pruned: %s:%s", exception.Source, exception.Name)
		}
	}
}

func TestRendererCanopyComplexityRatchetsOffline(t *testing.T) {
	clientJS, baseline := loadRendererArchitectureBaseline(t)
	bySource := map[string][]rendererSymbolMetric{}
	for _, symbol := range baseline.Symbols {
		bySource[symbol.Source] = append(bySource[symbol.Source], symbol)
	}
	for source, symbols := range bySource {
		raw, err := os.ReadFile(filepath.Join(clientJS, filepath.FromSlash(source)))
		if err != nil {
			t.Fatalf("read %s: %v", source, err)
		}
		actual, err := rendererMetrics(raw, symbols)
		if err != nil {
			t.Fatalf("measure %s: %v", source, err)
		}
		for _, want := range symbols {
			got := actual[want.Name]
			if regressions := rendererMetricRegressions(got, want); len(regressions) != 0 {
				t.Errorf("%s:%s complexity regressed: %s", source, want.Name, strings.Join(regressions, ", "))
			}
		}
	}
}

func TestRendererComplexityRatchetRejectsExceedance(t *testing.T) {
	baseline := rendererSymbolMetric{Lines: 10, Cyclomatic: 3, Cognitive: 4, MaxNesting: 2, Parameters: 1}
	if got := rendererMetricRegressions(rendererSymbolMetric{Lines: 9, Cyclomatic: 3, Cognitive: 3, MaxNesting: 1, Parameters: 1}, baseline); len(got) != 0 {
		t.Fatalf("ratchet rejected a reduction: %v", got)
	}
	actual := baseline
	actual.Cognitive++
	got := rendererMetricRegressions(actual, baseline)
	if len(got) != 1 || got[0] != "cognitive 5 > 4" {
		t.Fatalf("ratchet did not reject the negative fixture: %v", got)
	}
}

func TestRendererCompleteInventoryRejectsUnlistedHotspots(t *testing.T) {
	source := []byte(`
function okSmall() { return 1; }
const tooHot = () => {
  if (a) {}
  if (b) {}
  if (c) {}
  if (d) {}
  if (e) {}
}
class RendererProbe {
  draw() {
    if (a) {}
    if (b) {}
    if (c) {}
    if (d) {}
    if (e) {}
  }
}
`)
	metrics, err := rendererAllFunctionMetrics(source, "../runtime/scene3d/webgpu-probe.ts")
	if err != nil {
		t.Fatalf("measure fixture: %v", err)
	}
	names := map[string]bool{}
	defaults := rendererSymbolMetric{Lines: 200, Cyclomatic: 4, Cognitive: 4, MaxNesting: 4, Parameters: 8}
	for _, metric := range metrics {
		if regressions := rendererMetricRegressions(metric, defaults); len(regressions) != 0 {
			names[metric.Name] = true
		}
	}
	if !rendererHasMetricPrefix(names, "tooHot@") || !rendererHasMetricPrefix(names, "RendererProbe.draw@") {
		t.Fatalf("complete inventory did not catch arrow and method hotspots: %v", names)
	}
}

func TestRendererFunctionExceptionMustRemainAboveDefaults(t *testing.T) {
	defaults := rendererSymbolMetric{Lines: 200, Cyclomatic: 40, Cognitive: 60, MaxNesting: 4, Parameters: 8}
	current := rendererSymbolMetric{
		Source:     "../runtime/scene3d/probe.ts",
		Name:       "small@1:1",
		Lines:      12,
		Cyclomatic: 2,
		Cognitive:  2,
		MaxNesting: 1,
		Parameters: 1,
	}
	if regressions := rendererMetricRegressions(current, defaults); len(regressions) != 0 {
		t.Fatalf("stale-exception fixture should be below defaults: %v", regressions)
	}
	if regressions := rendererMetricRegressions(rendererSymbolMetric{
		Lines:      201,
		Cyclomatic: 2,
		Cognitive:  2,
		MaxNesting: 1,
		Parameters: 1,
	}, defaults); len(regressions) != 1 || regressions[0] != "lines 201 > 200" {
		t.Fatalf("default budget fixture stopped catching actual growth: %v", regressions)
	}
}

func rendererMetricRegressions(actual, baseline rendererSymbolMetric) []string {
	checks := []struct {
		name        string
		actual, max int
	}{
		{"lines", actual.Lines, baseline.Lines},
		{"cyclomatic", actual.Cyclomatic, baseline.Cyclomatic},
		{"cognitive", actual.Cognitive, baseline.Cognitive},
		{"max nesting", actual.MaxNesting, baseline.MaxNesting},
		{"parameters", actual.Parameters, baseline.Parameters},
	}
	var regressions []string
	for _, check := range checks {
		if check.actual > check.max {
			regressions = append(regressions, fmt.Sprintf("%s %d > %d", check.name, check.actual, check.max))
		}
	}
	return regressions
}

func rendererMetrics(source []byte, wanted []rendererSymbolMetric) (map[string]rendererSymbolMetric, error) {
	language := grammars.TypescriptLanguage()
	if language == nil {
		return nil, fmt.Errorf("TypeScript grammar is unavailable")
	}
	tree, err := gotreesitter.NewParser(language).Parse(source)
	if err != nil {
		return nil, err
	}
	defer tree.Release()
	wantedNames := map[string]bool{}
	for _, metric := range wanted {
		wantedNames[metric.Name] = true
	}
	found := map[string]rendererSymbolMetric{}
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		if node.Type(language) == "function_declaration" {
			nameNode := node.ChildByFieldName("name", language)
			name := ""
			if nameNode != nil {
				name = nameNode.Text(source)
			}
			if wantedNames[name] {
				if _, duplicate := found[name]; duplicate {
					found[name] = rendererSymbolMetric{Name: "duplicate:" + name}
				} else {
					found[name] = measureRendererFunction(node, language, source, name)
				}
			}
		}
		for _, child := range node.Children() {
			walk(child)
		}
	}
	walk(tree.RootNode())
	for name := range wantedNames {
		metric, ok := found[name]
		if !ok {
			return nil, fmt.Errorf("function %s is missing", name)
		}
		if strings.HasPrefix(metric.Name, "duplicate:") {
			return nil, fmt.Errorf("function %s is ambiguous", name)
		}
	}
	return found, nil
}

func rendererAllFunctionMetrics(source []byte, sourceName string) ([]rendererSymbolMetric, error) {
	language := grammars.TypescriptLanguage()
	if language == nil {
		return nil, fmt.Errorf("TypeScript grammar is unavailable")
	}
	tree, err := gotreesitter.NewParser(language).Parse(source)
	if err != nil {
		return nil, err
	}
	defer tree.Release()
	found := map[string]rendererSymbolMetric{}
	var metrics []rendererSymbolMetric
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		nodeType := node.Type(language)
		if rendererSupportedFunctionNode(nodeType) {
			name, ok := rendererFunctionName(node, language, source)
			if !ok {
				name = fmt.Sprintf("anonymous@%d:%d", node.StartPoint().Row+1, node.StartPoint().Column+1)
			} else {
				name = fmt.Sprintf("%s@%d:%d", name, node.StartPoint().Row+1, node.StartPoint().Column+1)
			}
			metric := measureRendererFunction(node, language, source, name)
			metric.Source = sourceName
			if _, duplicate := found[name]; duplicate {
				found[name] = rendererSymbolMetric{Name: "duplicate:" + name}
			} else {
				found[name] = metric
				metrics = append(metrics, metric)
			}
		} else if strings.Contains(nodeType, "function") && !rendererFunctionTypeKnown(nodeType) {
			found["unsupported"] = rendererSymbolMetric{Name: "unsupported:" + nodeType}
		}
		for _, child := range node.Children() {
			walk(child)
		}
	}
	walk(tree.RootNode())
	for name, metric := range found {
		if strings.HasPrefix(metric.Name, "duplicate:") {
			return nil, fmt.Errorf("function identity %s is ambiguous", name)
		}
		if strings.HasPrefix(metric.Name, "unsupported:") {
			return nil, fmt.Errorf("unsupported TypeScript function form %s", strings.TrimPrefix(metric.Name, "unsupported:"))
		}
	}
	return metrics, nil
}

func rendererSupportedFunctionNode(nodeType string) bool {
	switch nodeType {
	case "function_declaration", "generator_function_declaration", "method_definition", "function_expression", "arrow_function":
		return true
	default:
		return false
	}
}

func rendererFunctionTypeKnown(nodeType string) bool {
	return rendererSupportedFunctionNode(nodeType) || nodeType == "function" || nodeType == "function_signature" || nodeType == "function_type"
}

func rendererHasMetricPrefix(names map[string]bool, prefix string) bool {
	for name := range names {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func rendererFunctionName(node *gotreesitter.Node, language *gotreesitter.Language, source []byte) (string, bool) {
	if node.Type(language) == "method_definition" {
		if name := node.ChildByFieldName("name", language); name != nil {
			className := ""
			for ancestor := node.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
				if ancestor.Type(language) != "class_declaration" {
					continue
				}
				if class := ancestor.ChildByFieldName("name", language); class != nil {
					className = class.Text(source)
				}
				break
			}
			if className != "" {
				return className + "." + name.Text(source), true
			}
			return name.Text(source), true
		}
	}
	if name := node.ChildByFieldName("name", language); name != nil {
		if text := name.Text(source); text != "" {
			return text, true
		}
	}
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		switch parent.Type(language) {
		case "variable_declarator":
			if name := parent.ChildByFieldName("name", language); name != nil {
				return name.Text(source), true
			}
		case "method_definition":
			if name := parent.ChildByFieldName("name", language); name != nil {
				className := ""
				for ancestor := parent.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
					if ancestor.Type(language) != "class_declaration" {
						continue
					}
					if class := ancestor.ChildByFieldName("name", language); class != nil {
						className = class.Text(source)
					}
					break
				}
				if className != "" {
					return className + "." + name.Text(source), true
				}
				return name.Text(source), true
			}
		case "assignment_expression":
			if left := parent.ChildByFieldName("left", language); left != nil {
				return left.Text(source), true
			}
		case "program", "statement_block":
			return "", false
		}
	}
	return "", false
}

// measureRendererFunction mirrors the small AST walk used by Canopy v0.18's
// complexity report. Keeping it here makes the ratchet hermetic in ordinary CI;
// maintainers use Canopy to refresh the checked baseline, not to run the gate.
func measureRendererFunction(root *gotreesitter.Node, language *gotreesitter.Language, source []byte, name string) rendererSymbolMetric {
	metric := rendererSymbolMetric{Name: name, Cyclomatic: 1, Lines: rendererNonBlankLines(root.Text(source))}
	if parameters := root.ChildByFieldName("parameters", language); parameters != nil {
		metric.Parameters = parameters.NamedChildCount()
	}
	var walk func(*gotreesitter.Node, int)
	walk = func(node *gotreesitter.Node, depth int) {
		if node == nil {
			return
		}
		nodeType := node.Type(language)
		if rendererBranchingNode(nodeType) {
			metric.Cyclomatic++
			metric.Cognitive += 1 + depth
			depth++
			if depth > metric.MaxNesting {
				metric.MaxNesting = depth
			}
		}
		if rendererLogicalNode(nodeType) {
			text := node.Text(source)
			if strings.Contains(text, "&&") || strings.Contains(text, "||") || strings.Contains(text, " and ") || strings.Contains(text, " or ") {
				metric.Cyclomatic++
				metric.Cognitive++
			}
		}
		for _, child := range node.Children() {
			walk(child, depth)
		}
	}
	walk(root, 0)
	return metric
}

func rendererNonBlankLines(source string) int {
	count := 0
	for _, line := range strings.Split(source, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func rendererBranchingNode(nodeType string) bool {
	switch nodeType {
	case "if_statement", "if_expression", "if_let_expression",
		"for_statement", "for_expression", "for_in_statement",
		"while_statement", "while_expression",
		"switch_statement", "switch_expression",
		"match_expression", "match_statement",
		"case_clause", "case_statement", "match_arm",
		"try_statement", "catch_clause", "except_clause", "rescue",
		"conditional_expression", "ternary_expression", "elif_clause", "else_if_clause":
		return true
	default:
		return false
	}
}

func rendererLogicalNode(nodeType string) bool {
	return nodeType == "binary_expression" || nodeType == "boolean_operator" || nodeType == "logical_expression"
}

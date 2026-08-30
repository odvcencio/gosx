// Package perfbrowserpin validates the governed browser lanes in CI.
//
// The contract is deliberately structural. Exact text and indentation cannot
// prove that a value belongs to an action's with map, that a control applies to
// a step, or that the aggregate test job owns a dependency.
package perfbrowserpin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	browserJobName = "browser-tests"
	stableJobName  = "scene3d-v1-browser-renderer-proof"
	testJobName    = "test"

	latestSetupName = "Set up Chrome"
	driverName      = "Perf driver browser tests"
	pinnedSetupName = "Set up pinned Chromium for perf budget"
	identityName    = "Verify pinned perf browser identity"
	budgetName      = "Perf budget gate"
	uploadName      = "Upload perf budget failure diagnostics"
	stableSetupName = "Set up stable Chrome for Testing"

	latestChromePath = "${{ steps.chrome.outputs.chrome-path }}"
	pinnedChromePath = "${{ steps.perf-chrome.outputs.chrome-path }}"
	pinnedVersion    = "${{ steps.perf-chrome.outputs.chrome-version }}"
	pinnedSnapshot   = "1688711"
	productVersion   = "154.0.8034.0"

	identityRun = `sh scripts/verify-perf-browser-identity.sh \
  "$PERF_CHROME_PATH" \
  "$PERF_CHROME_VERSION" \
  build/perf-browser-identity.txt
`
	failureArtifactPaths = `build/perf-report.json
build/perf-server.log
build/perf-browser-identity.txt
`
)

var baseAggregateNeeds = []string{
	"release-gate",
	"go-tests",
	"go-race-tests",
	"go-cli-tests",
	"js-tests",
	"wasm-tests",
	"browser-tests",
}

// Validate checks a complete GitHub Actions workflow document.
func Validate(source []byte) error {
	root, err := decode(source)
	if err != nil {
		return err
	}
	if err := validateNodeShape(root, "workflow"); err != nil {
		return err
	}

	workflow, err := mapping(root, "workflow")
	if err != nil {
		return err
	}
	for _, inherited := range []string{"defaults", "env"} {
		if _, ok := workflow[inherited]; ok {
			return fmt.Errorf("workflow: forbidden inherited execution field %q", inherited)
		}
	}
	jobsNode, ok := workflow["jobs"]
	if !ok {
		return errors.New("workflow: missing jobs mapping")
	}
	jobs, err := mapping(jobsNode, "workflow.jobs")
	if err != nil {
		return err
	}

	browserJob, ok := jobs[browserJobName]
	if !ok {
		return errors.New("workflow.jobs: browser-tests job is missing")
	}
	if err := validateBrowserJob(browserJob); err != nil {
		return err
	}

	_, hasStableJob := jobs[stableJobName]
	if hasStableJob {
		if err := validateStableJob(jobs[stableJobName]); err != nil {
			return err
		}
	}

	testJob, ok := jobs[testJobName]
	if !ok {
		return errors.New("workflow.jobs: aggregate test job is missing")
	}
	if err := validateAggregateJob(testJob, hasStableJob); err != nil {
		return err
	}
	return nil
}

func decode(source []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("parse workflow YAML: %w", err)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, errors.New("workflow: expected one YAML document")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("parse trailing workflow YAML: %w", err)
		}
		return nil, errors.New("workflow: multiple YAML documents are not allowed")
	}
	return document.Content[0], nil
}

func validateNodeShape(node *yaml.Node, path string) error {
	switch node.Kind {
	case yaml.AliasNode:
		return fmt.Errorf("%s: YAML aliases are not allowed in the governed workflow", path)
	case yaml.MappingNode:
		if len(node.Content)%2 != 0 {
			return fmt.Errorf("%s: malformed mapping", path)
		}
		seen := make(map[string]struct{}, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			value := node.Content[i+1]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return fmt.Errorf("%s: mapping key must be a string", path)
			}
			if key.Value == "<<" {
				return fmt.Errorf("%s: YAML merge keys are not allowed", path)
			}
			if _, ok := seen[key.Value]; ok {
				return fmt.Errorf("%s: duplicate mapping key %q", path, key.Value)
			}
			seen[key.Value] = struct{}{}
			if err := validateNodeShape(value, path+"."+key.Value); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, child := range node.Content {
			if err := validateNodeShape(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateBrowserJob(node *yaml.Node) error {
	const label = "browser-tests job"
	job, err := exactMapping(node, label, "runs-on", "timeout-minutes", "steps")
	if err != nil {
		return err
	}
	if err := exactString(job["runs-on"], label+".runs-on", "ubuntu-latest"); err != nil {
		return err
	}
	if err := exactInt(job["timeout-minutes"], label+".timeout-minutes", "30"); err != nil {
		return err
	}

	steps, err := namedSteps(job["steps"], label+".steps")
	if err != nil {
		return err
	}
	governed, err := validateGovernedBrowserSteps(steps)
	if err != nil {
		return err
	}
	if err := validateBrowserOrder(governed); err != nil {
		return err
	}
	return validateBrowserIsolation(node)
}

type stepContract struct {
	name     string
	label    string
	validate func(*yaml.Node) error
}

func validateGovernedBrowserSteps(steps map[string]namedStep) ([]namedStep, error) {
	contracts := []stepContract{
		{latestSetupName, "latest browser setup", validateLatestSetup},
		{driverName, "perf driver browser tests", validateDriver},
		{pinnedSetupName, "pinned perf browser setup", validatePinnedSetup},
		{identityName, "pinned perf browser identity", validateIdentity},
		{budgetName, "perf budget gate", validateBudget},
		{uploadName, "perf failure diagnostic upload", validateUpload},
	}
	governed := make([]namedStep, 0, len(contracts))
	for _, contract := range contracts {
		step, err := requiredStep(steps, contract.name, contract.label)
		if err != nil {
			return nil, err
		}
		if err := contract.validate(step.node); err != nil {
			return nil, err
		}
		governed = append(governed, step)
	}
	return governed, nil
}

func validateBrowserOrder(governed []namedStep) error {
	latest, driver, pinned := governed[0], governed[1], governed[2]
	identity, budget, upload := governed[3], governed[4], governed[5]
	if !(latest.index < driver.index && driver.index+1 == pinned.index && pinned.index+1 == identity.index && identity.index+1 == budget.index && budget.index+1 == upload.index) {
		return errors.New("browser-tests job.steps: governed order must be latest setup before driver, followed immediately by pinned setup, identity, budget, and failure upload")
	}
	return nil
}

func validateBrowserIsolation(node *yaml.Node) error {
	if got := countExactScalar(node, pinnedSnapshot); got != 1 {
		return fmt.Errorf("browser-tests job: numeric snapshot occurs %d times, want 1", got)
	}
	if got := countContainingScalar(node, productVersion); got != 0 {
		return fmt.Errorf("browser-tests job: product version occurs %d times, want 0", got)
	}
	if got := countContainingScalar(node, pinnedChromePath); got != 2 {
		return fmt.Errorf("browser-tests job: pinned browser path is referenced %d times, want 2", got)
	}
	if got := countContainingScalar(node, pinnedVersion); got != 1 {
		return fmt.Errorf("browser-tests job: pinned browser version is referenced %d times, want 1", got)
	}
	if got := countContainingScalar(node, latestChromePath); got != 2 {
		return fmt.Errorf("browser-tests job: latest browser path is referenced %d times, want 2", got)
	}
	return nil
}

func validateLatestSetup(node *yaml.Node) error {
	const label = "latest browser setup"
	// No with map is intentional: setup-chrome v1's default is the floating
	// latest snapshot used by compatibility and perf-driver coverage.
	step, err := exactMapping(node, label, "name", "id", "uses")
	if err != nil {
		return err
	}
	return exactStrings(step, label, map[string]string{
		"name": latestSetupName,
		"id":   "chrome",
		"uses": "browser-actions/setup-chrome@v1",
	})
}

func validateDriver(node *yaml.Node) error {
	const label = "perf driver browser tests"
	step, err := exactMapping(node, label, "name", "env", "run")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": driverName,
		"run":  "make test-perf-browser",
	}); err != nil {
		return err
	}
	return exactStringMap(step["env"], label+".env", map[string]string{
		"CHROME_PATH":            latestChromePath,
		"GOSX_CHROME_NO_SANDBOX": "1",
	})
}

func validatePinnedSetup(node *yaml.Node) error {
	const label = "pinned perf browser setup"
	step, err := exactMapping(node, label, "name", "id", "uses", "with")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": pinnedSetupName,
		"id":   "perf-chrome",
		"uses": "browser-actions/setup-chrome@v2",
	}); err != nil {
		return err
	}
	return exactStringMap(step["with"], label+".with", map[string]string{
		"chrome-version": pinnedSnapshot,
	})
}

func validateIdentity(node *yaml.Node) error {
	const label = "pinned perf browser identity"
	step, err := exactMapping(node, label, "name", "env", "run")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": identityName,
		"run":  identityRun,
	}); err != nil {
		return err
	}
	return exactStringMap(step["env"], label+".env", map[string]string{
		"PERF_CHROME_PATH":    pinnedChromePath,
		"PERF_CHROME_VERSION": pinnedVersion,
	})
}

func validateBudget(node *yaml.Node) error {
	const label = "perf budget gate"
	step, err := exactMapping(node, label, "name", "env", "run")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": budgetName,
		"run":  "make perf-budget-ci",
	}); err != nil {
		return err
	}
	return exactStringMap(step["env"], label+".env", map[string]string{
		"CHROME_PATH": pinnedChromePath,
	})
}

func validateUpload(node *yaml.Node) error {
	const label = "perf failure diagnostic upload"
	step, err := exactMapping(node, label, "name", "if", "uses", "with")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": uploadName,
		"if":   "failure()",
		"uses": "actions/upload-artifact@v4",
	}); err != nil {
		return err
	}
	with, err := exactMapping(step["with"], label+".with", "name", "path", "if-no-files-found", "retention-days")
	if err != nil {
		return err
	}
	if err := exactStrings(with, label+".with", map[string]string{
		"name":              "perf-budget-diagnostics-${{ github.run_id }}-${{ github.run_attempt }}",
		"path":              failureArtifactPaths,
		"if-no-files-found": "ignore",
	}); err != nil {
		return err
	}
	return exactInt(with["retention-days"], label+".with.retention-days", "7")
}

func validateStableJob(node *yaml.Node) error {
	const label = "stable Scene3D renderer proof job"
	job, err := mapping(node, label)
	if err != nil {
		return err
	}
	for _, forbidden := range []string{"if", "continue-on-error"} {
		if _, ok := job[forbidden]; ok {
			return fmt.Errorf("%s: forbidden field %q", label, forbidden)
		}
	}
	stepsNode, ok := job["steps"]
	if !ok {
		return fmt.Errorf("%s: missing field %q", label, "steps")
	}
	steps, err := namedSteps(stepsNode, label+".steps")
	if err != nil {
		return err
	}
	setup, err := requiredStep(steps, stableSetupName, "stable Scene3D renderer proof setup")
	if err != nil {
		return err
	}
	step, err := exactMapping(setup.node, "stable Scene3D renderer proof setup", "name", "id", "uses", "with")
	if err != nil {
		return err
	}
	if err := exactStrings(step, "stable Scene3D renderer proof setup", map[string]string{
		"name": stableSetupName,
		"id":   "chrome",
		"uses": "browser-actions/setup-chrome@v2",
	}); err != nil {
		return err
	}
	if err := exactStringMap(step["with"], "stable Scene3D renderer proof setup.with", map[string]string{
		"chrome-version": "stable",
	}); err != nil {
		return err
	}
	for _, forbidden := range []string{pinnedSnapshot, productVersion, "perf-chrome"} {
		if got := countContainingScalar(node, forbidden); got != 0 {
			return fmt.Errorf("%s: forbidden perf fixture %q occurs %d times", label, forbidden, got)
		}
	}
	return nil
}

func validateAggregateJob(node *yaml.Node, hasStableJob bool) error {
	const label = "aggregate test job"
	job, err := exactMapping(node, label, "runs-on", "needs", "timeout-minutes", "steps")
	if err != nil {
		return err
	}
	if err := exactString(job["runs-on"], label+".runs-on", "ubuntu-latest"); err != nil {
		return err
	}
	if err := exactInt(job["timeout-minutes"], label+".timeout-minutes", "5"); err != nil {
		return err
	}
	needsNode := job["needs"]
	if needsNode.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s.needs: expected sequence", label)
	}
	want := slices.Clone(baseAggregateNeeds)
	if hasStableJob {
		want = slices.Insert(want, len(want)-1, stableJobName)
	}
	got := make([]string, 0, len(needsNode.Content))
	seen := make(map[string]struct{}, len(needsNode.Content))
	for i, need := range needsNode.Content {
		if need.Kind != yaml.ScalarNode || need.Tag != "!!str" {
			return fmt.Errorf("%s.needs[%d]: expected string", label, i)
		}
		if _, ok := seen[need.Value]; ok {
			return fmt.Errorf("%s.needs: duplicate job %q", label, need.Value)
		}
		seen[need.Value] = struct{}{}
		got = append(got, need.Value)
	}
	if len(got) != len(want) {
		return fmt.Errorf("%s.needs: got %v, want exact governed membership %v", label, got, want)
	}
	for _, required := range want {
		if _, ok := seen[required]; !ok {
			return fmt.Errorf("%s.needs: missing required job %q", label, required)
		}
	}
	if job["steps"].Kind != yaml.SequenceNode || len(job["steps"].Content) == 0 {
		return fmt.Errorf("%s.steps: expected non-empty sequence", label)
	}
	return nil
}

type namedStep struct {
	node  *yaml.Node
	index int
}

func namedSteps(node *yaml.Node, label string) (map[string]namedStep, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%s: expected sequence", label)
	}
	steps := make(map[string]namedStep, len(node.Content))
	for i, item := range node.Content {
		step, err := mapping(item, fmt.Sprintf("%s[%d]", label, i))
		if err != nil {
			return nil, err
		}
		nameNode, ok := step["name"]
		if !ok {
			return nil, fmt.Errorf("%s[%d]: every step must have a name", label, i)
		}
		if nameNode.Kind != yaml.ScalarNode || nameNode.Tag != "!!str" || nameNode.Value == "" {
			return nil, fmt.Errorf("%s[%d].name: expected non-empty string", label, i)
		}
		if _, ok := steps[nameNode.Value]; ok {
			return nil, fmt.Errorf("%s: duplicate step name %q", label, nameNode.Value)
		}
		steps[nameNode.Value] = namedStep{node: item, index: i}
	}
	return steps, nil
}

func requiredStep(steps map[string]namedStep, name, label string) (namedStep, error) {
	step, ok := steps[name]
	if !ok {
		return namedStep{}, fmt.Errorf("%s: step is missing", label)
	}
	return step, nil
}

func mapping(node *yaml.Node, label string) (map[string]*yaml.Node, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: expected mapping", label)
	}
	result := make(map[string]*yaml.Node, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		result[node.Content[i].Value] = node.Content[i+1]
	}
	return result, nil
}

func exactMapping(node *yaml.Node, label string, fields ...string) (map[string]*yaml.Node, error) {
	result, err := mapping(node, label)
	if err != nil {
		return nil, err
	}
	want := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		want[field] = struct{}{}
	}
	for field := range result {
		if _, ok := want[field]; !ok {
			return nil, fmt.Errorf("%s: unexpected field %q", label, field)
		}
	}
	for _, field := range fields {
		if _, ok := result[field]; !ok {
			return nil, fmt.Errorf("%s: missing field %q", label, field)
		}
	}
	return result, nil
}

func exactStrings(fields map[string]*yaml.Node, label string, expected map[string]string) error {
	for field, want := range expected {
		node, ok := fields[field]
		if !ok {
			return fmt.Errorf("%s: missing field %q", label, field)
		}
		if err := exactString(node, label+"."+field, want); err != nil {
			return err
		}
	}
	return nil
}

func exactStringMap(node *yaml.Node, label string, expected map[string]string) error {
	fields := make([]string, 0, len(expected))
	for field := range expected {
		fields = append(fields, field)
	}
	result, err := exactMapping(node, label, fields...)
	if err != nil {
		return err
	}
	return exactStrings(result, label, expected)
}

func exactString(node *yaml.Node, label, want string) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return fmt.Errorf("%s: expected string %q", label, want)
	}
	if node.Value != want {
		return fmt.Errorf("%s: got %q, want %q", label, node.Value, want)
	}
	return nil
}

func exactInt(node *yaml.Node, label, want string) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!int" || node.Value != want {
		return fmt.Errorf("%s: got %q, want integer %s", label, node.Value, want)
	}
	return nil
}

func countExactScalar(node *yaml.Node, target string) int {
	count := 0
	walkScalars(node, func(value string) {
		if value == target {
			count++
		}
	})
	return count
}

func countContainingScalar(node *yaml.Node, target string) int {
	count := 0
	walkScalars(node, func(value string) {
		if strings.Contains(value, target) {
			count++
		}
	})
	return count
}

func walkScalars(node *yaml.Node, visit func(string)) {
	if node.Kind == yaml.ScalarNode {
		visit(node.Value)
	}
	for _, child := range node.Content {
		walkScalars(child, visit)
	}
}

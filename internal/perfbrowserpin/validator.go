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

	latestSetupName   = "Set up Chrome"
	checkoutName      = "Check out repository"
	goSetupName       = "Set up Go"
	tinyGoName        = "Install TinyGo"
	docsName          = "Browser docs E2E gate"
	ouroborosName     = "Ouroboros media metadata browser smoke"
	driverName        = "Perf driver browser tests"
	pinnedSetupName   = "Set up pinned Chromium for perf budget"
	identityName      = "Verify pinned perf browser identity"
	budgetName        = "Perf budget gate"
	uploadName        = "Upload perf budget failure diagnostics"
	stableSetupName   = "Set up stable Chrome for Testing"
	stableNodeName    = "Set up Node.js"
	stableProofName   = "Run Scene3D CUBICSPLINE browser renderer proof"
	stableUploadName  = "Upload Scene3D proof diagnostics"
	stableCleanName   = "Clean Scene3D proof artifacts"
	aggregateStepName = "All test jobs passed"

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
	ouroborosRun = `chrome_bin="$(command -v google-chrome-stable || command -v google-chrome)"
GOSX_CHROME_BIN="$chrome_bin" go test ./examples/ouroboros-corpus -run '^TestFixtureMP4BrowserLoadsMetadata$' -count=1
`
	failureArtifactPaths = `build/perf-report.json
build/perf-server.log
build/perf-browser-identity.txt
`
	stableProofRun = `set -eu
if [ -z "${RUNNER_TEMP:-}" ] || [ "$RUNNER_TEMP" = "/" ]; then
  echo "unsafe RUNNER_TEMP" >&2
  exit 2
fi
artifact_dir="${RUNNER_TEMP}/gosx-cubic-proof"
rm -rf -- "$artifact_dir"
mkdir -p "$artifact_dir"
proof_status=0
trap 'if [ "$proof_status" -eq 0 ]; then rm -rf -- "$artifact_dir"; fi' EXIT
set +e
node client/js/testdata/scene3d-cubic-spline-browser-matrix.cjs "$GITHUB_WORKSPACE" "$artifact_dir"
proof_status=$?
set -e
if [ "$proof_status" -ne 0 ] && [ -f "$artifact_dir/matrix-report.json" ]; then
  cat "$artifact_dir/matrix-report.json"
fi
exit "$proof_status"
`
	stableCleanRun = `set -eu
if [ -z "${RUNNER_TEMP:-}" ] || [ "$RUNNER_TEMP" = "/" ]; then
  echo "unsafe RUNNER_TEMP" >&2
  exit 2
fi
rm -rf -- "${RUNNER_TEMP}/gosx-cubic-proof"
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

	if err := validateExactStepRoster(job["steps"], label+".steps", browserStepContracts()); err != nil {
		return err
	}
	return validateBrowserIsolation(node)
}

type stepContract struct {
	name     string
	label    string
	validate func(*yaml.Node) error
}

func browserStepContracts() []stepContract {
	return []stepContract{
		{checkoutName, "browser checkout", validateCheckout},
		{goSetupName, "browser Go setup", validateGoSetup},
		{latestSetupName, "latest browser setup", validateLatestSetup},
		{tinyGoName, "browser TinyGo install", validateTinyGo},
		{docsName, "browser docs E2E gate", validateDocs},
		{ouroborosName, "Ouroboros browser smoke", validateOuroboros},
		{driverName, "perf driver browser tests", validateDriver},
		{pinnedSetupName, "pinned perf browser setup", validatePinnedSetup},
		{identityName, "pinned perf browser identity", validateIdentity},
		{budgetName, "perf budget gate", validateBudget},
		{uploadName, "perf failure diagnostic upload", validateUpload},
	}
}

func stableStepContracts() []stepContract {
	return []stepContract{
		{checkoutName, "stable Scene3D renderer proof checkout", validateCheckout},
		{stableSetupName, "stable Scene3D renderer proof setup", validateStableSetup},
		{stableNodeName, "stable Scene3D renderer proof Node setup", validateStableNode},
		{stableProofName, "stable Scene3D renderer proof", validateStableProof},
		{stableUploadName, "stable Scene3D diagnostic upload", validateStableUpload},
		{stableCleanName, "stable Scene3D artifact cleanup", validateStableCleanup},
	}
}

func validateExactStepRoster(node *yaml.Node, label string, contracts []stepContract) error {
	// The roster is closed, not merely ordered. An otherwise valid extra run
	// step can persist BASH_ENV through GITHUB_ENV and turn later sh/make gates
	// into successful no-ops even when every named gate is textually exact.
	steps, err := namedSteps(node, label)
	if err != nil {
		return err
	}
	if len(steps) != len(contracts) {
		return fmt.Errorf("%s: got %d steps, want exact governed roster of %d", label, len(steps), len(contracts))
	}
	for index, contract := range contracts {
		step, err := requiredStep(steps, contract.name, contract.label)
		if err != nil {
			return err
		}
		if step.index != index {
			return fmt.Errorf("%s: step %q is at index %d, want exact governed index %d", label, contract.name, step.index, index)
		}
		if err := contract.validate(step.node); err != nil {
			return err
		}
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

func validateCheckout(node *yaml.Node) error {
	const label = "repository checkout"
	// No with map is intentional: the event's current revision must be checked
	// out, never an attacker-selected ref or path shared with another checkout.
	step, err := exactMapping(node, label, "name", "uses")
	if err != nil {
		return err
	}
	return exactStrings(step, label, map[string]string{
		"name": checkoutName,
		"uses": "actions/checkout@v4",
	})
}

func validateGoSetup(node *yaml.Node) error {
	const label = "browser Go setup"
	step, err := exactMapping(node, label, "name", "uses", "with")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": goSetupName,
		"uses": "actions/setup-go@v5",
	}); err != nil {
		return err
	}
	with, err := exactMapping(step["with"], label+".with", "go-version-file", "cache")
	if err != nil {
		return err
	}
	if err := exactString(with["go-version-file"], label+".with.go-version-file", "go.mod"); err != nil {
		return err
	}
	return exactBool(with["cache"], label+".with.cache", true)
}

func validateTinyGo(node *yaml.Node) error {
	const label = "browser TinyGo install"
	step, err := exactMapping(node, label, "name", "env", "run")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": tinyGoName,
		"run":  "scripts/install-ci-tinygo.sh",
	}); err != nil {
		return err
	}
	return exactStringMap(step["env"], label+".env", map[string]string{
		"TINYGO_VERSION": "0.41.1",
	})
}

func validateDocs(node *yaml.Node) error {
	const label = "browser docs E2E gate"
	step, err := exactMapping(node, label, "name", "env", "run")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": docsName,
		"run":  "make test-e2e",
	}); err != nil {
		return err
	}
	return exactStringMap(step["env"], label+".env", map[string]string{
		"GOSX_E2E_CHROME": latestChromePath,
	})
}

func validateOuroboros(node *yaml.Node) error {
	const label = "Ouroboros browser smoke"
	step, err := exactMapping(node, label, "name", "run")
	if err != nil {
		return err
	}
	return exactStrings(step, label, map[string]string{
		"name": ouroborosName,
		"run":  ouroborosRun,
	})
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

func validateStableSetup(node *yaml.Node) error {
	const label = "stable Scene3D renderer proof setup"
	step, err := exactMapping(node, label, "name", "id", "uses", "with")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": stableSetupName,
		"id":   "chrome",
		"uses": "browser-actions/setup-chrome@v2",
	}); err != nil {
		return err
	}
	return exactStringMap(step["with"], label+".with", map[string]string{
		"chrome-version": "stable",
	})
}

func validateStableNode(node *yaml.Node) error {
	const label = "stable Scene3D renderer proof Node setup"
	step, err := exactMapping(node, label, "name", "uses", "with")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": stableNodeName,
		"uses": "actions/setup-node@v4",
	}); err != nil {
		return err
	}
	return exactStringMap(step["with"], label+".with", map[string]string{
		"node-version": "22",
	})
}

func validateStableProof(node *yaml.Node) error {
	const label = "stable Scene3D renderer proof"
	step, err := exactMapping(node, label, "name", "env", "run")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": stableProofName,
		"run":  stableProofRun,
	}); err != nil {
		return err
	}
	return exactStringMap(step["env"], label+".env", map[string]string{
		"GOSX_CHROME_BIN":                  latestChromePath,
		"GOSX_EXPECTED_CHROME_VERSION":     "${{ steps.chrome.outputs.chrome-version }}",
		"GOSX_SCENE3D_CUBIC_WEBGPU_TARGET": "private-texture",
	})
}

func validateStableUpload(node *yaml.Node) error {
	const label = "stable Scene3D diagnostic upload"
	step, err := exactMapping(node, label, "name", "if", "uses", "with")
	if err != nil {
		return err
	}
	if err := exactStrings(step, label, map[string]string{
		"name": stableUploadName,
		"if":   "${{ failure() }}",
		"uses": "actions/upload-artifact@v4",
	}); err != nil {
		return err
	}
	with, err := exactMapping(step["with"], label+".with", "name", "path", "if-no-files-found", "retention-days")
	if err != nil {
		return err
	}
	if err := exactStrings(with, label+".with", map[string]string{
		"name":              "scene3d-v1-cubic-proof-${{ github.run_id }}-${{ github.run_attempt }}",
		"path":              "${{ runner.temp }}/gosx-cubic-proof",
		"if-no-files-found": "ignore",
	}); err != nil {
		return err
	}
	return exactInt(with["retention-days"], label+".with.retention-days", "7")
}

func validateStableCleanup(node *yaml.Node) error {
	const label = "stable Scene3D artifact cleanup"
	step, err := exactMapping(node, label, "name", "if", "run")
	if err != nil {
		return err
	}
	return exactStrings(step, label, map[string]string{
		"name": stableCleanName,
		"if":   "${{ always() }}",
		"run":  stableCleanRun,
	})
}

func validateStableJob(node *yaml.Node) error {
	const label = "stable Scene3D renderer proof job"
	job, err := exactMapping(node, label, "runs-on", "timeout-minutes", "steps")
	if err != nil {
		return err
	}
	if err := exactString(job["runs-on"], label+".runs-on", "ubuntu-latest"); err != nil {
		return err
	}
	if err := exactInt(job["timeout-minutes"], label+".timeout-minutes", "10"); err != nil {
		return err
	}
	if err := validateExactStepRoster(job["steps"], label+".steps", stableStepContracts()); err != nil {
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
	job, err := exactMapping(node, label, "if", "runs-on", "needs", "timeout-minutes", "steps")
	if err != nil {
		return err
	}
	if err := exactString(job["if"], label+".if", "${{ always() }}"); err != nil {
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
	want := aggregateNeeds(hasStableJob)
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
	if !slices.Equal(got, want) {
		return fmt.Errorf("%s.needs: got %v, want exact governed order %v", label, got, want)
	}
	contract := stepContract{
		name:  aggregateStepName,
		label: "aggregate dependency assertion",
		validate: func(step *yaml.Node) error {
			return validateAggregateStep(step, want)
		},
	}
	return validateExactStepRoster(job["steps"], label+".steps", []stepContract{contract})
}

func validateAggregateStep(node *yaml.Node, needs []string) error {
	const label = "aggregate dependency assertion"
	step, err := exactMapping(node, label, "name", "env", "run")
	if err != nil {
		return err
	}
	if err := exactString(step["name"], label+".name", aggregateStepName); err != nil {
		return err
	}
	expectedEnv := make(map[string]string, len(needs))
	for _, need := range needs {
		expectedEnv[aggregateResultEnv(need)] = fmt.Sprintf("${{ needs.%s.result }}", need)
	}
	if err := exactStringMap(step["env"], label+".env", expectedEnv); err != nil {
		return err
	}
	return exactString(step["run"], label+".run", aggregateRun(needs))
}

func aggregateNeeds(hasStableJob bool) []string {
	needs := slices.Clone(baseAggregateNeeds)
	if hasStableJob {
		// The stable proof is optional only as a workflow composition. Once
		// present, its exact job has no skip condition and is a required success.
		needs = slices.Insert(needs, len(needs)-1, stableJobName)
	}
	return needs
}

func aggregateResultEnv(need string) string {
	return strings.ToUpper(strings.ReplaceAll(need, "-", "_")) + "_RESULT"
}

func aggregateRun(needs []string) string {
	var run strings.Builder
	run.WriteString("set -eu\nfor result in \\\n")
	for index, need := range needs {
		suffix := " \\\n"
		if index == len(needs)-1 {
			suffix = "\n"
		}
		fmt.Fprintf(&run, "  \"%s=$%s\"%s", need, aggregateResultEnv(need), suffix)
	}
	run.WriteString(`do
  case "$result" in
    *=success) ;;
    *)
      echo "required test job did not succeed: $result" >&2
      exit 1
      ;;
  esac
done
echo "`)
	run.WriteString(strings.Join(needs[:len(needs)-1], ", "))
	run.WriteString(", and ")
	run.WriteString(needs[len(needs)-1])
	run.WriteString(" all passed\"\n")
	return run.String()
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

func exactBool(node *yaml.Node, label string, want bool) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
		return fmt.Errorf("%s: expected boolean %t", label, want)
	}
	wantValue := "false"
	if want {
		wantValue = "true"
	}
	if node.Value != wantValue {
		return fmt.Errorf("%s: got %q, want %q", label, node.Value, wantValue)
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

// Command citest owns the package partitions used by CI.
//
// Keeping discovery here instead of in shell pipelines makes a newly added
// package fail the partition validation if it is ever omitted or included
// twice. The focused race set is also validated against the current source:
// every entry must have tests and production concurrency primitives.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	cliRelativePath                   = "cmd/gosx"
	ouroborosRaceRelativePath         = "perf/ouroboros"
	exhaustiveRacePackageTimeout      = "40m"
	ouroborosScopedRacePackageTimeout = "10m"
)

type raceTarget struct {
	relativePath string
	reason       string
}

type raceSkipTarget struct {
	testName string
	reason   string
}

// prRaceTargets covers shared framework state and the server-authoritative 3D
// path. CPU-bound codecs and vector kernels remain covered by ordinary PR tests
// and by the full race run on protected-branch pushes.
var prRaceTargets = []raceTarget{
	{"auth", "concurrent in-memory credential and observer stores"},
	{"client/bridge", "cross-frame and canvas event atomics"},
	{"client/vm", "shared host, material, and render caches"},
	{"crdt", "concurrent documents and vector quantizer cache"},
	{"engine/surface", "asynchronous surface hosts and registries"},
	{"engine/surface/runtime", "runtime instance registry"},
	{"field", "parallel volumetric field work"},
	{"game/loop", "frame scheduling, observer admission, and concurrent stop/restart"},
	{"hub", "websocket client pumps and shared connection state"},
	{"hub/scene3d", "authoritative shared Scene3D state"},
	{"internal/chrometest", "Chrome startup, pipe draining, cancellation, and cleanup goroutines"},
	{"physics", "shared physics acceleration data"},
	{"render/bundle", "parallel render-bundle assembly"},
	{"route", "shared parser, source, and metadata caches"},
	{"scheduled", "scheduler and watchdog goroutines"},
	{"semantic", "concurrent semantic caches and router state"},
	{"server", "runtime caches, streaming, and revalidation state"},
	{"signal", "concurrent subscriptions, tracking, and batching"},
	{"sim", "server-authoritative simulation loop"},
	{"telemetry", "shared worker, admission, and context-bounded shutdown ownership"},
	{"telemetry/metric", "bounded registry admission and consistent histogram snapshots"},
	{"telemetry/telemetrytest", "concurrent deterministic clock and timer ownership"},
	{"scene", "parallel scene geometry work"},
}

// ouroborosRaceSkips is intentionally an exact-name allowlist, not a file or
// prefix exclusion. These tests build source identity from the real repository,
// repeatedly parsing and Brotli-compressing the complete browser-source corpus.
// The ordinary unit lane executes every one. The scoped race lane retains all
// fixture-sized evidence tests and the package's shared-state tests.
var ouroborosRaceSkips = []raceSkipTarget{
	{"TestBuildSizeEvidenceAttributesRouteAssetsAndDedupesTotals", "recomputes the real-repository source inventory before checking route attribution"},
	{"TestBuildSizeEvidenceResolvesHashedURLsWithQuery", "recomputes the real-repository source inventory before checking hashed URL resolution"},
	{"TestBuildSizeEvidenceResolvesCapabilityRuntimeVariants", "recomputes the real-repository source inventory before checking runtime variants"},
	{"TestBuildSizeEvidenceRecordsNoncanonicalUnresolvedRefs", "recomputes the real-repository source inventory before checking unresolved refs"},
	{"TestBuildSizeEvidenceRecordsNoncanonicalUnsafeManifestPaths", "recomputes the real-repository source inventory before checking unsafe paths"},
	{"TestBuildSizeEvidenceRecordsNoncanonicalSymlinkEscapedAsset", "recomputes the real-repository source inventory before checking symlink escapes"},
	{"TestBuildSizeEvidenceRejectsInventoryOverlayMismatch", "recomputes the real-repository source inventory to construct and reject a stale receipt"},
	{"TestCompatibilityAuditReceiptAndReconciliation", "recomputes and parses the real-repository compatibility inventory"},
	{"TestRunBrowserBaselineRemoteDialErrorRedactedInArtifacts", "recomputes real-repository source identity before exercising the remote dial boundary"},
}

type listedModule struct {
	Path string
}

type listedPackage struct {
	Dir          string
	ImportPath   string
	Module       *listedModule
	GoFiles      []string
	CgoFiles     []string
	TestGoFiles  []string
	XTestGoFiles []string
}

type concurrencyEvidence struct {
	tests        int
	goStatements int
	syncImports  int
}

type racePackage struct {
	listedPackage
	target   raceTarget
	evidence concurrencyEvidence
}

type scopedRacePackage struct {
	listedPackage
	testCount int
	skips     []raceSkipTarget
}

type testPlan struct {
	modulePath    string
	all           []listedPackage
	unit          []listedPackage
	cli           []listedPackage
	race          []racePackage
	fullRace      []listedPackage
	ouroborosRace scopedRacePackage
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "citest: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: citest verify | cli <0|1|2> | browser <0|1|2> | list <unit|cli|race|full-race> | test <unit|race|full-race|ouroboros-race>")
	}

	goBinary := os.Getenv("GOSX_CI_GO")
	if goBinary == "" {
		goBinary = "go"
	}
	switch args[0] {
	case "cli":
		return runCLIShard(args[1:], goBinary, stdout, stderr)
	case "browser":
		return runBrowserShard(args[1:], goBinary, stdout, stderr)
	}
	plan, err := buildTestPlan(goBinary)
	if err != nil {
		return err
	}

	switch args[0] {
	case "verify":
		if len(args) != 1 {
			return errors.New("verify takes no arguments")
		}
		printPlan(stdout, plan)
		if err := verifyCLIShardPins(stdout); err != nil {
			return err
		}
		return verifyBrowserShards(stdout)
	case "list":
		if len(args) != 2 {
			return errors.New("usage: citest list <unit|cli|race|full-race>")
		}
		packages, err := selectPackages(plan, args[1])
		if err != nil {
			return err
		}
		for _, pkg := range packages {
			fmt.Fprintln(stdout, pkg)
		}
		return nil
	case "test":
		if len(args) != 2 || (args[1] != "unit" && args[1] != "race" && args[1] != "full-race" && args[1] != "ouroboros-race") {
			return errors.New("usage: citest test <unit|race|full-race|ouroboros-race>")
		}
		printPlan(stderr, plan)
		commandArgs := []string{"test"}
		if args[1] != "unit" {
			commandArgs = append(commandArgs, "-race")
		}
		if args[1] == "full-race" {
			commandArgs = append(commandArgs, "-timeout", exhaustiveRacePackageTimeout)
		}
		var packages []string
		if args[1] == "ouroboros-race" {
			commandArgs = append(commandArgs,
				"-timeout", ouroborosScopedRacePackageTimeout,
				"-skip", raceSkipPattern(plan.ouroborosRace.skips),
			)
			packages = []string{plan.ouroborosRace.ImportPath}
		} else {
			packages, err = selectPackages(plan, args[1])
			if err != nil {
				return err
			}
		}
		commandArgs = append(commandArgs, packages...)
		fmt.Fprintf(stderr, "citest: running %s tests across %d packages\n", args[1], len(packages))
		command := exec.Command(goBinary, commandArgs...)
		command.Stdout = stdout
		command.Stderr = stderr
		command.Stdin = os.Stdin
		if err := command.Run(); err != nil {
			return fmt.Errorf("%s tests: %w", args[1], err)
		}
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func buildTestPlan(goBinary string) (testPlan, error) {
	packages, err := listPackages(goBinary)
	if err != nil {
		return testPlan{}, err
	}
	modulePath, err := rootModulePath(packages)
	if err != nil {
		return testPlan{}, err
	}

	cliImportPath := modulePath + "/" + cliRelativePath
	var unit, cli []listedPackage
	for _, pkg := range packages {
		if pkg.ImportPath == cliImportPath {
			cli = append(cli, pkg)
		} else {
			unit = append(unit, pkg)
		}
	}
	if len(cli) != 1 {
		return testPlan{}, fmt.Errorf("CLI partition contains %d packages, want exactly %q", len(cli), cliImportPath)
	}
	if err := validateCoverage(packages, map[string][]listedPackage{
		"unit": unit,
		"cli":  cli,
	}); err != nil {
		return testPlan{}, err
	}

	race, err := resolveRacePackages(modulePath, unit)
	if err != nil {
		return testPlan{}, err
	}
	fullRace, ouroborosRace, err := resolveExhaustiveRacePackages(modulePath, packages)
	if err != nil {
		return testPlan{}, err
	}
	return testPlan{
		modulePath:    modulePath,
		all:           packages,
		unit:          unit,
		cli:           cli,
		race:          race,
		fullRace:      fullRace,
		ouroborosRace: ouroborosRace,
	}, nil
}

func listPackages(goBinary string) ([]listedPackage, error) {
	command := exec.Command(goBinary, "list", "-json", "./...")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("go list ./...: %w\n%s", err, strings.TrimSpace(stderr.String()))
	}

	decoder := json.NewDecoder(&stdout)
	var packages []listedPackage
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		packages = append(packages, pkg)
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].ImportPath < packages[j].ImportPath
	})
	return packages, nil
}

func rootModulePath(packages []listedPackage) (string, error) {
	if len(packages) == 0 {
		return "", errors.New("go list ./... returned no packages")
	}
	var modulePath string
	for _, pkg := range packages {
		if pkg.Module == nil || pkg.Module.Path == "" {
			return "", fmt.Errorf("%s has no module path", pkg.ImportPath)
		}
		if modulePath == "" {
			modulePath = pkg.Module.Path
		}
		if pkg.Module.Path != modulePath {
			return "", fmt.Errorf("%s belongs to module %s, want %s", pkg.ImportPath, pkg.Module.Path, modulePath)
		}
	}
	return modulePath, nil
}

func validateCoverage(all []listedPackage, partitions map[string][]listedPackage) error {
	allSet := make(map[string]struct{}, len(all))
	for _, pkg := range all {
		if _, exists := allSet[pkg.ImportPath]; exists {
			return fmt.Errorf("go list returned duplicate package %s", pkg.ImportPath)
		}
		allSet[pkg.ImportPath] = struct{}{}
	}

	owners := make(map[string]string, len(all))
	for partition, packages := range partitions {
		for _, pkg := range packages {
			if _, exists := allSet[pkg.ImportPath]; !exists {
				return fmt.Errorf("%s partition contains unknown package %s", partition, pkg.ImportPath)
			}
			if owner, exists := owners[pkg.ImportPath]; exists {
				return fmt.Errorf("package %s overlaps %s and %s partitions", pkg.ImportPath, owner, partition)
			}
			owners[pkg.ImportPath] = partition
		}
	}

	var missing []string
	for importPath := range allSet {
		if _, exists := owners[importPath]; !exists {
			missing = append(missing, importPath)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("package partition has gaps: %s", strings.Join(missing, ", "))
	}
	return nil
}

func resolveRacePackages(modulePath string, unit []listedPackage) ([]racePackage, error) {
	byImportPath := make(map[string]listedPackage, len(unit))
	for _, pkg := range unit {
		byImportPath[pkg.ImportPath] = pkg
	}

	seen := make(map[string]struct{}, len(prRaceTargets))
	race := make([]racePackage, 0, len(prRaceTargets))
	for _, target := range prRaceTargets {
		importPath := modulePath + "/" + target.relativePath
		if _, exists := seen[importPath]; exists {
			return nil, fmt.Errorf("PR race package %s is listed more than once", importPath)
		}
		seen[importPath] = struct{}{}
		if strings.TrimSpace(target.reason) == "" {
			return nil, fmt.Errorf("PR race package %s has no review reason", importPath)
		}
		pkg, exists := byImportPath[importPath]
		if !exists {
			return nil, fmt.Errorf("PR race package %s is missing or outside the unit partition", importPath)
		}
		evidence, err := inspectConcurrency(pkg)
		if err != nil {
			return nil, err
		}
		if evidence.tests == 0 {
			return nil, fmt.Errorf("PR race package %s has no tests", importPath)
		}
		if evidence.goStatements == 0 && evidence.syncImports == 0 {
			return nil, fmt.Errorf("PR race package %s has no production goroutine or sync evidence", importPath)
		}
		race = append(race, racePackage{
			listedPackage: pkg,
			target:        target,
			evidence:      evidence,
		})
	}
	return race, nil
}

func inspectConcurrency(pkg listedPackage) (concurrencyEvidence, error) {
	evidence := concurrencyEvidence{
		tests: len(pkg.TestGoFiles) + len(pkg.XTestGoFiles),
	}
	files := append(append([]string{}, pkg.GoFiles...), pkg.CgoFiles...)
	for _, name := range files {
		path := filepath.Join(pkg.Dir, name)
		source, err := os.ReadFile(path)
		if err != nil {
			return concurrencyEvidence{}, fmt.Errorf("read %s: %w", path, err)
		}
		goStatements, syncImports, err := inspectConcurrencySource(path, source)
		if err != nil {
			return concurrencyEvidence{}, err
		}
		evidence.goStatements += goStatements
		evidence.syncImports += syncImports
	}
	return evidence, nil
}

func inspectConcurrencySource(filename string, source []byte) (int, int, error) {
	file, err := parser.ParseFile(token.NewFileSet(), filename, source, 0)
	if err != nil {
		return 0, 0, fmt.Errorf("parse %s: %w", filename, err)
	}

	goStatements := 0
	ast.Inspect(file, func(node ast.Node) bool {
		if _, ok := node.(*ast.GoStmt); ok {
			goStatements++
		}
		return true
	})

	syncImports := 0
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return 0, 0, fmt.Errorf("parse import in %s: %w", filename, err)
		}
		if path == "sync" || path == "sync/atomic" {
			syncImports++
		}
	}
	return goStatements, syncImports, nil
}

func resolveExhaustiveRacePackages(modulePath string, all []listedPackage) ([]listedPackage, scopedRacePackage, error) {
	ouroborosImportPath := modulePath + "/" + ouroborosRaceRelativePath
	fullRace := make([]listedPackage, 0, len(all)-1)
	var ouroboros listedPackage
	for _, pkg := range all {
		if pkg.ImportPath == ouroborosImportPath {
			if ouroboros.ImportPath != "" {
				return nil, scopedRacePackage{}, fmt.Errorf("exhaustive race package %s is listed more than once", ouroborosImportPath)
			}
			ouroboros = pkg
			continue
		}
		fullRace = append(fullRace, pkg)
	}
	if ouroboros.ImportPath == "" {
		return nil, scopedRacePackage{}, fmt.Errorf("exhaustive race package %s is missing", ouroborosImportPath)
	}
	if err := validateCoverage(all, map[string][]listedPackage{
		"full-race":      fullRace,
		"ouroboros-race": {ouroboros},
	}); err != nil {
		return nil, scopedRacePackage{}, err
	}

	testNames, err := inspectTestNames(ouroboros)
	if err != nil {
		return nil, scopedRacePackage{}, err
	}
	seen := make(map[string]struct{}, len(ouroborosRaceSkips))
	for _, skip := range ouroborosRaceSkips {
		if strings.TrimSpace(skip.reason) == "" {
			return nil, scopedRacePackage{}, fmt.Errorf("Ouroboros race skip %s has no review reason", skip.testName)
		}
		if _, duplicate := seen[skip.testName]; duplicate {
			return nil, scopedRacePackage{}, fmt.Errorf("Ouroboros race skip %s is listed more than once", skip.testName)
		}
		seen[skip.testName] = struct{}{}
		if _, exists := testNames[skip.testName]; !exists {
			return nil, scopedRacePackage{}, fmt.Errorf("Ouroboros race skip %s does not name a current test", skip.testName)
		}
	}
	if len(testNames) <= len(ouroborosRaceSkips) {
		return nil, scopedRacePackage{}, fmt.Errorf("Ouroboros scoped race lane would run no tests: discovered=%d skipped=%d", len(testNames), len(ouroborosRaceSkips))
	}
	return fullRace, scopedRacePackage{
		listedPackage: ouroboros,
		testCount:     len(testNames),
		skips:         append([]raceSkipTarget(nil), ouroborosRaceSkips...),
	}, nil
}

func inspectTestNames(pkg listedPackage) (map[string]struct{}, error) {
	names := make(map[string]struct{})
	files := append(append([]string{}, pkg.TestGoFiles...), pkg.XTestGoFiles...)
	for _, name := range files {
		path := filepath.Join(pkg.Dir, name)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			names[fn.Name.Name] = struct{}{}
		}
	}
	return names, nil
}

func raceSkipPattern(skips []raceSkipTarget) string {
	names := make([]string, 0, len(skips))
	for _, skip := range skips {
		names = append(names, regexp.QuoteMeta(skip.testName))
	}
	return "^(" + strings.Join(names, "|") + ")$"
}

func selectPackages(plan testPlan, partition string) ([]string, error) {
	var packages []string
	switch partition {
	case "unit":
		packages = make([]string, 0, len(plan.unit))
		for _, pkg := range plan.unit {
			packages = append(packages, pkg.ImportPath)
		}
	case "cli":
		packages = make([]string, 0, len(plan.cli))
		for _, pkg := range plan.cli {
			packages = append(packages, pkg.ImportPath)
		}
	case "race":
		packages = make([]string, 0, len(plan.race))
		for _, pkg := range plan.race {
			packages = append(packages, pkg.ImportPath)
		}
	case "full-race":
		packages = make([]string, 0, len(plan.fullRace))
		for _, pkg := range plan.fullRace {
			packages = append(packages, pkg.ImportPath)
		}
	default:
		return nil, fmt.Errorf("unknown package partition %q", partition)
	}
	return packages, nil
}

func printPlan(w io.Writer, plan testPlan) {
	fmt.Fprintf(
		w,
		"citest: partition verified module=%s total=%d unit=%d cli=%d pr-race=%d full-race=%d ouroboros-race-tests=%d ouroboros-race-skips=%d\n",
		plan.modulePath,
		len(plan.all),
		len(plan.unit),
		len(plan.cli),
		len(plan.race),
		len(plan.fullRace),
		plan.ouroborosRace.testCount,
		len(plan.ouroborosRace.skips),
	)
	fmt.Fprintf(w, "citest: cli %s\n", plan.cli[0].ImportPath)
	for _, pkg := range plan.race {
		fmt.Fprintf(
			w,
			"citest: race %s tests=%d goroutines=%d sync-imports=%d reason=%q\n",
			pkg.ImportPath,
			pkg.evidence.tests,
			pkg.evidence.goStatements,
			pkg.evidence.syncImports,
			pkg.target.reason,
		)
	}
	fmt.Fprintf(w, "citest: scoped race %s runs=%d skips=%d\n", plan.ouroborosRace.ImportPath, plan.ouroborosRace.testCount-len(plan.ouroborosRace.skips), len(plan.ouroborosRace.skips))
	for _, skip := range plan.ouroborosRace.skips {
		fmt.Fprintf(w, "citest: scoped race skip %s reason=%q\n", skip.testName, skip.reason)
	}
}

// A test's shard comes from a hash of its name, so adding a test never moves
// another one. Hashing the name picks a bucket in [0, 100); the cutoffs turn
// buckets into shards. Cutoffs are uneven on purpose: a lane that carries other
// steps (documentation checks, perf gates) takes a smaller share of tests.
// Pins override the hash for tests whose measured cost the hash cannot spread.
type shardLayout struct {
	name    string
	cutoffs []int          // len = shard count - 1, ascending, each in (0, 100)
	pins    map[string]int // test name -> shard, takes precedence over the hash
}

func (l shardLayout) count() int { return len(l.cutoffs) + 1 }

func (l shardLayout) shardOf(name string) int {
	if shard, ok := l.pins[name]; ok {
		return shard
	}
	sum := sha256.Sum256([]byte(name))
	bucket := int(binary.BigEndian.Uint16(sum[:2])) % 100
	for shard, cutoff := range l.cutoffs {
		if bucket < cutoff {
			return shard
		}
	}
	return len(l.cutoffs)
}

// validatePins fails on a pin that names no current test or targets a shard
// that does not exist.
func (l shardLayout) validatePins(current map[string]bool) error {
	for name, shard := range l.pins {
		if !current[name] {
			return fmt.Errorf("%s shard pin %q names no current test", l.name, name)
		}
		if shard < 0 || shard >= l.count() {
			return fmt.Errorf("%s shard pin %q targets shard %d, want 0..%d", l.name, name, shard, l.count()-1)
		}
	}
	return nil
}

// split assigns every name to exactly one shard. It fails on a duplicate or
// malformed name, a pin that names no current test or an out-of-range shard,
// and an empty shard, so a layout that drifts from the source stops the build.
func (l shardLayout) split(names []string) ([][]string, error) {
	for i, cutoff := range l.cutoffs {
		if cutoff <= 0 || cutoff >= 100 || (i > 0 && cutoff <= l.cutoffs[i-1]) {
			return nil, fmt.Errorf("%s shard cutoffs %v must ascend within (0, 100)", l.name, l.cutoffs)
		}
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if strings.ContainsAny(name, " \t/") || seen[name] {
			return nil, fmt.Errorf("invalid or duplicate %s test name %q", l.name, name)
		}
		seen[name] = true
	}
	if err := l.validatePins(seen); err != nil {
		return nil, err
	}
	shards := make([][]string, l.count())
	for _, name := range names {
		shard := l.shardOf(name)
		shards[shard] = append(shards[shard], name)
	}
	for i := range shards {
		if len(shards[i]) == 0 {
			return nil, fmt.Errorf("%s shard %d contains no tests", l.name, i)
		}
		sort.Strings(shards[i])
	}
	return shards, nil
}

// cliLayout: lane 0 also runs the documentation example tests, the tutorial
// build and the docs-site compile (about 160s), so it takes the smaller share.
//
// Pins spread the eight production-build tests that each take 90-230s; the hash
// leaves them lumpy (run 37910090971: lane 0 ran 582s of tests, lane 1 313s).
// Seconds are the measured test durations from that run. Re-measure with the
// -v output of a full run before moving a pin.
var cliLayout = shardLayout{name: "CLI", cutoffs: []int{28, 64}, pins: map[string]int{
	"TestRunBuildProdPreservesFileModuleHooksInStaticExport": 0, // 214s
	"TestRunBuildProdPrerenderDisabledKeepsServerAndAssets":  0, // 163s
	"TestRunBuildProdWritesHybridStaticBundleForStarterApp":  1, // 229s
	"TestControllerInputAssetsAcrossBuildModes":              1, // 194s
	"TestRunInitStrictFormsBuildAndServe":                    1, //  89s
	"TestExportStagesExternalFileCSS":                        2, // 187s
	"TestRunBuildRelocatedBundleRendersSiblingFragment":      2, // 167s
	"TestRunBuildProdHandlesRelativeProjectDir":              2, // 128s
}}

// browserLayout: shard 0 also runs the Ouroboros media smoke, the perf driver
// tests and the perf budget gate (about 270s), so it takes the smaller share.
//
// Pins spread the seven tests that each take 88-260s; the hash put all of them
// but two in one shard (run 37906534414: shards ran 63s, 423s and over 1000s of
// tests). Seconds are measured durations from runs 37906534414 and 37910090971.
var browserLayout = shardLayout{name: "browser", cutoffs: []int{21, 61}, pins: map[string]int{
	"TestDocsHomeSceneCanvasesStayBounded":                0, // 257s
	"TestPlaygroundDirectLoadMetadataAndMobileHeader":     1, // 197s
	"TestPlaygroundCounterHydratesAndUpdates":             1, // 196s
	"TestDocsSiteServes":                                  1, //  88s
	"TestProductionBuildHydratesStrictIsland":             2, // 168s
	"TestProductionBuildRunsMixedTinyGoAndStandardGoWASM": 2, // 182s
	"TestPrefixedProductionBuild":                         2, // 148s
}}

const (
	browserRelativePath = "e2e"
	browserBuildTag     = "e2e"
)

func shardIndex(args []string, layout shardLayout) (int, error) {
	usage := fmt.Errorf("usage: citest %s <0..%d>", strings.ToLower(layout.name), layout.count()-1)
	if len(args) != 1 {
		return 0, usage
	}
	index, err := strconv.Atoi(args[0])
	if err != nil || index < 0 || index >= layout.count() {
		return 0, usage
	}
	return index, nil
}

func runShard(layout shardLayout, index int, goBinary string, flags []string, pkg string, stdout, stderr io.Writer) error {
	listArgs := append(append([]string{"test"}, tagFlags(flags)...), "-list", ".", pkg)
	list := exec.Command(goBinary, listArgs...)
	list.Stderr = stderr
	output, err := list.Output()
	if err != nil {
		return fmt.Errorf("discover %s tests: %w", layout.name, err)
	}
	shards, err := layout.split(discoveredTests(string(output)))
	if err != nil {
		return err
	}
	total := 0
	for _, shard := range shards {
		total += len(shard)
	}
	names := make([]string, len(shards[index]))
	for i, name := range shards[index] {
		names[i] = regexp.QuoteMeta(name)
	}
	pattern := "^(" + strings.Join(names, "|") + ")$"
	fmt.Fprintf(stderr, "citest: %s shard %d runs %d of %d tests/seed corpora\n", layout.name, index, len(names), total)
	runArgs := append(append([]string{"test"}, flags...), "-run", pattern, pkg)
	command := exec.Command(goBinary, runArgs...)
	command.Stdout, command.Stderr, command.Stdin = stdout, stderr, os.Stdin
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s shard %d: %w", layout.name, index, err)
	}
	return nil
}

// Discover runnable tests with the same Go tool and build tags used to execute
// them, including examples and fuzz seed corpora. Both jobs run all subtests.
func runCLIShard(args []string, goBinary string, stdout, stderr io.Writer) error {
	index, err := shardIndex(args, cliLayout)
	if err != nil {
		return err
	}
	return runShard(cliLayout, index, goBinary, []string{"-v", "-timeout", "25m"}, "./cmd/gosx", stdout, stderr)
}

// runBrowserShard runs one share of the e2e browser suite. -v prints each
// test's duration so the layout can be rebalanced from a CI log.
func runBrowserShard(args []string, goBinary string, stdout, stderr io.Writer) error {
	index, err := shardIndex(args, browserLayout)
	if err != nil {
		return err
	}
	// Each docs test starts `gosx dev`, which waits only 45s for /readyz. In the
	// single-job suite earlier tests had already filled the Go build cache; a
	// shard whose first test is a docs test would start cold and miss that
	// deadline (run 37906534414, browser-tests-b). Build both programs first.
	for _, pkg := range []string{"./cmd/gosx", "./examples/gosx-docs"} {
		warm := exec.Command(goBinary, "build", "-o", os.DevNull, pkg)
		warm.Stdout, warm.Stderr = stderr, stderr
		fmt.Fprintf(stderr, "citest: warming build cache: go build %s\n", pkg)
		if err := warm.Run(); err != nil {
			return fmt.Errorf("warm build cache %s: %w", pkg, err)
		}
	}
	return runShard(browserLayout, index, goBinary,
		[]string{"-tags", browserBuildTag, "-v", "-timeout", "30m"}, "./"+browserRelativePath, stdout, stderr)
}

// tagFlags keeps only the build-tag flag pair: listing needs the tag but not
// the timeout or -v.
func tagFlags(flags []string) []string {
	for i := 0; i+1 < len(flags); i++ {
		if flags[i] == "-tags" {
			return flags[i : i+2]
		}
	}
	return nil
}

// discoveredTests keeps the runnable test names from `go test -list` output,
// dropping the trailing "ok" line.
func discoveredTests(output string) []string {
	var names []string
	for _, name := range strings.Split(output, "\n") {
		name = strings.TrimSpace(name)
		if strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Example") || strings.HasPrefix(name, "Fuzz") {
			names = append(names, name)
		}
	}
	return names
}

func splitBrowserTests(names []string) ([][]string, error) {
	return browserLayout.split(names)
}

// verifyCLIShardPins checks that every CLI pin names a test in cmd/gosx. It
// reads source rather than running `go test -list`, so it needs no compile.
// Files with build tags are included, which is a superset of what any one run
// lists; that is enough to catch a renamed or deleted pinned test.
func verifyCLIShardPins(w io.Writer) error {
	names, err := testNamesInDir(cliRelativePath)
	if err != nil {
		return err
	}
	current := make(map[string]bool, len(names))
	for _, name := range names {
		current[name] = true
	}
	if err := cliLayout.validatePins(current); err != nil {
		return err
	}
	fmt.Fprintf(w, "citest: CLI shard pins verified pins=%d\n", len(cliLayout.pins))
	return nil
}

// verifyBrowserShards checks the browser layout against the e2e test source:
// every Test function lands in exactly one shard and every pin is current.
func verifyBrowserShards(w io.Writer) error {
	names, err := browserTestNames(browserRelativePath)
	if err != nil {
		return err
	}
	shards, err := splitBrowserTests(names)
	if err != nil {
		return err
	}
	sizes := make([]string, len(shards))
	for i, shard := range shards {
		sizes[i] = strconv.Itoa(len(shard))
	}
	fmt.Fprintf(w, "citest: browser shards verified tests=%d shards=%s\n", len(names), strings.Join(sizes, "/"))
	return nil
}

// browserTestNames reads Test functions from the e2e package source. Every
// file there carries the e2e build tag, so parsing all of them matches what
// `go test -tags e2e -list` reports, without compiling the package.
func browserTestNames(dir string) ([]string, error) {
	names, err := testNamesInDir(dir)
	if err != nil {
		return nil, err
	}
	filtered := names[:0]
	for _, name := range names {
		if strings.HasPrefix(name, "Test") && name != "TestMain" {
			filtered = append(filtered, name)
		}
	}
	return filtered, nil
}

// testNamesInDir returns the Test, Fuzz and Example functions declared in the
// directory's _test.go files, sorted and without duplicates.
func testNamesInDir(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, err
	}
	var names []string
	seen := make(map[string]bool)
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			for _, prefix := range []string{"Test", "Fuzz", "Example"} {
				if strings.HasPrefix(fn.Name.Name, prefix) && !seen[fn.Name.Name] {
					seen[fn.Name.Name] = true
					names = append(names, fn.Name.Name)
				}
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

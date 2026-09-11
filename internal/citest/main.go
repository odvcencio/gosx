// Command citest owns the package partitions used by CI.
//
// Keeping discovery here instead of in shell pipelines makes a newly added
// package fail the partition validation if it is ever omitted or included
// twice. The focused race set is also validated against the current source:
// every entry must have tests and production concurrency primitives.
package main

import (
	"bytes"
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
		return errors.New("usage: citest verify | list <unit|cli|race|full-race> | test <unit|race|full-race|ouroboros-race>")
	}

	goBinary := os.Getenv("GOSX_CI_GO")
	if goBinary == "" {
		goBinary = "go"
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
		return nil
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

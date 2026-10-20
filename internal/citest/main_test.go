package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestValidateCoverageAcceptsExactDisjointPartition(t *testing.T) {
	all := fakePackages("example/a", "example/b", "example/c")
	err := validateCoverage(all, map[string][]listedPackage{
		"unit": {all[0], all[2]},
		"cli":  {all[1]},
	})
	if err != nil {
		t.Fatalf("validateCoverage() error = %v", err)
	}
}

func TestValidateCoverageRejectsGap(t *testing.T) {
	all := fakePackages("example/a", "example/b")
	err := validateCoverage(all, map[string][]listedPackage{
		"unit": {all[0]},
	})
	if err == nil || !strings.Contains(err.Error(), "gaps") {
		t.Fatalf("validateCoverage() error = %v, want gap", err)
	}
}

func TestValidateCoverageRejectsOverlap(t *testing.T) {
	all := fakePackages("example/a", "example/b")
	err := validateCoverage(all, map[string][]listedPackage{
		"unit": {all[0], all[1]},
		"cli":  {all[1]},
	})
	if err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("validateCoverage() error = %v, want overlap", err)
	}
}

func TestValidateCoverageRejectsUnknownPackage(t *testing.T) {
	all := fakePackages("example/a")
	unknown := listedPackage{ImportPath: "example/unknown"}
	err := validateCoverage(all, map[string][]listedPackage{
		"unit": {all[0], unknown},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("validateCoverage() error = %v, want unknown package", err)
	}
}

func TestInspectConcurrencySourceFindsRaceEvidence(t *testing.T) {
	source := []byte(`package sample

import (
	"sync"
	"sync/atomic"
)

var mu sync.Mutex
var n atomic.Int64

func start() {
	go func() {
		n.Add(1)
	}()
}
`)
	goStatements, syncImports, err := inspectConcurrencySource("sample.go", source)
	if err != nil {
		t.Fatalf("inspectConcurrencySource() error = %v", err)
	}
	if goStatements != 1 || syncImports != 2 {
		t.Fatalf("inspectConcurrencySource() = (%d, %d), want (1, 2)", goStatements, syncImports)
	}
}

func TestInspectConcurrencySourceRejectsInvalidGo(t *testing.T) {
	_, _, err := inspectConcurrencySource("broken.go", []byte("package broken\nfunc"))
	if err == nil {
		t.Fatal("inspectConcurrencySource() accepted invalid Go")
	}
}

func TestResolveExhaustiveRacePackagesValidatesExactOuroborosSkips(t *testing.T) {
	ouroboros := writeOuroborosRaceFixture(t, len(ouroborosRaceSkips))
	all := []listedPackage{
		{ImportPath: "example.dev/gosx/action"},
		ouroboros,
		{ImportPath: "example.dev/gosx/server"},
	}
	fullRace, scoped, err := resolveExhaustiveRacePackages("example.dev/gosx", all)
	if err != nil {
		t.Fatalf("resolveExhaustiveRacePackages() error = %v", err)
	}
	if len(fullRace) != 2 || scoped.ImportPath != ouroboros.ImportPath {
		t.Fatalf("race split = full %#v scoped %#v", fullRace, scoped)
	}
	if scoped.testCount != len(ouroborosRaceSkips)+1 || len(scoped.skips) != len(ouroborosRaceSkips) {
		t.Fatalf("scoped test accounting = tests %d skips %d", scoped.testCount, len(scoped.skips))
	}
	pattern, err := regexp.Compile(raceSkipPattern(scoped.skips))
	if err != nil {
		t.Fatalf("race skip pattern does not compile: %v", err)
	}
	for _, skip := range ouroborosRaceSkips {
		if !pattern.MatchString(skip.testName) {
			t.Fatalf("race skip pattern does not match %s", skip.testName)
		}
		if pattern.MatchString(skip.testName + "Suffix") {
			t.Fatalf("race skip pattern is not exact for %s", skip.testName)
		}
	}
	if pattern.MatchString("TestRetainedRaceCoverage") {
		t.Fatal("race skip pattern matched the retained coverage test")
	}
}

func TestResolveExhaustiveRacePackagesRejectsStaleSkipName(t *testing.T) {
	ouroboros := writeOuroborosRaceFixture(t, len(ouroborosRaceSkips)-1)
	_, _, err := resolveExhaustiveRacePackages("example.dev/gosx", []listedPackage{ouroboros})
	if err == nil || !strings.Contains(err.Error(), "does not name a current test") {
		t.Fatalf("resolveExhaustiveRacePackages() error = %v, want stale skip", err)
	}
}

func writeOuroborosRaceFixture(t *testing.T, skipCount int) listedPackage {
	t.Helper()
	dir := t.TempDir()
	var source strings.Builder
	source.WriteString("package ouroboros\n\nimport \"testing\"\n\n")
	for _, skip := range ouroborosRaceSkips[:skipCount] {
		source.WriteString("func " + skip.testName + "(t *testing.T) {}\n")
	}
	source.WriteString("func TestRetainedRaceCoverage(t *testing.T) {}\n")
	name := "race_test.go"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(source.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return listedPackage{
		Dir:         dir,
		ImportPath:  "example.dev/gosx/" + ouroborosRaceRelativePath,
		TestGoFiles: []string{name},
	}
}

func fakePackages(importPaths ...string) []listedPackage {
	packages := make([]listedPackage, len(importPaths))
	for i, importPath := range importPaths {
		packages[i] = listedPackage{ImportPath: importPath}
	}
	return packages
}

// splitSyntheticCLITests splits made-up names with the CLI cutoffs but no pins,
// because pins name real tests that a synthetic list does not contain.
func splitSyntheticCLITests(output string) ([][]string, error) {
	return shardLayout{name: "CLI", cutoffs: cliLayout.cutoffs}.split(discoveredTests(output))
}

func TestCLIShardsAreCompleteDisjointAndStable(t *testing.T) {
	var output strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&output, "TestCase%d\n", i)
	}
	output.WriteString("ExampleCLI\nFuzzCLI\nok example.test/cli 0.01s\n")
	shards, err := splitSyntheticCLITests(output.String())
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]int)
	for index, names := range shards {
		for _, name := range names {
			if _, exists := seen[name]; exists {
				t.Fatalf("overlapping test %s", name)
			}
			seen[name] = index
		}
	}
	if len(seen) != 102 {
		t.Fatalf("covered %d names, want 102", len(seen))
	}
	if len(shards) != 3 {
		t.Fatalf("got %d CLI shards, want 3", len(shards))
	}
	extended, err := splitSyntheticCLITests(output.String() + "TestAnotherCase\n")
	if err != nil {
		t.Fatal(err)
	}
	for index, names := range extended {
		for _, name := range names {
			if prior, exists := seen[name]; exists && prior != index {
				t.Fatalf("%s moved shards", name)
			}
		}
	}
	for _, output := range []string{"", "TestCase\nTestCase\n", "Test Bad\n"} {
		if _, err := splitSyntheticCLITests(output); err == nil {
			t.Fatalf("accepted invalid discovery %q", output)
		}
	}
}

func TestShardLayoutSplitIsCompleteDisjointAndStable(t *testing.T) {
	layout := shardLayout{name: "sample", cutoffs: []int{20, 60}}
	var names []string
	for i := 0; i < 300; i++ {
		names = append(names, fmt.Sprintf("TestSample%d", i))
	}
	shards, err := layout.split(names)
	if err != nil {
		t.Fatal(err)
	}
	if len(shards) != 3 {
		t.Fatalf("got %d shards, want 3", len(shards))
	}
	owner := make(map[string]int)
	for index, shard := range shards {
		for _, name := range shard {
			if prior, dup := owner[name]; dup {
				t.Fatalf("%s is in shards %d and %d", name, prior, index)
			}
			owner[name] = index
		}
	}
	if len(owner) != len(names) {
		t.Fatalf("covered %d of %d tests", len(owner), len(names))
	}
	// The cutoffs set the share: 20/40/40 of 300 within a loose tolerance.
	for index, want := range []int{60, 120, 120} {
		if got := len(shards[index]); got < want*7/10 || got > want*13/10 {
			t.Errorf("shard %d has %d tests, want about %d", index, got, want)
		}
	}
	grown, err := layout.split(append(append([]string{}, names...), "TestSampleNew"))
	if err != nil {
		t.Fatal(err)
	}
	for index, shard := range grown {
		for _, name := range shard {
			if prior, ok := owner[name]; ok && prior != index {
				t.Fatalf("%s moved from shard %d to %d after a test was added", name, prior, index)
			}
		}
	}
}

func TestShardLayoutPinsOverrideHash(t *testing.T) {
	layout := shardLayout{name: "sample", cutoffs: []int{50}}
	names := []string{"TestA", "TestB", "TestC", "TestD", "TestE", "TestF"}
	for _, target := range []int{0, 1} {
		layout.pins = map[string]int{"TestA": target}
		shards, err := layout.split(names)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, name := range shards[target] {
			found = found || name == "TestA"
		}
		if !found {
			t.Fatalf("TestA pinned to shard %d is not there: %v", target, shards)
		}
	}
}

func TestShardLayoutRejectsDrift(t *testing.T) {
	names := []string{"TestA", "TestB", "TestC", "TestD", "TestE", "TestF", "TestG", "TestH"}
	tests := []struct {
		name   string
		layout shardLayout
		names  []string
		want   string
	}{
		{"duplicate name", shardLayout{name: "sample", cutoffs: []int{50}}, append(append([]string{}, names...), "TestA"), "duplicate"},
		{"subtest name", shardLayout{name: "sample", cutoffs: []int{50}}, []string{"TestA/sub"}, "invalid"},
		{"stale pin", shardLayout{name: "sample", cutoffs: []int{50}, pins: map[string]int{"TestGone": 0}}, names, "names no current test"},
		{"pin out of range", shardLayout{name: "sample", cutoffs: []int{50}, pins: map[string]int{"TestA": 2}}, names, "targets shard 2"},
		{"unordered cutoffs", shardLayout{name: "sample", cutoffs: []int{60, 20}}, names, "must ascend"},
		{"empty shard", shardLayout{name: "sample", cutoffs: []int{50}, pins: map[string]int{"TestA": 1}}, []string{"TestA"}, "contains no tests"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.layout.split(test.names)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("split() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBrowserShardsCoverRepositoryE2ETests(t *testing.T) {
	names, err := browserTestNames(filepath.Join("..", "..", browserRelativePath))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 20 {
		t.Fatalf("found only %d e2e tests; discovery is broken", len(names))
	}
	shards, err := splitBrowserTests(names)
	if err != nil {
		t.Fatal(err)
	}
	if len(shards) != 3 {
		t.Fatalf("got %d browser shards, want 3", len(shards))
	}
	seen := make(map[string]int)
	for index, shard := range shards {
		for _, name := range shard {
			seen[name]++
			if seen[name] > 1 {
				t.Fatalf("%s runs in more than one browser shard (again in %d)", name, index)
			}
		}
	}
	for _, name := range names {
		if seen[name] != 1 {
			t.Fatalf("%s runs in %d browser shards, want 1", name, seen[name])
		}
	}
}

func TestCLIShardPinsNameCurrentTests(t *testing.T) {
	names, err := testNamesInDir(filepath.Join("..", "..", cliRelativePath))
	if err != nil {
		t.Fatal(err)
	}
	current := make(map[string]bool, len(names))
	for _, name := range names {
		current[name] = true
	}
	if err := cliLayout.validatePins(current); err != nil {
		t.Fatal(err)
	}
	delete(current, "TestControllerInputAssetsAcrossBuildModes")
	if err := cliLayout.validatePins(current); err == nil || !strings.Contains(err.Error(), "names no current test") {
		t.Fatalf("validatePins() accepted a deleted pinned test: %v", err)
	}
}

func TestPinnedShardsSpreadHeavyTests(t *testing.T) {
	for _, layout := range []shardLayout{cliLayout, browserLayout} {
		used := make(map[int]int)
		for _, shard := range layout.pins {
			used[shard]++
		}
		if len(used) != layout.count() {
			t.Errorf("%s pins use %d of %d shards: %v", layout.name, len(used), layout.count(), used)
		}
	}
}

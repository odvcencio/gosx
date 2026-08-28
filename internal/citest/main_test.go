package main

import (
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

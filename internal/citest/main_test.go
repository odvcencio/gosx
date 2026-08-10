package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestPartitionTestPackagesSeparatesUnitCLIAndOuroboros(t *testing.T) {
	all := fakePackages(
		"example.test/gosx/auth",
		"example.test/gosx/cmd/gosx",
		"example.test/gosx/perf/ouroboros",
		"example.test/gosx/server",
	)
	unit, cli, ouroboros, err := partitionTestPackages("example.test/gosx", all)
	if err != nil {
		t.Fatalf("partitionTestPackages() error = %v", err)
	}
	if got, want := importPaths(unit), []string{"example.test/gosx/auth", "example.test/gosx/server"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unit partition = %v, want %v", got, want)
	}
	if got, want := importPaths(cli), []string{"example.test/gosx/cmd/gosx"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cli partition = %v, want %v", got, want)
	}
	if got, want := importPaths(ouroboros), []string{"example.test/gosx/perf/ouroboros"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ouroboros partition = %v, want %v", got, want)
	}
	if err := validateCoverage(all, map[string][]listedPackage{
		"unit":      unit,
		"cli":       cli,
		"ouroboros": ouroboros,
	}); err != nil {
		t.Fatalf("validateCoverage() error = %v", err)
	}
}

func TestPartitionTestPackagesRequiresExactCLIAndOuroborosMembership(t *testing.T) {
	all := fakePackages(
		"example.test/gosx/auth",
		"example.test/gosx/cmd/gosx",
	)
	_, _, _, err := partitionTestPackages("example.test/gosx", all)
	if err == nil || !strings.Contains(err.Error(), "ouroboros partition contains 0 packages") {
		t.Fatalf("partitionTestPackages() error = %v, want missing ouroboros", err)
	}

	all = fakePackages(
		"example.test/gosx/auth",
		"example.test/gosx/perf/ouroboros",
	)
	_, _, _, err = partitionTestPackages("example.test/gosx", all)
	if err == nil || !strings.Contains(err.Error(), "CLI partition contains 0 packages") {
		t.Fatalf("partitionTestPackages() error = %v, want missing CLI", err)
	}
}

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

func TestValidateCoverageRejectsDuplicateGoListPackage(t *testing.T) {
	all := fakePackages("example/a", "example/a")
	err := validateCoverage(all, map[string][]listedPackage{
		"unit": {all[0]},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate package") {
		t.Fatalf("validateCoverage() error = %v, want duplicate package", err)
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

func fakePackages(importPaths ...string) []listedPackage {
	packages := make([]listedPackage, len(importPaths))
	for i, importPath := range importPaths {
		packages[i] = listedPackage{ImportPath: importPath}
	}
	return packages
}

func importPaths(packages []listedPackage) []string {
	paths := make([]string, len(packages))
	for i, pkg := range packages {
		paths[i] = pkg.ImportPath
	}
	return paths
}

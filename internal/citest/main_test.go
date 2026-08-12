package main

import (
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

func fakePackages(importPaths ...string) []listedPackage {
	packages := make([]listedPackage, len(importPaths))
	for i, importPath := range importPaths {
		packages[i] = listedPackage{ImportPath: importPath}
	}
	return packages
}

package main

import (
	"reflect"
	"testing"
)

func TestInterspersedPerfArgsMovesFlagsBeforeURLs(t *testing.T) {
	got := interspersedPerfArgs([]string{
		"http://localhost:3000/",
		"--mobile", "pixel7",
		"http://localhost:3000/scene",
		"--throttle=4",
		"--coverage",
		"--budget", "perf-budget.json",
	})
	want := []string{
		"--mobile", "pixel7",
		"--throttle=4",
		"--coverage",
		"--budget", "perf-budget.json",
		"http://localhost:3000/",
		"http://localhost:3000/scene",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interspersedPerfArgs() = %#v, want %#v", got, want)
	}
}

func TestInterspersedPerfArgsStopsAtSeparator(t *testing.T) {
	got := interspersedPerfArgs([]string{
		"--json",
		"--",
		"http://localhost:3000/",
		"--not-a-flag",
	})
	want := []string{
		"--json",
		"http://localhost:3000/",
		"--not-a-flag",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interspersedPerfArgs() = %#v, want %#v", got, want)
	}
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExportSkipsLoadAndActionsUnlessOptedIn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "safe-export")
	if err := RunInit(dir, "example.com/safe-export", ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, dir)
	for _, name := range []string{"loader", "actions", "opted", "fresh"} {
		mustWriteFile(t, filepath.Join(dir, "app", name, "page.gsx"), "package app\n\nfunc Page() Node {\n\treturn <p>snapshot</p>\n}\n")
		hooks := `Load: func(*route.RouteContext, route.FilePage) (any, error) { panic("default loader ran during export") },`
		if name == "actions" {
			hooks = `Actions: route.FileActions{"save": func(*action.Context) error { return nil }},`
		}
		if name == "opted" || name == "fresh" {
			hooks = `Load: func(*route.RouteContext, route.FilePage) (any, error) { return nil, nil },`
		}
		imports := `"m31labs.dev/gosx/route"`
		if name == "actions" {
			imports += `; "m31labs.dev/gosx/action"`
		}
		mustWriteFile(t, filepath.Join(dir, "app", name, "page.server.go"), "package app\nimport ("+imports+")\nfunc init() { if err := route.RegisterFileModuleHere(route.FileModuleOptions{"+hooks+"}); err != nil { panic(err) } }\n")
	}
	mustWriteFile(t, filepath.Join(dir, "app", "opted", "route.config.json"), `{"prerender":true}`)
	mustWriteFile(t, filepath.Join(dir, "app", "fresh", "route.config.json"), `{"prerender":true,"cache":{"public":true,"maxAge":"30s"}}`)
	tidyModule(t, dir)
	warnings, err := os.CreateTemp(t.TempDir(), "warnings")
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = warnings
	defer func() { os.Stderr = oldStderr; warnings.Close() }()
	if err := RunExport(dir); err != nil {
		t.Fatal(err)
	}
	os.Stderr = oldStderr
	var manifest exportManifest
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "dist", "export.json"))), &manifest); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"/stack": true, "/opted": true, "/fresh": true}
	if len(manifest.Pages) != len(want) {
		t.Fatalf("exported pages=%v", manifest.Pages)
	}
	for _, path := range manifest.Pages {
		if !want[path] {
			t.Fatalf("unexpected exported page %s", path)
		}
	}
	for _, path := range []string{"index.html", "loader/index.html", "actions/index.html"} {
		if _, err := os.Stat(filepath.Join(dir, "dist", "static", path)); !os.IsNotExist(err) {
			t.Fatalf("dynamic page artifact %s: %v", path, err)
		}
	}
	warningLog := readFile(t, warnings.Name())
	if !strings.Contains(warningLog, prerenderLoadWarning("/opted")) {
		t.Fatalf("missing frozen-loader warning: %s", warningLog)
	}
	if strings.Contains(warningLog, prerenderLoadWarning("/fresh")) {
		t.Fatal("revalidating loader produced a frozen-data warning")
	}
}

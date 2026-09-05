package strictcheck

import (
	"m31labs.dev/gosx/internal/gsxcorpus"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFileGoOwnedModels(t *testing.T) {
	dir := t.TempDir()
	if err := (gsxcorpus.Case{}).WriteStrictCheckModule(dir); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(dir, "page.gsx")
	model := filepath.Join(dir, "model.go")
	mustWrite(t, page, "package main\ncomponent Page(props: Props) {\n return <p>{props.Title}</p>\n}\n")
	good := "package main\ntype Props struct {Title string; private *int}\n"
	mustWrite(t, model, good)
	check := func(want string) {
		t.Helper()
		ctx, cancel := strictCheckCorpusContext(t)
		defer cancel()
		err := CheckFileWithOptions(ctx, page, Options{})
		if want == "" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("wanted %q got %v", want, err)
		}
	}
	check("")
	mustWrite(t, model, strings.Replace(good, "Title", "Other", 1))
	check("Title")
	mustWrite(t, model, good)
	check("")
	dup := filepath.Join(dir, "duplicate.go")
	mustWrite(t, dup, good)
	check("duplicate model Props")
	if err := os.Remove(dup); err != nil {
		t.Fatal(err)
	}
	check("")
	if err := os.Remove(model); err != nil {
		t.Fatal(err)
	}
	check("struct schema")
	mustWrite(t, model, good)
	check("")
}

func TestCheckFileSharedModelBuildSelection(t *testing.T) {
	root, page := sharedRefreshModule(t)
	mustWrite(t, page, "package main\nimport ui \"./ui\"\ncomponent Page() {\n return <ui.Badge title=\"hello\"/>\n}\n")
	mustWrite(t, filepath.Join(root, "app", "ui", "badge.gsx"), "package ui\ncomponent Badge(props: BadgeProps) {\n return <span>{props.Title}</span>\n}\n")
	mustWrite(t, filepath.Join(root, "app", "ui", "model.go"), "//go:build linux && !feature\n\npackage ui\ntype BadgeProps struct {Title string}\n")
	mustWrite(t, filepath.Join(root, "app", "ui", "alternate.go"), "//go:build windows || feature\n\npackage ui\ntype BadgeProps struct {Title int}\n")
	for _, tc := range []struct{ flags, want string }{{"-mod=readonly", ""}, {"-mod=readonly -tags=feature", "cannot use"}, {"-mod=readonly", ""}} {
		ctx, cancel := strictCheckCorpusContext(t)
		err := CheckFileWithOptions(ctx, page, Options{Env: append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0"), GOFLAGS: tc.flags, GOWORK: "off"})
		cancel()
		if tc.want == "" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: %v", tc.flags, err)
		}
	}
}

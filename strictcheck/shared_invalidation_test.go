package strictcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/gsxcorpus"
)

// These tests check sequential source snapshots in one process. They do not
// assert reverse-dependent checking or admit imported components into islands.
func TestCheckFileSharedSchemaRefreshProps(t *testing.T) {
	root, page := sharedRefreshModule(t)
	target := filepath.Join(root, "app", "ui", "badge.gsx")
	before := `package ui
type BadgeProps struct { Tone string }
component Badge(props: BadgeProps) {
	return <span>{props.Tone}</span>
}
`
	caller := `package main
import ui "./ui"
component Page() {
	return <ui.Badge Tone="home" />
}
`
	mustWrite(t, target, before)
	mustWrite(t, page, caller)
	checkSharedRefresh(t, page, "initial", "")

	// Rename both the declaration and its use so only the unchanged caller is
	// invalid. The real Go compiler checks the projected props literal.
	mustWrite(t, target, strings.ReplaceAll(before, "Tone", "Mood"))
	checkSharedRefresh(t, page, "renamed target", "unknown field Tone")

	mustWrite(t, page, strings.ReplaceAll(caller, "Tone", "Mood"))
	checkSharedRefresh(t, page, "repaired caller", "")
}

func TestCheckFileSharedSchemaRefreshSlots(t *testing.T) {
	root, page := sharedRefreshModule(t)
	target := filepath.Join(root, "app", "ui", "frame.gsx")
	before := `package ui
component Frame() {
	return <section>{slotTitle}{children}</section>
}
`
	mustWrite(t, target, before)
	mustWrite(t, page, `package main
import ui "./ui"
component Page() {
	return <ui.Frame><h1 slot="Title">Heading</h1><p>body</p></ui.Frame>
}
`)
	checkSharedRefresh(t, page, "initial", "")

	mustWrite(t, target, strings.ReplaceAll(before, "{slotTitle}", ""))
	checkSharedRefresh(t, page, "removed target slot", `shared component ui.Frame declares no slot named "Title"`)

	mustWrite(t, target, before)
	checkSharedRefresh(t, page, "restored target slot", "")
}

func TestCheckFileSharedSchemaRefreshDuplicate(t *testing.T) {
	root, page := sharedRefreshModule(t)
	component := `package ui
component Badge() {
	return <span>badge</span>
}
`
	mustWrite(t, filepath.Join(root, "app", "ui", "badge.gsx"), component)
	mustWrite(t, page, `package main
import ui "./ui"
component Page() {
	return <ui.Badge />
}
`)
	checkSharedRefresh(t, page, "initial", "")

	duplicate := filepath.Join(root, "app", "ui", "duplicate.gsx")
	mustWrite(t, duplicate, component)
	checkSharedRefresh(t, page, "added duplicate", "cannot be projected")

	if err := os.Remove(duplicate); err != nil {
		t.Fatal(err)
	}
	checkSharedRefresh(t, page, "deleted duplicate", "")
}

func sharedRefreshModule(t *testing.T) (root, page string) {
	t.Helper()
	root = t.TempDir()
	// No corpus files are requested: reuse only the real module-under-test
	// dependency setup, including its go.sum, then write this test's fixtures.
	if err := (gsxcorpus.Case{}).WriteStrictCheckModule(root); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, "app", "page.gsx")
}

func checkSharedRefresh(t *testing.T, page, stage, want string) {
	t.Helper()
	ctx, cancel := strictCheckCorpusContext(t)
	defer cancel()
	err := CheckFileWithOptions(ctx, page, Options{
		Env:     append(os.Environ(), "GOPROXY=off", "GOSUMDB=off"),
		GOWORK:  "off",
		GOFLAGS: "-mod=readonly",
	})
	if want == "" {
		if err != nil {
			t.Fatalf("%s: CheckFileWithOptions: %v", stage, err)
		}
	} else if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s: error = %v, want %q", stage, err, want)
	}
	t.Logf("%s: error = %v", stage, err)
}

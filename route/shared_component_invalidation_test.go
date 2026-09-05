package route

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
)

func renderSharedInvalidationPage(t *testing.T, root, rel string) string {
	t.Helper()
	node, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, rel), Pattern: "/"})
	if err != nil {
		t.Fatalf("DefaultFileRenderer(%s): %v", rel, err)
	}
	return gosx.RenderHTML(node)
}

func renderSharedInvalidationPageError(t *testing.T, root, rel string) error {
	t.Helper()
	_, err := DefaultFileRenderer(nil, FilePage{FilePath: filepath.Join(root, rel), Pattern: "/"})
	return err
}

func touchRouteFileFuture(t *testing.T, path string) {
	t.Helper()
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestSharedComponentInvalidationReloadsNormalBodyChange(t *testing.T) {
	root := t.TempDir()
	sharedPath := filepath.Join(root, "ui", "card.gsx")
	writeRouteFile(t, root, "ui/card.gsx", `package app

type CardProps struct {
	Tone string
}

component Card(props: CardProps) {
	return <section class={"card " + props.Tone}><header>{slotTitle}</header><main>{children}</main><footer>alpha</footer></section>
}
`)
	writeRouteFile(t, root, "page.gsx", `package app

import ui "./ui"

component Page() {
	return <ui.Card Tone="blue"><p>body</p><h1 slot="Title">Title</h1></ui.Card>
}
`)

	if got := renderSharedInvalidationPage(t, root, "page.gsx"); got != `<section class="card blue"><header><h1>Title</h1></header><main><p>body</p></main><footer>alpha</footer></section>` {
		t.Fatalf("first html = %q", got)
	}

	writeRouteFile(t, root, "ui/card.gsx", `package app

type CardProps struct {
	Tone string
}

component Card(props: CardProps) {
	return <section class={"card " + props.Tone}><header>{slotTitle}</header><main>{children}</main><footer>beta-and-more</footer></section>
}
`)
	if got := renderSharedInvalidationPage(t, root, "page.gsx"); got != `<section class="card blue"><header><h1>Title</h1></header><main><p>body</p></main><footer>beta-and-more</footer></section>` {
		t.Fatalf("second html = %q", got)
	}
	if info, err := os.Stat(sharedPath); err != nil || info.Size() <= 0 {
		t.Fatalf("shared file stat after rewrite = %v, %v", info, err)
	}
}

func TestSharedComponentInvalidationReloadsEqualSizeChangedMTime(t *testing.T) {
	root := t.TempDir()
	sharedPath := filepath.Join(root, "ui", "badge.gsx")
	first := `package app

component Badge() {
	return <span class="badge">alpha</span>
}
`
	second := `package app

component Badge() {
	return <span class="badge">omega</span>
}
`
	if len(first) != len(second) {
		t.Fatalf("fixture sources must have equal byte size: %d vs %d", len(first), len(second))
	}
	writeRouteFile(t, root, "ui/badge.gsx", first)
	writeRouteFile(t, root, "page.gsx", `package app

import ui "./ui"

component Page() {
	return <ui.Badge />
}
`)

	if got := renderSharedInvalidationPage(t, root, "page.gsx"); got != `<span class="badge">alpha</span>` {
		t.Fatalf("first html = %q", got)
	}

	if err := os.WriteFile(sharedPath, []byte(second), 0o644); err != nil {
		t.Fatalf("write equal-size shared edit: %v", err)
	}
	touchRouteFileFuture(t, sharedPath)
	if got := renderSharedInvalidationPage(t, root, "page.gsx"); got != `<span class="badge">omega</span>` {
		t.Fatalf("second html = %q", got)
	}
}

func TestSharedComponentInvalidationFileRenamePreservesComponentIdentityButComponentRenameFails(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "ui/card.gsx", `package app

component Card() {
	return <article>before</article>
}
`)
	writeRouteFile(t, root, "page.gsx", `package app

import ui "./ui"

component Page() {
	return <ui.Card />
}
`)

	if got := renderSharedInvalidationPage(t, root, "page.gsx"); got != `<article>before</article>` {
		t.Fatalf("first html = %q", got)
	}

	if err := os.Rename(filepath.Join(root, "ui", "card.gsx"), filepath.Join(root, "ui", "renamed_file.gsx")); err != nil {
		t.Fatalf("rename shared file: %v", err)
	}
	touchRouteFileFuture(t, filepath.Join(root, "ui", "renamed_file.gsx"))
	if got := renderSharedInvalidationPage(t, root, "page.gsx"); got != `<article>before</article>` {
		t.Fatalf("html after file rename = %q", got)
	}

	writeRouteFile(t, root, "ui/renamed_file.gsx", `package app

component Panel() {
	return <article>after</article>
}
`)
	touchRouteFileFuture(t, filepath.Join(root, "ui", "renamed_file.gsx"))
	err := renderSharedInvalidationPageError(t, root, "page.gsx")
	if err == nil || !strings.Contains(err.Error(), "declares no strict component named Card") {
		t.Fatalf("error after component rename = %v, want missing Card diagnostic", err)
	}
}

func TestSharedComponentInvalidationDeletionFailsClosed(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "ui/badge.gsx", `package app

component Badge() {
	return <span>alive</span>
}
`)
	writeRouteFile(t, root, "page.gsx", `package app

import ui "./ui"

component Page() {
	return <ui.Badge />
}
`)

	if got := renderSharedInvalidationPage(t, root, "page.gsx"); got != `<span>alive</span>` {
		t.Fatalf("first html = %q", got)
	}
	if err := os.Remove(filepath.Join(root, "ui", "badge.gsx")); err != nil {
		t.Fatalf("remove shared file: %v", err)
	}
	err := renderSharedInvalidationPageError(t, root, "page.gsx")
	if err == nil || !strings.Contains(err.Error(), "has no .gsx files") {
		t.Fatalf("error after deletion = %v, want empty shared directory diagnostic", err)
	}
}

func TestSharedComponentInvalidationDuplicateNamesFailWithoutCachedWinner(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "ui/a.gsx", `package app

component Badge() {
	return <span>a</span>
}
`)
	writeRouteFile(t, root, "page.gsx", `package app

import ui "./ui"

component Page() {
	return <ui.Badge />
}
`)

	if got := renderSharedInvalidationPage(t, root, "page.gsx"); got != `<span>a</span>` {
		t.Fatalf("first html = %q", got)
	}

	writeRouteFile(t, root, "ui/b.gsx", `package app

component Badge() {
	return <span>b</span>
}
`)
	err := renderSharedInvalidationPageError(t, root, "page.gsx")
	if err == nil || !strings.Contains(err.Error(), "duplicate strict component Badge") {
		t.Fatalf("error after duplicate = %v, want duplicate shared component diagnostic", err)
	}
}

func TestSharedComponentInvalidationIdenticalCallersResolveDistinctSourceDirs(t *testing.T) {
	root := t.TempDir()
	caller := `package app

import ui "./ui"

component Page() {
	return <ui.Badge />
}
`
	writeRouteFile(t, root, "home/page.gsx", caller)
	writeRouteFile(t, root, "away/page.gsx", caller)
	writeRouteFile(t, root, "home/ui/badge.gsx", `package app

component Badge() {
	return <span class="home">home-ui</span>
}
`)
	writeRouteFile(t, root, "away/ui/badge.gsx", `package app

component Badge() {
	return <span class="away">away-ui</span>
}
`)

	if got := renderSharedInvalidationPage(t, root, "home/page.gsx"); got != `<span class="home">home-ui</span>` {
		t.Fatalf("home html = %q", got)
	}
	if got := renderSharedInvalidationPage(t, root, "away/page.gsx"); got != `<span class="away">away-ui</span>` {
		t.Fatalf("away html = %q", got)
	}
	if got := renderSharedInvalidationPage(t, root, "home/page.gsx"); got != `<span class="home">home-ui</span>` {
		t.Fatalf("home repeat html = %q", got)
	}
}

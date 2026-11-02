//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Real compiled GoSX islands must preserve namespace and logical table paths;
// mock patch application alone cannot prove SSR parsing and WASM hydration agree.
func TestProductionBuildHydratesIslandsAcrossFiles(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	fixture := filepath.Join(t.TempDir(), "islands-files")
	copyFixtureTree(t, filepath.Join(root, "e2e", "testdata", "islands-files"), fixture)
	module := fmt.Sprintf("module example.com/gosx-islands-files\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\n\nreplace m31labs.dev/gosx => %s\n", filepath.ToSlash(root))
	if err := os.WriteFile(filepath.Join(fixture, "go.mod"), []byte(module), 0644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "run", "./cmd/gosx", "build", "--prod", fixture)
	build.Dir = root
	build.Env = append(os.Environ(), "GOWORK=off")
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("production fixture build: %v\n%s", err, output)
	}
	for _, name := range []string{"Counter", "Tally", "Chart", "ForeignContent", "Roster", "Sections", "ImportedMap"} {
		if !strings.Contains(string(output), name) {
			t.Fatalf("missing compiled island %s:\n%s", name, output)
		}
	}
	port := freeE2EPort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	// A deployed bundle must not need Go or the model package's source to
	// reconstruct imported island types at request time.
	t.Setenv("PATH", "/usr/bin:/bin")
	logs := startBuiltFixture(t, filepath.Join(fixture, "dist"), port)
	if err := waitForHealthy(baseURL+"/", 45*time.Second); err != nil {
		t.Fatalf("%v\n%s", err, logs.String())
	}
	for _, size := range []struct {
		name          string
		width, height int
	}{{"desktop", 1024, 768}, {"mobile-viewport", 390, 844}} {
		t.Run(size.name, func(t *testing.T) {
			page := newBrowserPage(t, chrome, nil, size.width, size.height, "", 90*time.Second)
			page.navigate(t, baseURL+"/")
			page.waitFor(t, islandsFilesReady, 20*time.Second, "all six delegated island handlers")
			click := func(selector string) {
				t.Helper()
				if err := chromedp.Run(page.ctx, chromedp.Click(selector, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
			}
			wait := func(script, label string) { t.Helper(); page.waitFor(t, script, 10*time.Second, label) }
			click("#sibling-inc")
			wait(`document.querySelector('#sibling-count').textContent==='1'`, "sibling island counter")
			click("#imported-map-show")
			wait(`document.querySelector('#imported-map-value')?.textContent==='false'`, "imported map zero value without Go")
			click("#shared-dec")
			wait(`document.querySelector('#shared-count').textContent==='9'`, "relative-imported island tally")
			click("#chart-add")
			wait(`document.querySelectorAll('#chart-host svg').length===1`, "dynamic SVG chart")
			wait(`Array.from(document.querySelectorAll('#chart-host svg, #chart-host g, #chart-host path, #chart-host polyline')).every(n=>n.namespaceURI==='http://www.w3.org/2000/svg') && document.querySelector('#chart-host svg').getBBox().width>0`, "visible SVG namespace and geometry")
			click("#foreign-toggle")
			wait(islandsFilesForeignNamespaces, "MathML/HTML/SVG namespace integration")
			click("#foreign-toggle")
			wait(`!document.querySelector('#equation')`, "foreign subtree removal")
			click("#foreign-toggle")
			wait(islandsFilesForeignNamespaces, "foreign subtree re-creation")
			click("#roster-add")
			wait(`document.querySelectorAll('#bare tr.row').length===4`, "row insertion")
			click("#roster-remove")
			wait(`Array.from(document.querySelectorAll('#bare tr.row'),n=>n.textContent).join(',')==='ada,cy'`, "row removal")
			page.eval(t, `window.__islandsFilesAda=Array.from(document.querySelectorAll('#bare tr.row')).find(n=>n.textContent==='ada')`, nil)
			click("#roster-reorder")
			wait(`Array.from(document.querySelectorAll('#bare tr.row'),n=>n.textContent).join(',')==='cy,ada,bo' && document.querySelectorAll('#bare tr.row')[1]===window.__islandsFilesAda`, "keyed row identity and reorder")
			for i := 0; i < 3; i++ {
				click("#section-toggle")
				wait(`Array.from(document.querySelectorAll('#split-table tr'),n=>n.textContent).join(',')==='left,middle,right' && document.querySelector('#split-middle').parentElement.id==='split-table'`, "explicit middle section preserves row order")
				click("#section-toggle")
				wait(`!document.querySelector('#split-middle') && Array.from(document.querySelectorAll('#split-table tr'),n=>n.textContent).join(',')==='left,right'`, "section removal preserves trailing row")
			}
			if errors := page.PageErrors(); len(errors) > 0 {
				t.Fatalf("page errors: %v", errors)
			}
			if strings.Contains(page.Console(), "[gosx/patch]") {
				t.Fatalf("patch warnings:\n%s", page.Console())
			}
		})
	}
}

const islandsFilesReady = `['#sibling-inc','#shared-dec','#chart-add','#foreign-toggle','#roster-add','#section-toggle','#imported-map-show'].every(selector=>{const button=document.querySelector(selector),root=button?.closest('[data-gosx-island]'),island=window.__gosx?.islands?.get(root?.id);return !!island&&island.root===root&&island.listeners.some(entry=>entry.type==='click'&&entry.target===root);})`
const islandsFilesForeignNamespaces = `(()=>{const ns=id=>document.getElementById(id)?.namespaceURI;return ns('equation')==='http://www.w3.org/1998/Math/MathML'&&ns('equation-glyph')==='http://www.w3.org/1998/Math/MathML'&&ns('equation-mark')==='http://www.w3.org/1998/Math/MathML'&&ns('equation-text')==='http://www.w3.org/1999/xhtml'&&ns('equation-note')==='http://www.w3.org/1999/xhtml'&&ns('foreign-vector')==='http://www.w3.org/2000/svg'&&ns('vector-note')==='http://www.w3.org/1999/xhtml';})()`

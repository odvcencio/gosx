package performance

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
	"m31labs.dev/gosx/route"
)

func TestCommittedPerformanceReceiptsMatchSchemaAndFreshness(t *testing.T) {
	receipts, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if err := receipts.Validate(); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(receipts.Commit) {
		t.Fatalf("measurement source commit %q is not a full Git SHA", receipts.Commit)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(receipts.Tree) {
		t.Fatalf("measurement source tree %q is not a full Git SHA", receipts.Tree)
	}
	if receipts.Lighthouse.LoadAverageStart == "" || receipts.Lighthouse.LoadAverageEnd == "" {
		t.Fatal("Lighthouse batch must record /proc/loadavg before and after measurement")
	}
	if age := time.Since(receipts.MeasuredAt); age < -time.Minute || age > 90*24*time.Hour {
		t.Fatalf("measurement date %s is outside the last 90 days", receipts.MeasuredAt)
	}
}

func TestCommittedPerformanceReceiptNamesItsMeasuredAncestor(t *testing.T) {
	receipts, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	root := gitOutput(t, "rev-parse", "--show-toplevel")

	if err := measuredCommitIsAncestor(root, receipts.Commit); err != nil {
		if gitOutputAt(t, root, "rev-parse", "--is-shallow-repository") == "true" {
			ref := os.Getenv("GITHUB_REF")
			if regexp.MustCompile(`^refs/pull/[0-9]+/merge$`).MatchString(ref) {
				// Leave enough history for the merge ref plus recent receipt-maintenance commits.
				fetch := exec.Command("git", "fetch", "--no-tags", "--deepen=8", "origin", ref)
				fetch.Dir = root
				if output, fetchErr := fetch.CombinedOutput(); fetchErr != nil {
					t.Fatalf("deepen shallow pull-request history: %v: %s", fetchErr, output)
				}
			}
		}
		if err := measuredCommitIsAncestor(root, receipts.Commit); err != nil {
			t.Fatalf("measured commit %s is not an ancestor of HEAD: %v", receipts.Commit, err)
		}
	}

	tree := gitOutputAt(t, root, "rev-parse", receipts.Commit+"^{tree}")
	if tree != receipts.Tree {
		t.Fatalf("measured commit tree = %s, receipt records %s", tree, receipts.Tree)
	}

	changed := strings.Fields(gitOutputAt(t, root, "diff", "--name-only", receipts.Commit, "HEAD"))
	// The receipt generator writes receipts.json as its only tracked output.
	allowed := map[string]bool{
		"examples/gosx-docs/app/performance/receipts.json": true,
		// Test changes do not alter the measured build or its evidence.
		"examples/gosx-docs/app/capabilities/page_test.go": true,
		"examples/gosx-docs/app/demos/catalog_test.go":     true,
		"examples/gosx-docs/app/performance/page_test.go":  true,
		"scripts/showcase-gpu-cadence.test.mjs":            true,
	}
	// These exact production-file blobs are measurement-neutral: the CSS keeps
	// the same verdict styles in Chrome while adding older-browser support; the
	// GPU scripts only reject undersampled captures and leave timing math intact;
	// the Makefile change only makes the guard test run in CI. Pin the blobs so
	// future edits to these paths still require a new receipt measurement.
	measurementNeutral := map[string]string{
		"Makefile": "c6d0f02a5c1de6673a104e011bb212ad645c6f01",
		"examples/gosx-docs/app/capabilities/page.css": "406f787b52c4d98bc86d5002ff4be02684803c57",
		"scripts/showcase-gpu-cadence.mjs":             "b798f91d123dc4de3cb8c08a8008574420bfa926",
		"scripts/showcase-gpu-capture.mjs":             "bb96528aa837899b5475a13da1fc6e5447323988",
		"scripts/showcase-receipts.mjs":                "c532380a3f9ef524ab9903089139d154e6315bf2",
	}
	if len(changed) == 0 {
		t.Fatal("receipt commit must follow the measured build commit")
	}
	for _, file := range changed {
		if !allowed[file] {
			wantBlob, ok := measurementNeutral[file]
			if !ok {
				t.Errorf("file %q changed after the measured build; only receipts, generated outputs, and pinned measurement-neutral paths may change", file)
				continue
			}
			if gotBlob := gitOutputAt(t, root, "rev-parse", "HEAD:"+file); gotBlob != wantBlob {
				t.Errorf("measurement-neutral file %q blob = %s, want reviewed blob %s", file, gotBlob, wantBlob)
			}
		}
	}
}

func measuredCommitIsAncestor(root, commit string) error {
	command := exec.Command("git", "merge-base", "--is-ancestor", commit, "HEAD")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func TestPerformancePageRendersEveryListedPageAndDemo(t *testing.T) {
	receipts, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	router := route.NewRouter()
	if err := router.AddDir(filepath.Dir(filepath.Dir(thisFile)), route.FileRoutesOptions{}); err != nil {
		t.Fatalf("add app routes: %v", err)
	}
	response := httptest.NewRecorder()
	router.Build().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/performance", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /performance status = %d", response.Code)
	}
	doc, err := html.Parse(strings.NewReader(response.Body.String()))
	if err != nil {
		t.Fatalf("parse performance page: %v", err)
	}
	pages := make(map[string]bool)
	demos := make(map[string]bool)
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			for _, attr := range node.Attr {
				switch attr.Key {
				case "data-lighthouse-path":
					pages[attr.Val] = true
				case "data-demo-path":
					demos[attr.Val] = true
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)

	if len(pages) != len(receipts.Lighthouse.Pages) {
		t.Fatalf("rendered Lighthouse rows = %d, receipts list %d", len(pages), len(receipts.Lighthouse.Pages))
	}
	for _, page := range receipts.Lighthouse.Pages {
		if !pages[page.Path] {
			t.Errorf("performance page is missing Lighthouse row %q", page.Path)
		}
	}
	if len(demos) != len(receipts.GPU.Scenes) {
		t.Fatalf("rendered demo rows = %d, receipts list %d", len(demos), len(receipts.GPU.Scenes))
	}
	for _, scene := range receipts.GPU.Scenes {
		if !demos[scene.Path] {
			t.Errorf("performance page is missing demo row %q", scene.Path)
		}
	}
}

func gitOutput(t *testing.T, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func gitOutputAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

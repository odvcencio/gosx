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
	const squashedMeasurementCommit = "4583a4d2997b44a9b11d47724e11aadbc2d22b05"
	const squashedMeasurementTree = "35b88f233a7eae48cc38134138cc7ff9fba9e7e4"

	if err := measuredCommitIsAncestor(root, receipts.Commit); err != nil {
		if gitOutputAt(t, root, "rev-parse", "--is-shallow-repository") == "true" {
			// CI checks out one commit. Fetch every commit since one day before
			// the measurement: if the measured commit is an ancestor of HEAD it
			// was authored no later than the measurement, so this range holds it
			// however many commits landed afterwards. A fixed --deepen count
			// broke as soon as more than a few commits followed the receipt.
			head := gitOutputAt(t, root, "rev-parse", "HEAD")
			since := receipts.MeasuredAt.Add(-24 * time.Hour).UTC().Format(time.RFC3339)
			fetch := exec.Command("git", "fetch", "--no-tags", "--shallow-since="+since, "origin", head)
			fetch.Dir = root
			if output, fetchErr := fetch.CombinedOutput(); fetchErr != nil {
				t.Fatalf("deepen shallow history to %s: %v: %s", since, fetchErr, output)
			}
		}
		if err := measuredCommitIsAncestor(root, receipts.Commit); err != nil {
			// PRs #392, #403 and #414 were squash-merged, so the measured source commit is not
			// reachable from main even though the receipt still records that
			// commit's exact tree. Fetch only this known source commit and accept
			// it only when both receipt hashes match; the diff checks below still
			// reject changes to the docs build and measurement inputs.
			if receipts.Commit != squashedMeasurementCommit || receipts.Tree != squashedMeasurementTree {
				t.Fatalf("measured commit %s is not an ancestor of HEAD: %v", receipts.Commit, err)
			}
			if !gitObjectExistsAt(root, receipts.Commit+"^{commit}") {
				fetch := exec.Command("git", "fetch", "--no-tags", "--depth=1", "origin", receipts.Commit)
				fetch.Dir = root
				if output, fetchErr := fetch.CombinedOutput(); fetchErr != nil {
					t.Fatalf("fetch squashed measurement commit %s: %v: %s", receipts.Commit, fetchErr, output)
				}
			}
		}
	}

	tree := gitOutputAt(t, root, "rev-parse", receipts.Commit+"^{tree}")
	if tree != receipts.Tree {
		t.Fatalf("measured commit tree = %s, receipt records %s", tree, receipts.Tree)
	}

	changed := strings.Fields(gitOutputAt(t, root, "diff", "--name-only", receipts.Commit, "HEAD"))
	if len(changed) == 0 {
		t.Fatal("receipt commit must follow the measured build commit")
	}
	inputs, err := docsPerformanceReceiptInputs(root)
	if err != nil {
		t.Fatalf("compute docs performance receipt inputs: %v", err)
	}
	for _, file := range changed {
		var blob string
		if _, pinned := performanceReceiptNeutralBlobs[file]; pinned {
			blob = gitOutputAt(t, root, "rev-parse", "HEAD:"+file)
		}
		if receiptInputNeedsRemeasure(file, blob, inputs) {
			t.Errorf("measurement input %q changed after the measured build; re-measure the docs performance receipt", file)
		}
	}
}

func TestPerformanceReceiptMeasurementInputScope(t *testing.T) {
	root := gitOutput(t, "rev-parse", "--show-toplevel")
	inputs, err := docsPerformanceReceiptInputs(root)
	if err != nil {
		t.Fatalf("compute docs performance receipt inputs: %v", err)
	}

	for _, test := range []struct {
		name string
		path string
		want bool
	}{
		{name: "desktop source is outside the measured build", path: "desktop/app.go", want: false},
		{name: "docs page is a measurement input", path: "examples/gosx-docs/app/docs/runtime/page.gsx", want: true},
		{name: "imported server package is a measurement input", path: "server/assets.go", want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := receiptInputNeedsRemeasure(test.path, "", inputs); got != test.want {
				t.Fatalf("receiptInputNeedsRemeasure(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}

type performanceReceiptInputs struct {
	packageDirs []string
}

var performanceReceiptNeutralBlobs = map[string]string{
	// Preserve the reviewed CSS fallback and incomplete-capture checks without
	// changing measured values. Exact blobs keep later edits to these inputs stale.
	// The showcase-receipts.sh, showcase-gpu-capture.mjs, and freevars.mjs blobs
	// changed only their machine-specific defaults (tools directory, evidence
	// directory, CDP address, acorn fallback); no measured value or build output
	// changed.
	"cmd/buildbootstrap/freevars.mjs":              "e0b207b4743858c44f667dd7029734369cb66d1d",
	"examples/gosx-docs/app/capabilities/page.css": "406f787b52c4d98bc86d5002ff4be02684803c57",
	"scripts/showcase-gpu-cadence.mjs":             "b798f91d123dc4de3cb8c08a8008574420bfa926",
	"scripts/showcase-gpu-capture.mjs":             "9d6f85a038dda0e04a3bcd220ccdc95166b55876",
	"scripts/showcase-receipts.mjs":                "c532380a3f9ef524ab9903089139d154e6315bf2",
	"scripts/showcase-receipts.sh":                 "11fce1fa8d40e7865b3723d8e5a5225ba306c2f9",
}

func docsPerformanceReceiptInputs(root string) (performanceReceiptInputs, error) {
	// The docs app's non-test Go dependency graph supplies the framework package
	// roots. The input matcher adds the docs content, browser bundle sources,
	// bundle builder, and the scripts that capture and generate this receipt.
	command := exec.Command("go", "list", "-deps", "-f", "{{if and .Module .Module.Main}}{{.Dir}}{{end}}", "./examples/gosx-docs")
	command.Dir = root
	command.Env = environmentWithGOWORKOff(os.Environ())
	output, err := command.CombinedOutput()
	if err != nil {
		return performanceReceiptInputs{}, fmt.Errorf("go list docs dependencies: %w: %s", err, strings.TrimSpace(string(output)))
	}

	inputs := performanceReceiptInputs{}
	for _, dir := range strings.Fields(string(output)) {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return performanceReceiptInputs{}, fmt.Errorf("make docs package path relative: %w", err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		inputs.packageDirs = append(inputs.packageDirs, filepath.ToSlash(rel))
	}
	return inputs, nil
}

func environmentWithGOWORKOff(environment []string) []string {
	filtered := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, "GOWORK=") {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, "GOWORK=off")
}

func (inputs performanceReceiptInputs) contains(file string) bool {
	file = filepath.ToSlash(filepath.Clean(file))
	if file == "examples/gosx-docs/app/performance/receipts.json" || strings.HasSuffix(file, "_test.go") {
		return false
	}
	switch file {
	case "go.mod", "go.sum",
		"scripts/showcase-receipts.sh",
		"scripts/showcase-receipts.mjs",
		"scripts/showcase-gpu-capture.mjs",
		"scripts/showcase-gpu-cadence.mjs":
		return true
	}
	for _, root := range []string{
		"examples/gosx-docs/app",
		"examples/gosx-docs/public",
		"examples/gosx-docs/samples",
	} {
		if pathUnder(file, root) {
			return true
		}
	}
	if strings.HasPrefix(file, "examples/gosx-docs/") {
		// The docs executable's package sources are listed by go list below;
		// deployment-only files beside main.go are outside the measured site.
		rel := strings.TrimPrefix(file, "examples/gosx-docs/")
		if !strings.Contains(rel, "/") && filepath.Ext(file) == ".go" {
			return true
		}
	}
	for _, root := range []string{"client/js", "client/runtime", "client/wasm", "cmd/buildbootstrap"} {
		if pathUnder(file, root) && !isTestOnlySource(file) {
			return true
		}
	}
	for _, dir := range inputs.packageDirs {
		if !pathUnder(file, dir) || isTestOnlySource(file) {
			continue
		}
		// The root and docs executable package directories also contain
		// unrelated project files. Their Go sources are build inputs.
		if dir == "." || dir == "examples/gosx-docs" {
			return filepath.Ext(file) == ".go"
		}
		return true
	}
	return false
}

func pathUnder(file, root string) bool {
	if root == "." {
		return !strings.Contains(file, "/")
	}
	return file == root || strings.HasPrefix(file, strings.TrimSuffix(root, "/")+"/")
}

func isTestOnlySource(file string) bool {
	if strings.Contains(file, ".test.") || strings.HasSuffix(file, ".dmj") || strings.HasSuffix(file, ".md") {
		return true
	}
	for _, component := range strings.Split(file, "/") {
		if component == "testdata" {
			return true
		}
	}
	return false
}

func receiptInputNeedsRemeasure(file, blob string, inputs performanceReceiptInputs) bool {
	if !inputs.contains(file) {
		return false
	}
	if want, pinned := performanceReceiptNeutralBlobs[file]; pinned && blob == want {
		return false
	}
	return true
}

func measuredCommitIsAncestor(root, commit string) error {
	command := exec.Command("git", "merge-base", "--is-ancestor", commit, "HEAD")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitObjectExistsAt(root, object string) bool {
	command := exec.Command("git", "cat-file", "-e", object)
	command.Dir = root
	return command.Run() == nil
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

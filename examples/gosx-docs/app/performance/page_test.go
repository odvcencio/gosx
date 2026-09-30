package performance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
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

// The receipt names the files it measured by content, not by commit. A squash
// merge deletes the branch commit the measurement ran on, so a commit ancestry
// check breaks on every squash. The digest below covers the blob of every
// measurement input at HEAD, so it only changes when an input changes.
//
// After re-measuring and committing receipts.json, refresh the digest with:
//
//	GOSX_WRITE_RECEIPT_INPUTS_DIGEST=1 go test ./examples/gosx-docs/app/performance -run TestCommittedPerformanceReceiptInputsMatchDigest
const receiptInputsDigestFile = "receipts.inputs-digest"

func TestCommittedPerformanceReceiptInputsMatchDigest(t *testing.T) {
	root := gitOutput(t, "rev-parse", "--show-toplevel")
	inputs, err := docsPerformanceReceiptInputs(root)
	if err != nil {
		t.Fatalf("compute docs performance receipt inputs: %v", err)
	}
	got := performanceReceiptInputsDigest(t, root, inputs)
	path := filepath.Join(root, "examples", "gosx-docs", "app", "performance", receiptInputsDigestFile)
	if os.Getenv("GOSX_WRITE_RECEIPT_INPUTS_DIGEST") == "1" {
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", receiptInputsDigestFile, err)
	}
	if want := strings.TrimSpace(string(data)); want != got {
		t.Fatalf("measurement inputs at HEAD have digest %s, receipt was measured at %s; re-measure the docs performance receipt, then run with GOSX_WRITE_RECEIPT_INPUTS_DIGEST=1 to record the new digest", got, want)
	}
}

// performanceReceiptInputsDigest hashes "path blob" for every measurement input
// tracked at HEAD. Pinned neutral blobs count as one constant so the reviewed
// no-measurement edits stay accepted, as receiptInputNeedsRemeasure allows.
func performanceReceiptInputsDigest(t *testing.T, root string, inputs performanceReceiptInputs) string {
	t.Helper()
	listing := gitOutputAt(t, root, "ls-tree", "-r", "-z", "HEAD")
	type entry struct{ path, blob string }
	var entries []entry
	for _, record := range strings.Split(listing, "\x00") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		meta, file, ok := strings.Cut(record, "\t")
		if !ok {
			t.Fatalf("unexpected git ls-tree record %q", record)
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 || fields[1] != "blob" || !inputs.contains(file) {
			continue
		}
		blob := fields[2]
		if want, pinned := performanceReceiptNeutralBlobs[file]; pinned && blob == want {
			blob = "neutral"
		}
		entries = append(entries, entry{file, blob})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	hash := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(hash, "%s %s\n", e.path, e.blob)
	}
	return hex.EncodeToString(hash.Sum(nil))
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
	if file == "examples/gosx-docs/app/performance/receipts.json" || file == "examples/gosx-docs/app/performance/"+receiptInputsDigestFile || strings.HasSuffix(file, "_test.go") {
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

func TestPerformanceReceiptInputsDigestFollowsInputContentOnly(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		gitOutputAt(t, repo, args...)
	}
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(message string) {
		t.Helper()
		run("add", "-A")
		run("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", message)
	}
	run("init", "-q")
	write("examples/gosx-docs/app/docs/page.gsx", "one")
	write("README.md", "unrelated")
	commit("measured")
	inputs := performanceReceiptInputs{}
	measured := performanceReceiptInputsDigest(t, repo, inputs)

	// A squash merge rewrites history but keeps input content: same digest.
	write("README.md", "still unrelated")
	write("examples/gosx-docs/app/performance/receipts.json", "{}")
	write("examples/gosx-docs/app/performance/"+receiptInputsDigestFile, measured)
	write("examples/gosx-docs/app/docs/page_test.go", "package docs")
	commit("squashed, non-input changes only")
	if got := performanceReceiptInputsDigest(t, repo, inputs); got != measured {
		t.Fatalf("digest changed for non-input edits: %s != %s", got, measured)
	}

	write("examples/gosx-docs/app/docs/page.gsx", "two")
	commit("input change")
	if got := performanceReceiptInputsDigest(t, repo, inputs); got == measured {
		t.Fatal("digest did not change when a measurement input changed")
	}
}

package docs

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene/capability"
)

func TestCapabilitiesPageRendersEveryMatrixRowAndBackendCell(t *testing.T) {
	pageSource, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pageSource), `<Scene3D`) {
		t.Fatal("server-rendered capabilities table must not mount the full Scene3D runtime")
	}
	if !strings.Contains(string(pageSource), `data-gosx-capability-probe`) || !strings.Contains(string(pageSource), `data-gosx-browser-renderer-label`) {
		t.Fatal("browser capability probe must expose its selected renderer")
	}
	if !strings.Contains(string(pageSource), "first available renderer") || !strings.Contains(string(pageSource), "required features are missing") {
		t.Fatal("browser capability page must describe what its lightweight probe checks")
	}
	probeSource, err := os.ReadFile("probe.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, behavior := range []string{"requestAdapter", `getContext("webgl2")`, `getContext("2d")`, "requestIdleCallback"} {
		if !strings.Contains(string(probeSource), behavior) {
			t.Errorf("browser capability probe is missing %q", behavior)
		}
	}
	cssSource, err := os.ReadFile("page.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, runtimeKind := range []string{"webgpu", "webgl", "canvas", "unsupported"} {
		if !strings.Contains(string(cssSource), `data-gosx-browser-renderer="`+runtimeKind+`"`) {
			t.Errorf("browser capability column does not handle browser renderer %q", runtimeKind)
		}
	}
	if strings.Contains(string(cssSource), ":has(") {
		t.Fatal("browser capability answers must not depend on CSS :has support")
	}
	if !strings.Contains(string(cssSource), `.capabilities-browser[data-gosx-browser-renderer="webgpu"] + .capabilities-table-wrap .capabilities-browser-cell__answer--webgpu`) {
		t.Fatal("browser capability answers must follow the probed renderer without :has")
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
	router.Build().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /capabilities status = %d", response.Code)
	}
	doc, err := html.Parse(strings.NewReader(response.Body.String()))
	if err != nil {
		t.Fatalf("parse capabilities page: %v", err)
	}
	var probePrecedesTable bool
	var checkProbeOrder func(*html.Node)
	checkProbeOrder = func(node *html.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && hasHTMLClass(child, "capabilities-browser") {
				for next := child.NextSibling; next != nil; next = next.NextSibling {
					if next.Type != html.ElementNode {
						continue
					}
					probePrecedesTable = hasHTMLClass(next, "capabilities-table-wrap")
					break
				}
			}
			checkProbeOrder(child)
		}
	}
	checkProbeOrder(doc)
	if !probePrecedesTable {
		t.Fatal("browser capability probe must be the table wrapper's previous element sibling")
	}

	wantBackends := map[string]capability.Backend{
		"webgpu":   capability.BackendWebGPU,
		"webgl":    capability.BackendWebGL,
		"canvas2d": capability.BackendCanvas2D,
	}
	seenRows := map[capability.Feature]bool{}
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "tr" {
			attrs := htmlAttributes(node)
			feature := capability.Feature(attrs["data-feature"])
			if feature != "" {
				seenRows[feature] = true
				seenCells := map[capability.Backend]bool{}
				for cell := node.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type != html.ElementNode || cell.Data != "td" {
						continue
					}
					cellAttrs := htmlAttributes(cell)
					backend, ok := wantBackends[cellAttrs["data-backend"]]
					if !ok {
						continue
					}
					seenCells[backend] = true
					want := capability.Supports(backend, feature)
					if cellAttrs["data-supported"] != "true" && cellAttrs["data-supported"] != "false" {
						t.Errorf("%s/%s support attribute is not boolean: %q", feature, backend, cellAttrs["data-supported"])
					}
					if (cellAttrs["data-supported"] == "true") != want {
						t.Errorf("%s/%s support cell = %q, Matrix says %t", feature, backend, cellAttrs["data-supported"], want)
					}
					if text := capabilityNodeText(cell); !strings.Contains(text, capabilityCellReasons[feature][backend]) {
						t.Errorf("%s/%s cell is missing its reason", feature, backend)
					}
				}
				for label, key := range wantBackends {
					if !seenCells[key] {
						t.Errorf("%s row is missing the %s backend cell", feature, label)
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)

	if len(seenRows) != len(capability.Matrix) {
		t.Fatalf("rendered capability rows = %d, Matrix rows = %d", len(seenRows), len(capability.Matrix))
	}
	for feature := range capability.Matrix {
		if !seenRows[feature] {
			t.Errorf("capabilities page is missing Matrix row %q", feature)
		}
	}
}

func hasHTMLClass(node *html.Node, want string) bool {
	for _, class := range strings.Fields(htmlAttributes(node)["class"]) {
		if class == want {
			return true
		}
	}
	return false
}

func TestCapabilitiesPageExplainsTheHonestyGateInThreeSentences(t *testing.T) {
	page, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, sentence := range []string{
		"GoSX computes a backend verdict per scene in Go.",
		"The browser obeys it.",
		"A scene never silently drops a feature.",
	} {
		if !strings.Contains(string(page), sentence) {
			t.Errorf("capabilities page is missing honesty-gate sentence %q", sentence)
		}
	}
}

func TestCapabilitiesPageUsesSharedColorAndShapeTokens(t *testing.T) {
	css, err := os.ReadFile("page.css")
	if err != nil {
		t.Fatal(err)
	}
	source := string(css)
	if regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`).MatchString(source) {
		t.Fatal("capabilities page CSS must use shared color tokens instead of raw hex colors")
	}
	for _, token := range []string{"var(--color-surface-raised)", "var(--radius-md)", "var(--radius-sm)"} {
		if !strings.Contains(source, token) {
			t.Errorf("capabilities page CSS is missing shared token %q", token)
		}
	}
}

func TestCapabilitiesPageSourceLinksPointToRendererDirectories(t *testing.T) {
	if capabilitySourceBase != "https://github.com/odvcencio/gosx/tree/main/" {
		t.Fatalf("capability source base = %q, want GitHub tree URL", capabilitySourceBase)
	}
	want := map[capability.Backend]string{
		capability.BackendWebGPU:   "client/runtime/scene3d/",
		capability.BackendWebGL:    "client/runtime/scene3d/",
		capability.BackendCanvas2D: "client/js/bootstrap-src/",
	}
	for backend, path := range want {
		if got := capabilityRendererSource(backend); got != path {
			t.Errorf("%s source directory = %q, want %q", backend, got, path)
		}
	}
}

func capabilityNodeText(node *html.Node) string {
	parts := []string{}
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.TextNode {
			parts = append(parts, current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return strings.Join(parts, " ")
}

func htmlAttributes(node *html.Node) map[string]string {
	attrs := make(map[string]string, len(node.Attr))
	for _, attr := range node.Attr {
		attrs[attr.Key] = attr.Val
	}
	return attrs
}

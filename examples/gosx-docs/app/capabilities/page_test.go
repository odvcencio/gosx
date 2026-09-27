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
	if !strings.Contains(string(pageSource), `<Scene3D class="capabilities-browser__scene" {...data.probe} stats={false} />`) {
		t.Fatal("browser capability probe must not render the public Scene3D stats overlay")
	}
	if !strings.Contains(string(pageSource), `data-gosx-scene3d-status="renderer"`) {
		t.Fatal("browser capability probe must expose the runtime's selected renderer")
	}
	cssSource, err := os.ReadFile("page.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, runtimeKind := range []string{"webgpu", "webgl", "canvas", "unsupported"} {
		if !strings.Contains(string(cssSource), `data-gosx-scene3d-renderer="`+runtimeKind+`"`) {
			t.Errorf("browser capability column does not handle runtime renderer %q", runtimeKind)
		}
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

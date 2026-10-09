package budget

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/assetmeasure"
	"m31labs.dev/gosx/internal/pagecaps"
)

// References are generated alongside markup, independently of the scanner.
// The model uses no production reachability, phase or summation helpers.
type closureNode struct {
	use         buildmanifest.PerfAssetUse
	body        []byte
	refs        []string
	contextRefs []closureReference
	baseHref    *string
}
type closureReference struct {
	url, base string
	worker    bool
}
type closureGraph struct {
	nodes     []closureNode
	redirects map[string]string
	responses map[string]closureNode // Verified responses without a final declaration.
}
type closureExpected struct {
	phases       map[string]string
	fetched      map[string]bool
	totals       PhaseBytes
	cold, wire   int64
	requests     int64
	framework    int64
	reachability string
}

func TestMeasureRedirectRelativeClosure(t *testing.T) {
	g := closureGraph{redirects: map[string]string{"/old/site.css": "/new/site.css"}}
	g.add("html", "/counter/", "html", "critical", `<link rel="stylesheet" href="/old/site.css">`, []string{"/old/site.css"})
	g.add("old-css", "/old/site.css", "css", "dormant", `@import "./child.css";`, []string{"./child.css"}, "old-child")
	g.add("new-css", "/new/site.css", "css", "dormant", `@import "./child.css";`, []string{"./child.css"}, "new-child")
	g.add("old-child", "/old/child.css", "css", "dormant", `.old{color:red}`, nil)
	g.add("new-child", "/new/child.css", "css", "dormant", `.new{color:blue;padding:10px}`, nil)
	report, fetched := measureClosureGraph(t, t.TempDir(), g)
	assertClosureModel(t, report, referenceClosure(g))
	if fetched["/old/child.css"] != 0 || fetched["/new/child.css"] == 0 {
		t.Fatal("relative reference followed the declaration instead of the final URL")
	}
}

func TestMeasureRedirectMissingFinalDeclarationRetainsPotentialClosure(t *testing.T) {
	g := closureGraph{redirects: map[string]string{"/old/site.css": "/new/site.css"}}
	g.add("html", "/counter/", "html", "critical", `<link rel="stylesheet" href="/old/site.css">`, []string{"/old/site.css"})
	g.add("css", "/old/site.css", "css", "dormant", `@import "./child.css";`, []string{"./child.css"}, "old-child")
	g.responses = map[string]closureNode{"/new/site.css": g.nodes[1]}
	g.add("old-child", "/old/child.css", "css", "dormant", `.old{color:red}`, nil)
	g.add("new-child", "/new/child.css", "css", "dormant", `.new{color:blue}`, nil)
	report, _ := measureClosureGraph(t, t.TempDir(), g)
	want := referenceClosure(g)
	if want.reachability != "unknown" {
		t.Fatal("model certified missing dependency evidence")
	}
	assertClosureModel(t, report, want)
}

func TestMeasureFinalURLClosureCorpus(t *testing.T) {
	const cases = 256
	rng := rand.New(rand.NewSource(54202))
	dir := t.TempDir()
	for i := 0; i < cases; i++ {
		g := generatedClosureGraph(i, rng)
		t.Run(fmt.Sprintf("graph-%03d", i), func(t *testing.T) {
			report, fetched := measureClosureGraph(t, dir, g)
			want := referenceClosure(g)
			assertClosureModel(t, report, want)
			gotFetches := map[string]bool{}
			for path := range fetched {
				gotFetches[path] = true
			}
			if !reflect.DeepEqual(gotFetches, want.fetched) {
				t.Errorf("verified request closure differs: got=%v want=%v", gotFetches, want.fetched)
			}
		})
	}
	t.Logf("fixed-seed final-URL closure corpus: %d graphs", cases)
}

func (g *closureGraph) add(id, path, kind, phase, body string, refs []string, deps ...string) {
	deps = append([]string{}, deps...)
	for i := range deps {
		deps[i] = "app/fixture/" + deps[i]
	}
	use := graphAsset("app/fixture/"+id, path, kind, phase, "always", []byte(body), deps...)
	g.nodes = append(g.nodes, closureNode{use: use, body: []byte(body), refs: refs})
}

func (g *closureGraph) referenceContext(id string, href *string, refs ...closureReference) {
	for i := range g.nodes {
		if strings.HasSuffix(g.nodes[i].use.ID, "/"+id) {
			g.nodes[i].baseHref, g.nodes[i].contextRefs = href, refs
			return
		}
	}
	panic("missing model node")
}

func generatedClosureGraph(i int, rng *rand.Rand) closureGraph {
	g := closureGraph{redirects: map[string]string{}}
	root := `<p>Fixture</p>`
	rootRefs := []string{}
	if i%2 == 0 {
		root += `<link rel="stylesheet" href="/old/site.css">`
		rootRefs = append(rootRefs, "/old/site.css")
	}
	if i%3 == 0 {
		root += `<iframe src="/panel/"></iframe>`
		rootRefs = append(rootRefs, "/panel/")
	}
	g.add("html", "/counter/", "html", "critical", root, rootRefs)
	if i%4 == 0 {
		g.redirects["/counter/"] = "/landing/"
		g.add("root-final", "/landing/", "html", "dormant", root, rootRefs)
	}
	main := `@import "./child.css";`
	mainRefs := []string{"./child.css"}
	if i%7 < 2 {
		unresolved := []string{"./unknown.png?q=1", "https://outside.invalid/image.png"}[i%7]
		main += `.a{background:url("` + unresolved + `")}`
		mainRefs = append(mainRefs, unresolved)
	}
	phase := []string{"critical", "startup", "after-ready", "dormant"}[i%4]
	g.add("old-css", "/old/site.css", "css", phase, main, mainRefs, "old-child")
	g.add("new-css", "/new/site.css", "css", "dormant", main, mainRefs, "new-child")
	previous := "/old/site.css"
	for j := 0; j < i%4; j++ {
		next := fmt.Sprintf("/hop%d/site.css", j)
		g.redirects[previous] = next
		previous = next
	}
	g.redirects[previous] = "/new/site.css"
	g.add("old-child", "/old/child.css", "css", "dormant", `.old{color:red}`, nil)
	child := `.a{background:url("./leaf.png")}`
	g.add("new-child", "/new/child.css", "css", "dormant", child, []string{"./leaf.png"}, "old-leaf")
	g.add("old-leaf", "/new/leaf.png", "image", "dormant", "old leaf", nil)
	if i%2 == 0 {
		g.redirects["/new/child.css"] = "../deep/child.css"
		g.add("child-final", "/deep/child.css", "css", "dormant", child, []string{"./leaf.png"}, "new-leaf")
		g.add("new-leaf", "/deep/leaf.png", "image", "dormant", "final leaf pixels", nil)
	}
	// A later declaration shares a final body with earlier references.
	g.add("shared", "/shared.css", "css", "after-ready", `@import "/new/site.css";`, []string{"/new/site.css"}, "new-css")
	panel := `<img src="./pixel.png">`
	panelRefs := []string{"./pixel.png"}
	if i%5 == 0 {
		panel += `<iframe src="/counter/"></iframe>`
		panelRefs = append(panelRefs, "/counter/") // HTML cycles need no cyclic typed edges.
	}
	g.add("panel", "/panel/", "html", "dormant", panel, panelRefs)
	g.add("panel-pixel", "/panel/pixel.png", "image", "dormant", "panel pixels", nil)
	if i%3 == 1 {
		g.redirects["/panel/"] = "/nested/panel/"
		g.add("panel-final", "/nested/panel/", "html", "dormant", panel, panelRefs)
		g.add("final-pixel", "/nested/panel/pixel.png", "image", "dormant", "final panel pixels", nil)
	}
	g.add("entry", "/entry.js", "js", "after-ready", `fetch("/panel/")`, []string{"/panel/"}, "panel")
	if i%7 == 2 {
		g.add("computed", "/computed.js", "js", "startup", `fetch(selected)`, []string{"?unresolved"})
	}
	// Same-origin absolute locations and relative locations both appear.
	redirectPaths := make([]string, 0, len(g.redirects))
	for from := range g.redirects {
		redirectPaths = append(redirectPaths, from)
	}
	sort.Strings(redirectPaths)
	for _, from := range redirectPaths {
		to := g.redirects[from]
		if rng.Intn(2) == 0 {
			u, _ := url.Parse("https://example.invalid" + from)
			v, _ := url.Parse(to)
			g.redirects[from] = u.ResolveReference(v).String()
		}
	}
	if i%2 == 0 {
		for j := range g.nodes {
			if g.nodes[j].use.URL == "/new/site.css" {
				g.nodes[j].use.ID = "framework/fixture/css"
				g.nodes[j].use.Owner = "framework"
			}
			for k, dep := range g.nodes[j].use.Dependencies {
				if dep == "app/fixture/new-css" {
					g.nodes[j].use.Dependencies[k] = "framework/fixture/css"
				}
			}
		}
	}
	rng.Shuffle(len(g.nodes), func(a, b int) { g.nodes[a], g.nodes[b] = g.nodes[b], g.nodes[a] })
	return g
}

// Breadth-first traversal follows redirects before interpreting references.
// Only reached requests acquire verified final identities. Unfetched inventory
// is reconciled with known final bodies, without assuming unobserved redirects.
func referenceClosure(g closureGraph) closureExpected {
	nodes := map[string]closureNode{}
	phase := map[string]int{}
	labels := []string{"critical", "startup", "after-ready", "dormant"}
	for _, n := range g.nodes {
		nodes[n.use.URL], phase[n.use.URL] = n, 3
	}
	rank := func(label string) int {
		for i, p := range labels {
			if p == label {
				return i
			}
		}
		panic("unknown model phase")
	}
	type pending struct {
		path             string
		phase            int
		document, worker string
	}
	queue := []pending{{path: "/counter/", phase: 0}}
	for _, n := range g.nodes {
		if n.use.Kind != "html" && n.use.Phase != "dormant" {
			queue = append(queue, pending{path: n.use.URL, phase: rank(n.use.Phase)})
		}
	}
	final := map[string]string{}
	redirectCount := map[string]int{}
	fetched := map[string]bool{}
	known := true
	rootBase := ""
	type location struct{ path, document, worker string }
	seen := map[location]int{}
	contexts := map[string][]location{}
	drain := func() {
		for len(queue) > 0 {
			next := 0
			for i, item := range queue {
				if item.document != "" || item.worker != "" || nodes[item.path].use.Kind != "js" {
					next = i
					break
				}
			}
			p := queue[next]
			queue = append(queue[:next], queue[next+1:]...)
			if p.phase < phase[p.path] {
				phase[p.path] = p.phase
			}
			key := location{p.path, p.document, p.worker}
			if previous, ok := seen[key]; ok && previous <= p.phase {
				continue
			}
			seen[key] = p.phase
			if nodes[p.path].use.Kind == "js" && p.document == "" && p.worker == "" && len(contexts[p.path]) > 0 {
				for _, c := range contexts[p.path] {
					queue = append(queue, pending{p.path, p.phase, c.document, c.worker})
				}
				continue
			}
			if p.document != "" || p.worker != "" {
				contexts[p.path] = append(contexts[p.path], key)
			}
			base, _ := url.Parse("https://example.invalid" + p.path)
			fetched[base.Path] = true
			hops := 0
			for g.redirects[base.Path] != "" {
				next, _ := url.Parse(g.redirects[base.Path])
				base = base.ResolveReference(next)
				fetched[base.Path] = true
				hops++
			}
			final[p.path], redirectCount[p.path] = base.Path, hops
			n, declared := nodes[base.Path]
			if !declared {
				n = g.responses[base.Path]
			}
			if base.Path != p.path && declared {
				queue = append(queue, pending{base.Path, p.phase, p.document, p.worker})
			}
			if n.use.Kind == "html" {
				document := *base
				if n.baseHref != nil {
					href, _ := url.Parse(*n.baseHref)
					document = *base.ResolveReference(href)
				}
				p.document, p.worker = document.String(), ""
				if p.path == "/counter/" {
					rootBase = p.document
				}
			}
			references := n.contextRefs
			if references == nil {
				for _, raw := range n.refs {
					rule := "source"
					if n.use.Kind == "html" {
						rule = "document"
					}
					references = append(references, closureReference{url: raw, base: rule})
				}
			}
			for _, ref := range references {
				r, err := url.Parse(ref.url)
				if err != nil || r.RawQuery != "" {
					known = false
					continue
				}
				parent := base
				switch ref.base {
				case "document":
					parent, _ = url.Parse(p.document)
				case "environment", "worker":
					address := p.document
					if p.worker != "" {
						address = "https://example.invalid" + p.worker
						u, _ := url.Parse(address)
						for g.redirects[u.Path] != "" {
							next, _ := url.Parse(g.redirects[u.Path])
							u = u.ResolveReference(next)
						}
						address = u.String()
					} else if ref.base == "worker" {
						known = false
						continue
					}
					if address == "" && strings.HasPrefix(ref.url, "/") && !strings.HasPrefix(ref.url, "//") {
						address = rootBase
					}
					if address == "" {
						known = false
						continue
					}
					parent, _ = url.Parse(address)
				}
				targetURL := parent.ResolveReference(r)
				if targetURL.Scheme != "https" || targetURL.Host != "example.invalid" {
					known = false
					continue
				}
				target := targetURL.Path
				if !declared && n.use.Kind != "html" {
					edge := false
					for _, dep := range n.use.Dependencies {
						edge = edge || dep == nodes[target].use.ID
					}
					known = known && edge
				}
				childPhase := p.phase
				if n.use.Kind == "html" && childPhase == 0 {
					childPhase = 1
				}
				if n.use.Kind != "html" && rank(nodes[target].use.Phase) < childPhase {
					childPhase = rank(nodes[target].use.Phase)
				}
				document, worker := p.document, p.worker
				if ref.worker {
					document, worker = "", target
				}
				queue = append(queue, pending{target, childPhase, document, worker})
			}
		}
	}
	drain()
	if !known {
		for _, n := range g.nodes {
			if n.use.Kind != "html" {
				queue = append(queue, pending{path: n.use.URL, phase: 1})
			}
		}
		drain()
	}
	// The physical phase is independent of which request first verified it.
	physical := map[string]int{}
	for request, destination := range final {
		if previous, ok := physical[destination]; !ok || phase[request] < previous {
			physical[destination] = phase[request]
		}
	}
	out := closureExpected{phases: map[string]string{}, fetched: fetched, reachability: "known"}
	if !known {
		out.reachability = "unknown"
	}
	type cost struct {
		phase    int
		size     int64
		owner    string
		observed bool
	}
	costs := map[string]cost{}
	for _, n := range g.nodes {
		key := n.use.URL
		if destination, ok := final[key]; ok {
			key = destination
		}
		p := phase[n.use.URL]
		if earliest, ok := physical[key]; ok {
			p = earliest
		}
		out.phases[n.use.ID] = labels[p]
		c := cost{phase: p, size: int64(len(n.body)), owner: n.use.Owner}
		_, c.observed = physical[key]
		if prior, ok := costs[key]; ok && prior.owner == "framework" {
			c.owner = "framework"
		}
		costs[key] = c
		for hop := 0; hop < redirectCount[n.use.URL]; hop++ {
			costs[fmt.Sprintf("redirect:%s:%d", n.use.URL, hop)] = cost{phase[n.use.URL], int64(len("redirect fixture")), n.use.Owner, true}
		}
	}
	for _, c := range costs {
		targets := []*int64{&out.totals.Critical, &out.totals.Startup, &out.totals.AfterReady, &out.totals.Dormant}
		*targets[c.phase] += c.size
		if c.phase < 2 {
			out.cold += c.size
			if c.owner == "framework" {
				out.framework += c.size
			}
			if c.observed {
				out.wire += c.size
				out.requests++
			}
		}
	}
	return out
}

func measureClosureGraph(t *testing.T, dir string, g closureGraph) (AppReport, map[string]int) {
	t.Helper()
	var root closureNode
	assets := []buildmanifest.PerfAssetUse{}
	byURL := map[string]closureNode{}
	for _, n := range g.nodes {
		assets = append(assets, n.use)
		byURL[n.use.URL] = n
		if n.use.URL == "/counter/" {
			root = n
		}
		file := strings.TrimPrefix(n.use.URL, "/")
		if n.use.Kind == "html" {
			file = strings.Trim(file, "/") + "/index.html"
		}
		file = filepath.Join(dir, file)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, n.body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	caps, err := pagecaps.FromHTML(root.body)
	if err != nil {
		t.Fatal(err)
	}
	types, err := pagecaps.Classify(caps, false)
	if err != nil {
		t.Fatal(err)
	}
	info := publicTestReport(t).Info
	manifest := &FixtureManifest{Schema: "gosx.perf-fixtures/v1", Version: 1, SourceSHA: info.SHA, CatalogSHA256: info.FixtureSHA256, Assets: assets,
		Routes: []FixtureRoute{{App: "fixture", RouteTemplate: "/counter/", SourcePath: "fixture/page.gsx", PageTypes: types, Capabilities: caps, CriticalAssetIDs: []string{root.use.ID}, InputSequenceID: "counter-input"}}}
	writeTestFixtureManifest(t, dir, manifest)
	info.ArtifactSHA256 = &manifest.FixturesSHA256
	fetched := map[string]int{}
	client := &http.Client{Transport: testRoundTrip(func(req *http.Request) (*http.Response, error) {
		fetched[req.URL.Path]++
		if target := g.redirects[req.URL.Path]; target != "" {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {target}}, Body: io.NopCloser(strings.NewReader("redirect fixture")), ContentLength: int64(len("redirect fixture")), Request: req}, nil
		}
		n, ok := byURL[req.URL.Path]
		if !ok {
			n, ok = g.responses[req.URL.Path]
		}
		if !ok {
			t.Errorf("unexpected fetch: %s", req.URL.Path)
		}
		media := map[string]string{"html": "text/html", "css": "text/css", "js": "text/javascript", "image": "image/png"}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {media[n.use.Kind]}}, Body: io.NopCloser(bytes.NewReader(n.body)), ContentLength: int64(len(n.body))}, nil
	})}
	// Equal lengths isolate closure/accounting from compressor behavior; hashes
	// still bind every fixture and served body through the production verifier.
	normalize := func(body []byte) (assetmeasure.Sizes, error) {
		n := int64(len(body))
		return assetmeasure.Sizes{Raw: n, Gzip: n, Brotli: n, SHA256: testMeasureHash(body)}, nil
	}
	report, err := measureApp(context.Background(), MeasureOptions{App: "fixture", DistDir: dir, BaseURL: "https://example.invalid", Client: client, Public: info}, normalize)
	if err != nil {
		t.Fatal(err)
	}
	return report, fetched
}

func assertClosureModel(t *testing.T, report AppReport, want closureExpected) {
	t.Helper()
	phases := map[string]string{}
	for _, a := range report.Assets {
		phases[a.ID] = a.Phase
	}
	if !reflect.DeepEqual(phases, want.phases) {
		t.Errorf("physical closure phases differ: got=%v want=%v", phases, want.phases)
	}
	row := report.Rows[0]
	if row.PhaseBytes != want.totals || row.NormalizedBytes != want.cold || row.WireBytes != want.wire || row.Requests != want.requests || row.FrameworkBytes != want.framework || row.AppBytes != want.cold-want.framework {
		t.Errorf("closure costs differ: phases=%v want=%v cold=%d/%d wire=%d/%d requests=%d/%d framework=%d/%d", row.PhaseBytes, want.totals, row.NormalizedBytes, want.cold, row.WireBytes, want.wire, row.Requests, want.requests, row.FrameworkBytes, want.framework)
	}
	if report.Coverage.Reachability != want.reachability {
		t.Errorf("reachability=%s want=%s", report.Coverage.Reachability, want.reachability)
	}
}

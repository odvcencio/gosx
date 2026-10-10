package budget

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"m31labs.dev/gosx/internal/pagecaps"
	"m31labs.dev/gosx/internal/pagecaps/embeddingtest"
)

// The reference works on the generated browser declarations, without calling
// the production parser, scanner or planner. Fetched bodies count once; their
// execution permission is the union of all embedding paths. Inline documents
// belong to the embedding occurrence, so different srcdoc copies stay distinct.
type modelDocument struct {
	script, event, classic bool
	element                string
	frames                 []modelFrame
}
type modelFrame struct {
	src      string
	inline   *modelDocument
	sandbox  *string
	template bool
}
type modelTreeResult struct {
	fetched   map[string]bool
	execution HTMLExecution
	known     bool
}

func renderModelDocument(doc *modelDocument) string {
	element := modelEmbeddingElement(doc)
	var body bytes.Buffer
	body.WriteString("<!doctype html><html><head>")
	markup := func(f modelFrame) string {
		attrs := ""
		if f.sandbox != nil {
			attrs += ` sandbox="` + html.EscapeString(*f.sandbox) + `"`
		}
		if f.src != "" {
			attrs += ` src="` + html.EscapeString(f.src) + `"`
		}
		if f.inline != nil {
			attrs += ` srcdoc="` + html.EscapeString(renderModelDocument(f.inline)) + `"`
		}
		return element.Markup(attrs)
	}
	// Templates live in the head so frameset parsing cannot ignore their opening
	// tag and accidentally turn an intended inert frame into a live embedding.
	for _, f := range doc.frames {
		if f.template {
			body.WriteString("<template>" + markup(f) + "</template>")
		}
	}
	if doc.script {
		if doc.classic {
			body.WriteString(`<script>run()</script>`)
		} else {
			body.WriteString(`<script type="module">run()</script>`)
		}
	}
	body.WriteString("</head>")
	if element.Frameset {
		body.WriteString("<frameset")
		if doc.event {
			body.WriteString(` onload="run()"`)
		}
		body.WriteString(">")
	} else {
		body.WriteString("<body>")
	}
	if doc.event && !element.Frameset {
		body.WriteString(`<button onclick="run()">Run</button>`)
	}
	for _, f := range doc.frames {
		if f.template {
			continue
		}
		body.WriteString(markup(f))
	}
	if element.Frameset {
		body.WriteString("</frameset>")
	} else {
		body.WriteString("</body>")
	}
	body.WriteString("</html>")
	return body.String()
}

func modelEmbeddingElement(doc *modelDocument) embeddingtest.Element {
	name := doc.element
	if name == "" {
		name = "iframe"
	}
	return embeddingtest.Lookup(name)
}

func referenceDocumentTree(docs map[string]*modelDocument) modelTreeResult {
	out := modelTreeResult{fetched: map[string]bool{"/counter/": true}, known: true}
	counted := map[string]bool{}
	var visit func(*modelDocument, string, bool, int, map[string]bool)
	visit = func(doc *modelDocument, key string, allowed bool, depth int, path map[string]bool) {
		if depth > pagecaps.MaxSrcdocDepth {
			out.known = false
			return
		}
		if allowed && !counted[key] {
			counted[key] = true
			if doc.script {
				out.execution.ExecutableSources++
				out.execution.ExecutableScripts++
				out.execution.InlineAppScriptBytes += 5
				out.execution.InlineAppScriptMax = 5
				if doc.classic {
					out.execution.SyncExecutableScripts++
				}
			}
			if doc.event {
				out.execution.ExecutableSources++
				out.execution.InlineAppScriptBytes += int64(len("run()"))
				out.execution.InlineAppScriptMax = max(out.execution.InlineAppScriptMax, int64(len("run()")))
			}
		}
		element := modelEmbeddingElement(doc)
		for i, f := range doc.frames {
			if f.template {
				continue
			}
			permitted := allowed && (!element.Sandbox || f.sandbox == nil || *f.sandbox == "allow-scripts")
			if element.Srcdoc && f.inline != nil {
				visit(f.inline, fmt.Sprintf("inline:%s/frame%d", key, i), permitted, depth+1, path)
			} else if element.Src && f.src != "" {
				out.fetched[f.src] = true
				child, ok := docs[f.src]
				if !ok || path[f.src] {
					out.known = false
					continue
				}
				next := make(map[string]bool, len(path)+1)
				for p, v := range path {
					next[p] = v
				}
				next[f.src] = true
				visit(child, "url:"+f.src, permitted, depth+1, next)
			}
		}
	}
	visit(docs["/counter/"], "url:/counter/", true, 0, map[string]bool{"/counter/": true})
	return out
}

func measureModelTree(t *testing.T, docs map[string]*modelDocument) (AppReport, map[string][]byte, map[string]int) {
	t.Helper()
	return measureModelTreeRedirects(t, docs, nil)
}

func measureModelTreeRedirects(t *testing.T, docs map[string]*modelDocument, redirects map[string]string) (AppReport, map[string][]byte, map[string]int) {
	t.Helper()
	dir := t.TempDir()
	info := publicTestReport(t).Info
	bodies := map[string][]byte{}
	ids := map[string]string{}
	urls := make([]string, 0, len(docs))
	for u := range docs {
		urls = append(urls, u)
	}
	sort.Strings(urls)
	manifest := &FixtureManifest{Schema: "gosx.perf-fixtures/v1", Version: 1, SourceSHA: info.SHA, CatalogSHA256: info.FixtureSHA256}
	for i, u := range urls {
		body := []byte(renderModelDocument(docs[u]))
		bodies[u] = body
		id := fmt.Sprintf("app/fixture/document-%d", i)
		ids[u] = id
		phase := "dormant"
		if u == "/counter/" {
			phase = "critical"
		}
		manifest.Assets = append(manifest.Assets, graphAsset(id, u, "html", phase, "always", body))
		p := filepath.Join(dir, u[1:], "index.html")
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	caps, err := pagecaps.FromHTML(bodies["/counter/"])
	if err != nil {
		t.Fatal(err)
	}
	manifest.Routes = []FixtureRoute{{App: "fixture", RouteTemplate: "/counter/", SourcePath: "fixture/page.gsx", PageTypes: []string{"static", "enhanced"}, Capabilities: caps, CriticalAssetIDs: []string{ids["/counter/"]}, InputSequenceID: "counter-input"}}
	writeTestFixtureManifest(t, dir, manifest)
	info.ArtifactSHA256 = &manifest.FixturesSHA256
	requests := map[string]int{}
	client := &http.Client{Transport: testRoundTrip(func(req *http.Request) (*http.Response, error) {
		if target := redirects[req.URL.Path]; target != "" {
			requests[req.URL.Path]++
			return &http.Response{StatusCode: 302, Request: req, ContentLength: 5, Header: http.Header{"Location": {target}}, Body: io.NopCloser(bytes.NewReader([]byte("moved")))}, nil
		}
		body, ok := bodies[req.URL.Path]
		if !ok {
			t.Errorf("undeclared document fetch")
			body = []byte("missing")
		}
		requests[req.URL.Path]++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}, nil
	})}
	got, err := measureApp(context.Background(), MeasureOptions{App: "fixture", DistDir: dir, BaseURL: "https://example.invalid", Client: client, Public: info}, executionCorpusEncoder)
	if err != nil {
		t.Fatal(err)
	}
	return got, bodies, requests
}

func TestCheckDocumentTreeReferenceCorpus(t *testing.T) {
	rng := rand.New(rand.NewSource(55004))
	blocked, enabled := "", "allow-scripts"
	corpus := []map[string]*modelDocument{}
	for n := 0; n < 128; n++ {
		leaf := &modelDocument{script: rng.Intn(2) == 0, event: rng.Intn(2) == 0, classic: rng.Intn(2) == 0}
		docs := map[string]*modelDocument{"/leaf/": leaf, "/unused/": {script: true}}
		var branch func(int) *modelDocument
		branch = func(depth int) *modelDocument {
			doc := &modelDocument{script: rng.Intn(5) == 0, event: rng.Intn(5) == 0, classic: rng.Intn(2) == 0,
				element: embeddingtest.Elements[rng.Intn(len(embeddingtest.Elements))].Name}
			if depth == 0 {
				doc.frames = []modelFrame{{src: "/leaf/"}}
				return doc
			}
			for f := 0; f < 1+rng.Intn(2); f++ {
				frame := modelFrame{src: "/leaf/", template: rng.Intn(5) == 0}
				switch rng.Intn(3) {
				case 0:
					frame.sandbox = &blocked
				case 1:
					frame.sandbox = &enabled
				}
				if rng.Intn(2) == 0 {
					frame.inline = branch(depth - 1)
				}
				doc.frames = append(doc.frames, frame)
			}
			return doc
		}
		docs["/child/"] = branch(1 + rng.Intn(3))
		docs["/counter/"] = branch(1 + rng.Intn(3))
		// Exercise both source override and permission union in both orders.
		order := []modelFrame{{src: "/child/", sandbox: &blocked}, {src: "/child/", sandbox: &enabled}, {src: "/unused/", inline: &modelDocument{}, sandbox: &blocked}}
		rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		docs["/counter/"].frames = append(docs["/counter/"].frames, order...)
		corpus = append(corpus, docs)
	}
	// Fetched and mixed inline/fetched chains reach the documented boundary.
	for _, element := range embeddingtest.Elements {
		for variant := 0; variant < 4; variant++ {
			docs := map[string]*modelDocument{"/counter/": {}, "/unused/": {script: true}}
			current := docs["/counter/"]
			for depth := 1; depth <= pagecaps.MaxSrcdocDepth; depth++ {
				next := &modelDocument{}
				current.element = element.Name
				frame := modelFrame{}
				if variant >= 2 && depth%2 == 0 && element.Srcdoc {
					frame.inline = next
				} else {
					frame.src = fmt.Sprintf("/d%02d/", depth)
					docs[frame.src] = next
				}
				if variant%2 == 1 && depth == 1 {
					frame.sandbox = &blocked
				}
				current.frames = []modelFrame{frame}
				current = next
			}
			current.script, current.event = true, true
			corpus = append(corpus, docs)
		}
	}
	// Inline occurrence identities must not collide with fetched URL identities.
	corpus = append(corpus, map[string]*modelDocument{
		"/counter/":       {frames: []modelFrame{{src: "/child"}}},
		"/child":          {frames: []modelFrame{{inline: &modelDocument{}}, {src: "/child/srcdoc/0"}}},
		"/child/srcdoc/0": {script: true},
	})
	disagreements := 0
	for i, docs := range corpus {
		t.Run(fmt.Sprintf("tree-%03d", i), func(t *testing.T) {
			want := referenceDocumentTree(docs)
			got, bodies, requests := measureModelTree(t, docs)
			observed := map[string]bool{}
			for u := range requests {
				observed[u] = true
			}
			if !reflect.DeepEqual(observed, want.fetched) {
				disagreements++
				t.Errorf("fetched closure=%v want=%v", observed, want.fetched)
			}
			if got.execution["/counter/"] != want.execution {
				disagreements++
				t.Errorf("execution=%+v want=%+v", got.execution["/counter/"], want.execution)
			}
			known := got.Coverage.Reachability == "known"
			if known != want.known {
				disagreements++
				t.Errorf("reachability=%s want known=%v", got.Coverage.Reachability, want.known)
			}
			var cold int64
			for u := range want.fetched {
				cold += int64(len(bodies[u]))
			}
			row := got.Rows[0]
			if row.NormalizedBytes != cold || row.Requests != int64(len(want.fetched)) {
				disagreements++
				t.Errorf("bytes/requests=%d/%d want=%d/%d", row.NormalizedBytes, row.Requests, cold, len(want.fetched))
			}
			for _, p := range row.Policies {
				if p.Name == "zero-js" && p.Passed != (want.execution.ExecutableSources == 0) {
					disagreements++
					t.Errorf("zero-js=%v want=%v", p.Passed, want.execution.ExecutableSources == 0)
				}
			}
		})
	}
	t.Logf("seed=55004 trees=%d disagreements=%d", len(corpus), disagreements)
}

func TestCheckDocumentTreeRedirectPermissionUnion(t *testing.T) {
	blocked := ""
	for _, restrictedAlias := range []bool{false, true} {
		t.Run(fmt.Sprint(restrictedAlias), func(t *testing.T) {
			alias, target := modelFrame{src: "/alias/"}, modelFrame{src: "/child/"}
			if restrictedAlias {
				alias.sandbox = &blocked
			} else {
				target.sandbox = &blocked
			}
			child := &modelDocument{script: true}
			docs := map[string]*modelDocument{"/counter/": {frames: []modelFrame{alias, target}}, "/alias/": child, "/child/": child}
			got, _, _ := measureModelTreeRedirects(t, docs, map[string]string{"/alias/": "/child/"})
			if got.execution["/counter/"].ExecutableSources != 1 {
				t.Fatal("redirect identity lost permitted execution", got.execution)
			}
		})
	}
}

func TestCheckDocumentTreeRedirectIsUnresolved(t *testing.T) {
	child := &modelDocument{}
	docs := map[string]*modelDocument{"/counter/": {frames: []modelFrame{{src: "/alias/"}}}, "/alias/": child, "/child/": child}
	got, _, _ := measureModelTreeRedirects(t, docs, map[string]string{"/alias/": "/child/"})
	if got.Coverage.Reachability != "unknown" {
		t.Fatal("changed document URL certified declaration-based tree", got.Coverage)
	}
	for _, p := range got.Rows[0].Policies {
		if p.Name == "declared-fetches" && p.Passed {
			t.Fatal("unresolved final URL passed declaration policy")
		}
	}
}

func TestCheckDocumentTreeSeparatesInlineAndURLIdentity(t *testing.T) {
	docs := map[string]*modelDocument{
		"/counter/":       {frames: []modelFrame{{src: "/child"}}},
		"/child":          {frames: []modelFrame{{inline: &modelDocument{}}, {src: "/child/srcdoc/0"}}},
		"/child/srcdoc/0": {script: true},
	}
	got, _, _ := measureModelTree(t, docs)
	if got.execution["/counter/"].ExecutableSources != 1 {
		t.Fatal("inline occurrence hid a fetched document", got.execution)
	}
}

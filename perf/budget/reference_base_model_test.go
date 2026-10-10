package budget

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func TestMeasureReferenceBaseClosureCorpus(t *testing.T) {
	const cases = 128
	rng := rand.New(rand.NewSource(54203))
	dir := t.TempDir()
	for i := 0; i < cases; i++ {
		t.Run(fmt.Sprintf("base-%03d", i), func(t *testing.T) {
			g := generatedReferenceBaseGraph(i, rng)
			report, fetched := measureClosureGraph(t, dir, g)
			want := referenceClosure(g)
			assertClosureModel(t, report, want)
			got := map[string]bool{}
			for path := range fetched {
				got[path] = true
			}
			if !reflect.DeepEqual(got, want.fetched) {
				t.Errorf("request closure differs: got=%v want=%v", got, want.fetched)
			}
		})
	}
	t.Logf("seed=54203 reference-base closure corpus: %d graphs", cases)
}

func generatedReferenceBaseGraph(i int, rng *rand.Rand) closureGraph {
	g := closureGraph{redirects: map[string]string{}}
	scriptDir := fmt.Sprintf("/scripts/s%d/", rng.Intn(17))
	docDir := "/counter/"
	var href *string
	root := ""
	if i%4 != 0 {
		docDir = fmt.Sprintf("/content/d%d/", rng.Intn(17))
		value := docDir
		if i%4 == 2 {
			value = ".." + docDir
		}
		if i%4 == 3 {
			value = "https://example.invalid" + docDir
		}
		href = &value
		root = `<base href="` + value + `"><base href="/ignored/">`
	}
	entryURL, styleURL := scriptDir+"entry.js", fmt.Sprintf("/styles/c%d/main.css", rng.Intn(17))
	entryRequest := entryURL
	if i%3 == 0 {
		entryRequest = fmt.Sprintf("/aliases/a%d/entry.js", i)
		g.redirects[entryRequest] = entryURL
	}
	root += `<script type="module" src="` + entryRequest + `"></script><link rel="stylesheet" href="` + styleURL + `"><style>.inline{background:url('./inline.png')}</style>`
	refs := []closureReference{{entryRequest, "document", false}, {styleURL, "document", false}, {"./inline.png", "document", false}}
	if i%2 == 0 {
		root += `<script type="module">fetch("./inline.js");import("./inline.js");</script>`
		refs = append(refs, closureReference{"./inline.js", "environment", false}, closureReference{"./inline.js", "document", false})
	}
	g.add("html", "/counter/", "html", "critical", root, nil)
	g.referenceContext("html", href, refs...)
	g.add("inline-image", docDir+"inline.png", "image", "dormant", "inline image", nil)
	g.add("inline-script", docDir+"inline.js", "js", "dormant", `const inline=1`, nil)
	code := `fetch("./data.js");import("./data.js");new Worker("./worker.js");new SharedWorker(new URL("./worker.js",import.meta.url));`
	deps := []string{"document-data", "module-data", "document-worker", "module-worker"}
	if i%4 == 1 {
		// A later document executes the same script with a different API base.
		g.add("late-loader", "/late/loader.js", "js", "after-ready", `fetch("/frames/view/");`, nil, "frame")
		g.referenceContext("late-loader", nil, closureReference{"/frames/view/", "environment", false})
		frame := `<base href="/frame-data/"><script type="module" src="` + entryURL + `"></script>`
		g.add("frame", "/frames/view/", "html", "dormant", frame, nil)
		frameHref := "/frame-data/"
		g.referenceContext("frame", &frameHref, closureReference{entryURL, "document", false})
		g.add("frame-data", "/frame-data/data.js", "js", "dormant", `const frameData=1`, nil)
		g.add("frame-worker", "/frame-data/worker.js", "js", "dormant", `const frameWorker=1`, nil)
		deps = append(deps, "frame-data", "frame-worker")
	}
	phase := "dormant"
	if i%5 == 0 {
		phase = "critical"
	}
	g.add("entry", entryURL, "js", phase, code, nil, deps...)
	g.referenceContext("entry", nil, closureReference{"./data.js", "environment", false}, closureReference{"./data.js", "source", false}, closureReference{"./worker.js", "environment", true}, closureReference{"./worker.js", "source", true})
	g.add("document-data", docDir+"data.js", "js", "dormant", `const documentData=1`, nil)
	g.add("module-data", scriptDir+"data.js", "js", "dormant", `const moduleData=1`, nil)
	api := []string{`fetch("./data.js");`, `new XMLHttpRequest().open("GET","./data.js");`, `new EventSource("./data.js");`, `new WebSocket("./data.js");`}[i%4]
	for _, worker := range []struct{ id, dir, data string }{{"document", docDir, "document-data"}, {"module", scriptDir, "module-data"}} {
		body := api + `import("./modules/helper.js");importScripts("./classic.js");`
		g.add(worker.id+"-worker", worker.dir+"worker.js", "js", "dormant", body, nil, worker.data, worker.id+"-helper", worker.id+"-classic")
		g.referenceContext(worker.id+"-worker", nil, closureReference{"./data.js", "environment", false}, closureReference{"./modules/helper.js", "source", false}, closureReference{"./classic.js", "worker", false})
		// Imported modules retain the worker entry URL for environment APIs.
		helper := `fetch("./data.js");fetch(new URL("./own.png",import.meta.url));`
		g.add(worker.id+"-helper", worker.dir+"modules/helper.js", "js", "dormant", helper, nil, worker.data, worker.id+"-own")
		g.referenceContext(worker.id+"-helper", nil, closureReference{"./data.js", "environment", false}, closureReference{"./own.png", "source", false})
		g.add(worker.id+"-own", worker.dir+"modules/own.png", "image", "dormant", "module image", nil)
		g.add(worker.id+"-classic", worker.dir+"classic.js", "js", "dormant", `const classic=1`, nil)
	}
	g.add("style", styleURL, "css", "dormant", `@import "./child.css";.a{background:url('./pixel.png')}`, []string{"./child.css", "./pixel.png"}, "child-style", "style-image")
	styleDir := styleURL[:len(styleURL)-len("main.css")]
	g.add("child-style", styleDir+"child.css", "css", "dormant", `.child{color:red}`, nil)
	g.add("style-image", styleDir+"pixel.png", "image", "dormant", "style pixels", nil)
	if i%3 == 0 {
		g.add("entry-alias", entryRequest, "js", "startup", code, nil, deps...)
		g.referenceContext("entry-alias", nil, closureReference{"./data.js", "environment", false}, closureReference{"./data.js", "source", false}, closureReference{"./worker.js", "environment", true}, closureReference{"./worker.js", "source", true})
	}
	if i%8 == 0 {
		g.add("orphan", "/unanchored/entry.js", "js", "after-ready", `fetch("./unknown.png");`, nil)
		g.referenceContext("orphan", nil, closureReference{"./unknown.png", "environment", false})
	}
	g.add("unused", "/unused/pixel.png", "image", "dormant", "unused pixels", nil)
	rng.Shuffle(len(g.nodes), func(a, b int) { g.nodes[a], g.nodes[b] = g.nodes[b], g.nodes[a] })
	return g
}

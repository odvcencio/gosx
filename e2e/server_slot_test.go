//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/internal/gsxcorpus"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/transpile"
)

const serverSlotServedSource = `package app
import "m31labs.dev/gosx/signal"
type PanelsProps struct { Instance string }
//gosx:island
component Panels(props: PanelsProps) {
 visible := signal.New(true)
 toggle := func() { visible.Set(!visible.Get()) }
 return <div><button id={props.Instance + "-toggle"} onClick={toggle}>Toggle</button><ServerSlot name="Profile" hidden={!visible.Get()} /><ServerSlot name="Security" hidden={visible.Get()} /></div>
}
component Plain() {
 return <button>Ordinary</button>
}
component OrdinaryPage() {
 return <main><Plain /></main>
}
component Page() {
 return <main><Panels Instance="a"><section slot="Profile"><input id="input-a" value="seed-a" /></section><section slot="Security">Security A</section></Panels><Panels Instance="b"><section slot="Profile"><input id="input-b" value="seed-b" /></section><section slot="Security">Security B</section></Panels></main>
}
`

func TestServerSlotServedGo(t *testing.T) {
	testServerSlotServed(t, "GOSX_E2B_GO_WASM", filepath.Join(runtime.GOROOT(), "lib", "wasm", "wasm_exec.js"), false)
}
func TestServerSlotServedTinyGo(t *testing.T) {
	testServerSlotServed(t, "GOSX_E2B_TINYGO_WASM", os.Getenv("GOSX_E2B_TINYGO_EXEC"), true)
}

func testServerSlotServed(t *testing.T, env, execPath string, tiny bool) {
	wasmPath := os.Getenv(env)
	if !filepath.IsAbs(wasmPath) || !filepath.IsAbs(execPath) {
		t.Fatalf("explicit fresh artifact and wasm_exec paths required: %s", env)
	}
	wasm, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(wasm)
	hash := fmt.Sprintf("%x", sum[:8])
	t.Logf("fresh runtime=%s bytes=%d hash=%s tiny=%v", wasmPath, len(wasm), hash, tiny)
	p, err := gosx.Compile([]byte(serverSlotServedSource))
	if err != nil {
		t.Fatal(err)
	}
	progs := map[string]*program.Program{}
	for i, c := range p.Components {
		if c.IsIsland {
			v, e := ir.LowerIsland(p, i)
			if e != nil {
				t.Fatal(e)
			}
			progs[c.Name] = v
		}
	}
	app := server.New()
	generated := serverSlotGeneratedPages(t, hash, int64(len(wasm)), tiny)
	app.SetRuntimeRoot(e2eRepoRoot(t))
	assets := app.Build()
	var chunkRequests atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gosx/runtime.wasm" || r.URL.Path == "/gosx/runtime-islands.wasm" || r.URL.Path == "/gosx/assets/runtime/runtime-core.wasm" {
			w.Header().Set("Content-Type", "application/wasm")
			w.Write(wasm)
			return
		}
		if r.URL.Path == "/gosx/wasm_exec.js" {
			http.ServeFile(w, r, execPath)
			return
		}
		if r.URL.Path == "/gosx/server-slots.js" {
			chunkRequests.Add(1)
			assets.ServeHTTP(w, r)
			return
		}
		format := r.URL.Query().Get("format")
		if format != "bin" {
			format = "json"
		}
		encode := func(prog *program.Program) []byte {
			var b []byte
			var e error
			if format == "bin" {
				b, e = program.EncodeBinary(prog)
			} else {
				b, e = program.EncodeJSON(prog)
			}
			if e != nil {
				panic(e)
			}
			return b
		}
		if strings.HasPrefix(r.URL.Path, "/program/") {
			name := strings.TrimPrefix(r.URL.Path, "/program/")
			if v := progs[name]; v != nil {
				w.Write(encode(v))
				return
			}
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/gosx/") {
			assets.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/generated" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, generated[format])
			return
		}
		renderer := island.NewRenderer("gosx-page")
		renderer.SetRuntime("/gosx/runtime.wasm", hash, int64(len(wasm)))
		if tiny {
			if e := renderer.ApplyBuildManifest(&buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{WASMVariants: map[string]buildmanifest.RuntimeVariantAsset{"core": {HashedAsset: buildmanifest.HashedAsset{File: "runtime-core.wasm", Hash: hash, Size: int64(len(wasm))}, Variant: "core"}}}}, "/gosx/assets"); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		for name, prog := range progs {
			data := encode(prog)
			digest := sha256.Sum256(data)
			renderer.SetProgramAsset(name, "/program/"+name+"?format="+format, format, fmt.Sprintf("%x", digest[:8]))
		}
		entry := "Page"
		if r.URL.Path == "/ordinary" {
			entry = "OrdinaryPage"
		}
		body, e := route.RenderProgramComponent(p, entry, route.ProgramRenderEnv{RenderIsland: renderer.RenderIslandFromProgram, RenderServerSlots: renderer.RenderWithServerSlots})
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		if r.URL.Query().Get("stale") == "1" && renderer.Manifest().ServerSlotHost != nil {
			renderer.Manifest().ServerSlotHost.Integrity = "sha256-" + strings.Repeat("A", 43) + "="
		}
		if r.URL.Query().Get("missing") == "1" {
			renderer.Manifest().ServerSlotHost = nil
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<!doctype html><html><head>", gosx.RenderHTML(gosx.Fragment(renderer.PreloadHints(), renderer.PageHead())), "</head><body>", body, "</body></html>")
	}))
	defer s.Close()
	t.Run("ordinary", func(t *testing.T) {
		before := chunkRequests.Load()
		page := newBrowserPage(t, e2eChromePath(t), nil, 800, 600, "", 30*time.Second)
		page.navigate(t, s.URL+"/ordinary")
		var ordinary bool
		page.eval(t, `document.querySelector('main button')?.textContent==='Ordinary'&&!document.querySelector('[data-gosx-server-slot]')&&!document.documentElement.innerHTML.includes('serverSlotHost')`, &ordinary)
		if !ordinary || chunkRequests.Load() != before {
			t.Fatal("ordinary page unexpectedly published/fetched projection capability")
		}
	})
	for _, mode := range []string{"missing", "stale"} {
		t.Run(mode, func(t *testing.T) {
			page := newBrowserPage(t, e2eChromePath(t), nil, 800, 600, "", 30*time.Second)
			page.navigate(t, s.URL+"/?"+mode+"=1")
			if err := chromedp.Run(page.ctx, chromedp.Poll(`window.__gosx?.host?.hydration`, nil, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
				t.Fatal(err)
			}
			var retained bool
			page.eval(t, `document.querySelector('#input-a')?.value==='seed-a'&&!document.querySelector('#input-a').closest('[data-gosx-server-slot]').hidden&&(!window.__gosx.islands||window.__gosx.islands.size===0)`, &retained)
			if !retained {
				t.Fatal("unavailable capability did not preserve visible SSR")
			}
		})
	}
	for _, leg := range []string{"file", "generated"} {
		for _, format := range []string{"json", "bin"} {
			t.Run(leg+"/"+format, func(t *testing.T) {
				before := chunkRequests.Load()
				page := newBrowserPage(t, e2eChromePath(t), nil, 800, 600, "", 30*time.Second)
				path := "/"
				if leg == "generated" {
					path = "/generated"
				}
				page.navigate(t, s.URL+path+"?format="+format)
				if err := chromedp.Run(page.ctx, chromedp.Poll(`window.__gosx?.islands?.size===2`, nil, chromedp.WithPollingTimeout(15*time.Second))); err != nil {
					t.Fatalf("hydration: %v console=%s", err, page.Console())
				}
				if chunkRequests.Load()-before != 1 {
					t.Fatalf("two owners fetched %d chunks", chunkRequests.Load()-before)
				}
				page.eval(t, `window.savedInput=document.querySelector('#input-a');savedInput.value='edited';window.savedOwner=window.__gosx.islands.get('gosx-island-0');`, nil)
				if err := chromedp.Run(page.ctx, chromedp.Click("#a-toggle", chromedp.ByQuery), chromedp.Poll(`document.querySelector('#input-a').closest('[data-gosx-server-slot]').hidden`, nil, chromedp.WithPollingTimeout(5*time.Second)), chromedp.Click("#a-toggle", chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
				var preserved bool
				page.eval(t, `savedInput===document.querySelector('#input-a')&&savedInput.value==='edited'&&!document.querySelector('#input-b').closest('[data-gosx-server-slot]').hidden&&document.querySelector('#input-b').value==='seed-b'&&window.__gosx.runtime.serverSlots.version===1`, &preserved)
				if !preserved {
					t.Fatal("caller identity/value or independent owner lost")
				}
				for _, failure := range []string{"capability", "inventory", "write"} {
					var retained bool
					page.eval(t, `(async()=>{const mode=`+strconv.Quote(failure)+`;const g=window.__gosx,m=JSON.parse(document.querySelector('#gosx-manifest').textContent),entry=structuredClone(m.islands[0]),owner=g.islands.get(entry.id),input=document.querySelector('#input-a'),shell=input.closest('[data-gosx-server-slot]'),api=g.runtime.serverSlots,version=api.version;input.value='edited';try{if(mode==='capability')api.version=999;if(mode==='inventory')entry.serverSlots=['Profile','Profile'];if(mode==='write')Object.defineProperty(shell,'hidden',{configurable:true,get(){return false},set(){throw Error('persistent fixture setter')}});await g.host.hydration.hydrateIsland(entry,m.serverSlotHost)}finally{api.version=version;if(mode==='write')delete shell.hidden}return g.islands.get(entry.id)===owner&&document.querySelector('#input-a')===input&&input.value==='edited'&&g.islands.size===2})()`, &retained)
					if !retained {
						t.Fatalf("%s failure lost previous owner/caller", failure)
					}
					if err := chromedp.Run(page.ctx, chromedp.Click("#a-toggle", chromedp.ByQuery), chromedp.Poll(`document.querySelector('#input-a').closest('[data-gosx-server-slot]').hidden`, nil, chromedp.WithPollingTimeout(5*time.Second)), chromedp.Click("#a-toggle", chromedp.ByQuery), chromedp.Poll(`!document.querySelector('#input-a').closest('[data-gosx-server-slot]').hidden`, nil, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
						t.Fatalf("%s old listener/state not live: %v", failure, err)
					}
				}
			})
		}
	}
}

// Execute actual opt-in generated Page calls; no handwritten island registration.
func serverSlotGeneratedPages(t *testing.T, hash string, size int64, tiny bool) map[string]string {
	t.Helper()
	source := strings.Replace(serverSlotServedSource, "package app", "package main", 1)
	output, err := transpile.Transpile([]byte(source), transpile.Options{RuntimeIslands: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := (gsxcorpus.Case{}).WriteStrictCheckModule(dir); err != nil {
		t.Fatal(err)
	}
	manifest := buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{WASM: buildmanifest.HashedAsset{File: "runtime-full.wasm", Hash: hash, Size: size}}}
	if tiny {
		manifest.Runtime.WASMVariants = map[string]buildmanifest.RuntimeVariantAsset{"core": {HashedAsset: buildmanifest.HashedAsset{File: "runtime-core.wasm", Hash: hash, Size: size}, Variant: "core"}}
	}
	b, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "page.go"), []byte(output), 0600); err != nil {
		t.Fatal(err)
	}
	runner := `package main
import("crypto/sha256";"encoding/json";"fmt";"os";"m31labs.dev/gosx";"m31labs.dev/gosx/ir";"m31labs.dev/gosx/island/program";"m31labs.dev/gosx/server")
func main(){p,e:=gosx.Compile([]byte(` + strconv.Quote(source) + `));if e!=nil{panic(e)};pages:=map[string]string{};for _,format:=range []string{"json","bin"}{rt:=server.NewPageRuntime();for i,c:=range p.Components{if !c.IsIsland{continue};ip,e:=ir.LowerIsland(p,i);if e!=nil{panic(e)};var b []byte;if format=="json"{b,e=program.EncodeJSON(ip)}else{b,e=program.EncodeBinary(ip)};if e!=nil{panic(e)};sum:=sha256.Sum256(b);rt.SetProgramAsset(c.Name,"/program/"+c.Name+"?format="+format,format,fmt.Sprintf("%x",sum[:8]))};n,e:=Page(rt);if e!=nil{panic(e)};if rt.Summary().Islands!=2{panic("expected two generated owners")};pages[format]="<!doctype html><html><head>"+gosx.RenderHTML(rt.Head())+"</head><body>"+gosx.RenderHTML(n)+"</body></html>"};json.NewEncoder(os.Stdout).Encode(pages)}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(runner), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", ".")
	cmd.Dir = dir
	actual, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated caller: %v\n%s", err, actual)
	}
	var pages map[string]string
	if err := json.Unmarshal(actual, &pages); err != nil {
		t.Fatalf("generated output: %v\n%s", err, actual)
	}
	for format, html := range pages {
		start := strings.Index(html, `id="gosx-manifest"`)
		if start < 0 {
			t.Fatal("missing generated manifest")
		}
		payload := html[start:]
		payload = payload[strings.Index(payload, ">")+1:]
		payload = payload[:strings.Index(payload, "</script>")]
		var m hydrate.Manifest
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			t.Fatal(err)
		}
		variant, path := "full", "/gosx/runtime.wasm?v="+hash
		if tiny {
			variant, path = "core", "/gosx/assets/runtime/runtime-core.wasm"
		}
		if m.Runtime.Hash != hash || m.Runtime.Variant != variant || m.Runtime.Path != path {
			t.Fatalf("generated %s runtime identity: %+v, want %s %s %s", format, m.Runtime, path, hash, variant)
		}
	}
	return pages
}

package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestParseExportArgs(t *testing.T) {
	no, yes := false, true
	cases := []struct {
		name    string
		args    []string
		dir     string
		want    ExportOptions
		wantErr string
	}{
		{name: "defaults", dir: "."},
		{name: "dir only", args: []string{"app"}, dir: "app"},
		{name: "zip auto", args: []string{"--zip"}, dir: ".", want: ExportOptions{Zip: "auto"}},
		{name: "zip path", args: []string{"--zip=g.zip", "app"}, dir: "app", want: ExportOptions{Zip: "g.zip"}},
		{name: "out", args: []string{"--out=site", "app"}, dir: "app", want: ExportOptions{Out: "site"}},
		{name: "out without value", args: []string{"--out"}, wantErr: "--out needs a folder"},
		{name: "out empty value", args: []string{"--out="}, wantErr: "--out needs a folder"},
		{name: "precompressed false", args: []string{"--precompressed=false"}, dir: ".", want: ExportOptions{Precompressed: &no}},
		{name: "precompressed 0", args: []string{"--precompressed=0"}, dir: ".", want: ExportOptions{Precompressed: &no}},
		{name: "precompressed bare", args: []string{"--precompressed"}, dir: ".", want: ExportOptions{Precompressed: &yes}},
		{name: "precompressed junk", args: []string{"--precompressed=maybe"}, wantErr: "true or false"},
		{name: "single file auto", args: []string{"--single-file"}, dir: ".", want: ExportOptions{SingleFile: "auto"}},
		{name: "single file path", args: []string{"--single-file=g.html"}, dir: ".", want: ExportOptions{SingleFile: "g.html"}},
		{name: "single file empty path", args: []string{"--single-file="}, wantErr: "needs a path"},
		{name: "all", args: []string{"--zip", "--out=o", "--precompressed=false", "--single-file=x.html", "d"}, dir: "d",
			want: ExportOptions{Zip: "auto", Out: "o", Precompressed: &no, SingleFile: "x.html"}},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: "unknown flag --nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, opts, err := parseExportArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if dir != tc.dir || !reflect.DeepEqual(opts, tc.want) {
				t.Fatalf("got dir=%q opts=%+v, want dir=%q opts=%+v", dir, opts, tc.dir, tc.want)
			}
		})
	}
}

func TestExportOptionsKeepPrecompressedDefaultsToTrue(t *testing.T) {
	no := false
	if !(ExportOptions{}).keepPrecompressed() {
		t.Fatal("zero options must keep the .gz and .br copies")
	}
	if (ExportOptions{Precompressed: &no}).keepPrecompressed() {
		t.Fatal("Precompressed=false must drop them")
	}
}

func TestCheckExportOutDir(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	mustWriteFile(t, filepath.Join(project, "dist", "static", "index.html"), "x")
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(root, "previous")
	mustWriteFile(t, filepath.Join(previous, "index.html"), "old")
	foreign := filepath.Join(root, "foreign")
	mustWriteFile(t, filepath.Join(foreign, "notes.txt"), "keep me")
	source := filepath.Join(root, "source")
	mustWriteFile(t, filepath.Join(source, "index.html"), "x")
	mustWriteFile(t, filepath.Join(source, "go.mod"), "module x")
	file := filepath.Join(root, "file")
	mustWriteFile(t, file, "x")

	cases := []struct {
		name, out, wantErr string
	}{
		{"missing", filepath.Join(root, "new", "deep"), ""},
		{"empty", empty, ""},
		{"previous export", previous, ""},
		{"foreign folder", foreign, "no index.html"},
		{"source folder", source, "go.mod"},
		{"a file", file, "is a file"},
		{"project", project, "project folder"},
		{"parent of project", root, "project folder"},
		{"dist/static", filepath.Join(project, "dist", "static"), "dist/static"},
		{"inside dist/static", filepath.Join(project, "dist", "static", "sub"), "dist/static"},
		{"dist sibling", filepath.Join(project, "dist", "game"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkExportOutDir(tc.out, project)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestCopyStaticBundleSkipsPrecompressedAndReplacesOldExport(t *testing.T) {
	src := filepath.Join(t.TempDir(), "static")
	mustWriteFile(t, filepath.Join(src, "index.html"), "page")
	mustWriteFile(t, filepath.Join(src, "index.html.gz"), "gz")
	mustWriteFile(t, filepath.Join(src, "gosx", "app.js"), "js")
	mustWriteFile(t, filepath.Join(src, "gosx", "app.js.br"), "br")
	mustWriteFile(t, filepath.Join(src, "data", "real.gz"), "an app's own archive")

	dst := filepath.Join(t.TempDir(), "out")
	mustWriteFile(t, filepath.Join(dst, "index.html"), "old page")
	mustWriteFile(t, filepath.Join(dst, "stale.abc123.js"), "stale")

	n, err := copyStaticBundle(src, dst, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("copied %d files, want 3", n)
	}
	for rel, want := range map[string]bool{
		"index.html": true, "gosx/app.js": true, "data/real.gz": true,
		"index.html.gz": false, "gosx/app.js.br": false, "stale.abc123.js": false,
	} {
		_, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel)))
		if (err == nil) != want {
			t.Errorf("%s present = %v, want %v", rel, err == nil, want)
		}
	}

	// With the default, the copies come along.
	dst2 := filepath.Join(t.TempDir(), "out2")
	if n, err = copyStaticBundle(src, dst2, true); err != nil || n != 5 {
		t.Fatalf("copy with precompressed: n=%d err=%v, want 5 files", n, err)
	}
}

func TestRemovePrecompressedKeepsFilesWithoutAnOriginal(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.js"), "js")
	mustWriteFile(t, filepath.Join(dir, "a.js.gz"), "gz")
	mustWriteFile(t, filepath.Join(dir, "a.js.br"), "br")
	mustWriteFile(t, filepath.Join(dir, "lone.gz"), "kept")
	if err := removePrecompressed(dir); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]bool{"a.js": true, "a.js.gz": false, "a.js.br": false, "lone.gz": true} {
		if _, err := os.Stat(filepath.Join(dir, rel)); (err == nil) != want {
			t.Errorf("%s present = %v, want %v", rel, err == nil, want)
		}
	}
}

// singleFileFixture writes a small static bundle and returns its files.
func singleFileFixture(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	var pic bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.NRGBA{R: 200, A: 255})
	if err := png.Encode(&pic, img); err != nil {
		t.Fatal(err)
	}
	// (module (func (export "add") (param i32 i32) (result i32) local.get 0 local.get 1 i32.add))
	addWasm := []byte{0, 'a', 's', 'm', 1, 0, 0, 0, 1, 7, 1, 0x60, 2, 0x7f, 0x7f, 1, 0x7f, 3, 2, 1, 0,
		7, 7, 1, 3, 'a', 'd', 'd', 0, 0, 0x0a, 9, 1, 7, 0, 0x20, 0, 0x20, 1, 0x6a, 0x0b}
	files := map[string][]byte{
		"index.html": []byte(`<!DOCTYPE html><html lang="fr"><head><meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover"/>
<title>Fixture &amp; Page</title><meta name="theme-color" content="#123456"/>
<link rel="stylesheet" href="css/site.css"/>
<script id="manifest" type="application/json">{"data":"data/info.json","wasm":"wasm/add.wasm","model":"models/box.gltf"}</script>
</head><body><img id="pic" src="img/pic.png" alt=""/><div id="bg"></div><pre id="out" data-done="0">pending</pre>
<script src="js/app.js"></script></body></html>`),
		"404.html":        []byte(`<!DOCTYPE html><title>Not found</title>`),
		"css/site.css":    []byte("@import \"extra.css\";\n#bg{width:20px;height:20px;background:url(../img/bg.png) no-repeat}\n"),
		"css/extra.css":   []byte("#out{border-top:7px solid #333}\n"),
		"img/pic.png":     pic.Bytes(),
		"img/bg.png":      pic.Bytes(),
		"data/info.json":  []byte(`{"hello":"from a bundled file"}`),
		"wasm/add.wasm":   addWasm,
		"models/box.gltf": []byte(`{"asset":{"version":"2.0"},"buffers":[{"uri":"box.bin","byteLength":4}]}`),
		"models/box.bin":  {1, 2, 3, 4},
		"js/mod.js":       []byte("export const answer = 42;\n"),
		"js/nested.js":    []byte("import {answer} from \"./mod.js\";\nexport const nested = answer;\n"),
		"js/worker.js":    []byte("postMessage('worker-ok');\n"),
		"js/worker-imports.js": []byte("try { importScripts('worker-lib.js'); postMessage('imported'); }\n" +
			"catch (e) { postMessage('importScripts failed'); }\n"),
		"js/worker-lib.js": []byte("self.libLoaded = true;\n"),
		"js/worklet.js":    []byte("class Quiet extends AudioWorkletProcessor { process() { return true; } }\nregisterProcessor('quiet', Quiet);\n"),
		"js/app.js":        []byte(fixtureAppJS),
		// Everything below must stay out of the packed file.
		"index.html.gz":    []byte("gz"),
		"index.html.br":    []byte("br"),
		"js/app.js.map":    []byte("{}"),
		"about/index.html": []byte("<!DOCTYPE html><title>About</title>"),
	}
	for rel, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

var packagePattern = regexp.MustCompile(`<script id="package" type="application/octet-stream">([^<]*)</script>`)

func decodeSingleFile(t *testing.T, packed []byte) (shell string, payload singleFilePayload) {
	t.Helper()
	m := packagePattern.FindSubmatch(packed)
	if m == nil {
		t.Fatal("no package script in the packed file")
	}
	gz, err := base64.StdEncoding.DecodeString(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	if !zr.ModTime.IsZero() || zr.Name != "" {
		t.Errorf("gzip header carries metadata: mtime=%v name=%q", zr.ModTime, zr.Name)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return string(packagePattern.ReplaceAll(packed, nil)), payload
}

func TestWriteSingleFileRoundTripsAndIsDeterministic(t *testing.T) {
	static := filepath.Join(t.TempDir(), "static")
	files := singleFileFixture(t, static)
	outDir := t.TempDir()

	res, err := writeSingleFile(static, filepath.Join(outDir, "a", "game.html"))
	if err != nil {
		t.Fatal(err)
	}
	packed, err := os.ReadFile(filepath.Join(outDir, "a", "game.html"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Bytes != int64(len(packed)) {
		t.Errorf("reported %d bytes, file has %d", res.Bytes, len(packed))
	}
	if !reflect.DeepEqual(res.OtherPages, []string{"about/index.html"}) {
		t.Errorf("OtherPages = %v, want [about/index.html]", res.OtherPages)
	}

	shell, payload := decodeSingleFile(t, packed)
	if payload.Page != string(files["index.html"]) {
		t.Error("page does not round-trip")
	}
	wantAssets := 0
	for rel, data := range files {
		skip := rel == "index.html" || rel == "404.html" || rel == "about/index.html" ||
			strings.HasSuffix(rel, ".gz") || strings.HasSuffix(rel, ".br") || strings.HasSuffix(rel, ".map")
		got, ok := payload.Assets[rel]
		if skip {
			if ok {
				t.Errorf("%s must not be packed", rel)
			}
			continue
		}
		wantAssets++
		if !ok {
			t.Errorf("%s missing from the payload", rel)
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(got[1])
		if err != nil || !bytes.Equal(decoded, data) {
			t.Errorf("%s does not round-trip (err=%v)", rel, err)
		}
	}
	if len(payload.Assets) != wantAssets || res.Files != wantAssets {
		t.Errorf("assets in payload = %d, reported = %d, want %d", len(payload.Assets), res.Files, wantAssets)
	}
	for rel, want := range map[string]string{
		"wasm/add.wasm": "application/wasm", "js/app.js": "text/javascript", "css/site.css": "text/css",
		"models/box.gltf": "model/gltf+json", "img/pic.png": "image/png", "models/box.bin": "application/octet-stream",
	} {
		if got := payload.Assets[rel][0]; got != want {
			t.Errorf("%s type = %q, want %q", rel, got, want)
		}
	}

	for _, want := range []string{
		`<html lang="fr">`,
		`<title>Fixture &amp; Page</title>`,
		`<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">`,
		`<meta name="theme-color" content="#123456">`,
		`background:#123456`,
		`Opening Fixture &amp; Page…`,
		`DecompressionStream`,
	} {
		if !strings.Contains(shell, want) {
			t.Errorf("shell lacks %q", want)
		}
	}
	for _, banned := range []string{"__", "http://", "https://cdn", "Solmara"} {
		// "https://bundle.invalid/" is a parsing base, not a request.
		if strings.Contains(strings.ReplaceAll(shell, "https://bundle.invalid/", ""), banned) {
			t.Errorf("shell contains %q", banned)
		}
	}

	// Same input bytes, same file.
	if _, err := writeSingleFile(static, filepath.Join(outDir, "b.html")); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(filepath.Join(outDir, "b.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(packed, again) {
		t.Error("two runs on the same folder wrote different bytes")
	}
}

func TestRenderSingleFileShellDefaultsWithoutMetadata(t *testing.T) {
	shell := renderSingleFileShell("<p>bare</p>", "AAAA")
	for _, want := range []string{`<html lang="en">`, `<title></title>`, `content="width=device-width, initial-scale=1"`, `>Opening…</p>`, `background:#111111`} {
		if !strings.Contains(shell, want) {
			t.Errorf("shell lacks %q", want)
		}
	}
	if strings.Contains(shell, "theme-color") {
		t.Error("shell invents a theme-color")
	}
	// A theme colour that is not a plain hex value must not reach the CSS.
	shell = renderSingleFileShell(`<meta name="theme-color" content="red;}body{display:none">`, "AAAA")
	if strings.Contains(shell, "display:none") {
		t.Error("untrusted theme-color reached the stylesheet")
	}
	// Light backgrounds get dark text.
	shell = renderSingleFileShell(`<meta name="theme-color" content="#ffffff">`, "AAAA")
	if !strings.Contains(shell, "color:#111111") || !strings.Contains(shell, "background:#ffffff") {
		t.Error("light theme-color should give dark text")
	}
}

func TestWriteSingleFileNeedsIndexHTML(t *testing.T) {
	if _, err := writeSingleFile(t.TempDir(), filepath.Join(t.TempDir(), "x.html")); err == nil {
		t.Fatal("expected an error for a folder with no index.html")
	}
}

const fixtureAppJS = `(async () => {
  const out = document.getElementById("out");
  const r = {};
  const attempt = async (name, fn) => {
    try { r[name] = await fn(); } catch (e) { r[name] = "error: " + (e && e.message || e); }
  };
  const manifest = JSON.parse(document.getElementById("manifest").textContent);
  r.manifestRewritten = manifest.data.startsWith("blob:") && manifest.wasm.startsWith("blob:");
  r.modelKept = manifest.model;
  await attempt("info", async () => (await (await fetch("data/info.json")).json()).hello);
  await attempt("sum", async () => {
    const {instance} = await WebAssembly.instantiateStreaming(fetch("wasm/add.wasm"));
    return instance.exports.add(2, 40);
  });
  await attempt("sumFromManifest", async () => {
    const {instance} = await WebAssembly.instantiateStreaming(fetch(manifest.wasm));
    return instance.exports.add(20, 22);
  });
  await attempt("model", async () => (await fetch(manifest.model).then(x => x.json())).buffers[0].uri);
  await attempt("modelBuffer", async () => (await (await fetch(new URL("box.bin", new URL(manifest.model, document.baseURI)))).arrayBuffer()).byteLength);
  await attempt("dynamicImport", async () => (await import("./js/mod.js")).answer);
  await attempt("nestedImport", async () => (await import("./js/nested.js")).nested);
  await attempt("worker", () => new Promise((resolve, reject) => {
    const w = new Worker("js/worker.js");
    w.onmessage = e => resolve(e.data);
    w.onerror = e => reject(new Error(e.message || "worker error"));
  }));
  await attempt("workerImportScripts", () => new Promise((resolve, reject) => {
    const w = new Worker("js/worker-imports.js");
    w.onmessage = e => resolve(e.data);
    w.onerror = e => reject(new Error(e.message || "worker error"));
  }));
  await attempt("worklet", async () => {
    const ctx = new AudioContext();
    await ctx.audioWorklet.addModule("js/worklet.js");
    await ctx.close();
    return "ok";
  });
  await attempt("passthrough", async () => "ok:" + await (await fetch("not-bundled.txt")).text());
  out.textContent = JSON.stringify(r);
  out.dataset.done = "1";
})();
`

func TestRunExportWritesOutFolderWithoutPrecompressedCopiesAndSingleFile(t *testing.T) {
	useFakePrebuiltRuntime(t)
	dir := filepath.Join(t.TempDir(), "export-app")
	if err := RunInit(dir, "example.com/export-app", ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, dir)
	mustWriteFile(t, filepath.Join(dir, "app", "route.config.json"), `{"prerender":true}`)
	tidyModule(t, dir)

	out := filepath.Join(t.TempDir(), "site")
	single := filepath.Join(t.TempDir(), "one", "app.html")
	no := false
	if err := RunExportWithOptions(dir, ExportOptions{Out: out, Precompressed: &no, SingleFile: single}); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{"index.html", "stack/index.html", "404.html", "styles.css"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
			t.Errorf("--out folder lacks %s: %v", rel, err)
		}
	}
	var sidecars []string
	_ = filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, ".gz") || strings.HasSuffix(p, ".br")) {
			sidecars = append(sidecars, p)
		}
		return nil
	})
	if len(sidecars) != 0 {
		t.Errorf("--precompressed=false left %v in the --out folder", sidecars)
	}
	// The build's own dist/static keeps its copies when --out is given.
	if _, err := os.Stat(filepath.Join(dir, "dist", "static", "index.html")); err != nil {
		t.Errorf("dist/static was not built: %v", err)
	}

	packed, err := os.ReadFile(single)
	if err != nil {
		t.Fatal(err)
	}
	_, payload := decodeSingleFile(t, packed)
	if !strings.Contains(payload.Page, "<title>My GoSX App</title>") {
		t.Error("single file does not carry the app's index.html")
	}
	if _, ok := payload.Assets["styles.css"]; !ok {
		t.Error("single file lacks styles.css")
	}
	if !strings.Contains(string(packed), "<title>My GoSX App</title>") {
		t.Error("shell does not take the app's title")
	}

	// A second export into the same folder replaces it, and a foreign folder is refused.
	if err := RunExportWithOptions(dir, ExportOptions{Out: out, Precompressed: &no}); err != nil {
		t.Fatalf("re-export into a previous export: %v", err)
	}
	foreign := filepath.Join(t.TempDir(), "docs")
	mustWriteFile(t, filepath.Join(foreign, "notes.txt"), "keep")
	if err := RunExportWithOptions(dir, ExportOptions{Out: foreign}); err == nil || !strings.Contains(err.Error(), "no index.html") {
		t.Fatalf("export into a foreign folder: err = %v", err)
	}
	if got := readFile(t, filepath.Join(foreign, "notes.txt")); got != "keep" {
		t.Error("a refused export changed the folder")
	}
}

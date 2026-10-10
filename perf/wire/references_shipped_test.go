package wire

import (
	"bytes"
	"compress/gzip"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/evanw/esbuild/pkg/api"
	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
)

func TestReferencesGlobalNonLoaderMembers(t *testing.T) {
	methods := []string{
		`addEventListener("popstate", function() {})`,
		`removeEventListener("pageswap", function() {})`,
		`dispatchEvent(new Event("pagereveal"))`,
		`setTimeout(function() {}, 10, "focus")`,
		`setInterval(function() {}, 10, "load")`,
		`setTimeout("focus", 10)`,
		`setInterval("load", 10)`,
		`requestAnimationFrame(function() {})`,
		`matchMedia("(prefers-reduced-motion: reduce)")`,
		`unknownAppCallback("/not-a-fetch.json", "css-class", "data-attribute")`,
	}
	for _, alias := range strings.Fields("globalThis window self top parent frames opener defaultView document.defaultView window.parent") {
		for _, method := range methods {
			for _, context := range loaderExecutableContexts {
				t.Run(alias+"/"+method+"/"+context, func(t *testing.T) {
					code := `fetch("/before.json");` + alias + "." + method + `;import("/after.js");`
					set, err := scanLoaderInventoryContext(t, code, context)
					want := []Reference{{"/after.js", KindScript, false}, {"/before.json", KindOther, false}}
					complete := !strings.Contains(context, "javascript-url") && !strings.HasPrefix(method, `setTimeout("`) && !strings.HasPrefix(method, `setInterval("`)
					if err != nil || set.Complete != complete || !reflect.DeepEqual(set.Resources, want) {
						t.Fatalf("non-loader arguments became references or lost policy: %+v err=%v", set, err)
					}
				})
			}
		}
	}
	// A static call does not relax the existing rule for global objects as values.
	for _, code := range []string{`const root = window; root.addEventListener("load", callback);`, `consume(globalThis);`, `const copy = {...self};`} {
		set, err := ScanReferences([]byte(code), KindScript)
		if err != nil || set.Complete || len(set.Resources) != 0 {
			t.Fatalf("global value escaped: %+v err=%v", set, err)
		}
	}
}

// Discover generated browser outputs from their build declarations and browser
// sources from Go embeds. A new bundle/embed is scanned without updating a glob
// or a hand-maintained file list. Source maps, tests and tool scripts are absent
// from these production declarations.
func shippedJavaScriptFiles(t *testing.T) []string {
	t.Helper()
	root := "../.."
	paths := map[string]bool{}
	add := func(path string) { paths[filepath.ToSlash(path)] = true }
	built, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "cmd/buildbootstrap/main.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(built, func(n ast.Node) bool {
		pair, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok || key.Name != "name" {
			return true
		}
		value, ok := pair.Value.(*ast.BasicLit)
		if !ok || value.Kind != token.STRING {
			return true
		}
		name, err := strconv.Unquote(value.Value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".js") {
			add(filepath.Clean(filepath.Join("client/js", name)))
		}
		return true
	})
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "testdata", "examples", "build", "dist", "vendor":
				return filepath.SkipDir
			}
			if rel == "cmd" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, group := range parsed.Comments {
			for _, comment := range group.List {
				if !strings.HasPrefix(comment.Text, "//go:embed ") {
					continue
				}
				for _, pattern := range strings.Fields(strings.TrimPrefix(comment.Text, "//go:embed ")) {
					matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), strings.TrimPrefix(pattern, "all:")))
					if err != nil {
						return err
					}
					for _, match := range matches {
						err = filepath.WalkDir(match, func(candidate string, child fs.DirEntry, err error) error {
							if err != nil {
								return err
							}
							if !child.IsDir() && (strings.HasSuffix(candidate, ".js") || strings.HasSuffix(candidate, ".ts")) {
								rel, err := filepath.Rel(root, candidate)
								if err != nil {
									return err
								}
								add(rel)
							}
							return nil
						})
						if err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The HLS distribution is staged separately from the generated outputs.
	add("client/js/vendor/hls.min.js")
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

// Each permitted literal must be an actual fetching expression, rather than an
// event/class/attribute string. Dynamic framework targets stay unresolved; they
// do not justify treating any ordinary call's strings as URLs.
var shippedReferenceAllowlist = map[string][]Reference{
	"server/emoji_complete.ts": {{URL: "/_gosx/emoji-codes.json", Kind: KindOther}}, // fetch at server/emoji_complete.ts:17
}

func TestReferencesShippedJavaScript(t *testing.T) {
	complete, scanned := 0, 0
	files := shippedJavaScriptFiles(t)
	files = append(files, "toolchain/go/wasm_exec.js", "toolchain/tinygo/wasm_exec.js")
	sort.Strings(files)
	for _, path := range files {
		t.Run(path, func(t *testing.T) {
			body := readShippedJavaScript(t, path)
			scanned++
			set, err := ScanReferences(body, KindScript)
			witness := "none"
			if !set.Complete {
				witness = shippedIncompleteWitness(t, body, err != nil)
			}
			if err != nil && (path != "client/js/bootstrap.js" || err.Error() != "invalid-input at references/body" || !strings.HasPrefix(witness, "AST node limit (250000)")) {
				t.Fatalf("new shipped JavaScript scan failure: %v; %s", err, witness)
			}
			if !set.Complete && witness == "none" {
				t.Fatal("incomplete shipped code has no mandatory policy witness")
			}
			want := shippedReferenceAllowlist[path]
			if want == nil {
				want = []Reference{}
			}
			if !reflect.DeepEqual(set.Resources, want) {
				t.Errorf("framework references must match fetching-line allowlist: got=%+v want=%+v", set.Resources, want)
			}
			if path == "auth/webauthn_runtime.ts" || path == "server/emoji_complete.ts" {
				// These embeds are rendered inline; use the document entry point
				// as well as the fetched-script entry used for bundles/chunks.
				inline, inlineErr := ScanReferences(append(append([]byte("<script>"), body...), []byte("</script>")...), KindDocument)
				if inlineErr != nil || inline.Complete != set.Complete || !reflect.DeepEqual(inline.Resources, set.Resources) {
					t.Fatalf("inline embed differs: %+v err=%v", inline, inlineErr)
				}
			}
			if set.Complete {
				complete++
			}
			t.Logf("SHIPPED | %s | %t | %d | %v | %s", path, set.Complete, len(set.Resources), err, witness)
		})
	}
	t.Logf("shipped corpus: %d/%d complete; %d incomplete", complete, scanned, scanned-complete)
	for path := range shippedReferenceAllowlist {
		if !containsShippedPath(files, path) {
			t.Errorf("allowlist source no longer shipped: %s", path)
		}
	}
}

func containsShippedPath(paths []string, path string) bool {
	index := sort.SearchStrings(paths, path)
	return index < len(paths) && paths[index] == path
}

func TestReferencesLiteralCallsHaveFetchingInventoryRows(t *testing.T) {
	got := map[string]bool{}
	for _, row := range loaderInventory {
		for _, name := range strings.Fields(row.LiteralCalls) {
			if !row.Fetches || row.Classification != loaderLiteral {
				t.Errorf("literal loader %s lacks a fetching model: %s", name, row.ID)
			}
			got[name] = true
		}
	}
	want := map[string]bool{"fetch": true, "import": true, "Worker": true, "SharedWorker": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("literal fetching call models=%v want=%v", got, want)
	}
}

// Find a concrete syntax witness for the mandatory completeness rules. This
// diagnostic runs the same node policy, without weakening it for framework
// code. It is not a substitute for the public scan/reference assertions.
func shippedIncompleteWitness(t *testing.T, body []byte, verifyLimit bool) string {
	t.Helper()
	lang := grammars.JavascriptLanguage()
	tree, err := ts.NewParser(lang).Parse(body)
	if err != nil || tree == nil {
		t.Fatalf("witness parse: %v", err)
	}
	if tree.RootNode().HasErrorOrMissing() {
		tree.Release()
		formatted := api.Transform(string(body), api.TransformOptions{Loader: api.LoaderJS, Target: api.ESNext, Charset: api.CharsetUTF8, TreeShaking: api.TreeShakingFalse, LogLevel: api.LogLevelSilent})
		if len(formatted.Errors) != 0 {
			t.Fatalf("witness format: %v", formatted.Errors)
		}
		body = formatted.Code
		tree, err = ts.NewParser(lang).Parse(body)
		if err != nil || tree == nil {
			t.Fatalf("witness formatted parse: %v", err)
		}
	}
	defer tree.Release()
	if tree.RootNode().HasErrorOrMissing() {
		return "unresolved parser syntax"
	}
	stack := []*ts.Node{tree.RootNode()}
	count := 0
	witness := ""
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		if count > 250000 {
			return "AST node limit (250000); " + witness
		}
		if witness == "" {
			state := referenceScanner{ReferenceSet: ReferenceSet{Complete: true}}
			moduleReference(n, lang, body, &state)
			if !state.Complete {
				text := n.Text(body)
				if len(text) > 100 {
					text = text[:100] + "..."
				}
				text = strings.ReplaceAll(text, "\n", " ")
				family := "unmodelled loader form"
				switch n.Type(lang) {
				case "subscript_expression", "computed_property_name":
					family = "computed access"
				case "identifier", "property_identifier", "shorthand_property_identifier", "shorthand_property_identifier_pattern":
					name := n.Text(body)
					switch {
					case javaScriptGlobalObjectAlias(name):
						family = "global object used as a value"
					case unmodeledJavaScriptEnumeration(name):
						family = "enumeration/reflection"
					case unmodeledJavaScriptLoader(name):
						family = "denied capability token"
					case name == "setTimeout" || name == "setInterval":
						family = "opaque timer callback"
					}
				}
				witness = family + ": " + text
				if !verifyLimit {
					return witness
				}
			}
		}
		for i := n.NamedChildCount() - 1; i >= 0; i-- {
			stack = append(stack, n.NamedChild(i))
		}
	}
	if witness == "" {
		return "none"
	}
	return witness
}

func readShippedJavaScript(t *testing.T, path string) []byte {
	t.Helper()
	file := filepath.Join("../..", path)
	switch path {
	case "toolchain/go/wasm_exec.js":
		file = filepath.Join(runtime.GOROOT(), "lib/wasm/wasm_exec.js")
		if _, err := os.Stat(file); os.IsNotExist(err) {
			file = filepath.Join(runtime.GOROOT(), "misc/wasm/wasm_exec.js")
		}
	case "toolchain/tinygo/wasm_exec.js":
		tool, err := exec.LookPath("tinygo")
		if err != nil {
			t.Skip("TinyGo shim not installed; scanned by runtime-build lanes with TinyGo")
		}
		root, err := exec.Command(tool, "env", "TINYGOROOT").Output()
		if err != nil {
			t.Fatal(err)
		}
		file = filepath.Join(strings.TrimSpace(string(root)), "targets/wasm_exec.js")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if path == "client/runtime/host/navigation-runtime.min.js" {
		// Exercise the bytes embedded for serving, including decoded gzip/Brotli
		// representations; compression does not create an untested runtime variant.
		if string(body) != runtimehost.NavigationRuntime {
			t.Fatal("served navigation embed differs from source")
		}
		gz, err := gzip.NewReader(bytes.NewReader(runtimehost.NavigationRuntimeGzip))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := io.ReadAll(gz)
		gz.Close()
		if err != nil || !bytes.Equal(body, decoded) {
			t.Fatal("gzip navigation bytes differ", err)
		}
		decoded, err = io.ReadAll(brotli.NewReader(bytes.NewReader(runtimehost.NavigationRuntimeBrotli)))
		if err != nil || !bytes.Equal(body, decoded) {
			t.Fatal("Brotli navigation bytes differ", err)
		}
	}
	return body
}

func TestReferencesShippedAllowlistCitesFetch(t *testing.T) {
	body := readShippedJavaScript(t, "server/emoji_complete.ts")
	lines := strings.Split(string(body), "\n")
	if len(lines) < 17 || !strings.Contains(lines[16], `fetch("/_gosx/emoji-codes.json")`) {
		t.Fatal("emoji allowlist fetching line moved or changed; audit the literal target")
	}
}

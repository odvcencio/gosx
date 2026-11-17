package bundle

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Keep this list in sync with client/js/scene3d-wgsl-reserved-words.test.js.
const wgslReserved = `NULL Self abstract active alignas alignof as asm asm_fragment async attribute auto await become binding_array cast catch class co_await co_return co_yield coherent column_major common compile compile_fragment concept const_cast consteval constexpr constinit crate debugger decltype delete demote demote_to_helper do dynamic_cast enum explicit export extends extern external fallthrough filter final finally friend from fxgroup get goto groupshared highp impl implements import inline instanceof interface layout lowp macro macro_rules match mediump meta mod module move mut mutable namespace new nil noexcept noinline nointerpolation non_coherent noncoherent noperspective null nullptr of operator package packoffset partition pass patch pixelfragment precise precision premerge priv protected pub public readonly ref regardless register reinterpret_cast require resource restrict self set shared sizeof smooth snorm static static_assert static_cast std subroutine super target template this thread_local throw trait try type typedef typeid typename typeof union unless unorm unsafe unsized use using varying virtual volatile wgsl where with writeonly yield`
const wgslKeywords = `alias break case const const_assert continue continuing default diagnostic discard else enable false fn for if let loop override requires return struct switch true var while`

var wgslToken = regexp.MustCompile(`%\[[0-9]+\][a-zA-Z]|%[a-zA-Z]|\{\{[^}]*\}\}|\$\{[^}]*\}|[A-Za-z_][A-Za-z_0-9]*|[^\s]`)
var wgslComments = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`)
var wgslShape = regexp.MustCompile(`\b(?:let|const|override|var)\s*(?:<[^>]*>\s*)?[A-Za-z_]\w*\s*[:=]|\bfn\s+(?:[A-Za-z_]\w*|%[a-zA-Z]|%\[[0-9]+\][a-zA-Z]|\{\{[^}]*\}\}|\$\{[^}]*\})\s*\(|\bstruct\s+(?:[A-Za-z_]\w*|%[a-zA-Z]|%\[[0-9]+\][a-zA-Z]|\{\{[^}]*\}\}|\$\{[^}]*\})\s*\{`)
var wgslOnly = regexp.MustCompile(`\b(?:vec[234][fiuh]|mat[234]x[234][fh]|f32|u32|i32|sampler(?:_comparison)?)\b|\b(?:vec[234]|mat[234]x[234]|array|texture_\w+)\s*<|@\w+\s*\(`)
var rustFunction = regexp.MustCompile(`\bpub\s+fn\b`)
var otherLanguage = regexp.MustCompile("(?m)^\\s*(?:package\\s+\\w+|material\\s+\\w+|(?:export\\s+)?function\\s+\\w+\\s*\\(|<(!doctype|html|script)\\b|\\(function\\s*\\(|const\\s+\\w+\\s*=\\s*(?:`|\\[))")
var identifier = regexp.MustCompile(`^[A-Za-z_]\w*$`)

func wgslSelected(source string) bool {
	return wgslShape.MatchString(source) && wgslOnly.MatchString(source) && !rustFunction.MatchString(source) && !otherLanguage.MatchString(source) && !strings.Contains(source, `\`)
}

type wgslDeclaration struct {
	name   string
	offset int
}

func wgslDeclarations(source string) []wgslDeclaration {
	cleaned := wgslComments.ReplaceAllStringFunc(source, func(s string) string {
		b := []byte(s)
		for i := range b {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
		return string(b)
	})
	indexes := wgslToken.FindAllStringIndex(cleaned, -1)
	tokens := make([]string, len(indexes))
	for i, p := range indexes {
		tokens[i] = cleaned[p[0]:p[1]]
	}
	var names []wgslDeclaration
	add := func(i int) {
		if i < len(tokens) && identifier.MatchString(tokens[i]) {
			names = append(names, wgslDeclaration{tokens[i], indexes[i][0]})
		}
	}
	for i := 0; i < len(tokens); i++ {
		kind := tokens[i]
		switch kind {
		case "let", "const", "override", "var", "fn", "struct", "alias":
			j := i + 1
			if kind == "var" && j < len(tokens) && tokens[j] == "<" {
				for j < len(tokens) && tokens[j] != ">" {
					j++
				}
				j++
			}
			if j >= len(tokens) {
				continue
			}
			next := j + 1
			open, close := "", ""
			switch kind {
			case "fn":
				open, close = "(", ")"
			case "struct":
				open, close = "{", "}"
			}
			if open != "" {
				if next >= len(tokens) || tokens[next] != open {
					continue
				}
				add(j)
				depth, angle := 1, 0
				for k := next + 1; k < len(tokens) && depth > 0; k++ {
					switch tokens[k] {
					case open:
						depth++
					case close:
						depth--
					case "<":
						angle++
					case ">":
						if angle > 0 {
							angle--
						}
					}
					if depth == 1 && angle == 0 && k+1 < len(tokens) && tokens[k+1] == ":" {
						add(k)
					}
				}
			} else if next < len(tokens) && ((kind == "alias" && tokens[next] == "=") || (kind != "alias" && (tokens[next] == ":" || tokens[next] == "="))) {
				add(j)
			}
		}
	}
	return names
}
func wgslRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if filepath.Dir(dir) == dir {
			t.Fatal("cannot find go.mod")
		}
	}
}
func TestEmbeddedWGSLReservedWords(t *testing.T) {
	forbidden := map[string]bool{}
	for _, w := range strings.Fields(wgslReserved + " " + wgslKeywords) {
		forbidden[w] = true
	}
	root := wgslRoot(t)
	anchors := map[string]bool{"render/bundle/particles.go": false, "render/bundle/lit.go": false, "skinnedSignature": false, "skinnedTransform": false}
	scanned := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "testdata", "node_modules", "tmp", "data", "build", "dist", "vendor":
				return filepath.SkipDir
			}
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || path == filepath.Join(root, "render/bundle/wgsl_reserved_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Logf("skipping unparseable Go file %s: %v", path, err)
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		literalNames := map[*ast.BasicLit]string{}
		ast.Inspect(file, func(n ast.Node) bool {
			if spec, ok := n.(*ast.ValueSpec); ok {
				for i, value := range spec.Values {
					if i < len(spec.Names) {
						if lit, ok := value.(*ast.BasicLit); ok {
							literalNames[lit] = spec.Names[i].Name
						}
					}
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			source, err := strconv.Unquote(lit.Value)
			if err != nil || !wgslSelected(source) {
				return true
			}
			scanned++
			if _, ok := anchors[rel]; ok {
				anchors[rel] = true
			}
			if rel == "render/bundle/lit.go" {
				if _, ok := anchors[literalNames[lit]]; ok {
					anchors[literalNames[lit]] = true
				}
			}
			for _, d := range wgslDeclarations(source) {
				if forbidden[d.name] {
					pos := fset.Position(lit.Pos())
					if strings.HasPrefix(lit.Value, "`") {
						pos.Line += strings.Count(source[:d.offset], "\n")
					}
					t.Errorf("%s:%d: WGSL declares reserved identifier %q", rel, pos.Line, d.name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("selected %d WGSL literals", scanned)
	for name, found := range anchors {
		if !found {
			t.Errorf("guard scanned nothing useful: missing %s", name)
		}
	}
}
func TestWGSLReservedListsMatch(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(wgslRoot(t), "client/js/scene3d-wgsl-reserved-words.test.js"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?s)const RESERVED = new Set\(\[([^]]*)\]\);`).FindSubmatch(data)
	if match == nil {
		t.Fatal("JavaScript reserved list not found")
	}
	js := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([A-Za-z_][A-Za-z_0-9]*)"`).FindAllSubmatch(match[1], -1) {
		js[string(m[1])] = true
	}
	goWords := map[string]bool{}
	for _, w := range strings.Fields(wgslReserved) {
		goWords[w] = true
	}
	var onlyGo, onlyJS []string
	for w := range goWords {
		if !js[w] {
			onlyGo = append(onlyGo, w)
		}
	}
	for w := range js {
		if !goWords[w] {
			onlyJS = append(onlyJS, w)
		}
	}
	sort.Strings(onlyGo)
	sort.Strings(onlyJS)
	if len(onlyGo)+len(onlyJS) > 0 {
		t.Errorf("reserved lists differ: only Go: %v; only JavaScript: %v", onlyGo, onlyJS)
	}
}
func TestWGSLSelector(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(wgslRoot(t), "render/bundle/lit.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	fragments := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		if spec, ok := n.(*ast.ValueSpec); ok {
			for i, value := range spec.Values {
				if i < len(spec.Names) {
					if lit, ok := value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						fragments[spec.Names[i].Name], _ = strconv.Unquote(lit.Value)
					}
				}
			}
		}
		return true
	})
	cases := []struct {
		name, source string
		want         bool
	}{
		{"skinnedSignature", fragments["skinnedSignature"], true},
		{"skinnedTransform", fragments["skinnedTransform"], true},
		{"prose", "shader needs an @fragment fn with a color output", false}, {"rust", "pub fn new(x: u32) -> Self {", false},
		{"sql", "select name from things where id = 1", false}, {"javascript", "const value = 1; function foo() {}", false},
		{"go", "package handlers\ntype vec2 struct { X, Y float64 }\nvar positions = map[string]vec2{}", false},
		{"javascript with shader", "function init() { const shader = 'fn main(x: f32) {'; }", false},
		{"javascript template", "const shader = `fn main(x: f32) {}`;", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wgslSelected(tc.source); got != tc.want {
				t.Errorf("selected = %v, want %v", got, tc.want)
			}
		})
	}
}
func TestWGSLDeclarations(t *testing.T) {
	cases := []struct{ source, want string }{
		{`// let ignored = 0;
 struct S { @align(16) meta: vec4<f32>, good: f32, };
 alias target = f32;
 fn example(@builtin(position) value: vec4<f32>, pass: f32) { var<private> active: f32; let ok = 1; }`, "S,meta,good,target,example,value,pass,active,ok"},
		{"shader needs an @fragment fn with a color output", ""}, {"pub fn new(x: u32) -> Self {", "new,x"},
		{"struct %s { meta : vec4f }", "meta"}, {"fn %s(target : f32)", "target"},
		{"struct %[1]s { meta: vec4f } fn {{name}}(target: f32) fn ${name}(pass: f32) fn %v(active: f32)", "meta,target,pass,active"},
	}
	for _, tc := range cases {
		var names []string
		for _, d := range wgslDeclarations(tc.source) {
			names = append(names, d.name)
		}
		if got := strings.Join(names, ","); got != tc.want {
			t.Errorf("%q: declarations = %q, want %q", tc.source, got, tc.want)
		}
	}
}

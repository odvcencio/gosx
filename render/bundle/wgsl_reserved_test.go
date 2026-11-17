package bundle

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Keep this list in sync with client/js/scene3d-wgsl-reserved-words.test.js.
const wgslReserved = `NULL Self abstract active alignas alignof as asm asm_fragment async attribute auto await become binding_array cast catch class co_await co_return co_yield coherent column_major common compile compile_fragment concept const_cast consteval constexpr constinit crate debugger decltype delete demote demote_to_helper do dynamic_cast enum explicit export extends extern external fallthrough filter final finally friend from fxgroup get goto groupshared highp impl implements import inline instanceof interface layout lowp macro macro_rules match mediump meta mod module move mut mutable namespace new nil noexcept noinline nointerpolation noperspective null nullptr of operator package packoffset partition pass patch pixelfragment precise precision premerge priv protected pub public readonly ref regardless register reinterpret_cast require resource restrict self set shared sizeof smooth snorm static static_assert static_cast std subroutine super target template this thread_local throw trait try type typedef typeid typename typeof union unless unorm unsafe unsized use using varying virtual volatile wgsl where with writeonly yield`
const wgslKeywords = `alias break case const const_assert continue continuing default diagnostic discard else enable false fn for if let loop override requires return struct switch true var while`

var wgslToken = regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*|[^\s]`)
var wgslMarker = regexp.MustCompile(`@(group|binding|vertex|fragment|compute)\b|\bfn\s+\w+\s*\([^\n]*\)\s*(->|\{)|\bstruct\s+\w+\s*\{[^}]*\b(vec[234]|mat[234]|array)\s*<`)
var wgslComments = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`)

// Ignore comments and nested attribute arguments or generic types.
func wgslDeclarations(source string) []string {
	tokens := wgslToken.FindAllString(wgslComments.ReplaceAllString(source, " "), -1)
	var names []string
	for i := 0; i < len(tokens); i++ {
		switch tokens[i] {
		case "let", "const", "override", "var", "fn", "struct", "alias":
			j := i + 1
			if tokens[i] == "var" && j < len(tokens) && tokens[j] == "<" {
				for j < len(tokens) && tokens[j] != ">" {
					j++
				}
				j++
			}
			if j < len(tokens) {
				names = append(names, tokens[j])
			}
			if tokens[i] != "struct" && tokens[i] != "fn" {
				continue
			}
			open, close := "{", "}"
			if tokens[i] == "fn" {
				open, close = "(", ")"
			}
			j++
			if j >= len(tokens) || tokens[j] != open {
				continue
			}
			depth, angle := 1, 0
			for j++; j < len(tokens) && depth > 0; j++ {
				switch tokens[j] {
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
				if depth == 1 && angle == 0 && j+1 < len(tokens) && tokens[j+1] == ":" {
					names = append(names, tokens[j])
				}
			}
		}
	}
	return names
}

func TestEmbeddedWGSLReservedWords(t *testing.T) {
	forbidden := map[string]bool{}
	for _, word := range strings.Fields(wgslReserved + " " + wgslKeywords) {
		forbidden[word] = true
	}
	root := filepath.Join("..", "..")
	scanned := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			source, err := strconv.Unquote(lit.Value)
			if err != nil || !wgslMarker.MatchString(source) {
				return true
			}
			scanned++
			for _, name := range wgslDeclarations(source) {
				if forbidden[name] {
					t.Errorf("%s: WGSL declares reserved identifier %q", fset.Position(lit.Pos()), name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 20 {
		t.Fatalf("found only %d WGSL strings", scanned)
	}
}

func TestWGSLDeclarations(t *testing.T) {
	source := `// let ignored = 0;
 struct S { @align(16) meta: vec4<f32>, good: f32, };
 alias target = f32;
 fn example(@builtin(position) value: vec4<f32>, pass: f32) { var<private> active: f32; let ok = 1; }
 `
	got := strings.Join(wgslDeclarations(source), ",")
	want := "S,meta,good,target,example,value,pass,active,ok"
	if got != want {
		t.Fatalf("declarations = %q, want %q", got, want)
	}
}

package bundle

import (
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/scene3drenderersource"
)

// This file holds the reader that the *_drift_test.go guards in this package
// share. It exists because the browser WebGPU renderer keeps its WGSL as a JS
// array of quoted lines, so a Go test cannot read the shader text directly.
//
// Every function here fails the test instead of skipping it. The one Go-to-JS
// shader guard that already existed (render/boardgpu/board_text_drift_test.go)
// calls t.Skipf when the relative path breaks. A skip turns a broken guard into
// a green run, which is the exact failure mode a drift guard must prevent. Do
// not add a skip to this file.

// jsWebGPURendererFile is the browser WebGPU renderer, relative to this package
// directory. Go runs a test with its package directory as the working
// directory, so this path is stable.
var jsWebGPURendererFile = scene3drenderersource.BackendLabel("webgpu")

// readJSWebGPURenderer returns the whole browser WebGPU renderer source.
func readJSWebGPURenderer(t *testing.T) string {
	t.Helper()
	return scene3drenderersource.ReadBackend(t, "webgpu")
}

// jsWebGLRendererFile is the browser WebGL2 renderer, relative to this package
// directory. Two guards read it: the shadow cull guard, because the WebGL2
// shadow pass decides which face fills the map with a gl.cullFace call rather
// than with shader text, and the tone-map table guard, because WebGL2 carries
// two name-to-number tables where WebGPU carries one.
var jsWebGLRendererFile = scene3drenderersource.BackendLabel("webgl")

// readJSWebGLRenderer returns the whole browser WebGL2 renderer source.
func readJSWebGLRenderer(t *testing.T) string {
	t.Helper()
	return scene3drenderersource.ReadBackend(t, "webgl")
}

// jsShaderSource resolves one shader constant in the browser WebGPU renderer to
// the WGSL text the browser compiles.
//
// The renderer normally declares each shader as `var NAME = [ "line",
// OTHER_NAME, ... ].join("\n")`. Shared snippets may instead use one compact
// quoted string to avoid shipping line-array punctuation. This function
// resolves either form to the exact WGSL text the browser compiles. For arrays
// it walks every element, unquotes string elements, and resolves bare
// identifier elements by recursion.
func jsShaderSource(t *testing.T, file, name string) string {
	t.Helper()
	return jsShaderSourceAt(t, file, name, 0)
}

func jsShaderSourceAt(t *testing.T, file, name string, depth int) string {
	t.Helper()
	if depth > 8 {
		t.Fatalf("shader constant %s in %s nests more than 8 levels; suspect a reference cycle", name, jsWebGPURendererFile)
	}
	if source, ok := jsQuotedShaderSource(t, file, name); ok {
		return source
	}
	body := jsArrayBody(t, file, name)
	parts := make([]string, 0, 256)
	for _, elem := range jsArrayElements(t, name, body) {
		if elem.isRef {
			parts = append(parts, jsShaderSourceAt(t, file, elem.text, depth+1))
			continue
		}
		parts = append(parts, elem.text)
	}
	return strings.Join(parts, "\n")
}

// jsQuotedShaderSource resolves `var NAME = "..."` declarations. It returns
// false when NAME is not a compact quoted shader so the array reader can try
// the normal representation.
func jsQuotedShaderSource(t *testing.T, file, name string) (string, bool) {
	t.Helper()
	for _, keyword := range []string{"var ", "const ", "let "} {
		needle := keyword + name + " = "
		idx := strings.Index(file, needle)
		if idx < 0 {
			continue
		}
		start := idx + len(needle)
		if start >= len(file) || file[start] != '"' {
			return "", false
		}
		for i := start + 1; i < len(file); i++ {
			if file[i] == '\\' {
				i++
				continue
			}
			if file[i] != '"' {
				continue
			}
			value, err := strconv.Unquote(file[start : i+1])
			if err != nil {
				t.Fatalf("decode quoted shader constant %s in %s: %v", name, jsWebGPURendererFile, err)
			}
			return value, true
		}
		t.Fatalf("quoted shader constant %s in %s has no closing quote", name, jsWebGPURendererFile)
	}
	return "", false
}

// jsArrayBody returns the text between the brackets of `var NAME = [ ... ]`.
func jsArrayBody(t *testing.T, file, name string) string {
	t.Helper()
	start := jsArrayOpenBracket(t, file, name)
	depth := 0
	for i := start; i < len(file); i++ {
		switch file[i] {
		case '"', '\'', '`':
			quote := file[i]
			i++
			for i < len(file) && file[i] != quote {
				if file[i] == '\\' {
					i++
				}
				i++
			}
		case '/':
			if i+1 < len(file) && file[i+1] == '/' {
				for i < len(file) && file[i] != '\n' {
					i++
				}
			}
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return file[start+1 : i]
			}
		}
	}
	t.Fatalf("shader constant %s in %s has no closing bracket", name, jsWebGPURendererFile)
	return ""
}

// jsArrayOpenBracket returns the index of the `[` that opens the array assigned
// to name. It matches a declaration at the start of a line so a mention of the
// name inside another expression cannot win.
func jsArrayOpenBracket(t *testing.T, file, name string) int {
	t.Helper()
	for _, keyword := range []string{"var ", "const ", "let "} {
		needle := keyword + name + " = ["
		if idx := strings.Index(file, needle); idx >= 0 {
			return idx + len(needle) - 1
		}
	}
	t.Fatalf("shader constant %s not found in %s as `var %s = [`. Was it renamed, or is it no longer an array of lines? Update this guard together with the rename.", name, jsWebGPURendererFile, name)
	return 0
}

type jsArrayElement struct {
	text  string
	isRef bool
}

// jsArrayElements splits one array body into its elements. It accepts double
// quoted string literals and bare identifier references, and fails on anything
// else so a new element form cannot pass through unnoticed.
func jsArrayElements(t *testing.T, name, body string) []jsArrayElement {
	t.Helper()
	out := make([]jsArrayElement, 0, 256)
	for i := 0; i < len(body); {
		c := body[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			i++
		case c == '/' && i+1 < len(body) && body[i+1] == '/':
			for i < len(body) && body[i] != '\n' {
				i++
			}
		case c == '"':
			end := i + 1
			for end < len(body) && body[end] != '"' {
				if body[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(body) {
				t.Fatalf("shader constant %s in %s has an unterminated string element", name, jsWebGPURendererFile)
			}
			text, err := strconv.Unquote(body[i : end+1])
			if err != nil {
				t.Fatalf("shader constant %s in %s: cannot unquote element %s: %v", name, jsWebGPURendererFile, body[i:end+1], err)
			}
			out = append(out, jsArrayElement{text: text})
			i = end + 1
		case isJSIdentStart(c):
			end := i
			for end < len(body) && isJSIdentPart(body[end]) {
				end++
			}
			out = append(out, jsArrayElement{text: body[i:end], isRef: true})
			i = end
		default:
			t.Fatalf("shader constant %s in %s has an element this guard cannot read, starting at %q. Extend jsArrayElements rather than weakening the guard.", name, jsWebGPURendererFile, jsSnippet(body[i:]))
		}
	}
	return out
}

func isJSIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isJSIdentPart(c byte) bool {
	return isJSIdentStart(c) || (c >= '0' && c <= '9')
}

func jsSnippet(s string) string {
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

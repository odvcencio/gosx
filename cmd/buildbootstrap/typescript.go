package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// sourceLanguage describes the syntax one source file uses. Each source
// transpiles with its own loader (see transpileSource): a typed source in a
// chunk never promotes its JavaScript neighbors to the TypeScript parser.
//
// TSX is not a supported extension yet. The bootstrap runtime configures no
// JSX factory, so an esbuild TSX transform would emit React.createElement
// calls into a bundle that ships no React. TSX support returns once a JSX
// factory is configured for the bootstrap runtime.
type sourceLanguage uint8

const (
	sourceJavaScript sourceLanguage = iota
	sourceTypeScript
)

func languageForSource(src source) (sourceLanguage, error) {
	switch strings.ToLower(filepath.Ext(src.rel)) {
	case ".js", ".mjs", ".cjs":
		return sourceJavaScript, nil
	case ".ts", ".mts", ".cts":
		return sourceTypeScript, nil
	default:
		return sourceJavaScript, fmt.Errorf("source %s has an unsupported browser source extension", src.rel)
	}
}

func (language sourceLanguage) esbuildLoader() esbuild.Loader {
	if language == sourceTypeScript {
		return esbuild.LoaderTS
	}
	return esbuild.LoaderJS
}

// validateTypedSource validates a standalone TypeScript authority while keeping
// diagnostics tied to its original file and source range. Bootstrap sources may
// also be intentional prefix/suffix fragments, so bundle construction validates
// their concatenated chunk through esbuild rather than parsing each fragment.
func validateTypedSource(src source, body []byte) error {
	language, err := languageForSource(src)
	if err != nil {
		return err
	}
	if language == sourceJavaScript {
		return nil
	}

	grammar := grammars.TypescriptLanguage()
	if grammar == nil {
		return fmt.Errorf("parse %s: gotreesitter TypeScript grammar is unavailable", src.rel)
	}

	tree, err := gotreesitter.NewParser(grammar).Parse(body)
	if err != nil {
		return fmt.Errorf("parse %s: %w", src.rel, err)
	}
	root := tree.RootNode()
	if root == nil {
		return fmt.Errorf("parse %s: gotreesitter returned no syntax tree", src.rel)
	}
	if !root.IsError() && !root.HasError() {
		return nil
	}

	bad := firstSyntaxError(root)
	if bad == nil {
		bad = root
	}
	start := bad.StartPoint()
	end := bad.EndPoint()
	kind := bad.Type(grammar)
	if bad.IsMissing() {
		kind = "missing " + kind
	}
	return fmt.Errorf(
		"parse %s:%d:%d-%d:%d: TypeScript syntax error (%s)",
		src.rel,
		start.Row+1,
		start.Column+1,
		end.Row+1,
		end.Column+1,
		kind,
	)
}

func firstSyntaxError(node *gotreesitter.Node) *gotreesitter.Node {
	if node == nil {
		return nil
	}
	if node.IsError() || node.IsMissing() {
		return node
	}
	for index := 0; index < node.ChildCount(); index++ {
		child := node.Child(index)
		if child == nil || (!child.IsError() && !child.IsMissing() && !child.HasError()) {
			continue
		}
		if bad := firstSyntaxError(child); bad != nil {
			return bad
		}
	}
	if node.HasError() {
		return node
	}
	return nil
}

// transpileSource erases TypeScript types from one source with esbuild and
// returns the erased code plus the "mappings" field of esbuild's own source
// map from the erased code back to the given body. A JavaScript source
// returns unchanged, with no mappings: its content never reaches the
// TypeScript parser, so a JavaScript source beside a .ts source in the same
// chunk parses exactly as authored, generic-looking comparison operators
// included.
func transpileSource(src source, body string) (code string, mappings string, err error) {
	language, err := languageForSource(src)
	if err != nil {
		return "", "", err
	}
	if language == sourceJavaScript {
		return body, "", nil
	}

	result := esbuild.Transform(body, esbuild.TransformOptions{
		Charset:       esbuild.CharsetUTF8,
		LegalComments: esbuild.LegalCommentsNone,
		Loader:        language.esbuildLoader(),
		Sourcefile:    src.rel,
		Target:        esbuild.ES2020,
		Sourcemap:     esbuild.SourceMapExternal,
	})
	if len(result.Errors) > 0 {
		return "", "", fmt.Errorf("transpile %s: %s", src.rel, result.Errors[0].Text)
	}

	var parsed struct {
		Mappings string `json:"mappings"`
	}
	if err := json.Unmarshal(result.Map, &parsed); err != nil {
		return "", "", fmt.Errorf("decode transpile map for %s: %w", src.rel, err)
	}
	return string(result.Code), parsed.Mappings, nil
}

// transpileTypedChunk builds one chunk's plain-JavaScript text for the
// closure and symbol-placement checks. It transpiles each typed source on
// its own, with its own loader, and leaves each JavaScript source untouched,
// then concatenates the results in chunk order. It never feeds a .js
// source's content through the TypeScript parser, so a JavaScript file next
// to a .ts file in the same chunk parses exactly as authored.
//
// sourceBodies holds one already-read, newline-normalized body per entry in
// entry.sources, in the same order.
func transpileTypedChunk(entry output, sourceBodies []string) (string, error) {
	if len(sourceBodies) != len(entry.sources) {
		return "", fmt.Errorf("transpile %s: got %d source bodies for %d sources", entry.name, len(sourceBodies), len(entry.sources))
	}
	var b strings.Builder
	for index, src := range entry.sources {
		code, _, err := transpileSource(src, sourceBodies[index])
		if err != nil {
			return "", err
		}
		b.WriteString(code)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// decodeVLQSegment decodes one comma-free base64 VLQ run from a source map
// mappings string into its delta fields, in field order (see the Source Map
// v3 spec: generatedColumn, sourceIndex, sourceLine, sourceColumn, name).
func decodeVLQSegment(segment string) ([]int, error) {
	const continuationBit = 32
	var fields []int
	value, shift := 0, 0
	for i := 0; i < len(segment); i++ {
		digit := strings.IndexByte(base64Chars, segment[i])
		if digit < 0 {
			return nil, fmt.Errorf("source map segment %q holds a byte that is not a base64 digit: %q", segment, segment[i])
		}
		value |= (digit & 31) << shift
		if digit&continuationBit != 0 {
			shift += 5
			continue
		}
		signed := value >> 1
		if value&1 != 0 {
			signed = -signed
		}
		fields = append(fields, signed)
		value, shift = 0, 0
	}
	return fields, nil
}

// firstOriginalLinePerGeneratedLine decodes one esbuild per-source transpile
// map and returns, for each generated (erased) line, the 0-based original
// line its first mapped token names, or -1 when a generated line names no
// source position. buildCompactedBundle composes this per-source result into
// the bundle's hand-rolled, line-only compacted map, so a chunk still reports
// diagnostics against the original .ts file and line after type erasure has
// shifted its lines.
func firstOriginalLinePerGeneratedLine(mappings string) ([]int, error) {
	var origins []int
	sourceLine := 0
	for _, lineSegments := range strings.Split(mappings, ";") {
		found := -1
		if lineSegments != "" {
			for _, segment := range strings.Split(lineSegments, ",") {
				fields, err := decodeVLQSegment(segment)
				if err != nil {
					return nil, err
				}
				if len(fields) < 3 {
					continue
				}
				sourceLine += fields[2]
				if found < 0 {
					found = sourceLine
				}
			}
		}
		origins = append(origins, found)
	}
	return origins, nil
}

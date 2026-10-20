package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// sourcemap_test.go covers the hand-written parts of the output format: the
// line compactor, the VLQ encoder, the source map body, and the JavaScript
// string quoter.
//
// These four pieces are hand-written for one reason: the bundles must stay byte
// identical to what the retired Node pipeline produced, so the committed
// bundles do not churn. A defect in any of them is quiet. A wrong VLQ digit
// points every stack frame at the wrong line, and a wrong escape breaks the
// sourcesContent of a map no test reads.

// decodedMapping is one segment of a decoded source map line.
type decodedMapping struct {
	source       int
	originalLine int
	column       int
}

// decodeVLQMappings decodes the mappings string that encodeMappings produces.
// It accepts the one shape the compacted map uses: at most one segment per
// generated line, always at generated column 0.
func decodeVLQMappings(t *testing.T, mappings string) []*decodedMapping {
	t.Helper()
	var out []*decodedMapping
	source, line, column := 0, 0, 0
	for _, segment := range strings.Split(mappings, ";") {
		if segment == "" {
			out = append(out, nil)
			continue
		}
		fields := decodeVLQFields(t, segment)
		if len(fields) != 4 {
			t.Fatalf("segment %q decodes to %d fields, want 4", segment, len(fields))
		}
		if fields[0] != 0 {
			t.Fatalf("segment %q starts at generated column %d, want 0", segment, fields[0])
		}
		source += fields[1]
		line += fields[2]
		column += fields[3]
		out = append(out, &decodedMapping{source: source, originalLine: line, column: column})
	}
	return out
}

func decodeVLQFields(t *testing.T, segment string) []int {
	t.Helper()
	const continuationBit = 32
	var fields []int
	value, shift := 0, 0
	for i := 0; i < len(segment); i++ {
		digit := strings.IndexByte(base64Chars, segment[i])
		if digit < 0 {
			t.Fatalf("segment %q holds a byte that is not a base64 digit: %q", segment, segment[i])
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
	return fields
}

// TestBase64VLQEncodeMatchesTheSpec pins the encoder against values a reader
// can check by hand. These are the canonical examples of the base64 VLQ format
// every source map consumer implements.
func TestBase64VLQEncodeMatchesTheSpec(t *testing.T) {
	cases := []struct {
		value int
		want  string
	}{
		{0, "A"},
		{1, "C"},
		{-1, "D"},
		{2, "E"},
		{-2, "F"},
		{15, "e"},
		{16, "gB"},
		{-16, "hB"},
		{17, "iB"},
		{511, "+f"},
		{512, "ggB"},
		{-1000, "x+B"},
	}
	for _, tc := range cases {
		if got := base64VLQEncode(tc.value); got != tc.want {
			t.Errorf("base64VLQEncode(%d) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

// TestCompactSourceDropsCommentsAndKeepsLineNumbers covers the compactor and
// the line map together. The line map is what makes the source map point at the
// right original line, so an off-by-one here misplaces every frame.
func TestCompactSourceDropsCommentsAndKeepsLineNumbers(t *testing.T) {
	// Line numbers, zero based: 0 blank, 1 comment, 2 code, 3 blank, 4 blank,
	// 5 code with trailing spaces, 6 comment, 7 code, 8 blank.
	input := "\n// a comment line\nvar alpha = 1;\n\n\nvar beta = 2;   \n  // an indented comment\nvar gamma = 3;\n\n"
	got := compactSource(input)

	wantCode := "var alpha = 1;\n\nvar beta = 2;\nvar gamma = 3;\n"
	if got.code != wantCode {
		t.Errorf("compactSource code = %q, want %q", got.code, wantCode)
	}
	wantMap := []int{2, 3, 5, 7}
	if len(got.lineMap) != len(wantMap) {
		t.Fatalf("compactSource line map = %v, want %v", got.lineMap, wantMap)
	}
	for i := range wantMap {
		if got.lineMap[i] != wantMap[i] {
			t.Fatalf("compactSource line map = %v, want %v", got.lineMap, wantMap)
		}
	}
}

// TestCompactSourceNormalizesEveryNewlineForm proves a CRLF checkout produces
// the same bundle as an LF checkout. Otherwise every bundle would look stale on
// a Windows clone and the --check gate would be unusable there.
func TestCompactSourceNormalizesEveryNewlineForm(t *testing.T) {
	lf := compactSource("var alpha = 1;\nvar beta = 2;\n")
	crlf := compactSource("var alpha = 1;\r\nvar beta = 2;\r\n")
	cr := compactSource("var alpha = 1;\rvar beta = 2;\r")
	if lf.code != crlf.code || lf.code != cr.code {
		t.Errorf("newline form changes the output: LF %q, CRLF %q, CR %q", lf.code, crlf.code, cr.code)
	}
}

// TestCompactedBundleMapPointsAtTheRightSourceLines walks the map end to end: a
// generated line of the compacted bundle must resolve to the file it came from
// and to the line it came from.
func TestCompactedBundleMapPointsAtTheRightSourceLines(t *testing.T) {
	f := newFixture(t)
	first := f.writeSource("10-first.js", "// dropped comment\nvar first = 1;\nvar second = 2;\n")
	second := f.writeSource("20-second.js", "\n\nvar third = 3;\n")

	built, err := buildCompactedBundle(f.dir, chunk("mapped.js", first, second))
	if err != nil {
		t.Fatalf("buildCompactedBundle: %v", err)
	}

	var parsed struct {
		Version        int      `json:"version"`
		File           string   `json:"file"`
		Sources        []string `json:"sources"`
		SourcesContent []string `json:"sourcesContent"`
		Mappings       string   `json:"mappings"`
	}
	if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
		t.Fatalf("the compacted map is not valid JSON: %v", err)
	}
	if parsed.Version != 3 {
		t.Errorf("map version = %d, want 3", parsed.Version)
	}
	if parsed.File != "mapped.js" {
		t.Errorf("map file = %q, want %q", parsed.File, "mapped.js")
	}
	if len(parsed.Sources) != 2 || parsed.Sources[0] != first || parsed.Sources[1] != second {
		t.Fatalf("map sources = %v, want [%s %s]", parsed.Sources, first, second)
	}
	if len(parsed.SourcesContent) != 2 {
		t.Fatalf("map sourcesContent holds %d entries, want 2", len(parsed.SourcesContent))
	}
	if !strings.Contains(parsed.SourcesContent[0], "dropped comment") {
		t.Error("sourcesContent[0] lost the comment; the map must carry the raw source, not the compacted source")
	}

	// The compacted bundle holds: first.js lines 1 and 2, one blank separator
	// line, then second.js line 2. Zero based, that is (0,1), (0,2), nil, (1,2).
	want := []*decodedMapping{
		{source: 0, originalLine: 1},
		{source: 0, originalLine: 2},
		nil,
		{source: 1, originalLine: 2},
	}
	got := decodeVLQMappings(t, parsed.Mappings)
	if len(got) != len(want) {
		t.Fatalf("map holds %d generated lines, want %d (mappings %q)", len(got), len(want), parsed.Mappings)
	}
	for i := range want {
		if (got[i] == nil) != (want[i] == nil) {
			t.Fatalf("generated line %d: got %v, want %v", i, got[i], want[i])
		}
		if want[i] == nil {
			continue
		}
		if got[i].source != want[i].source || got[i].originalLine != want[i].originalLine {
			t.Errorf("generated line %d maps to source %d line %d, want source %d line %d",
				i, got[i].source, got[i].originalLine, want[i].source, want[i].originalLine)
		}
	}
}

// TestCompactedBundleMapAccountsForTypeErasureLineShift proves the compacted
// map still names the correct original .ts line after buildCompactedBundle
// erases a multi-line type declaration, which shifts every line below it. A
// map built from pre-erasure line numbers would still claim the interface's
// own line for the code that follows it.
func TestCompactedBundleMapAccountsForTypeErasureLineShift(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("10-typed.ts", "interface Foo {\n  a: number;\n  b: number;\n}\nglobalThis.__gosx_typed_marker = 1;\n")

	built, err := buildCompactedBundle(f.dir, chunk("typed.js", rel))
	if err != nil {
		t.Fatalf("buildCompactedBundle: %v", err)
	}
	if strings.Contains(built.code, "interface") {
		t.Fatalf("the compacted bundle kept TypeScript-only syntax: %q", built.code)
	}

	var parsed struct {
		SourcesContent []string `json:"sourcesContent"`
		Mappings       string   `json:"mappings"`
	}
	if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
		t.Fatalf("the compacted map is not valid JSON: %v", err)
	}
	if !strings.Contains(parsed.SourcesContent[0], "interface Foo") {
		t.Fatalf("sourcesContent lost the original TypeScript source: %v", parsed.SourcesContent)
	}

	// The interface occupies original lines 0-3 (zero based); the erased
	// bundle's sole surviving statement was originally line 4.
	got := decodeVLQMappings(t, parsed.Mappings)
	if len(got) != 1 || got[0] == nil {
		t.Fatalf("map holds %v, want exactly one generated line naming a source position", got)
	}
	if got[0].originalLine != 4 {
		t.Errorf("generated line maps to original line %d, want 4 (erasing the interface must not leave a stale line number)", got[0].originalLine)
	}
}

// TestJSQuoteMatchesJSONStringify pins the quoter against the JavaScript
// semantics the bundles need. encoding/json cannot do this job: it escapes
// U+2028 and U+2029, and it writes \b and \f as \u escapes. Either difference
// changes every committed source map, so the tool carries its own quoter.
func TestJSQuoteMatchesJSONStringify(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"quote and backslash", `a "b" \c`, `"a \"b\" \\c"`},
		{"control shorthands", "\b\t\n\f\r", `"\b\t\n\f\r"`},
		{"other control byte", "\x01", "\"\\u0001\""},
		{"line separator stays raw", "a b", "\"a b\""},
		{"paragraph separator stays raw", "a b", "\"a b\""},
		{"non-ASCII stays raw", "café 中", "\"café 中\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := jsQuote(tc.in); got != tc.want {
				t.Errorf("jsQuote(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	// State the difference from encoding/json, so a later reader does not
	// "simplify" the quoter and churn every committed map.
	viaJSON, err := json.Marshal("a b")
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(viaJSON) == jsQuote("a b") {
		t.Error("encoding/json now matches jsQuote for U+2028; re-check whether the tool still needs its own quoter")
	}
}

// TestJSQuoteArrayKeepsOrderAndSeparators pins the array form the map uses for
// sources and sourcesContent.
func TestJSQuoteArrayKeepsOrderAndSeparators(t *testing.T) {
	if got, want := jsQuoteArray(nil), "[]"; got != want {
		t.Errorf("jsQuoteArray(nil) = %q, want %q", got, want)
	}
	if got, want := jsQuoteArray([]string{"a", "b"}), `["a","b"]`; got != want {
		t.Errorf("jsQuoteArray = %q, want %q", got, want)
	}
}

// TestNormalizeGeneratedCodeAlwaysEndsWithOneNewline covers the trailer rule.
// The debug trailer is opt-in, because a sourceMappingURL in a shipped bundle
// makes every browser fetch the map.
func TestNormalizeGeneratedCodeAlwaysEndsWithOneNewline(t *testing.T) {
	if got, want := normalizeGeneratedCode("var a=1;", "b.js.map", false), "var a=1;\n"; got != want {
		t.Errorf("normalizeGeneratedCode = %q, want %q", got, want)
	}
	if got, want := normalizeGeneratedCode("var a=1;\n", "b.js.map", false), "var a=1;\n"; got != want {
		t.Errorf("normalizeGeneratedCode added a second newline: %q, want %q", got, want)
	}
	got := normalizeGeneratedCode("var a=1;", "b.js.map", true)
	if want := "var a=1;\n//# sourceMappingURL=b.js.map\n"; got != want {
		t.Errorf("debug trailer = %q, want %q", got, want)
	}
}

// TestBuildBundleRejectsAnUnknownMinifier proves a typo in -minifier fails
// instead of falling back to an unminified bundle.
func TestBuildBundleRejectsAnUnknownMinifier(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("10-only.js", "var alpha = 1;\n")
	if _, err := buildBundle(f.dir, chunk("x.js", rel), "esbuild-typo", false); err == nil {
		t.Fatal("buildBundle accepted an unknown minifier")
	}
}

// TestMinifiedBundleCarriesAComposedMap proves the esbuild path keeps the
// original file names, not the intermediate bundle name. A map that names one
// bundle instead of 90 source files is useless for a stack trace.
func TestMinifiedBundleCarriesAComposedMap(t *testing.T) {
	f := newFixture(t)
	head := f.writeSource("10-head.js", "(function () {\n  \"use strict\";\n")
	body := f.writeSource("20-body.js", compressibleSource("mapHelper", 20))
	tail := f.writeSource("30-tail.js", "  window.__gosx_mapped = mapHelper0(1, 2);\n})();\n")

	built, err := buildBundle(f.dir, chunk("composed.js", head, body, tail), "esbuild", false)
	if err != nil {
		t.Fatalf("buildBundle: %v", err)
	}
	var parsed struct {
		Version        int      `json:"version"`
		File           string   `json:"file"`
		Sources        []string `json:"sources"`
		SourcesContent []string `json:"sourcesContent"`
		Mappings       string   `json:"mappings"`
	}
	if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
		t.Fatalf("the composed map is not valid JSON: %v", err)
	}
	if parsed.Version != 3 {
		t.Errorf("map version = %d, want 3", parsed.Version)
	}
	if parsed.File != "composed.js" {
		t.Errorf("map file = %q, want %q", parsed.File, "composed.js")
	}
	wantSources := map[string]bool{head: false, body: false, tail: false}
	for _, source := range parsed.Sources {
		if _, ok := wantSources[source]; ok {
			wantSources[source] = true
		}
	}
	for source, found := range wantSources {
		if !found {
			t.Errorf("the composed map does not name %s; got %v", source, parsed.Sources)
		}
	}
	if parsed.Mappings == "" {
		t.Error("the composed map holds no mappings")
	}
	if len(parsed.SourcesContent) != len(parsed.Sources) {
		t.Errorf("map holds %d sources but %d sourcesContent entries", len(parsed.Sources), len(parsed.SourcesContent))
	}
	if strings.Contains(built.code, "sourceMappingURL") {
		t.Error("the shipped bundle carries a sourceMappingURL; that makes every browser fetch the map")
	}
}

// Native-side tests for the surface runtime package.
//
// Build constraint: keep this off the js+wasm target so it runs in the
// standard host test cycle (`go test ./engine/surface/runtime`).

//go:build !(js && wasm)

package runtime

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestBootstrapKindTableMatchesRuntimeRuntime guards the symbol table that
// the JS bootstrap (engine/surface/runtime/bootstrap.js) uses against the
// canonical table in runtime.go lines 18-37. If these drift, surface events
// silently route to the wrong Go handler — this test catches that.
func TestBootstrapKindTableMatchesRuntimeRuntime(t *testing.T) {
	pkgDir := thisPackageDir(t)

	goTable, err := parseRuntimeGoKinds(filepath.Join(pkgDir, "runtime.go"))
	if err != nil {
		t.Fatalf("parse runtime.go kinds: %v", err)
	}
	jsTable, err := parseBootstrapJSKinds(filepath.Join(pkgDir, "bootstrap.js"))
	if err != nil {
		t.Fatalf("parse bootstrap.js KIND_TABLE: %v", err)
	}

	if len(goTable) == 0 {
		t.Fatal("runtime.go: no kinds parsed from the canonical table — check the comment grammar")
	}
	if len(jsTable) == 0 {
		t.Fatal("bootstrap.js: no kinds parsed from KIND_TABLE")
	}

	// Every Go kind must appear in JS with the same numeric value.
	for name, goVal := range goTable {
		jsVal, ok := jsTable[name]
		if !ok {
			t.Errorf("bootstrap.js missing kind %q (runtime.go: %d)", name, goVal)
			continue
		}
		if jsVal != goVal {
			t.Errorf("kind %q: runtime.go=%d, bootstrap.js=%d", name, goVal, jsVal)
		}
	}
	// And the JS table must not invent extras (they would route to nothing on the Go side).
	for name, jsVal := range jsTable {
		if _, ok := goTable[name]; !ok {
			t.Errorf("bootstrap.js declares kind %q=%d not present in runtime.go", name, jsVal)
		}
	}
}

// thisPackageDir returns the directory containing the runtime package source.
func thisPackageDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Dir(thisFile)
}

// parseRuntimeGoKinds scans runtime.go for the canonical kind table rows in
// the package doc comment. The expected row shape is:
//
//	// | <num> | <name> | ...
//
// embedded in the doc comment around lines 18-37.
func parseRuntimeGoKinds(path string) (map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := make(map[string]int)
	// Row pattern: `// | <int> | <name> | ...`. The name column may include a
	// trailing dispose entry; treat the second pipe-separated value as the
	// event name. Skip the header rows where the second column isn't numeric.
	row := regexp.MustCompile(`^//\s*\|\s*(\d+)\s*\|\s*([a-zA-Z][a-zA-Z0-9_]*)\s*\|`)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		m := row.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(m[2]))
		out[name] = n
	}
	return out, scanner.Err()
}

// parseBootstrapJSKinds extracts the JS object literal at "var KIND_TABLE = { ... };"
// in bootstrap.js. We do not run a JS parser; instead we match `<key>: <int>`
// inside the brace block. The bootstrap deliberately keeps the table simple
// so this lightweight scanner suffices.
func parseBootstrapJSKinds(path string) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src := string(data)
	startIdx := strings.Index(src, "KIND_TABLE")
	if startIdx < 0 {
		return nil, nil
	}
	openIdx := strings.Index(src[startIdx:], "{")
	if openIdx < 0 {
		return nil, nil
	}
	openIdx += startIdx
	depth := 0
	endIdx := -1
	for i := openIdx; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				endIdx = i
			}
		}
		if endIdx >= 0 {
			break
		}
	}
	if endIdx < 0 {
		return nil, nil
	}
	body := src[openIdx+1 : endIdx]
	pair := regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9_]*)\s*:\s*(\d+)`)
	matches := pair.FindAllStringSubmatch(body, -1)
	out := make(map[string]int, len(matches))
	for _, m := range matches {
		name := strings.ToLower(m[1])
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		out[name] = n
	}
	return out, nil
}

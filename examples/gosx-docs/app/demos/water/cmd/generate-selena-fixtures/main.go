// Command generate-selena-fixtures rebuilds the browser-runtime fixtures from
// the same Selena sources and compiler used by the GoSX water demo.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/selena"
)

const sourceDir = "examples/gosx-docs/app/demos/water/shaders/jeantimex-water.selena"
const outputDir = "client/js/testdata"

type fixture struct {
	Comment string `json:"_comment"`
	Layout  any    `json:"layout"`
	WGSL    string `json:"wgsl"`
}

func main() {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		fatalf("read Selena water source directory: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sel") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		sourcePath := filepath.Join(sourceDir, name)
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			fatalf("read %s: %v", sourcePath, err)
		}
		result, err := selena.Compile(source, selena.CompileOptions{Targets: []selena.Target{selena.TargetWGSL}})
		if err != nil {
			fatalf("compile %s: %v", sourcePath, err)
		}
		artifact, ok := result.Artifact(selena.TargetWGSL)
		if !ok || strings.TrimSpace(artifact.Source) == "" {
			fatalf("compile %s: no WGSL artifact", sourcePath)
		}
		slug := strings.TrimSuffix(name, ".sel")
		outPath := filepath.Join(outputDir, "water-"+slug+"-selena.json")
		payload := fixture{
			Comment: fmt.Sprintf("Generated fixture: real Selena-compiled %s WGSL + host binding descriptor (bindings.Layout). Regenerate with `go run ./examples/gosx-docs/app/demos/water/cmd/generate-selena-fixtures`. Source: %s", name, sourcePath),
			Layout:  result.Layout,
			WGSL:    artifact.Source,
		}
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(payload); err != nil {
			fatalf("encode %s: %v", outPath, err)
		}
		if err := os.WriteFile(outPath, encoded.Bytes(), 0o644); err != nil {
			fatalf("write %s: %v", outPath, err)
		}
		fmt.Println(outPath)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "generate-selena-fixtures: "+format+"\n", args...)
	os.Exit(1)
}

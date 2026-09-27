//go:build ignore

// Generate the source receipt rendered by the shared tabletop demo. Run from
// the repository root with:
//
//	GOWORK=off go run scripts/generate-tabletop-receipts.go
package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type receipt struct {
	SourceFiles     []sourceFile `json:"sourceFiles"`
	JavaScriptLines int          `json:"javascriptLines"`
}

type sourceFile struct {
	Path  string `json:"path"`
	Lines int    `json:"lines"`
}

func main() {
	const appDir = "examples/gosx-docs/app/demos/tabletop"
	const publicDir = "examples/gosx-docs/public/tabletop"
	files := []string{"rooms.go", "page.server.go", "page.gsx"}
	out := receipt{SourceFiles: make([]sourceFile, 0, len(files))}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(appDir, name))
		if err != nil {
			fail(err)
		}
		out.SourceFiles = append(out.SourceFiles, sourceFile{Path: name, Lines: countLines(data)})
	}
	for _, dir := range []string{appDir, publicDir} {
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".js") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out.JavaScriptLines += countLines(data)
			return nil
		})
		if err != nil {
			fail(err)
		}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "receipts.json"), append(data, '\n'), 0644); err != nil {
		fail(err)
	}
	fmt.Printf("tabletop receipts: %d Go/GSX source files; %d authored JavaScript lines\n", len(out.SourceFiles), out.JavaScriptLines)
}

func countLines(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	lines := strings.Count(string(data), "\n")
	if data[len(data)-1] != '\n' {
		lines++
	}
	return lines
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

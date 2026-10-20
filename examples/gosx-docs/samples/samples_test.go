package samples

import (
	"encoding/json"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

var goImportLine = regexp.MustCompile(`(?m)^import\s+[^\n]+\n`)
var gosxPackageDeclaration = regexp.MustCompile(`(?m)^package\s+[A-Za-z_][A-Za-z0-9_]*\s*$`)
var goPackageDeclaration = regexp.MustCompile(`(?m)^package\s+[A-Za-z_][A-Za-z0-9_]*\s*$`)

func TestEveryGoSampleParses(t *testing.T) {
	for _, path := range Files() {
		if !strings.HasSuffix(path, ".go.sample") {
			continue
		}
		t.Run(path, func(t *testing.T) {
			source, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			wrapped := goSampleHarness(source)
			if _, err := parser.ParseFile(token.NewFileSet(), path, wrapped, parser.AllErrors); err != nil {
				t.Fatalf("parse Go sample: %v", err)
			}
		})
	}
}

func TestEveryGoSXSampleCompiles(t *testing.T) {
	for _, path := range Files() {
		if !strings.HasSuffix(path, ".gosx.sample") && !strings.HasSuffix(path, ".gsx.sample") {
			continue
		}
		t.Run(path, func(t *testing.T) {
			source, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			if !gosxPackageDeclaration.MatchString(source) {
				if strings.Contains(source, "component ") || strings.Contains(source, "type ") {
					source = "package sample\n" + source
				} else {
					source = "package sample\nfunc Example() Node {\nreturn <div>\n" + source + "\n</div>\n}\n"
				}
			}
			if _, err := gosx.Compile([]byte(source)); err != nil {
				t.Fatalf("compile GoSX sample: %v", err)
			}
		})
	}
}

func TestExecutableSamplesHaveValidSyntax(t *testing.T) {
	for _, path := range Files() {
		switch filepath.Ext(strings.TrimSuffix(path, ".sample")) {
		case ".bash":
			t.Run(path, func(t *testing.T) {
				source, err := Read(path)
				if err != nil {
					t.Fatal(err)
				}
				command := exec.Command("bash", "-n")
				command.Stdin = strings.NewReader(source)
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("parse shell sample: %v\n%s", err, output)
				}
			})
		case ".js":
			t.Run(path, func(t *testing.T) {
				source, err := Read(path)
				if err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(t.TempDir(), "sample.mjs")
				if err := os.WriteFile(file, []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
				if output, err := exec.Command("node", "--check", file).CombinedOutput(); err != nil {
					t.Fatalf("parse JavaScript sample: %v\n%s", err, output)
				}
			})
		case ".json":
			t.Run(path, func(t *testing.T) {
				source, err := Read(path)
				if err != nil {
					t.Fatal(err)
				}
				if !json.Valid([]byte(source)) {
					t.Fatal("invalid JSON sample")
				}
			})
		}
	}
}

func goSampleHarness(source string) string {
	source = strings.TrimSpace(source)
	if goPackageDeclaration.MatchString(source) || strings.HasPrefix(source, "//go:build ") {
		return source
	}
	if regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*\s*:`).MatchString(source) {
		return "package sample\nimport \"m31labs.dev/gosx/scene\"\nfunc Example() { _ = scene.Props{\n" + source + "\n} }\n"
	}
	prefix := "package sample\n"
	if _, err := parser.ParseFile(token.NewFileSet(), "sample.go", prefix+source, parser.AllErrors); err == nil {
		return prefix + source
	}
	if match := goImportLine.FindStringIndex(source); match != nil {
		imports := source[:match[1]]
		body := strings.TrimSpace(source[match[1]:])
		if body == "" {
			return prefix + imports
		}
		return prefix + imports + "func Example() {\n" + body + "\n}\n"
	}
	_, parseErr := parser.ParseFile(token.NewFileSet(), "sample.go", prefix+source, parser.AllErrors)
	if errors, ok := parseErr.(scanner.ErrorList); ok && len(errors) > 0 {
		offset := errors[0].Pos.Offset - len(prefix)
		if offset > 0 && offset < len(source) {
			declarations := strings.TrimSpace(source[:offset])
			body := strings.TrimSpace(source[offset:])
			return prefix + declarations + "\nfunc Example() {\n" + body + "\n}\n"
		}
	}
	return prefix + "func Example() {\n" + source + "\n}\n"
}

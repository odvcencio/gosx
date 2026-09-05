// Package gsxcorpus loads the bounded GoSX authoring corpus used by package
// tests. It is a test helper: production code should not depend on corpus
// expectations.
package gsxcorpus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	MaxCases       = 15
	MaxSourceBytes = 8 << 10
)

type Expectation string

const (
	Pass   Expectation = "pass"
	Reject Expectation = "reject"
	Skip   Expectation = "skip"
)

type Manifest struct {
	Cases []Case `json:"cases"`
}

type Case struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Main          string                 `json:"main"`
	Files         []string               `json:"files"`
	Parse         Expectation            `json:"parse"`
	Compile       Expectation            `json:"compile"`
	Transpile     Expectation            `json:"transpile"`
	Format        Expectation            `json:"format"`
	LSPAnalyze    Expectation            `json:"lspAnalyze"`
	LSPFormat     Expectation            `json:"lspFormat"`
	StrictCheck   Expectation            `json:"strictCheck"`
	Route         Expectation            `json:"route"`
	IRIsland      Expectation            `json:"irIsland"`
	IslandTarget  string                 `json:"islandTarget"`
	RouteHTML     string                 `json:"routeHTML"`
	Errors        map[string]string      `json:"errors"`
	Notes         map[string]string      `json:"notes,omitempty"`
	NotApplicable map[string]string      `json:"notApplicable,omitempty"`
	Meta          map[string]interface{} `json:"meta,omitempty"`
}

func RootDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("locate corpus helper")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

func CorpusDir() (string, error) {
	root, err := RootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "testdata", "gsx-authoring-corpus"), nil
}

func Load() ([]Case, error) {
	dir, err := CorpusDir()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	if len(manifest.Cases) == 0 || len(manifest.Cases) > MaxCases {
		return nil, fmt.Errorf("corpus case count %d outside 1..%d", len(manifest.Cases), MaxCases)
	}
	seen := map[string]bool{}
	for _, c := range manifest.Cases {
		if c.Name == "" || strings.Contains(c.Name, "/") || strings.Contains(c.Name, `\`) {
			return nil, fmt.Errorf("invalid corpus case name %q", c.Name)
		}
		if seen[c.Name] {
			return nil, fmt.Errorf("duplicate corpus case %q", c.Name)
		}
		seen[c.Name] = true
		if c.Main == "" || len(c.Files) == 0 {
			return nil, fmt.Errorf("case %s has no main/files", c.Name)
		}
		if !contains(c.Files, c.Main) {
			return nil, fmt.Errorf("case %s main %s is not listed in files", c.Name, c.Main)
		}
		for target, status := range c.TargetStatuses() {
			if status != Pass && status != Reject && status != Skip {
				return nil, fmt.Errorf("case %s target %s has invalid status %q", c.Name, target, status)
			}
			if status == Skip && strings.TrimSpace(c.NotApplicable[target]) == "" {
				return nil, fmt.Errorf("case %s target %s is not-applicable without a reason", c.Name, target)
			}
		}
		for _, rel := range c.Files {
			if err := validateRel(rel); err != nil {
				return nil, fmt.Errorf("case %s: %w", c.Name, err)
			}
			src, err := c.Source(rel)
			if err != nil {
				return nil, err
			}
			if len(src) == 0 || len(src) > MaxSourceBytes {
				return nil, fmt.Errorf("case %s file %s has %d bytes, want 1..%d", c.Name, rel, len(src), MaxSourceBytes)
			}
		}
	}
	return manifest.Cases, nil
}

func (c Case) Source(rel string) ([]byte, error) {
	if err := validateRel(rel); err != nil {
		return nil, err
	}
	dir, err := CorpusDir()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, c.Name, rel))
}

func (c Case) MainSource() ([]byte, error) {
	return c.Source(c.Main)
}

func (c Case) WriteTo(dir string) error {
	for _, rel := range c.Files {
		src, err := c.Source(rel)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, src, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (c Case) WriteStrictCheckModule(dir string) error {
	modulePath, err := RootDir()
	if err != nil {
		return err
	}
	if info, err := os.Stat(filepath.Join(modulePath, "go.mod")); err != nil {
		return fmt.Errorf("GoSX module under test unavailable at %s: %w", modulePath, err)
	} else if info.IsDir() {
		return fmt.Errorf("GoSX module under test go.mod path is a directory: %s", filepath.Join(modulePath, "go.mod"))
	}
	if sum, err := os.ReadFile(filepath.Join(modulePath, "go.sum")); err == nil {
		if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o600); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	moduleGoMod, err := os.ReadFile(filepath.Join(modulePath, "go.mod"))
	if err != nil {
		return err
	}
	mod, err := strictCheckGoMod(moduleGoMod, modulePath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600); err != nil {
		return err
	}
	return c.WriteTo(dir)
}

func strictCheckGoMod(moduleGoMod []byte, modulePath string) (string, error) {
	text := string(moduleGoMod)
	lines := strings.SplitAfter(text, "\n")
	offset := 0
	moduleEnd := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") || strings.HasPrefix(trimmed, "module\t") {
			fields := strings.Fields(trimmed)
			if len(fields) != 2 || fields[1] != "m31labs.dev/gosx" {
				return "", fmt.Errorf("unexpected module directive %q", trimmed)
			}
			moduleEnd = offset + len(line)
			break
		}
		offset += len(line)
	}
	if moduleEnd < 0 {
		return "", fmt.Errorf("go.mod does not declare module m31labs.dev/gosx")
	}
	tail := strings.TrimLeft(text[moduleEnd:], "\r\n")
	quotedPath := strconv.Quote(filepath.ToSlash(modulePath))
	return "module example.test/corpus\n\n" + tail + "\nrequire m31labs.dev/gosx v0.0.0\nreplace m31labs.dev/gosx => " + quotedPath + "\n", nil
}

func (c Case) MainPath(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(c.Main))
}

func (c Case) ErrorContains(target string) string {
	if c.Errors == nil {
		return ""
	}
	return c.Errors[target]
}

func (c Case) TargetStatuses() map[string]Expectation {
	return map[string]Expectation{
		"parse":       c.Parse,
		"compile":     c.Compile,
		"transpile":   c.Transpile,
		"format":      c.Format,
		"lspAnalyze":  c.LSPAnalyze,
		"lspFormat":   c.LSPFormat,
		"strictCheck": c.StrictCheck,
		"route":       c.Route,
		"irIsland":    c.IRIsland,
	}
}

func validateRel(rel string) error {
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return fmt.Errorf("invalid relative path %q", rel)
	}
	return nil
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

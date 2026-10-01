// Package uirecipe owns the offline, source-installed GoSX UI recipe catalog.
package uirecipe

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	goformat "go/format"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"m31labs.dev/gosx"
	gosxformat "m31labs.dev/gosx/format"
)

const (
	embeddedCatalogRoot = "recipes/v1"
	manifestFile        = "manifest.json"
)

//go:embed recipes/v1
var embeddedCatalogFS embed.FS

var recipeNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type catalogManifest struct {
	SchemaVersion  int              `json:"schemaVersion"`
	CatalogVersion string           `json:"catalogVersion"`
	Source         string           `json:"source"`
	License        string           `json:"license"`
	Provenance     string           `json:"provenance"`
	Recipes        []recipeManifest `json:"recipes"`
}

type recipeManifest struct {
	Name         string         `json:"name"`
	Version      string         `json:"version"`
	Description  string         `json:"description"`
	Dependencies []string       `json:"dependencies"`
	Files        []fileManifest `json:"files"`
}

type fileManifest struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// Summary is one stable row returned by gosx ui list.
type Summary struct {
	Name         string
	Version      string
	Description  string
	Dependencies []string
	Files        []string
}

type recipeFile struct {
	Source  string
	Target  string
	Content []byte
	SHA256  string
}

type recipe struct {
	Summary
	files []recipeFile
}

// Catalog is a validated immutable recipe catalog.
type Catalog struct {
	version    string
	source     string
	license    string
	provenance string
	recipes    map[string]recipe
}

// Load returns the source-owned catalog embedded in the gosx CLI.
func Load() (*Catalog, error) {
	sub, err := fs.Sub(embeddedCatalogFS, embeddedCatalogRoot)
	if err != nil {
		return nil, fmt.Errorf("open embedded UI catalog: %w", err)
	}
	return loadCatalog(sub)
}

func loadCatalog(fsys fs.FS) (*Catalog, error) {
	data, err := fs.ReadFile(fsys, manifestFile)
	if err != nil {
		return nil, fmt.Errorf("read UI catalog manifest: %w", err)
	}
	var manifest catalogManifest
	if err := decodeDocument(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode UI catalog manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported UI catalog schema %d", manifest.SchemaVersion)
	}
	for field, value := range map[string]string{
		"catalogVersion": manifest.CatalogVersion,
		"source":         manifest.Source,
		"license":        manifest.License,
		"provenance":     manifest.Provenance,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("UI catalog %s is required", field)
		}
	}

	catalog := &Catalog{
		version:    manifest.CatalogVersion,
		source:     manifest.Source,
		license:    manifest.License,
		provenance: manifest.Provenance,
		recipes:    make(map[string]recipe, len(manifest.Recipes)),
	}
	targetOwners := map[string]string{}
	for _, item := range manifest.Recipes {
		if !recipeNamePattern.MatchString(item.Name) {
			return nil, fmt.Errorf("invalid UI recipe name %q", item.Name)
		}
		if _, exists := catalog.recipes[item.Name]; exists {
			return nil, fmt.Errorf("duplicate UI recipe %q", item.Name)
		}
		if strings.TrimSpace(item.Version) == "" || strings.TrimSpace(item.Description) == "" {
			return nil, fmt.Errorf("UI recipe %q requires version and description", item.Name)
		}
		sort.Strings(item.Dependencies)
		if duplicate := firstDuplicate(item.Dependencies); duplicate != "" {
			return nil, fmt.Errorf("UI recipe %q repeats dependency %q", item.Name, duplicate)
		}

		entry := recipe{Summary: Summary{
			Name:         item.Name,
			Version:      item.Version,
			Description:  item.Description,
			Dependencies: append([]string(nil), item.Dependencies...),
		}}
		for _, file := range item.Files {
			if err := validateCatalogPath("source", file.Source); err != nil {
				return nil, fmt.Errorf("UI recipe %q: %w", item.Name, err)
			}
			if err := validateCatalogPath("target", file.Target); err != nil {
				return nil, fmt.Errorf("UI recipe %q: %w", item.Name, err)
			}
			if owner, exists := targetOwners[file.Target]; exists {
				return nil, fmt.Errorf("UI recipes %q and %q both target %q", owner, item.Name, file.Target)
			}
			targetOwners[file.Target] = item.Name
			content, err := fs.ReadFile(fsys, file.Source)
			if err != nil {
				return nil, fmt.Errorf("read UI recipe %q source %q: %w", item.Name, file.Source, err)
			}
			if err := validateRecipeSource(file.Source, content); err != nil {
				return nil, fmt.Errorf("UI recipe %q source %q: %w", item.Name, file.Source, err)
			}
			entry.files = append(entry.files, recipeFile{
				Source:  file.Source,
				Target:  file.Target,
				Content: append([]byte(nil), content...),
				SHA256:  contentHash(content),
			})
			entry.Files = append(entry.Files, file.Target)
		}
		if len(entry.files) == 0 {
			return nil, fmt.Errorf("UI recipe %q has no files", item.Name)
		}
		sort.Slice(entry.files, func(i, j int) bool { return entry.files[i].Target < entry.files[j].Target })
		sort.Strings(entry.Files)
		catalog.recipes[item.Name] = entry
	}
	if len(catalog.recipes) == 0 {
		return nil, fmt.Errorf("UI catalog has no recipes")
	}
	for name, item := range catalog.recipes {
		for _, dependency := range item.Dependencies {
			if _, ok := catalog.recipes[dependency]; !ok {
				return nil, fmt.Errorf("UI recipe %q depends on unknown recipe %q", name, dependency)
			}
		}
	}
	if err := catalog.validateDependencyCycles(); err != nil {
		return nil, err
	}
	return catalog, nil
}

// List returns recipe summaries sorted by name.
func (c *Catalog) List() []Summary {
	names := make([]string, 0, len(c.recipes))
	for name := range c.recipes {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Summary, 0, len(names))
	for _, name := range names {
		item := c.recipes[name].Summary
		item.Dependencies = append([]string(nil), item.Dependencies...)
		item.Files = append([]string(nil), item.Files...)
		out = append(out, item)
	}
	return out
}

func (c *Catalog) closure(name string) ([]recipe, error) {
	if _, ok := c.recipes[name]; !ok {
		return nil, fmt.Errorf("unknown UI recipe %q; run `gosx ui list`", name)
	}
	seen := map[string]bool{}
	var visit func(string)
	var names []string
	visit = func(current string) {
		if seen[current] {
			return
		}
		seen[current] = true
		for _, dependency := range c.recipes[current].Dependencies {
			visit(dependency)
		}
		names = append(names, current)
	}
	visit(name)
	sort.Strings(names)
	out := make([]recipe, 0, len(names))
	for _, current := range names {
		out = append(out, c.recipes[current])
	}
	return out, nil
}

func (c *Catalog) validateDependencyCycles() error {
	const (
		unseen = iota
		visiting
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(string) error
	visit = func(name string) error {
		switch state[name] {
		case visiting:
			return fmt.Errorf("UI recipe dependency cycle: %s -> %s", strings.Join(stack, " -> "), name)
		case done:
			return nil
		}
		state[name] = visiting
		stack = append(stack, name)
		for _, dependency := range c.recipes[name].Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = done
		return nil
	}
	for name := range c.recipes {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func validateCatalogPath(kind, value string) error {
	if value == "" || len(value) > 240 || !utf8.ValidString(value) || strings.ContainsAny(value, "\\:<>\"|?*") || path.IsAbs(value) {
		return fmt.Errorf("invalid %s path %q", kind, value)
	}
	clean := path.Clean(value)
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("invalid %s path %q", kind, value)
	}
	for _, part := range strings.Split(value, "/") {
		if strings.TrimRight(part, " .") != part || len(part) > 100 {
			return fmt.Errorf("invalid %s path %q", kind, value)
		}
		for _, r := range part {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return fmt.Errorf("invalid %s path %q", kind, value)
			}
		}
		device := strings.ToUpper(strings.TrimRight(strings.SplitN(part, ".", 2)[0], " "))
		if device == "CON" || device == "PRN" || device == "AUX" || device == "NUL" || device == "CONIN$" || device == "CONOUT$" ||
			(len(device) == 4 && (strings.HasPrefix(device, "COM") || strings.HasPrefix(device, "LPT")) && strings.ContainsRune("123456789", rune(device[3]))) ||
			strings.HasPrefix(device, "COM") && strings.ContainsAny(strings.TrimPrefix(device, "COM"), "¹²³") ||
			strings.HasPrefix(device, "LPT") && strings.ContainsAny(strings.TrimPrefix(device, "LPT"), "¹²³") {
			return fmt.Errorf("invalid %s path %q", kind, value)
		}
	}
	return nil
}

// Both catalog and installed metadata contain exactly one JSON document.
func decodeDocument(data []byte, target any) error {
	// encoding/json otherwise accepts duplicate object members with last-value
	// wins semantics, which makes an ownership ledger ambiguous to reviewers.
	if err := uniqueJSONMembers(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON document")
	}
	return nil
}

func uniqueJSONMembers(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate or invalid JSON object member")
			}
			seen[name] = true
		}
		if err := uniqueJSONMembers(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func validateRecipeSource(name string, content []byte) error {
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return fmt.Errorf("source must be UTF-8 text without NUL bytes")
	}
	if len(content) == 0 || content[len(content)-1] != '\n' {
		return fmt.Errorf("source must end with a newline")
	}
	switch path.Ext(name) {
	case ".gsx":
		if _, err := gosx.Compile(content); err != nil {
			return fmt.Errorf("compile: %w", err)
		}
		formatted, err := gosxformat.Source(content)
		if err != nil {
			return fmt.Errorf("format: %w", err)
		}
		if !bytes.Equal(content, formatted) {
			return fmt.Errorf("source is not in canonical gosx fmt form")
		}
	case ".go":
		if _, err := parser.ParseFile(token.NewFileSet(), name, content, parser.AllErrors); err != nil {
			return fmt.Errorf("parse Go: %w", err)
		}
		formatted, err := goformat.Source(content)
		if err != nil {
			return fmt.Errorf("format Go: %w", err)
		}
		if !bytes.Equal(content, formatted) {
			return fmt.Errorf("source is not in canonical gofmt form")
		}
	case ".css":
		if err := validateCSS(content); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported source extension %q", path.Ext(name))
	}
	return nil
}

func validateCSS(content []byte) error {
	depth := 0
	inComment := false
	var quote byte
	for i := 0; i < len(content); i++ {
		current := content[i]
		if inComment {
			if current == '*' && i+1 < len(content) && content[i+1] == '/' {
				inComment = false
				i++
			}
			continue
		}
		if quote != 0 {
			if current == '\\' {
				i++
				continue
			}
			if current == quote {
				quote = 0
			}
			continue
		}
		if current == '/' && i+1 < len(content) && content[i+1] == '*' {
			inComment = true
			i++
			continue
		}
		if current == '\'' || current == '"' {
			quote = current
			continue
		}
		switch current {
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return fmt.Errorf("CSS has an unmatched closing brace")
			}
		}
	}
	if inComment || quote != 0 || depth != 0 {
		return fmt.Errorf("CSS has an unterminated comment, string, or block")
	}
	return nil
}

func firstDuplicate(values []string) string {
	for i := 1; i < len(values); i++ {
		if values[i] == values[i-1] {
			return values[i]
		}
	}
	return ""
}

package uirecipe

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const installedManifestPath = ".gosx/ui/manifest.json"

// AddOptions controls source installation. Update never overwrites a locally
// modified file; it only advances files that still match their installed hash.
type AddOptions struct {
	Update bool
}

// FileAction describes one deterministic add result row.
type FileAction struct {
	Path   string
	Action string
}

// AddResult summarizes one source installation.
type AddResult struct {
	Recipe  string
	Version string
	Files   []FileAction
}

type installedManifest struct {
	SchemaVersion  int               `json:"schemaVersion"`
	CatalogVersion string            `json:"catalogVersion"`
	Source         string            `json:"source"`
	License        string            `json:"license"`
	Provenance     string            `json:"provenance"`
	Recipes        []installedRecipe `json:"recipes"`
}

type installedRecipe struct {
	Name    string          `json:"name"`
	Version string          `json:"version"`
	Files   []installedFile `json:"files"`
}

type installedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type plannedFile struct {
	recipe recipe
	file   recipeFile
	path   string
	action string
}

// Add installs a recipe and its dependency closure into an application root.
func (c *Catalog) Add(root, name string, options AddOptions) (AddResult, error) {
	root, err := resolveAppRoot(root)
	if err != nil {
		return AddResult{}, err
	}
	closure, err := c.closure(name)
	if err != nil {
		return AddResult{}, err
	}
	installed, priorHashes, err := readInstalledManifest(root)
	if err != nil {
		return AddResult{}, err
	}

	plan := make([]plannedFile, 0)
	for _, item := range closure {
		for _, file := range item.files {
			target, err := secureTarget(root, file.Target)
			if err != nil {
				return AddResult{}, err
			}
			current, readErr := os.ReadFile(target)
			switch {
			case readErr == nil && bytes.Equal(current, file.Content):
				plan = append(plan, plannedFile{recipe: item, file: file, path: target, action: "unchanged"})
			case readErr == nil && !options.Update:
				return AddResult{}, sourceConflict(name, file.Target, "differs from the catalog")
			case readErr == nil && options.Update:
				prior, tracked := priorHashes[file.Target]
				if !tracked {
					return AddResult{}, sourceConflict(name, file.Target, "is not tracked by the installed manifest")
				}
				if contentHash(current) != prior {
					return AddResult{}, sourceConflict(name, file.Target, "was modified after installation")
				}
				plan = append(plan, plannedFile{recipe: item, file: file, path: target, action: "update"})
			case errors.Is(readErr, os.ErrNotExist):
				if options.Update {
					if _, tracked := priorHashes[file.Target]; tracked {
						return AddResult{}, sourceConflict(name, file.Target, "was removed after installation")
					}
				}
				plan = append(plan, plannedFile{recipe: item, file: file, path: target, action: "create"})
			default:
				return AddResult{}, fmt.Errorf("read %s: %w", file.Target, readErr)
			}
		}
	}

	lockPath, err := secureTarget(root, installedManifestPath)
	if err != nil {
		return AddResult{}, err
	}
	installed = c.mergeInstalledManifest(installed, closure)
	lockData, err := marshalInstalledManifest(installed)
	if err != nil {
		return AddResult{}, err
	}

	// Every read, ownership check, and symlink check above happens before the
	// first write. A conflict therefore cannot leave a partially added recipe.
	for _, file := range plan {
		if file.action == "unchanged" {
			continue
		}
		if err := writeOwnedFile(root, file.path, file.file.Content); err != nil {
			return AddResult{}, fmt.Errorf("%s %s: %w", file.action, file.file.Target, err)
		}
	}
	if err := writeOwnedFile(root, lockPath, lockData); err != nil {
		return AddResult{}, fmt.Errorf("write %s: %w", installedManifestPath, err)
	}

	selected := c.recipes[name]
	result := AddResult{Recipe: name, Version: selected.Version}
	for _, file := range plan {
		result.Files = append(result.Files, FileAction{Path: file.file.Target, Action: file.action})
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	return result, nil
}

func sourceConflict(recipeName, target, cause string) error {
	return fmt.Errorf("refusing to overwrite %s: %s; review with `gosx ui diff %s` (v1 has no force mode)", target, cause, recipeName)
}

func (c *Catalog) mergeInstalledManifest(current installedManifest, closure []recipe) installedManifest {
	byName := make(map[string]installedRecipe, len(current.Recipes)+len(closure))
	for _, item := range current.Recipes {
		byName[item.Name] = item
	}
	for _, item := range closure {
		entry := installedRecipe{Name: item.Name, Version: item.Version}
		for _, file := range item.files {
			entry.Files = append(entry.Files, installedFile{Path: file.Target, SHA256: file.SHA256})
		}
		sort.Slice(entry.Files, func(i, j int) bool { return entry.Files[i].Path < entry.Files[j].Path })
		byName[item.Name] = entry
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := installedManifest{
		SchemaVersion:  1,
		CatalogVersion: c.version,
		Source:         c.source,
		License:        c.license,
		Provenance:     c.provenance,
	}
	for _, name := range names {
		out.Recipes = append(out.Recipes, byName[name])
	}
	return out
}

func readInstalledManifest(root string) (installedManifest, map[string]string, error) {
	path, err := secureTarget(root, installedManifestPath)
	if err != nil {
		return installedManifest{}, nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return installedManifest{}, map[string]string{}, nil
	}
	if err != nil {
		return installedManifest{}, nil, fmt.Errorf("read %s: %w", installedManifestPath, err)
	}
	var manifest installedManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return installedManifest{}, nil, fmt.Errorf("decode %s: %w", installedManifestPath, err)
	}
	if manifest.SchemaVersion != 1 {
		return installedManifest{}, nil, fmt.Errorf("unsupported installed UI manifest schema %d", manifest.SchemaVersion)
	}
	hashes := map[string]string{}
	for _, item := range manifest.Recipes {
		if !recipeNamePattern.MatchString(item.Name) || strings.TrimSpace(item.Version) == "" {
			return installedManifest{}, nil, fmt.Errorf("invalid recipe entry in %s", installedManifestPath)
		}
		for _, file := range item.Files {
			if err := validateCatalogPath("installed", file.Path); err != nil {
				return installedManifest{}, nil, fmt.Errorf("%s: %w", installedManifestPath, err)
			}
			if len(file.SHA256) != sha256.Size*2 {
				return installedManifest{}, nil, fmt.Errorf("%s: invalid hash for %s", installedManifestPath, file.Path)
			}
			if _, err := hex.DecodeString(file.SHA256); err != nil {
				return installedManifest{}, nil, fmt.Errorf("%s: invalid hash for %s", installedManifestPath, file.Path)
			}
			if prior, duplicate := hashes[file.Path]; duplicate && prior != file.SHA256 {
				return installedManifest{}, nil, fmt.Errorf("%s: conflicting hashes for %s", installedManifestPath, file.Path)
			}
			hashes[file.Path] = file.SHA256
		}
	}
	return manifest, hashes, nil
}

func marshalInstalledManifest(manifest installedManifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode installed UI manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func resolveAppRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve application root: %w", err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve application root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("application root %s is not a directory", abs)
	}
	for _, required := range []string{"go.mod", "app"} {
		path := filepath.Join(abs, required)
		info, err := os.Lstat(path)
		if err != nil {
			return "", fmt.Errorf("application root %s is missing %s", abs, required)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("application root %s uses a symlink for %s", abs, required)
		}
		if required == "app" && !info.IsDir() {
			return "", fmt.Errorf("application root %s has non-directory app", abs)
		}
		if required == "go.mod" && !info.Mode().IsRegular() {
			return "", fmt.Errorf("application root %s has non-regular go.mod", abs)
		}
	}
	return abs, nil
}

func secureTarget(root, relative string) (string, error) {
	if err := validateCatalogPath("target", filepath.ToSlash(relative)); err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target %q escapes application root", relative)
	}
	current := root
	parts := strings.Split(filepath.FromSlash(relative), string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect target %q: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing target %q: %s is a symlink", relative, filepath.ToSlash(strings.Join(parts[:index+1], "/")))
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("refusing target %q: parent %s is not a directory", relative, current)
		}
		if index == len(parts)-1 && info.IsDir() {
			return "", fmt.Errorf("refusing target %q: destination is a directory", relative)
		}
	}
	return target, nil
}

func writeOwnedFile(root, target string, content []byte) error {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	directory := filepath.Dir(target)
	if err := makeSecureDirectories(root, directory); err != nil {
		return err
	}
	// Recheck after directory creation to narrow the preflight/write race and
	// refuse a symlink swapped into place before the atomic rename.
	if _, err := secureTarget(root, filepath.ToSlash(relative)); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".gosx-ui-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o644); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, target)
}

func makeSecureDirectories(root, directory string) error {
	rel, err := filepath.Rel(root, directory)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("directory %s escapes application root", directory)
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("refusing directory %s: not a real directory", current)
		}
	}
	return nil
}

func contentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

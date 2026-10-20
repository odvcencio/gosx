package uirecipe

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const installedManifestPath = ".gosx/ui/manifest.json"

// AddOptions controls source installation. Update never overwrites a locally
// modified file; it only advances files that still match their installed hash.
type AddOptions struct{ Update bool }

// FileAction describes one deterministic add result row.
type FileAction struct{ Path, Action string }

// AddResult summarizes one source installation.
type AddResult struct {
	Recipe, Version string
	Files           []FileAction
	Warnings        []string
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

// Add installs a recipe and its dependency closure into an application root.
func (c *Catalog) Add(root, name string, options AddOptions) (AddResult, error) {
	return c.add(root, name, options, nil)
}

func (c *Catalog) add(root, name string, options AddOptions, hook transactionHook) (AddResult, error) {
	closure, err := c.closure(name)
	if err != nil {
		return AddResult{}, err
	}
	app, err := openAppRoot(root)
	if err != nil {
		return AddResult{}, err
	}
	defer app.Close()
	metadata, err := openParent(app, installedManifestPath, true)
	if err != nil {
		return AddResult{}, err
	}
	defer metadata.Close()
	unlock, err := lockInstall(metadata)
	if err != nil {
		return AddResult{}, err
	}
	defer unlock()
	if _, err := metadata.Lstat("transaction.json"); err == nil {
		return AddResult{}, fmt.Errorf("unfinished UI transaction: inspect .gosx/ui/transaction.json and its recovery files before installing")
	} else if !errors.Is(err, os.ErrNotExist) {
		return AddResult{}, err
	}
	// Ownership and content are read only after the cross-process lock is held.
	installed, priorHashes, err := c.readInstalledManifest(app)
	if err != nil {
		return AddResult{}, err
	}
	tx := transaction{app: app, metadata: metadata, hook: hook}
	defer tx.close()
	result := AddResult{Recipe: name, Version: c.recipes[name].Version}
	for _, item := range closure {
		for _, file := range item.files {
			entry, err := tx.prepare(file.Target, file.Content)
			if err != nil {
				return AddResult{}, err
			}
			action := "create"
			switch {
			case entry.exists && bytes.Equal(entry.before, file.Content):
				action = "unchanged"
			case entry.exists && !options.Update:
				return AddResult{}, sourceConflict(name, file.Target, "differs from the catalog")
			case entry.exists:
				prior, tracked := priorHashes[file.Target]
				if !tracked {
					return AddResult{}, sourceConflict(name, file.Target, "is not tracked by the installed manifest")
				}
				if contentHash(entry.before) != prior {
					return AddResult{}, sourceConflict(name, file.Target, "was modified after installation")
				}
				action = "update"
			case options.Update:
				if _, tracked := priorHashes[file.Target]; tracked {
					return AddResult{}, sourceConflict(name, file.Target, "was removed after installation")
				}
			}
			entry.changed = action != "unchanged"
			result.Files = append(result.Files, FileAction{Path: file.Target, Action: action})
		}
	}
	data, err := marshalInstalledManifest(c.mergeInstalledManifest(installed, closure))
	if err != nil {
		return AddResult{}, err
	}
	entry, err := tx.prepare(installedManifestPath, data)
	if err != nil {
		return AddResult{}, err
	}
	entry.changed = !entry.exists || !bytes.Equal(entry.before, data)
	result.Warnings, err = tx.commit()
	if err != nil {
		return AddResult{}, err
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
	out := installedManifest{SchemaVersion: 1, CatalogVersion: c.version, Source: c.source, License: c.license, Provenance: c.provenance}
	for _, name := range names {
		out.Recipes = append(out.Recipes, byName[name])
	}
	return out
}

func (c *Catalog) readInstalledManifest(root *os.Root) (installedManifest, map[string]string, error) {
	data, err := readRootFile(root, installedManifestPath, maxSourceBytes)
	if errors.Is(err, os.ErrNotExist) {
		return installedManifest{}, map[string]string{}, nil
	}
	if err != nil {
		return installedManifest{}, nil, fmt.Errorf("read %s: %w", installedManifestPath, err)
	}
	var manifest installedManifest
	if err := decodeDocument(data, &manifest); err != nil {
		return installedManifest{}, nil, fmt.Errorf("decode %s: %w", installedManifestPath, err)
	}
	if err := c.validateInstalledManifest(manifest); err != nil {
		return installedManifest{}, nil, err
	}
	hashes := map[string]string{}
	for _, item := range manifest.Recipes {
		for _, file := range item.Files {
			hashes[file.Path] = file.SHA256
		}
	}
	return manifest, hashes, nil
}

func (c *Catalog) validateInstalledManifest(manifest installedManifest) error {
	bad := func(cause string) error { return fmt.Errorf("invalid %s: %s", installedManifestPath, cause) }
	if manifest.SchemaVersion != 1 {
		return bad("unsupported schema")
	}
	if manifest.Source != c.source || manifest.License != c.license || manifest.Provenance != c.provenance {
		return bad("unknown source, license, or provenance")
	}
	if !supportedVersion(manifest.CatalogVersion, c.version) {
		return bad("unsupported catalog version")
	}
	if len(manifest.Recipes) == 0 || len(manifest.Recipes) > len(c.recipes) {
		return bad("invalid recipe count")
	}
	seen := map[string]bool{}
	paths := map[string]bool{}
	for _, item := range manifest.Recipes {
		known, ok := c.recipes[item.Name]
		if !ok || seen[item.Name] {
			return bad("unknown or duplicate recipe")
		}
		seen[item.Name] = true
		if !supportedVersion(item.Version, known.Version) {
			return bad("unsupported recipe version")
		}
		// Older versions may be a subset of the current recipe's owned paths;
		// this supports additive upgrades. Renamed/removed paths need an explicit
		// future migration, never silently transferring ownership to another recipe.
		owned := map[string]string{}
		for _, file := range known.files {
			owned[file.Target] = file.SHA256
		}
		if len(item.Files) == 0 || len(item.Files) > len(owned) {
			return bad("invalid recipe file count")
		}
		for _, file := range item.Files {
			if err := validateCatalogPath("installed", file.Path); err != nil {
				return err
			}
			knownHash, ok := owned[file.Path]
			if !ok || paths[file.Path] {
				return bad("unknown or duplicate recipe file")
			}
			paths[file.Path] = true
			if len(file.SHA256) != sha256.Size*2 || strings.ToLower(file.SHA256) != file.SHA256 {
				return bad("invalid file hash")
			}
			if _, err := hex.DecodeString(file.SHA256); err != nil {
				return bad("invalid file hash")
			}
			if item.Version == known.Version && file.SHA256 != knownHash {
				return bad("hash does not match the declared recipe version")
			}
		}
		if item.Version == known.Version && len(item.Files) != len(owned) {
			return bad("missing file for declared recipe version")
		}
	}
	for _, item := range manifest.Recipes {
		for _, dependency := range c.recipes[item.Name].Dependencies {
			if !seen[dependency] {
				return bad("missing recipe dependency")
			}
		}
	}
	return nil
}

var releaseVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)

// v1 accepts released versions from 1.0.0 through this CLI's version, within
// the same major. Metadata is an ownership ledger, not a signature of old code.
func supportedVersion(installed, current string) bool {
	if !releaseVersionPattern.MatchString(installed) || !releaseVersionPattern.MatchString(current) {
		return false
	}
	a, b := strings.Split(installed, "."), strings.Split(current, ".")
	if a[0] != b[0] || a[0] == "0" {
		return false
	}
	for i := range a {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if x != y {
			return x < y
		}
	}
	return true
}

func marshalInstalledManifest(manifest installedManifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode installed UI manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func contentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

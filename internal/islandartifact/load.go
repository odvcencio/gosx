// Package islandartifact loads the build's verified island program for server
// rendering, so production does not need compiler tools or dependency sources.
package islandartifact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/filepolicy"
	"m31labs.dev/gosx/island/program"
)

// Load returns a matching build artifact only when its manifest identifies the
// same source file and both source and artifact hashes match. Missing or older
// manifests without source evidence leave the caller free to compile locally.
func Load(sourceFile, name string) (*program.Program, bool, error) {
	if sourceFile == "" || name == "" || os.Getenv("GOSX_DEV") == "1" {
		return nil, false, nil
	}
	source, err := filepath.Abs(sourceFile)
	if err != nil {
		return nil, false, err
	}
	for parent := filepath.Dir(source); ; parent = filepath.Dir(parent) {
		for _, root := range []string{parent, filepath.Join(parent, "dist")} {
			manifestPath := filepath.Join(root, "build.json")
			if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return nil, false, err
			}
			manifest, err := buildmanifest.Load(manifestPath)
			if err != nil {
				continue // An unrelated ancestor may contain its own build.json.
			}
			asset, ok := manifest.IslandAssetByName(name)
			if !ok || asset.SourceFile == "" || asset.SourceHash == "" {
				continue
			}
			rel := filepath.FromSlash(asset.SourceFile)
			if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			insideBuild := source == filepath.Clean(filepath.Join(root, rel))
			besideBuild := source == filepath.Clean(filepath.Join(filepath.Dir(root), rel))
			if !insideBuild && !besideBuild {
				continue
			}
			appRoot, _ := filepath.Abs(os.Getenv("GOSX_APP_ROOT"))
			if !filepolicy.Immutable() && (!insideBuild || os.Getenv("GOSX_APP_ROOT") == "" || filepath.Clean(appRoot) != filepath.Clean(root)) {
				continue
			}
			body, err := os.ReadFile(source)
			if err != nil || buildmanifest.ContentHash(body) != asset.SourceHash {
				return nil, true, fmt.Errorf("island %s source differs from its build; rebuild the application", name)
			}
			if asset.File == "" || filepath.Base(asset.File) != asset.File || strings.ContainsAny(asset.File, `/\\`) {
				return nil, true, fmt.Errorf("island %s has an invalid artifact path", name)
			}
			path := filepath.Join(root, "assets", "islands", asset.File)
			resolvedRoot, rootErr := filepath.EvalSymlinks(root)
			resolvedPath, pathErr := filepath.EvalSymlinks(path)
			if rootErr != nil || pathErr != nil {
				return nil, true, fmt.Errorf("island %s artifact cannot be resolved", name)
			}
			relPath, relErr := filepath.Rel(resolvedRoot, resolvedPath)
			if relErr != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) || filepath.IsAbs(relPath) {
				return nil, true, fmt.Errorf("island %s artifact escapes the build directory", name)
			}
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
				return nil, true, fmt.Errorf("island %s artifact is missing or not a regular file", name)
			}
			data, err := os.ReadFile(path)
			if err != nil || asset.Hash == "" || buildmanifest.ContentHash(data) != asset.Hash || asset.Size > 0 && int64(len(data)) != asset.Size || asset.Integrity != "" && buildmanifest.ContentIntegrity(data) != asset.Integrity {
				return nil, true, fmt.Errorf("island %s artifact failed its build hash check", name)
			}
			var result *program.Program
			switch asset.Format {
			case "bin":
				result, err = program.DecodeBinary(data)
			case "json":
				result, err = program.DecodeProgramJSON(data)
			default:
				return nil, true, fmt.Errorf("island %s has unsupported artifact format %q", name, asset.Format)
			}
			if err != nil || result.Name != name {
				return nil, true, fmt.Errorf("island %s artifact cannot be decoded as its declared program", name)
			}
			result.Surface = program.SurfaceDOM
			return result, true, nil
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	return nil, false, nil
}

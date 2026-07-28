package inspect

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	sceneschema "m31labs.dev/gosx/scene/schema"
)

// This file answers a question the loop could not answer before: will this
// scene find its assets? A texture path that resolves on the author's machine
// and nowhere else used to fail at runtime, in a browser, with a network error.
// Resolution runs here against real directories on disk.

// AssetReference names one place in the scene document that asked for a source.
type AssetReference struct {
	ID string `json:"id,omitempty"`
	// Path is the document location, for example "objects[2].texture".
	Path string `json:"path"`
}

// AssetResolution records whether one source was found on disk.
type AssetResolution struct {
	Src string `json:"src"`
	// Remote is true for sources that name a network or inline payload. They
	// are reported, not resolved, because no directory can answer for them.
	Remote bool `json:"remote,omitempty"`
	// Resolved is true when a file was found. It stays false for Remote
	// sources, which the report never counts as missing either.
	Resolved bool `json:"resolved"`
	// File is the path of the file that satisfied the source.
	File string `json:"file,omitempty"`
	// Bytes is the size of that file, read from the filesystem.
	Bytes int64 `json:"bytes,omitempty"`
	// Tried lists the candidate paths that were checked and did not exist.
	Tried      []string         `json:"tried,omitempty"`
	References []AssetReference `json:"references"`
}

// AssetResolutionReport summarizes asset reachability for one scene.
type AssetResolutionReport struct {
	// Checked is false when no asset root was supplied. A report that did not
	// look must not read as a report that found everything.
	Checked    bool              `json:"checked"`
	Roots      []string          `json:"roots,omitempty"`
	Sources    int               `json:"sources"`
	Resolved   int               `json:"resolved"`
	Unresolved int               `json:"unresolved"`
	Remote     int               `json:"remote"`
	Assets     []AssetResolution `json:"assets,omitempty"`
}

type assetReferenceIndex struct {
	order      []string
	references map[string][]AssetReference
}

func newAssetReferenceIndex() *assetReferenceIndex {
	return &assetReferenceIndex{references: map[string][]AssetReference{}}
}

func (index *assetReferenceIndex) add(src, id, path string) {
	src = strings.TrimSpace(src)
	if src == "" || strings.HasPrefix(src, "var(") {
		return
	}
	if _, seen := index.references[src]; !seen {
		index.order = append(index.order, src)
	}
	index.references[src] = append(index.references[src], AssetReference{ID: id, Path: path})
}

// resolveAssets checks every referenced source against the supplied roots and
// records what it found. It never guesses: a source is resolved only when a
// file exists.
func resolveAssets(index *assetReferenceIndex, roots []string) AssetResolutionReport {
	report := AssetResolutionReport{Sources: len(index.order)}
	cleanRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		if trimmed := strings.TrimSpace(root); trimmed != "" {
			cleanRoots = append(cleanRoots, filepath.Clean(trimmed))
		}
	}
	sort.Strings(cleanRoots)
	report.Roots = cleanRoots
	report.Checked = len(cleanRoots) > 0

	sources := append([]string(nil), index.order...)
	sort.Strings(sources)
	for _, src := range sources {
		resolution := AssetResolution{Src: src, References: index.references[src]}
		sort.SliceStable(resolution.References, func(i, j int) bool {
			return resolution.References[i].Path < resolution.References[j].Path
		})
		if isRemoteSource(src) {
			resolution.Remote = true
			report.Remote++
			report.Assets = append(report.Assets, resolution)
			continue
		}
		if !report.Checked {
			report.Assets = append(report.Assets, resolution)
			continue
		}
		for _, candidate := range candidatePaths(src, cleanRoots) {
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() {
				resolution.Resolved = true
				resolution.File = candidate
				resolution.Bytes = info.Size()
				break
			}
			resolution.Tried = append(resolution.Tried, candidate)
		}
		if resolution.Resolved {
			resolution.Tried = nil
			report.Resolved++
		} else {
			report.Unresolved++
		}
		report.Assets = append(report.Assets, resolution)
	}
	return report
}

// candidatePaths lists every filesystem location a source could name, in a
// stable order. Absolute-looking sources are web-root paths, so they join each
// root rather than reading from the machine root.
func candidatePaths(src string, roots []string) []string {
	clean := stripQuery(src)
	clean = strings.TrimPrefix(clean, "./")
	trimmed := strings.TrimPrefix(clean, "/")
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		out = append(out, filepath.Join(root, filepath.FromSlash(trimmed)))
	}
	return out
}

func isRemoteSource(src string) bool {
	lowered := strings.ToLower(src)
	for _, prefix := range []string{"http://", "https://", "data:", "blob:", "//"} {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return false
}

// appendUnresolvedAssetDiagnostics turns each missing file into a validation
// error that names the record, the document path, and the source it wanted.
func appendUnresolvedAssetDiagnostics(validation *sceneschema.Report, resolution AssetResolutionReport) {
	if !resolution.Checked {
		return
	}
	for _, asset := range resolution.Assets {
		if asset.Resolved || asset.Remote {
			continue
		}
		for _, reference := range asset.References {
			validation.Diagnostics = append(validation.Diagnostics, sceneschema.Diagnostic{
				Severity: sceneschema.Error,
				Code:     "scene.asset.unresolved",
				Message:  "Scene asset " + asset.Src + " was not found under any asset root",
				Path:     reference.Path,
				ID:       reference.ID,
				Data: map[string]any{
					"src":   asset.Src,
					"roots": resolution.Roots,
					"tried": asset.Tried,
				},
			})
		}
		validation.Valid = false
	}
}

package budget

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

type producerPublicFile struct {
	id, kind, source, url, target string
}

// preflightProducerPaths runs before the first snapshot write. It reserves
// production inputs and every destination, including encoding cleanup and
// temporary writes, so later copies cannot change previously verified bodies.
func preflightProducerPaths(root *os.Root, opts ProducerOptions, routes []FixtureRoute, public []producerPublicFile) error {
	fail := func() error { return &InputError{Code: "wrong-fixture", Reference: "producer", Pointer: "/outputs"} }
	reserved := []string{
		"app", "content", "public", "static", "server", "edge", "platform", "offline", "msix",
		"assets/runtime", "assets/islands", "assets/css", "assets/images",
	}
	for _, file := range []string{
		"build.json", "export.json", "scene-assets.json", "gosx-grammar.blob", "bundle-policy.json",
		"run.sh", "README.md", "app.msix", "app.appinstaller",
	} {
		reserved = append(reserved, file, file+".new")
		for _, sidecar := range fixtureSidecars {
			reserved = append(reserved, file+sidecar.suffix)
		}
	}
	for _, use := range opts.Build.PerfAssetUses.Assets {
		file, err := fixtureFilePath(use.URL, use.Kind)
		if err != nil {
			return fail()
		}
		reserved = append(reserved, file, file+".new")
		for _, sidecar := range fixtureSidecars {
			reserved = append(reserved, file+sidecar.suffix)
		}
	}
	// Bound configuration, font and source files can live inside the root too.
	dist, err := filepath.EvalSymlinks(opts.DistDir)
	if err != nil {
		return fail()
	}
	dist, err = filepath.Abs(dist)
	if err != nil {
		return fail()
	}
	refs := []Ref{opts.Inputs.File.Profile, opts.Inputs.File.Coefficients, opts.Inputs.File.Toolchain, opts.Inputs.File.Fixtures}
	refs = append(refs, opts.Inputs.Toolchain.Fonts...)
	for _, route := range routes {
		refs = append(refs, Ref{File: route.SourcePath})
	}
	for _, ref := range refs {
		input, err := filepath.Abs(filepath.Join(opts.Inputs.RootDir(), filepath.FromSlash(ref.File)))
		if err != nil {
			return fail()
		}
		resolved, err := filepath.EvalSymlinks(input)
		if err == nil {
			input = resolved
		} else if !os.IsNotExist(err) {
			return fail()
		}
		if !strings.EqualFold(filepath.VolumeName(dist), filepath.VolumeName(input)) {
			continue
		}
		relative, err := filepath.Rel(dist, input)
		if err != nil {
			return fail()
		}
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			reserved = append(reserved, filepath.ToSlash(relative))
		}
	}
	if opts.Build.SceneAssets != nil && opts.Build.SceneAssets.File != "" {
		if !safePath(opts.Build.SceneAssets.File) {
			return fail()
		}
		reserved = append(reserved, opts.Build.SceneAssets.File)
	}
	for _, entry := range public {
		reserved = append(reserved, entry.source)
	}
	outputs := []string{fixtureManifestFile}
	for _, entry := range public {
		outputs = append(outputs, entry.target)
	}
	for _, route := range routes {
		if !validRoute(route.RouteTemplate) || strings.ContainsAny(route.RouteTemplate, "[]") {
			return fail()
		}
		file, err := fixtureFilePath(route.RouteTemplate, "html")
		if err != nil {
			return fail()
		}
		outputs = append(outputs, file)
	}
	// Each body write replaces the body and discards old encodings. Encoding
	// writes only replace the encoding; they do not remove further sidecars.
	writes := []string{}
	for _, file := range outputs {
		writes = append(writes, file, file+".new")
		for _, sidecar := range fixtureSidecars {
			writes = append(writes, file+sidecar.suffix, file+sidecar.suffix+".new")
		}
	}
	for _, file := range writes {
		if !safePath(file) {
			return fail()
		}
		for _, input := range reserved {
			if producerPathsOverlap(file, input) {
				return fail()
			}
		}
		// Confined symlinks can still alias a verified input inside the root.
		for parent := file; parent != "."; parent = path.Dir(parent) {
			info, err := root.Lstat(parent)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || info.Mode()&os.ModeSymlink != 0 || parent != file && !info.IsDir() || parent == file && !info.Mode().IsRegular() {
				return fail()
			}
		}
	}
	occupied := map[string]bool{}
	for _, file := range writes {
		key := strings.ToLower(file)
		if occupied[key] {
			return fail()
		}
		occupied[key] = true
	}
	for file := range occupied {
		for parent := path.Dir(file); parent != "."; parent = path.Dir(parent) {
			if occupied[parent] {
				return fail()
			}
		}
	}
	return nil
}

func producerPathsOverlap(a, b string) bool {
	// Case folding conservatively covers distributions on case-insensitive
	// filesystems as well as the case-sensitive build host.
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

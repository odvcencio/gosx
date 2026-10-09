package budget

import (
	"io/fs"
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
func preflightProducerPaths(root *os.Root, opts ProducerOptions, routes []FixtureRoute, public []producerPublicFile) (*producerPathProtection, error) {
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
			return nil, fail()
		}
		reserved = append(reserved, file, file+".new")
		for _, sidecar := range fixtureSidecars {
			reserved = append(reserved, file+sidecar.suffix)
		}
	}
	// Bound configuration, font and source files can live inside the root too.
	dist, err := filepath.EvalSymlinks(opts.DistDir)
	if err != nil {
		return nil, fail()
	}
	dist, err = filepath.Abs(dist)
	if err != nil {
		return nil, fail()
	}
	identities := []os.FileInfo{}
	refs := []Ref{opts.Inputs.File.Profile, opts.Inputs.File.Coefficients, opts.Inputs.File.Toolchain, opts.Inputs.File.Fixtures}
	refs = append(refs, opts.Inputs.Toolchain.Fonts...)
	for _, route := range routes {
		refs = append(refs, Ref{File: route.SourcePath})
	}
	for _, ref := range refs {
		input, err := filepath.Abs(filepath.Join(opts.Inputs.RootDir(), filepath.FromSlash(ref.File)))
		if err != nil {
			return nil, fail()
		}
		resolved, err := filepath.EvalSymlinks(input)
		if err == nil {
			input = resolved
		} else if !os.IsNotExist(err) {
			return nil, fail()
		}
		info, err := os.Stat(input)
		if err == nil {
			if !info.Mode().IsRegular() {
				return nil, fail()
			}
			identities = append(identities, info)
		} else if !os.IsNotExist(err) {
			return nil, fail()
		}
		if !strings.EqualFold(filepath.VolumeName(dist), filepath.VolumeName(input)) {
			continue
		}
		relative, err := filepath.Rel(dist, input)
		if err != nil {
			return nil, fail()
		}
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			reserved = append(reserved, filepath.ToSlash(relative))
		}
	}
	if opts.Build.SceneAssets != nil && opts.Build.SceneAssets.File != "" {
		if !safePath(opts.Build.SceneAssets.File) {
			return nil, fail()
		}
		reserved = append(reserved, opts.Build.SceneAssets.File)
	}
	for _, entry := range public {
		reserved = append(reserved, entry.source)
	}
	protection := &producerPathProtection{paths: reserved, identities: identities}
	for _, input := range reserved {
		if err := protection.reserve(root, input); err != nil {
			return nil, err
		}
	}
	outputs := []string{fixtureManifestFile}
	for _, entry := range public {
		outputs = append(outputs, entry.target)
	}
	for _, route := range routes {
		if !validRoute(route.RouteTemplate) || strings.ContainsAny(route.RouteTemplate, "[]") {
			return nil, fail()
		}
		file, err := fixtureFilePath(route.RouteTemplate, "html")
		if err != nil {
			return nil, fail()
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
		if err := protection.check(root, file); err != nil {
			return nil, err
		}
	}
	occupied := map[string]bool{}
	for _, file := range writes {
		key := strings.ToLower(file)
		if occupied[key] {
			return nil, fail()
		}
		occupied[key] = true
	}
	for file := range occupied {
		for parent := path.Dir(file); parent != "."; parent = path.Dir(parent) {
			if occupied[parent] {
				return nil, fail()
			}
		}
	}
	return protection, nil
}

func producerPathsOverlap(a, b string) bool {
	// Case folding conservatively covers distributions on case-insensitive
	// filesystems as well as the case-sensitive build host.
	a, b = strings.ToLower(a), strings.ToLower(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// File identity is independent of a path's spelling: hard links share it.
// Input and destination symlinks are rejected, including directory aliases.
type producerPathProtection struct {
	paths      []string
	identities []os.FileInfo
}

func producerOutputFailure() error {
	return &InputError{Code: "wrong-fixture", Reference: "producer", Pointer: "/outputs"}
}

func (p *producerPathProtection) reserve(root *os.Root, input string) error {
	if err := producerCheckParents(root, input, false); err != nil {
		return err
	}
	_, err := root.Lstat(input)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return producerOutputFailure()
	}
	return fs.WalkDir(root.FS(), input, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.Type()&os.ModeSymlink != 0 {
			return producerOutputFailure()
		}
		info, err := entry.Info()
		if err != nil {
			return producerOutputFailure()
		}
		if info.Mode().IsRegular() {
			p.identities = append(p.identities, info)
		} else if !info.IsDir() {
			return producerOutputFailure()
		}
		return nil
	})
}

func (p *producerPathProtection) check(root *os.Root, file string) error {
	if !safePath(file) {
		return producerOutputFailure()
	}
	for _, input := range p.paths {
		if producerPathsOverlap(file, input) {
			return producerOutputFailure()
		}
	}
	if err := producerCheckParents(root, file, true); err != nil {
		return err
	}
	info, err := root.Stat(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return producerOutputFailure()
	}
	for _, input := range p.identities {
		if os.SameFile(info, input) {
			return producerOutputFailure()
		}
	}
	return nil
}

func producerCheckParents(root *os.Root, file string, output bool) error {
	for parent := file; parent != "."; parent = path.Dir(parent) {
		info, err := root.Lstat(parent)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || parent != file && !info.IsDir() || parent == file && output && !info.Mode().IsRegular() {
			return producerOutputFailure()
		}
	}
	return nil
}

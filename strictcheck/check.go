// Package strictcheck performs package-aware Go type checking for strict .gsx
// components by projecting only renderer-supported declarations to ordinary Go.
package strictcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/transpile"
)

// Options controls the Go command environment. Empty fields preserve the
// caller's process environment; build/dev pass their dependency-resolution
// environment explicitly while interactive check/render remain workspace-aware.
type Options struct {
	Env     []string
	GOWORK  string
	GOFLAGS string

	// ExtraLints registers third-party, per-file lints that run alongside
	// strictcheck's own checks and report through the same error returned by
	// CheckFileWithOptions/CheckPackageWithOptions/CheckTreeWithOptions.
	//
	// EXPERIMENTAL (gosx#186): see the Lint doc comment for the full
	// compatibility posture. A nil or empty ExtraLints (including the zero
	// value of Options) leaves check behavior byte-for-byte unchanged.
	//
	// The returned error takes one of two shapes once ExtraLints is
	// non-empty (gosx#186 M3):
	//   - a bare *ir.DiagnosticsError when only ExtraLints findings exist
	//     (every built-in stage passed) -- errors.As(err, &diagErr) reaches
	//     it directly, same as any other strictcheck.DiagnosticsError;
	//   - errors.Join(builtinErr, extraErr) when a built-in stage also
	//     failed. builtinErr (a strict-syntax or Go-compiler error) is never
	//     itself an *ir.DiagnosticsError, so errors.As(err, &diagErr) on the
	//     joined error still succeeds -- it finds extraErr, the only
	//     DiagnosticsError present -- but that recovers only the ExtraLints
	//     findings; builtinErr's text is present in err.Error() but not
	//     reachable through that structured type. A caller that special-
	//     cases *ir.DiagnosticsError (gosx's own lsp package does, for
	//     example) must call err.Error() as a fallback rather than assume a
	//     successful errors.As means the diagnostics it got back are
	//     complete. gosx's lsp package does not feed it ExtraLints results
	//     today, so it is unaffected; this note is for a consumer that does.
	ExtraLints []Lint
}

// CheckFile checks the complete .gsx package containing path.
func CheckFile(ctx context.Context, path string) error {
	return CheckFileWithOptions(ctx, path, Options{})
}

// CheckFileWithOptions checks the package containing path with an explicit Go
// command environment.
func CheckFileWithOptions(ctx context.Context, path string, opts Options) error {
	files, err := transpile.LoadPackage(path)
	if err != nil {
		return err
	}
	return checkPackage(ctx, files, opts)
}

// CheckPackage checks every distinct .gsx package found directly in dir.
func CheckPackage(ctx context.Context, dir string) error {
	return CheckPackageWithOptions(ctx, dir, Options{})
}

// CheckPackageWithOptions checks a package directory with an explicit Go
// command environment.
func CheckPackageWithOptions(ctx context.Context, dir string, opts Options) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".gsx") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	return checkSourcePackages(ctx, paths, opts)
}

// checkPackage runs every built-in strictcheck stage over files, then runs
// every registered extra lint (Options.ExtraLints), and joins their
// findings. The order is load-bearing (gosx#186 B1/M1): runBuiltinChecks
// runs to completion -- success or failure, through every stage that reads
// file.Program -- before runExtraLints is called at all, so a lint can never
// observe a *ir.Program that a built-in stage has not already finished
// reading. That is what makes the compatibility claim in Lint.Check's doc
// (a lint only adds diagnostics; it cannot suppress or alter a built-in
// finding) structurally true rather than merely intended: a lint that
// mutates or empties file.Program now runs strictly after every built-in
// read of it, whatever else it does.
//
// Extra lints run over every file regardless of strict syntax and regardless
// of whether a built-in stage failed: a consumer's per-file catalog
// (gosx#186) targets ordinary legacy-syntax .gsx files too, not just strict
// components, and a package that fails render-entry validation, import
// resolution, or transpilation still has a fully compiled *ir.Program for
// every file a lint can check (see LintFile.Program). Their findings join
// into every return path, including every one of those early-error paths.
func checkPackage(ctx context.Context, files []transpile.PackageFile, opts Options) error {
	builtinErr := runBuiltinChecks(ctx, files, opts)
	extraErr := runExtraLints(files, opts.ExtraLints)
	switch {
	case builtinErr != nil && extraErr != nil:
		return errors.Join(builtinErr, extraErr)
	case builtinErr != nil:
		return builtinErr
	default:
		return extraErr
	}
}

// runBuiltinChecks runs every built-in strictcheck stage over files: strict
// render-entry validation, import-name resolution, IR-to-Go projection, and
// the Go compiler pass over that projection. It returns the first failing
// stage's error, or nil once every stage passes (or is skipped because the
// package has no strict syntax). No extra lint (Options.ExtraLints) runs
// inside this function; see checkPackage for why that ordering matters.
func runBuiltinChecks(ctx context.Context, files []transpile.PackageFile, opts Options) error {
	if err := validateStrictRenderEntries(files); err != nil {
		return err
	}
	if !packageHasStrict(files) {
		return nil
	}
	importNames, err := resolveImportNames(ctx, files, opts)
	if err != nil {
		return err
	}
	generated, err := transpile.TranspilePackageWithImportNames(files, importNames)
	if err != nil {
		return err
	}
	if len(generated) == 0 {
		return nil
	}
	return goCheck(ctx, files, generated, opts)
}

func validateStrictRenderEntries(files []transpile.PackageFile) error {
	for _, file := range files {
		if file.Program == nil || len(file.Program.Components) == 0 {
			continue
		}
		if !strictFileRouteName(file.Path) {
			continue
		}
		components := file.Program.Components
		preferred := []string{"Page"}
		if strings.EqualFold(strings.TrimSuffix(filepath.Base(file.Path), filepath.Ext(file.Path)), "layout") {
			preferred = []string{"Layout", "Page"}
		}
		entry := &components[0]
		for _, name := range preferred {
			found := false
			for i := range components {
				if components[i].Name == name {
					entry = &components[i]
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if entry.Syntax == ir.ComponentSyntaxStrict && strings.TrimSpace(entry.PropsType) != "" {
			return fmt.Errorf("%s: strict render entry %s accepts props %s, but file routes do not bind root props; use a zero-props Page/Layout entry", file.Path, entry.Name, entry.PropsType)
		}
	}
	return nil
}

func strictFileRouteName(path string) bool {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	switch name {
	case "page", "index", "layout", "not-found", "error":
		return true
	default:
		return false
	}
}

func packageHasStrict(files []transpile.PackageFile) bool {
	for _, file := range files {
		if file.Program == nil {
			continue
		}
		for _, component := range file.Program.Components {
			if component.Syntax == ir.ComponentSyntaxStrict {
				return true
			}
		}
	}
	return false
}

func resolveImportNames(ctx context.Context, files []transpile.PackageFile, opts Options) (map[string]string, error) {
	imports := transpile.UnaliasedImportPaths(files)
	if len(imports) == 0 || len(files) == 0 {
		return nil, nil
	}
	args := []string{"list", "-e", "-find", "-buildvcs=false", "-f={{if not .Error}}{{.ImportPath}}{{\"\\t\"}}{{.Name}}{{end}}"}
	if !hasModuleMode(opts.GOFLAGS) {
		args = append(args, "-mod=readonly")
	}
	args = append(args, imports...)
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = filepath.Dir(files[0].Path)
	cmd.Env = commandEnv(opts)
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// With -e, unresolved legacy-only imports produce empty template output
		// rather than failing. Any remaining process failure is environmental and
		// should not be hidden behind a later undefined-selector diagnostic.
		return nil, fmt.Errorf("resolve strict import names: %w", err)
	}
	names := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(fields) == 2 && fields[0] != "" && fields[1] != "" {
			names[fields[0]] = fields[1]
		}
	}
	return names, nil
}

func goCheck(ctx context.Context, files []transpile.PackageFile, generated map[string]string, opts Options) error {
	if len(files) == 0 || len(generated) == 0 {
		return nil
	}
	dir := filepath.Dir(files[0].Path)
	tempDir, err := os.MkdirTemp("", "gosx-strictcheck-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	sourcePaths := make([]string, 0, len(generated))
	for sourcePath := range generated {
		sourcePaths = append(sourcePaths, sourcePath)
	}
	sort.Strings(sourcePaths)
	overlay := make(map[string]string, len(sourcePaths))
	usedVirtual := make(map[string]struct{}, len(sourcePaths))
	for i, sourcePath := range sourcePaths {
		tempPath := filepath.Join(tempDir, "projection_"+strconv.Itoa(i)+".go")
		if err := os.WriteFile(tempPath, []byte(generated[sourcePath]), 0o600); err != nil {
			return err
		}
		virtual := uniqueVirtualGoPath(dir, i, usedVirtual)
		usedVirtual[virtual] = struct{}{}
		overlay[virtual] = tempPath
	}
	overlayPath := filepath.Join(tempDir, "overlay.json")
	data, err := json.Marshal(struct {
		Replace map[string]string `json:"Replace"`
	}{Replace: overlay})
	if err != nil {
		return err
	}
	if err := os.WriteFile(overlayPath, data, 0o600); err != nil {
		return err
	}

	args := []string{"list", "-export", "-buildvcs=false", "-overlay", overlayPath}
	if !hasModuleMode(opts.GOFLAGS) {
		args = append(args, "-mod=readonly")
	}
	args = append(args, ".")
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = commandEnv(opts)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("strict Go type check: %s", message)
	}
	return nil
}

func uniqueVirtualGoPath(dir string, index int, used map[string]struct{}) string {
	for suffix := index; ; suffix++ {
		candidate := filepath.Join(dir, "zz_gosx_strictcheck_"+strconv.Itoa(suffix)+".go")
		_, alreadyUsed := used[candidate]
		if _, err := os.Stat(candidate); !alreadyUsed && os.IsNotExist(err) {
			return candidate
		}
	}
}

func hasModuleMode(goFlags string) bool {
	for _, field := range strings.Fields(goFlags) {
		if strings.HasPrefix(field, "-mod=") {
			return true
		}
	}
	return false
}

func commandEnv(opts Options) []string {
	base := opts.Env
	if base == nil {
		base = os.Environ()
	}
	env := append([]string(nil), base...)
	if opts.GOWORK != "" {
		env = setEnv(env, "GOWORK", opts.GOWORK)
	}
	if opts.GOFLAGS != "" {
		env = setEnv(env, "GOFLAGS", opts.GOFLAGS)
	}
	return env
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := env[:0]
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}

// CheckTree checks every package containing .gsx files below root.
func CheckTree(ctx context.Context, root string) error {
	return CheckTreeWithOptions(ctx, root, Options{})
}

// CheckTreeWithOptions checks a tree with an explicit Go command environment.
// Generated/dependency/fixture/dot/nested-Git trees are skipped. Route segment
// directories beginning with one or two underscores remain first-class.
func CheckTreeWithOptions(ctx context.Context, root string, opts Options) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	var sources []string
	err = filepath.WalkDir(abs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if shouldSkipDir(abs, path, entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".gsx") {
			sources = append(sources, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return checkSourcePackages(ctx, sources, opts)
}

// checkSourcePackages checks every distinct package found among paths and
// accumulates every failure rather than stopping at the first (gosx#186 B3):
// a lint finding is a package error like any other now, and CheckTree /
// CheckPackage covering several offending packages must surface all of
// them, not just whichever sorts first.
func checkSourcePackages(ctx context.Context, paths []string, opts Options) error {
	sort.Strings(paths)
	seen := make(map[string]struct{})
	var errs []error
	for _, path := range paths {
		files, err := transpile.LoadPackage(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if len(files) == 0 || files[0].Program == nil {
			continue
		}
		key := filepath.Dir(files[0].Path) + "\x00" + files[0].Program.Package
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if err := checkPackage(ctx, files, opts); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func shouldSkipDir(root, path, name string) bool {
	if filepath.Clean(root) == filepath.Clean(path) {
		return false
	}
	rel, relErr := filepath.Rel(root, path)
	if relErr == nil && filepath.Dir(rel) == "." {
		switch name {
		case "build", "dist", "node_modules", "vendor", "testdata":
			return true
		}
	}
	switch name {
	case ".git", ".tiller", "node_modules":
		return true
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	_, err := os.Lstat(filepath.Join(path, ".git"))
	return err == nil
}

// Package buildsurface compiles a *ir.SurfaceProgram into a cached WASM module
// that the browser-side bootstrap can mount onto a canvas element.
//
// DEPRECATED (ADR 0003, Slice X.D 2026-05-25): engine surfaces lower
// to shared-VM bytecode by default through engine/surface/lowering.go
// (LowerToBytecode). This package now serves only the `surface=wasm`
// escape-hatch path defined by ADR 0006. The package is scheduled for
// deletion after the 90-day coexistence window from ADR 0005 expires;
// the clock starts when Slice X.D ships.
//
// New code should NOT depend on this package. Authors who need a
// capability the bytecode lowerer doesn't yet support should file a
// gap entry in gosx-vm-capability-gaps.md per ADR 0006 §1 instead.
package buildsurface

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"m31labs.dev/gosx/internal/version"
	"m31labs.dev/gosx/ir"
)

// Compiler selects the Go toolchain to use when compiling surface WASM modules.
type Compiler int

const (
	// CompilerGo uses the standard gc toolchain (go build). Preferred in dev
	// mode for fast incremental builds.
	CompilerGo Compiler = iota

	// CompilerTinyGo uses TinyGo for smaller WASM output. Preferred in
	// production builds.
	CompilerTinyGo
)

// Options controls how Build compiles and caches a surface WASM module.
type Options struct {
	// Compiler selects the toolchain. Use CompilerGo during development (called
	// from Discover); CompilerTinyGo for production (called from the CLI build).
	Compiler Compiler

	// CacheDir is the directory used for content-addressed WASM caching.
	// Typically <projectRoot>/.gosx/cache/surfaces.
	CacheDir string

	// OutputDir is the public serving directory where the cached WASM is copied
	// so that the HTTP handler can serve it.
	OutputDir string

	// GoSXRoot is the absolute path to the GoSX module root. When empty, Build
	// resolves it via `go list`.
	GoSXRoot string

	// ProjectDir is the directory used to resolve module roots. When empty,
	// the current working directory is used.
	ProjectDir string
}

const gosxModuleImportPath = "m31labs.dev/gosx"

// Build compiles sp into a WASM module using the toolchain selected by opts.
// Results are content-addressed by a fingerprint derived from sp.SourceFingerprint,
// the compiler tag, and the GoSX version. The compiled WASM is cached inside
// opts.CacheDir and also copied to opts.OutputDir for serving.
//
// Returns the absolute output file path and the full fingerprint hash.
func Build(ctx context.Context, sp *ir.SurfaceProgram, opts Options) (wasmPath, hash string, err error) {
	if sp == nil {
		return "", "", fmt.Errorf("buildsurface.Build: nil SurfaceProgram")
	}
	if opts.CacheDir == "" {
		return "", "", fmt.Errorf("buildsurface.Build: CacheDir is required")
	}
	if opts.OutputDir == "" {
		return "", "", fmt.Errorf("buildsurface.Build: OutputDir is required")
	}

	projectDir := opts.ProjectDir
	if projectDir == "" {
		projectDir, err = os.Getwd()
		if err != nil {
			return "", "", fmt.Errorf("buildsurface.Build: getwd: %w", err)
		}
	}

	gosxRoot := opts.GoSXRoot
	if gosxRoot == "" {
		gosxRoot, err = resolveModuleRoot(projectDir, gosxModuleImportPath)
		if err != nil {
			return "", "", fmt.Errorf("buildsurface.Build: resolve gosx root: %w", err)
		}
	}

	// 1. Compute fingerprint.
	fp := buildFingerprint(sp, opts.Compiler)

	// 2. Cache lookup.
	if cachedPath, ok := lookupCache(opts.CacheDir, fp); ok {
		out := outputWASMPath(opts.OutputDir, sp.Name, fp)
		if err := copyToOutput(out, cachedPath); err != nil {
			return "", "", fmt.Errorf("buildsurface.Build: copy from cache: %w", err)
		}
		return out, fp, nil
	}

	// 3. Build scratch module.
	// Pick a `go` directive compatible with the chosen compiler. TinyGo
	// (currently ≤0.40.x) caps at Go 1.25; the standard gc toolchain uses
	// whatever the host has (typically 1.26+).
	goDirective := "1.26"
	if opts.Compiler == CompilerTinyGo {
		goDirective = "1.25"
	}
	scratchDir, cleanup, err := prepareScratchModule(sp, fp, gosxRoot, goDirective)
	if err != nil {
		return "", "", fmt.Errorf("buildsurface.Build: prepare scratch module: %w", err)
	}
	defer cleanup()

	// 4. Compile.
	compiledPath := filepath.Join(scratchDir, "out.wasm")
	if err := compileSurface(ctx, opts.Compiler, scratchDir, compiledPath); err != nil {
		return "", "", fmt.Errorf("buildsurface.Build: compile: %w", err)
	}

	// 5. Optional wasm-opt.
	_ = tryOptimizeWASM(compiledPath)

	// 6. Read compiled bytes.
	data, err := os.ReadFile(compiledPath)
	if err != nil {
		return "", "", fmt.Errorf("buildsurface.Build: read compiled wasm: %w", err)
	}

	// 7. Write cache atomically.
	cachedPath, err := writeCache(opts.CacheDir, fp, data)
	if err != nil {
		return "", "", fmt.Errorf("buildsurface.Build: write cache: %w", err)
	}

	// 8. Copy to output.
	out := outputWASMPath(opts.OutputDir, sp.Name, fp)
	if err := copyToOutput(out, cachedPath); err != nil {
		return "", "", fmt.Errorf("buildsurface.Build: copy to output: %w", err)
	}

	return out, fp, nil
}

// buildFingerprint combines the surface source fingerprint with the compiler
// tag and the GoSX version, so the cache invalidates when either changes.
func buildFingerprint(sp *ir.SurfaceProgram, compiler Compiler) string {
	compilerTag := "go"
	if compiler == CompilerTinyGo {
		compilerTag = "tinygo"
	}
	h := sha256.New()
	h.Write([]byte(sp.SourceFingerprint))
	h.Write([]byte{0})
	h.Write([]byte(compilerTag))
	h.Write([]byte{0})
	h.Write([]byte(version.Current))
	return hex.EncodeToString(h.Sum(nil))
}

// prepareScratchModule creates a temporary module directory containing:
//   - go.mod with a replace directive pointing to the GoSX module root
//   - user/ subdirectory holding every file in sp.SourceFiles
//   - main.go at the module root, generated by GenerateMain
//   - go.sum copied from the GoSX root (if present)
func prepareScratchModule(sp *ir.SurfaceProgram, fp, gosxRoot, goDirective string) (string, func(), error) {
	short := fp
	if len(short) > 8 {
		short = short[:8]
	}
	modName := fmt.Sprintf("gosx_surface_%s_%s", sanitizeName(sp.Name), short)

	scratchDir, err := os.MkdirTemp("", "gosx-surface-wasm-*")
	if err != nil {
		return "", nil, fmt.Errorf("create scratch dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(scratchDir) }

	if err := writeScratchGoMod(scratchDir, modName, gosxRoot, goDirective); err != nil {
		cleanup()
		return "", nil, err
	}

	// Copy user source files into user/ subdirectory.
	userDir := filepath.Join(scratchDir, "user")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("create user dir: %w", err)
	}
	for _, sf := range sp.SourceFiles {
		dst := filepath.Join(userDir, sf.Path)
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("create dir for %s: %w", sf.Path, err)
		}
		if err := os.WriteFile(dst, sf.Content, 0644); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("write user file %s: %w", sf.Path, err)
		}
	}

	// Generate main.go.
	userImportPath := modName + "/user"
	mainSrc, err := GenerateMain(sp, userImportPath)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("generate main.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(scratchDir, "main.go"), mainSrc, 0644); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write main.go: %w", err)
	}

	// Copy go.sum from GoSX root to satisfy the module requirements of the replace.
	if data, err := os.ReadFile(filepath.Join(gosxRoot, "go.sum")); err == nil {
		_ = os.WriteFile(filepath.Join(scratchDir, "go.sum"), data, 0644)
	}

	return scratchDir, cleanup, nil
}

// writeScratchGoMod writes a minimal go.mod for the scratch surface module.
// goDirective is the Go-language version line (e.g. "1.25" or "1.26") and
// must be ≤ the Go version of the toolchain that will build the module.
func writeScratchGoMod(dir, modName, gosxRoot, goDirective string) error {
	var sb strings.Builder
	sb.WriteString("module ")
	sb.WriteString(modName)
	sb.WriteString("\n\ngo ")
	sb.WriteString(goDirective)
	sb.WriteString("\n")
	sb.WriteString("\nrequire ")
	sb.WriteString(gosxModuleImportPath)
	sb.WriteString(" v0.0.0\n")
	sb.WriteString("\nreplace ")
	sb.WriteString(gosxModuleImportPath)
	sb.WriteString(" => ")
	sb.WriteString(gosxRoot)
	sb.WriteString("\n")
	return os.WriteFile(filepath.Join(dir, "go.mod"), []byte(sb.String()), 0644)
}

// compileSurface dispatches to the appropriate compiler.
func compileSurface(ctx context.Context, compiler Compiler, scratchDir, outputPath string) error {
	switch compiler {
	case CompilerTinyGo:
		return compileTinyGo(ctx, scratchDir, outputPath)
	default:
		return compileGo(ctx, scratchDir, outputPath)
	}
}

// compileGo runs `GOOS=js GOARCH=wasm go build -o outputPath .` in scratchDir.
// Uses -ldflags="-s -w" -trimpath to strip debug symbols and embedded paths
// (~30–50% size reduction on Go-WASM output). Real production builds should
// still prefer TinyGo when available, but this keeps standard-Go output
// reasonable.
func compileGo(ctx context.Context, scratchDir, outputPath string) error {
	cmd := exec.CommandContext(ctx, "go", "build",
		"-trimpath",
		"-ldflags=-s -w",
		"-o", outputPath, ".")
	cmd.Dir = scratchDir
	cmd.Env = append(execEnvWithoutGoFlags(),
		"GOOS=js",
		"GOARCH=wasm",
		"GOWORK=off",
		"GOFLAGS=-mod=mod -buildvcs=false",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// compileTinyGo invokes TinyGo on scratchDir targeting wasm.
func compileTinyGo(ctx context.Context, scratchDir, outputPath string) error {
	tinygoPath, err := exec.LookPath("tinygo")
	if err != nil {
		return fmt.Errorf("tinygo not found on PATH: %w", err)
	}

	root := tinyGoCompatibleGoRoot()
	env := tinygoEnvFor(root)

	args := []string{
		"build",
		"-target", "wasm",
		"-no-debug",
		"-panic=trap",
		"-o", outputPath,
		".",
	}

	cmd := exec.CommandContext(ctx, tinygoPath, args...)
	cmd.Dir = scratchDir
	cmd.Env = env
	outBytes, err := cmd.CombinedOutput()
	if err != nil {
		raw := strings.TrimSpace(string(outBytes))
		if hint := parseTinyGoToolchainError(raw); hint != "" {
			return fmt.Errorf("tinygo build: %w\n%s\n\n%s", err, raw, hint)
		}
		return fmt.Errorf("tinygo build: %w\n%s", err, raw)
	}
	return nil
}

// tinygoEnvFor returns the env slice that compileTinyGo should pass to the
// tinygo subprocess. When goroot != "" the function pins GOROOT, prepends
// <goroot>/bin to PATH, and sets GOTOOLCHAIN=auto.
//
// GOTOOLCHAIN was previously set to "local" — that pinned TinyGo's nested
// `go build` to the selected SDK and blocked it from satisfying a higher
// `go` directive in the gosx replace target. Setting "auto" lets the nested
// `go` resolve / download the needed toolchain when present. Closes spec §C.
func tinygoEnvFor(goroot string) []string {
	env := execEnvWithoutGoFlags()
	env = setEnvVar(env, "GOWORK", "off")
	env = setEnvVar(env, "GOFLAGS", "-mod=mod -buildvcs=false")
	if goroot != "" {
		env = setEnvVar(env, "GOROOT", goroot)
		env = setEnvVar(env, "GOTOOLCHAIN", "auto")
		oldPath := envValueOf(env, "PATH")
		env = setEnvVar(env, "PATH", filepath.Join(goroot, "bin")+string(os.PathListSeparator)+oldPath)
	}
	return env
}

// toolchainMismatchPattern captures TinyGo's nested `go build` complaint when
// the module's go directive exceeds the selected SDK. Example stderr:
//
//	go: module /home/.../gosx requires go >= 1.26 (running go 1.25.9; GOTOOLCHAIN=local)
var toolchainMismatchPattern = regexp.MustCompile(`requires go >= (?P<want>[0-9]+(?:\.[0-9]+)+).*?running go (?P<have>[0-9]+(?:\.[0-9]+)+)`)

// parseTinyGoToolchainError inspects raw TinyGo stderr for a Go-version
// mismatch and, if one is present, returns a human-readable clarifying
// message that names the target version, observed version, and the env
// var overrides that can unblock the build. Returns "" when no mismatch
// pattern is found.
func parseTinyGoToolchainError(stderr string) string {
	m := toolchainMismatchPattern.FindStringSubmatch(stderr)
	if m == nil {
		return ""
	}
	want := m[toolchainMismatchPattern.SubexpIndex("want")]
	have := m[toolchainMismatchPattern.SubexpIndex("have")]
	return fmt.Sprintf(
		"hint: tinygo's nested go build requires Go >= %s but the selected SDK is %s; "+
			"set GOSX_TINYGO_GOROOT to a compatible SDK or run with GOSX_FORCE_GO_COMPILER=1 "+
			"to fall back to the standard gc toolchain.",
		want, have,
	)
}

// tinyGoCompatibleGoRoot finds the newest installed Go SDK under
// $HOME/sdk/go1.* whose version is within TinyGo's supported range
// (1.19–1.25 for the current TinyGo line). Returns "" if none.
// Honors $GOSX_TINYGO_GOROOT as an explicit override.
func tinyGoCompatibleGoRoot() string {
	if explicit := strings.TrimSpace(os.Getenv("GOSX_TINYGO_GOROOT")); explicit != "" {
		return explicit
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(home, "sdk", "go1.*"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	type candidate struct {
		root  string
		minor int
	}
	var picks []candidate
	for _, root := range matches {
		// extract "go1.N[.x]" minor version from the directory name
		base := filepath.Base(root)
		if !strings.HasPrefix(base, "go1.") {
			continue
		}
		rest := strings.TrimPrefix(base, "go1.")
		dot := strings.Index(rest, ".")
		minorStr := rest
		if dot >= 0 {
			minorStr = rest[:dot]
		}
		var minor int
		if _, err := fmt.Sscanf(minorStr, "%d", &minor); err != nil {
			continue
		}
		if minor < 19 || minor > 25 {
			continue
		}
		// confirm the SDK actually has a usable go binary
		if _, err := os.Stat(filepath.Join(root, "bin", "go")); err != nil {
			continue
		}
		picks = append(picks, candidate{root, minor})
	}
	if len(picks) == 0 {
		return ""
	}
	best := picks[0]
	for _, c := range picks[1:] {
		if c.minor > best.minor {
			best = c
		}
	}
	return best.root
}

// envValueOf returns the value of key in env, or "" if absent.
func envValueOf(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix)
		}
	}
	return ""
}

// tryOptimizeWASM runs wasm-opt -Oz on path if wasm-opt is available.
// Errors are silently ignored — optimisation is best-effort.
func tryOptimizeWASM(path string) bool {
	woptPath, err := exec.LookPath("wasm-opt")
	if err != nil {
		return false
	}
	optTmp := path + ".opt"
	cmd := exec.Command(woptPath, "-Oz",
		"--enable-bulk-memory",
		"--enable-nontrapping-float-to-int",
		"--strip-debug",
		"--strip-producers",
		path, "-o", optTmp)
	if cmd.Run() != nil {
		return false
	}
	if err := os.Rename(optTmp, path); err != nil {
		_ = os.Remove(optTmp)
		return false
	}
	return true
}

// resolveModuleRoot returns the on-disk root directory of importPath by running
// `go list -f {{.Dir}} <importPath>` in projectDir.
func resolveModuleRoot(projectDir, importPath string) (string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", importPath)
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list %s: %w", importPath, err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("go list %s: empty result", importPath)
	}
	return dir, nil
}

// sanitizeName replaces non-alphanumeric runes with underscores.
func sanitizeName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	return sb.String()
}

// execEnvWithoutGoFlags returns os.Environ() with any GOFLAGS entry stripped.
func execEnvWithoutGoFlags() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "GOFLAGS=") {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// setEnvVar sets key=value in env, replacing any existing entry for key.
func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			if !replaced {
				out = append(out, prefix+value)
				replaced = true
			}
			continue
		}
		out = append(out, entry)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}

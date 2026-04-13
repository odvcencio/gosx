package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const tinyGoScratchGoVersion = "1.25"

type tinyGoPackage struct {
	ImportPath string        `json:"ImportPath"`
	Dir        string        `json:"Dir"`
	Standard   bool          `json:"Standard"`
	Module     *tinyGoModule `json:"Module"`
	EmbedFiles []string      `json:"EmbedFiles"`
}

type tinyGoModule struct {
	Path    string        `json:"Path"`
	Version string        `json:"Version"`
	Dir     string        `json:"Dir"`
	Replace *tinyGoModule `json:"Replace"`
}

type tinyGoExternalModule struct {
	Path    string
	Version string
	Replace *tinyGoModule
}

type tinyGoBuildEnv struct {
	label string
	env   []string
}

func buildTinyGoWASM(projectDir, gosxRoot, outputPath, tinygoPath string, extraTags ...string) error {
	scratchDir, cleanup, err := prepareTinyGoWASMModule(projectDir, gosxRoot)
	if err != nil {
		return err
	}
	defer cleanup()

	envs := tinyGoBuildEnvironments()
	var failures []string
	for _, candidate := range envs {
		cmd := exec.Command(tinygoPath, tinyGoBuildArgs(outputPath, extraTags...)...)
		cmd.Dir = scratchDir
		cmd.Env = setEnv(candidate.env, "GOFLAGS", "-mod=mod")
		out, err := cmd.CombinedOutput()
		if err == nil {
			if candidate.label != "" {
				fmt.Printf("    TinyGo toolchain: %s\n", candidate.label)
			}
			return nil
		}
		failures = append(failures, tinyGoFailure(candidate.label, err, out))
		if !tinyGoFailureLooksToolchainRelated(out) {
			break
		}
	}

	if len(failures) == 0 {
		return fmt.Errorf("tinygo build did not run")
	}
	return errors.New(strings.Join(failures, "\n"))
}

func tinyGoBuildArgs(outputPath string, extraTags ...string) []string {
	args := []string{
		"build",
		"-target", "wasm",
	}
	tags := make([]string, 0, 1+len(extraTags))
	if !tinyGoFullRuntimeEnabled() {
		tags = append(tags, "gosx_tiny_runtime")
	}
	tags = append(tags, extraTags...)
	if len(tags) > 0 {
		args = append(args, "-tags="+strings.Join(tags, " "))
	}
	args = append(args,
		"-no-debug",
		"-panic=trap",
		"-o", outputPath,
		gosxModuleImportPath+"/client/wasm",
	)
	return args
}

func tinyGoFullRuntimeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOSX_TINYGO_FULL_RUNTIME"))) {
	case "1", "true", "yes", "full":
		return true
	default:
		return false
	}
}

func prepareTinyGoWASMModule(projectDir, gosxRoot string) (string, func(), error) {
	packages, modules, err := tinyGoWASMDependencyClosure(projectDir)
	if err != nil {
		return "", nil, err
	}

	scratchDir, err := os.MkdirTemp("", "gosx-tinygo-wasm-*")
	if err != nil {
		return "", nil, fmt.Errorf("create tinygo scratch module: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(scratchDir)
	}

	for _, pkg := range packages {
		if err := copyTinyGoPackage(gosxRoot, scratchDir, pkg); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	if err := writeTinyGoModuleFile(scratchDir, modules); err != nil {
		cleanup()
		return "", nil, err
	}
	if data, err := os.ReadFile(filepath.Join(gosxRoot, "go.sum")); err == nil {
		if err := os.WriteFile(filepath.Join(scratchDir, "go.sum"), data, 0644); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("write tinygo go.sum: %w", err)
		}
	}

	return scratchDir, cleanup, nil
}

func tinyGoWASMDependencyClosure(projectDir string) ([]tinyGoPackage, []tinyGoExternalModule, error) {
	cmd := exec.Command("go", "list", "-deps", "-json", gosxModuleImportPath+"/client/wasm")
	cmd.Dir = projectDir
	cmd.Env = append(execEnvWithoutGoFlags(), "GOFLAGS=-mod=mod", "GOOS=js", "GOARCH=wasm")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, nil, fmt.Errorf("list wasm dependencies: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, nil, fmt.Errorf("list wasm dependencies: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(out))
	packageByDir := map[string]tinyGoPackage{}
	moduleByPath := map[string]tinyGoExternalModule{}
	for {
		var pkg tinyGoPackage
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("decode wasm dependency graph: %w", err)
		}
		if pkg.Standard || pkg.Module == nil {
			continue
		}
		if pkg.Module.Path == gosxModuleImportPath {
			packageByDir[pkg.Dir] = pkg
			continue
		}
		module := pkg.Module
		moduleByPath[module.Path] = tinyGoExternalModule{
			Path:    module.Path,
			Version: module.Version,
			Replace: module.Replace,
		}
	}

	packages := make([]tinyGoPackage, 0, len(packageByDir))
	for _, pkg := range packageByDir {
		packages = append(packages, pkg)
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].ImportPath < packages[j].ImportPath
	})

	modules := make([]tinyGoExternalModule, 0, len(moduleByPath))
	for _, module := range moduleByPath {
		modules = append(modules, module)
	}
	sort.Slice(modules, func(i, j int) bool {
		return modules[i].Path < modules[j].Path
	})
	return packages, modules, nil
}

func copyTinyGoPackage(gosxRoot, scratchRoot string, pkg tinyGoPackage) error {
	relDir, err := filepath.Rel(gosxRoot, pkg.Dir)
	if err != nil {
		return fmt.Errorf("relative tinygo package %s: %w", pkg.ImportPath, err)
	}
	if relDir == "." || strings.HasPrefix(relDir, ".."+string(filepath.Separator)) || relDir == ".." || filepath.IsAbs(relDir) {
		return fmt.Errorf("tinygo package %s is outside GoSX root: %s", pkg.ImportPath, pkg.Dir)
	}

	dstDir := filepath.Join(scratchRoot, relDir)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("create tinygo package dir %s: %w", relDir, err)
	}
	entries, err := os.ReadDir(pkg.Dir)
	if err != nil {
		return fmt.Errorf("read tinygo package dir %s: %w", pkg.Dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			if err := copyTinyGoFile(filepath.Join(pkg.Dir, name), filepath.Join(dstDir, name)); err != nil {
				return err
			}
		}
	}
	for _, embedFile := range pkg.EmbedFiles {
		src := filepath.Join(pkg.Dir, embedFile)
		dst := filepath.Join(dstDir, embedFile)
		if err := copyTinyGoFile(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func writeTinyGoModuleFile(dir string, modules []tinyGoExternalModule) error {
	var b strings.Builder
	b.WriteString("module ")
	b.WriteString(gosxModuleImportPath)
	b.WriteString("\n\n")
	b.WriteString("go ")
	b.WriteString(tinyGoScratchGoVersion)
	b.WriteString("\n")

	if len(modules) > 0 {
		b.WriteString("\nrequire (\n")
		for _, module := range modules {
			if module.Version == "" {
				continue
			}
			b.WriteString("\t")
			b.WriteString(module.Path)
			b.WriteString(" ")
			b.WriteString(module.Version)
			b.WriteString("\n")
		}
		b.WriteString(")\n")
	}

	for _, module := range modules {
		if module.Replace == nil {
			continue
		}
		b.WriteString("\nreplace ")
		b.WriteString(module.Path)
		b.WriteString(" => ")
		b.WriteString(module.Replace.Path)
		if module.Replace.Version != "" {
			b.WriteString(" ")
			b.WriteString(module.Replace.Version)
		}
		b.WriteString("\n")
	}

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("write tinygo go.mod: %w", err)
	}
	return nil
}

func tinyGoBuildEnvironments() []tinyGoBuildEnv {
	base := setEnv(execEnvWithoutGoFlags(), "GOFLAGS", "-mod=mod")
	envs := []tinyGoBuildEnv{{label: "current Go", env: base}}
	seenRoots := map[string]bool{}
	for _, root := range tinyGoCompatibleGoRoots() {
		if seenRoots[root] {
			continue
		}
		seenRoots[root] = true
		version, ok := goVersionForRoot(root)
		if !ok {
			continue
		}
		envs = append(envs, tinyGoBuildEnv{
			label: fmt.Sprintf("%s at %s", version, root),
			env:   envWithGoRoot(base, root),
		})
	}
	return envs
}

func tinyGoCompatibleGoRoots() []string {
	var roots []string
	if root := strings.TrimSpace(os.Getenv("GOSX_TINYGO_GOROOT")); root != "" {
		roots = append(roots, root)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if matches, err := filepath.Glob(filepath.Join(home, "sdk", "go1.*")); err == nil {
			roots = append(roots, matches...)
		}
	}
	roots = append(roots, "/usr/local/go")

	sort.Slice(roots, func(i, j int) bool {
		vi, _ := goVersionForRoot(roots[i])
		vj, _ := goVersionForRoot(roots[j])
		return compareGoVersion(vi, vj) > 0
	})

	out := roots[:0]
	for _, root := range roots {
		version, ok := goVersionForRoot(root)
		if !ok || !tinyGoSupportsGoVersion(version) {
			continue
		}
		out = append(out, root)
	}
	return out
}

func goVersionForRoot(root string) (string, bool) {
	goBin := filepath.Join(root, "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		return "", false
	}
	cmd := exec.Command(goBin, "version")
	cmd.Env = setEnv(execEnvWithoutGoFlags(), "GOTOOLCHAIN", "local")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 || !strings.HasPrefix(fields[2], "go") {
		return "", false
	}
	return strings.TrimPrefix(fields[2], "go"), true
}

func tinyGoSupportsGoVersion(version string) bool {
	major, minor, ok := parseGoMajorMinor(version)
	return ok && major == 1 && minor >= 19 && minor <= 25
}

func envWithGoRoot(base []string, root string) []string {
	env := setEnv(base, "GOROOT", root)
	env = setEnv(env, "GOTOOLCHAIN", "local")
	oldPath := envValue(env, "PATH")
	env = setEnv(env, "PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+oldPath)
	return env
}

func setEnv(env []string, key, value string) []string {
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

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func parseGoMajorMinor(version string) (int, int, bool) {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(strings.TrimPrefix(parts[0], "go"))
	if err != nil {
		return 0, 0, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func compareGoVersion(a, b string) int {
	amajor, aminor, aok := parseGoMajorMinor(a)
	bmajor, bminor, bok := parseGoMajorMinor(b)
	if !aok && !bok {
		return 0
	}
	if !aok {
		return -1
	}
	if !bok {
		return 1
	}
	if amajor != bmajor {
		return amajor - bmajor
	}
	if aminor != bminor {
		return aminor - bminor
	}
	apatch := goPatchVersion(a)
	bpatch := goPatchVersion(b)
	return apatch - bpatch
}

func goPatchVersion(version string) int {
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return 0
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0
	}
	return patch
}

func tinyGoFailure(candidate string, err error, out []byte) string {
	message := strings.TrimSpace(string(out))
	if message == "" {
		message = err.Error()
	} else {
		message = message + ": " + err.Error()
	}
	if candidate == "" {
		return message
	}
	return candidate + ": " + message
}

func tinyGoFailureLooksToolchainRelated(out []byte) bool {
	text := string(out)
	return strings.Contains(text, "requires go version") ||
		strings.Contains(text, "go.mod requires go >=") ||
		strings.Contains(text, "GOTOOLCHAIN=local")
}

func copyTinyGoFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, info.Mode()); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

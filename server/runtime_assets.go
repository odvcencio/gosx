package server

import (
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gosx/buildmanifest"
)

// SetRuntimeRoot overrides the filesystem root used to resolve `/gosx/*`
// compatibility assets for engines and future page runtimes.
func (a *App) SetRuntimeRoot(root string) {
	a.runtimeRoot = strings.TrimSpace(root)
}

func (a *App) effectiveRuntimeRoot() string {
	if a == nil {
		return ""
	}
	if root := strings.TrimSpace(a.runtimeRoot); root != "" {
		return root
	}
	if publicDir := strings.TrimSpace(a.publicDir); publicDir != "" {
		parent := filepath.Dir(publicDir)
		if parent != "" && parent != "." {
			return parent
		}
	}
	return ResolveAppRoot("")
}

func (a *App) hasCompatRuntimeAssets() bool {
	root := a.effectiveRuntimeRoot()
	if root == "" {
		return false
	}
	if isFile(filepath.Join(root, "build", "bootstrap.js")) {
		return true
	}
	return isFile(filepath.Join(root, "build.json")) && isDir(filepath.Join(root, "assets"))
}

func (a *App) serveRuntimeAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/gosx/")), "/")
	if name == "" || strings.HasPrefix(name, "../") {
		http.NotFound(w, r)
		return
	}

	root := a.effectiveRuntimeRoot()
	if root == "" {
		http.NotFound(w, r)
		return
	}

	if fsPath, ok := runtimeCompatSourcePath(root, name); ok {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		MarkObservedRequest(r, "runtime", "/gosx/"+name)
		http.ServeFile(w, r, fsPath)
		return
	}

	if fsPath, ok := runtimeCompatBuiltPath(root, name); ok {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		MarkObservedRequest(r, "runtime", "/gosx/"+name)
		http.ServeFile(w, r, fsPath)
		return
	}

	http.NotFound(w, r)
}

func runtimeCompatSourcePath(root, name string) (string, bool) {
	buildDir := filepath.Join(root, "build")
	candidates := map[string]string{
		"runtime.wasm": filepath.Join(buildDir, "gosx-runtime.wasm"),
		"wasm_exec.js": filepath.Join(buildDir, "wasm_exec.js"),
		"bootstrap.js": filepath.Join(buildDir, "bootstrap.js"),
		"patch.js":     filepath.Join(buildDir, "patch.js"),
	}
	if direct, ok := candidates[name]; ok && isFile(direct) {
		return direct, true
	}
	if strings.HasPrefix(name, "islands/") || strings.HasPrefix(name, "css/") {
		target := filepath.Join(buildDir, filepath.FromSlash(name))
		if isFile(target) {
			return target, true
		}
	}
	return "", false
}

func runtimeCompatBuiltPath(root, name string) (string, bool) {
	manifest, err := buildmanifest.Load(filepath.Join(root, "build.json"))
	if err != nil {
		return "", false
	}
	assetsDir := filepath.Join(root, "assets")

	switch name {
	case "runtime.wasm":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.WASM.File)
	case "wasm_exec.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.WASMExec.File)
	case "bootstrap.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.Bootstrap.File)
	case "patch.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.Patch.File)
	}

	if strings.HasPrefix(name, "islands/") {
		base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
		if asset, ok := manifest.IslandAssetByName(base); ok {
			return runtimeManifestAssetPath(assetsDir, "islands", asset.File)
		}
	}
	if strings.HasPrefix(name, "css/") {
		requested := filepath.Base(name)
		for _, asset := range manifest.CSS {
			if filepath.Base(asset.Source) == requested || strings.TrimSpace(asset.Component)+".css" == requested {
				return runtimeManifestAssetPath(assetsDir, "css", asset.File)
			}
		}
	}
	return "", false
}

func runtimeManifestAssetPath(assetsDir, bucket, file string) (string, bool) {
	if strings.TrimSpace(file) == "" {
		return "", false
	}
	target := filepath.Join(assetsDir, bucket, file)
	if !isFile(target) {
		return "", false
	}
	return target, true
}

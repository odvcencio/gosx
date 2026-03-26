package server

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveAppRoot returns the best runtime app root for a GoSX application.
//
// Resolution order:
// 1. `GOSX_APP_ROOT`
// 2. current executable directory and its parent
// 3. the provided caller file directory
// 4. current working directory
//
// The first candidate that looks like an app root wins. If none match, the
// best-effort fallback is the caller directory, then cwd, then ".".
func ResolveAppRoot(callerFile string) string {
	candidates := []string{}
	if envRoot := strings.TrimSpace(os.Getenv("GOSX_APP_ROOT")); envRoot != "" {
		candidates = append(candidates, envRoot)
	}
	if exePath, err := os.Executable(); err == nil && strings.TrimSpace(exePath) != "" {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates, exeDir, filepath.Dir(exeDir))
	}
	if callerFile = strings.TrimSpace(callerFile); callerFile != "" {
		candidates = append(candidates, filepath.Dir(callerFile))
	}
	if wd, err := os.Getwd(); err == nil && strings.TrimSpace(wd) != "" {
		candidates = append(candidates, wd)
	}

	seen := map[string]struct{}{}
	fallbacks := []string{}
	for _, candidate := range candidates {
		candidate = absCleanPath(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		fallbacks = append(fallbacks, candidate)
		if looksLikeAppRoot(candidate) {
			return candidate
		}
	}
	if len(fallbacks) > 0 {
		return fallbacks[len(fallbacks)-1]
	}
	return "."
}

func looksLikeAppRoot(root string) bool {
	if root == "" {
		return false
	}
	if isDir(filepath.Join(root, "app")) {
		return true
	}
	if isDir(filepath.Join(root, "public")) {
		return true
	}
	if isFile(filepath.Join(root, "go.mod")) {
		return true
	}
	return false
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func absCleanPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

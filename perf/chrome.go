package perf

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

// FindChrome returns the path to a Chrome or Chromium executable.
// Resolution order:
//  1. CHROME_PATH env var (if set and executable)
//  2. PATH lookup for well-known binary names
//  3. Platform-specific default install paths
func FindChrome() (string, error) {
	if p := os.Getenv("CHROME_PATH"); p != "" {
		if isExecutable(p) {
			return p, nil
		}
		return "", errors.New("CHROME_PATH is set but not executable: " + p)
	}

	candidates := []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
	}
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}

	var defaults []string
	switch runtime.GOOS {
	case "darwin":
		defaults = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		}
	case "windows":
		defaults = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		}
	default: // Linux / WSL2
		defaults = []string{
			"/mnt/c/Program Files/Google/Chrome/Application/chrome.exe",
		}
	}
	for _, p := range defaults {
		if isExecutable(p) {
			return p, nil
		}
	}

	return "", errors.New("chrome not found: install Chrome/Chromium or set CHROME_PATH")
}

// isExecutable reports whether path exists and is a regular executable file.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() && info.Mode()&0111 != 0
}

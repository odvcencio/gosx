// Package env loads .env files for GoSX applications.
package env

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Options configures .env file loading.
type Options struct {
	Mode     string
	Override bool
}

// Mode returns the active environment mode for config loading.
func Mode() string {
	for _, key := range []string{"GOSX_ENV", "APP_ENV", "ENV", "NODE_ENV"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "development"
}

// LoadDir loads the standard GoSX .env file set from a directory.
func LoadDir(dir string, mode string) error {
	return LoadDirWithOptions(dir, Options{Mode: mode})
}

// LoadDirWithOptions loads the standard GoSX .env file set from a directory.
func LoadDirWithOptions(dir string, opts Options) error {
	locked := lockedEnv()
	for _, file := range baseFiles(dir) {
		if err := loadFile(file, opts.Override, locked); err != nil {
			return err
		}
	}
	mode := strings.TrimSpace(opts.Mode)
	if mode == "" {
		mode = Mode()
	}
	for _, file := range modeFiles(dir, mode) {
		if err := loadFile(file, opts.Override, locked); err != nil {
			return err
		}
	}
	return nil
}

func baseFiles(dir string) []string {
	return []string{
		filepath.Join(dir, ".env"),
		filepath.Join(dir, ".env.local"),
	}
}

func modeFiles(dir string, mode string) []string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return nil
	}
	return dedupe([]string{
		filepath.Join(dir, ".env."+mode),
		filepath.Join(dir, ".env."+mode+".local"),
	})
}

func loadFile(path string, override bool, locked map[string]bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		key, value, ok, err := parseLine(scanner.Text())
		if err != nil {
			return fmt.Errorf("parse %s:%d: %w", path, lineNo, err)
		}
		if !ok {
			continue
		}
		if !override && locked[key] {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

func parseLine(line string) (string, string, bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false, nil
	}
	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}

	key, raw, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", "", false, fmt.Errorf("missing '='")
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false, fmt.Errorf("empty key")
	}

	value, err := parseValue(strings.TrimSpace(raw))
	if err != nil {
		return "", "", false, err
	}
	return key, value, true, nil
}

func parseValue(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", err
		}
		return unquoted, nil
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") && len(value) >= 2 {
		return value[1 : len(value)-1], nil
	}
	return value, nil
}

func lockedEnv() map[string]bool {
	locked := make(map[string]bool)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			locked[key] = true
		}
	}
	return locked
}

func dedupe(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

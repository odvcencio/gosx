package route

import (
	"path"
	"path/filepath"
	"strings"
)

func normalizeRouteModuleSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	source = filepath.ToSlash(filepath.Clean(source))
	source = strings.TrimPrefix(source, "./")
	return source
}

func appRelativeRouteModuleSource(source string) (string, bool) {
	source = normalizeRouteModuleSource(source)
	if source == "" {
		return "", false
	}
	parts := strings.Split(source, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "app" {
			continue
		}
		if i == len(parts)-1 {
			return "", true
		}
		rel := path.Clean(strings.Join(parts[i+1:], "/"))
		if rel == "." {
			return "", true
		}
		return rel, true
	}
	return "", false
}

func fileModuleLookupKeys(source string) []string {
	source = normalizeRouteModuleSource(source)
	if source == "" {
		return nil
	}
	keys := []string{source}
	if rel, ok := appRelativeRouteModuleSource(source); ok && rel != "" {
		keys = append(keys, rel, path.Join("app", rel))
	}
	return appendUniqueStrings(nil, keys...)
}

func dirModuleLookupKeys(source string) []string {
	source = normalizeRouteModuleSource(source)
	if source == "" {
		return nil
	}
	keys := []string{source}
	if rel, ok := appRelativeRouteModuleSource(source); ok {
		if rel == "" {
			keys = append(keys, "app")
		} else {
			keys = append(keys, rel, path.Join("app", rel))
		}
	}
	return appendUniqueStrings(nil, keys...)
}

func moduleLookupKeySet(keys []string) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		set[key] = struct{}{}
	}
	return set
}

func moduleLookupKeysOverlap(keys []string, set map[string]struct{}) bool {
	for _, key := range keys {
		if _, ok := set[key]; ok {
			return true
		}
	}
	return false
}

package navigationlink

import (
	"net/url"
	"strings"
)

func NormalizeNavigationLinkCurrentPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return "auto"
	case "page", "ancestor", "none":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "none"
	}
}

// NormalizeNavigationLinkPrefetch normalizes the declarative prefetch policy
// for managed links. The bool reports whether the author explicitly set a
// prefetch policy.
func NormalizeNavigationLinkPrefetch(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", false
	case "off", "intent", "render", "force":
		return strings.ToLower(strings.TrimSpace(value)), true
	default:
		return strings.ToLower(strings.TrimSpace(value)), true
	}
}

// ResolveNavigationLinkCurrent derives the link's visible current relation from
// the current request path plus the author's declarative current policy.
func ResolveNavigationLinkCurrent(href, currentPath, policy string) string {
	switch normalized := NormalizeNavigationLinkCurrentPolicy(policy); normalized {
	case "page", "ancestor", "none":
		return normalized
	}

	target := navigationTargetParts(href, currentPath)
	current := navigationTargetParts(currentPath, currentPath)
	if !sameNavigationCurrentTarget(target, current) {
		if ancestorNavigationTarget(target, current) {
			return "ancestor"
		}
		return "none"
	}
	return "page"
}

type navigationTarget struct {
	origin string
	path   string
	search string
}

func navigationTargetParts(value string, currentPath string) *navigationTarget {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	base := &url.URL{
		Scheme: "https",
		Host:   "gosx.local",
		Path:   navigationBasePath(currentPath),
	}
	parsed, err := base.Parse(trimmed)
	if err != nil {
		return nil
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil
	}
	path := strings.TrimSpace(parsed.EscapedPath())
	if path == "" {
		path = strings.TrimSpace(parsed.Path)
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}
	search := ""
	if parsed.RawQuery != "" {
		search = "?" + parsed.RawQuery
	}
	return &navigationTarget{
		origin: parsed.Scheme + "://" + parsed.Host,
		path:   path,
		search: search,
	}
}

func navigationBasePath(currentPath string) string {
	currentPath = strings.TrimSpace(currentPath)
	if currentPath == "" {
		return "/"
	}
	return currentPath
}

func sameNavigationTarget(left, right *navigationTarget) bool {
	return left != nil && right != nil && left.origin == right.origin && left.path == right.path && left.search == right.search
}

// sameNavigationCurrentTarget treats a queryless authored link as naming the
// route regardless of the current request's query. Once the author includes a
// query, it remains an exact current-view match.
func sameNavigationCurrentTarget(target, current *navigationTarget) bool {
	return target != nil && current != nil && target.origin == current.origin && target.path == current.path &&
		(target.search == "" || target.search == current.search)
}

func ancestorNavigationTarget(parent, child *navigationTarget) bool {
	if parent == nil || child == nil || parent.origin != child.origin {
		return false
	}
	if parent.path == "/" || parent.search != "" {
		return false
	}
	return child.path == parent.path || strings.HasPrefix(child.path, parent.path+"/")
}

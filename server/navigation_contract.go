package server

import (
	"net/http"
	"net/url"
	"strings"

	"m31labs.dev/gosx"
)

const (
	NavigationEnhanceAttr      = gosx.EnhancementAttr
	NavigationEnhanceLayerAttr = gosx.EnhancementLayerAttr
	NavigationFallbackAttr     = gosx.RuntimeFallbackAttr

	NavigationLinkAttr               = "data-gosx-link"
	NavigationLinkStateAttr          = "data-gosx-link-state"
	NavigationLinkCurrentAttr        = "data-gosx-link-current"
	NavigationLinkCurrentPolicyAttr  = "data-gosx-link-current-policy"
	NavigationLinkPrefetchAttr       = "data-gosx-prefetch"
	NavigationLinkPrefetchStateAttr  = "data-gosx-prefetch-state"
	NavigationLinkManagedCurrentAttr = "data-gosx-aria-current-managed"

	NavigationFormAttr        = gosx.ManagedFormAttr
	NavigationFormModeAttr    = gosx.ManagedFormModeAttr
	NavigationFormStateAttr   = gosx.ManagedFormStateAttr
	NavigationFormProjectAttr = gosx.ManagedFormProjectAttr

	// NavigationRevalidateIntervalAttr and NavigationRevalidateSrcAttr declare
	// periodic revalidation on an element. The navigation runtime polls
	// NavigationRevalidateSrcAttr (same-origin only) on the
	// NavigationRevalidateIntervalAttr period and revalidates the page when the
	// response body changes; without a src it revalidates unconditionally.
	NavigationRevalidateIntervalAttr = "data-gosx-revalidate-interval"
	NavigationRevalidateSrcAttr      = "data-gosx-revalidate-src"

	// NavigationCountdownAttr declares a countdown to an RFC3339 instant
	// (gosx#178). The author writes the element's initial text or each
	// segment's initial value directly (optionally computed server-side in
	// the page loader from the server's own clock); the navigation runtime
	// keeps them moving with one shared 1-second timer starting at the
	// first tick after the page loads. NavigationCountdownFormatAttr
	// selects the compact render ("dhms" or "mm:ss") on the same element;
	// NavigationCountdownSegmentAttr marks a descendant the runtime fills
	// instead, leaving the rest of the element's markup to the app.
	//
	// NavigationCountdownWarnAttr and NavigationCountdownCueAttr (gosx#213)
	// share one grammar: a comma-separated list of threshold:token pairs
	// (for example "30s:is-warn,10s:is-critical"). -warn's token is a CSS
	// class the runtime toggles on the countdown root at or below the
	// threshold, re-evaluated every tick; -cue's token is a name from the
	// runtime's fixed synthesized tone vocabulary ("beep" or "chime"),
	// fired once the first time the remainder crosses at or below the
	// threshold, played through one AudioContext shared by the whole
	// runtime and primed on the page's first user gesture. Before gosx#213
	// NavigationCountdownWarnAttr took one bare duration and always
	// toggled a single fixed class; that single-value form is no longer
	// accepted.
	//
	// NavigationCountdownThenAttr set to "revalidate" fires one
	// revalidation of the page's revalidate root (see
	// NavigationRevalidateIntervalAttr) when the countdown first reaches
	// zero, and does nothing while the page has no active revalidation
	// poll. `gosx check` rejects a static NavigationCountdownAttr value
	// that is not a valid RFC3339 instant, a static
	// NavigationCountdownFormatAttr outside its two supported values, a
	// static NavigationCountdownSegmentAttr outside its four supported
	// names, a static NavigationCountdownWarnAttr or NavigationCountdownCueAttr
	// with a malformed pair list, and a static NavigationCountdownThenAttr
	// other than "revalidate" (see ir/validate.go); a dynamic expression
	// value is checked at run time instead, and fails inert there.
	NavigationCountdownAttr        = "data-gosx-countdown"
	NavigationCountdownFormatAttr  = "data-gosx-countdown-format"
	NavigationCountdownSegmentAttr = "data-gosx-countdown-segment"
	NavigationCountdownWarnAttr    = "data-gosx-countdown-warn"
	NavigationCountdownCueAttr     = "data-gosx-countdown-cue"
	NavigationCountdownThenAttr    = "data-gosx-countdown-then"

	// NavigationWatchAttr declares a condition over one of the watch
	// element's own attributes (gosx#214):
	// "<attrName>=<value>" compares that attribute's live value against a
	// literal, or "<attrName>=@<selector>" / "<attrName>=@<selector>[<attrName>]"
	// against another element's trimmed text content or named attribute.
	// NavigationWatchEffectAttr declares a comma-separated effect list run
	// on a false-to-true transition of that condition: "class:<name>" (or
	// "class:<name>@<selector>" for a selector target) and "title" (using
	// the message from NavigationWatchTitleAttr, until window focus or the
	// condition returns to false) are level-tied and re-evaluated on every
	// rescan; "cue:<name>" (the same synthesized tone vocabulary
	// NavigationCountdownCueAttr uses) is edge-triggered and fires exactly
	// once per false-to-true transition. A watch condition is evaluated at
	// page boot and after every soft navigation or revalidation swap — the
	// same rescan lifecycle NavigationCountdownAttr follows — not via a
	// live DOM observer.
	NavigationWatchAttr       = "data-gosx-watch"
	NavigationWatchEffectAttr = "data-gosx-watch-effect"
	NavigationWatchTitleAttr  = "data-gosx-watch-title"

	// NavigationReorderAttr declares a sortable list (gosx#212). Each direct
	// child that can move carries NavigationReorderItemAttr set to that
	// item's identity; a descendant of the item (or the item itself, if none
	// is marked) carries NavigationReorderHandleAttr and is the pointer and
	// keyboard drag handle. NavigationReorderActionAttr, on the container,
	// names the managed action a drop or a keyboard commit posts the new
	// order to, in the same "METHOD /url" grammar data-gosx-action uses — a
	// dedicated attribute, not data-gosx-action itself, because a reorder
	// container is very often a clickable, non-form element and sharing the
	// attribute would let the declarative-actions click handler fire a
	// spurious empty action from the same element. The POST carries the
	// moved item's identity and its zero-based target index under
	// NavigationReorderDefaultItemField/NavigationReorderDefaultIndexField,
	// or the field names NavigationReorderItemFieldAttr/
	// NavigationReorderIndexFieldAttr declare instead. The runtime owns the
	// full interaction: Pointer Events with setPointerCapture, an optimistic
	// DOM reorder that reverts through the same NavigationFormStateAttr /
	// "data-gosx-pending" error surface managed forms already use on
	// failure, a keyboard path (grab with Space or Enter, move with arrows, drop
	// with Space, cancel with Escape) with aria-live announcements, and its
	// own periodic-revalidation pause for the whole gesture. See the client
	// runtime guide's "Declarative reorder" section for the full contract.
	NavigationReorderAttr              = "data-gosx-reorder"
	NavigationReorderActionAttr        = "data-gosx-reorder-action"
	NavigationReorderItemAttr          = "data-gosx-reorder-item"
	NavigationReorderHandleAttr        = "data-gosx-reorder-handle"
	NavigationReorderItemFieldAttr     = "data-gosx-reorder-item-field"
	NavigationReorderIndexFieldAttr    = "data-gosx-reorder-index-field"
	NavigationReorderPlaceholderAttr   = "data-gosx-reorder-placeholder"
	NavigationReorderDraggingClass     = "gosx-reorder--dragging"
	NavigationReorderLiftedClass       = "gosx-reorder-item--lifted"
	NavigationReorderPlaceholderClass  = "gosx-reorder-item--placeholder"
	NavigationReorderGrabbedClass      = "gosx-reorder-item--grabbed"
	NavigationReorderDefaultItemField  = "item_id"
	NavigationReorderDefaultIndexField = "index"
)

// NormalizeNavigationLinkCurrentPolicy normalizes the declarative "current"
// policy for managed links. Empty values default to "auto".
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
	if !sameNavigationTarget(target, current) {
		if ancestorNavigationTarget(target, current) {
			return "ancestor"
		}
		return "none"
	}
	return "page"
}

// NormalizeNavigationFormMode reports whether a form can be enhanced and, if
// so, returns the managed method policy the client runtime should use.
func NormalizeNavigationFormMode(method, action, target, defaultMethod string) string {
	if strings.TrimSpace(target) != "" {
		return ""
	}
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	if normalizedMethod == "" {
		normalizedMethod = strings.ToUpper(strings.TrimSpace(defaultMethod))
	}
	if normalizedMethod == "" {
		normalizedMethod = http.MethodGet
	}
	switch normalizedMethod {
	case http.MethodGet, http.MethodPost:
	default:
		return ""
	}
	switch normalizedAction := strings.ToLower(strings.TrimSpace(action)); {
	case strings.HasPrefix(normalizedAction, "javascript:"),
		strings.HasPrefix(normalizedAction, "mailto:"),
		strings.HasPrefix(normalizedAction, "tel:"):
		return ""
	}
	return strings.ToLower(normalizedMethod)
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

func ancestorNavigationTarget(parent, child *navigationTarget) bool {
	if parent == nil || child == nil || parent.origin != child.origin {
		return false
	}
	if parent.path == "/" || parent.search != "" {
		return false
	}
	return child.path == parent.path || strings.HasPrefix(child.path, parent.path+"/")
}

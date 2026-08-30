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

	// NavigationLiveSrcAttr and NavigationLiveIntervalAttr declare a
	// live-bound text region (gosx#217): the runtime polls a same-origin JSON
	// object on NavigationLiveIntervalAttr's period (the same duration grammar
	// NavigationRevalidateIntervalAttr uses) and patches the text of every
	// descendant that carries NavigationLiveBindAttr. A bind value is a
	// top-level key into the polled object, or a dot-separated chain of keys
	// through nested objects (never through an array) for one level of
	// grouping, for example "score:t42" or "status.mode" — there is no path
	// language beyond that: an app that needs to key off a record identity
	// (a team, a matchup) names its own flat key server-side, the same way
	// the polled object's shape is already the app's own choice. Only a
	// string, number, or boolean value is bindable; a missing key, a null
	// value, or an object/array value leaves the element's current text
	// untouched rather than blanking it. NavigationLiveFlashClassAttr, on the
	// same bound element, names a class the runtime removes and re-adds
	// (retriggering a CSS animation) whenever that element's resolved text
	// actually changes, including on the region's first tick if the
	// server-rendered text was already stale.
	//
	// This is the regional, high-frequency sibling of
	// NavigationRevalidateIntervalAttr's page-wide fingerprint poll: many
	// independent live regions can exist on one page, each on its own timer,
	// and each fires an immediate tick at setup (subject to the same guards)
	// rather than waiting out a full interval, because the tick's own action
	// here is the cheap text patch itself, not a decision about whether a
	// much heavier full-page revalidation is worth doing. A region that
	// finds the document's focused element, or an element under an active
	// pointer, anywhere inside it skips that tick entirely and retries next
	// tick — the same "never disturb an interaction in progress" contract
	// NavigationReorderAttr's own periodic-revalidation pause enforces for
	// its drag gesture, scoped here to the one live region instead of the
	// whole page. It also sends `If-None-Match` once a response has carried
	// an `ETag`, and skips re-applying a response whose body is
	// byte-identical to the last one even without an `ETag` — the same
	// body-diff short-circuit NavigationRevalidateSrcAttr's own poll
	// already uses.
	//
	// A structurally growing or reordering list the server is already
	// positioned to render (an activity feed, a signal wire) is the other
	// gosx#217 shape: RegionIntervalAttr, in package gosx's own
	// runtime_contract.go, adds periodic polling to the existing
	// data-gosx-region fragment-swap primitive (RegionAttr /
	// RegionURLAttr) instead of a second, competing fragment mechanism
	// living here — see that constant's own doc comment for the full
	// contract.
	//
	// NavigationLiveSignalAttr and NavigationLiveOnAttr (gosx#228) give a
	// live region a manual-refresh trigger, mirroring RegionSignalAttr and
	// RegionEventsAttr in package gosx's own runtime_contract.go exactly: a
	// shared signal named by NavigationLiveSignalAttr, or a hub event named
	// (space/comma-separated) in NavigationLiveOnAttr, forces one immediate
	// fetch of that one region — a managed action's result updating the
	// signal, or broadcasting the hub event, is the declarative "Sync now"
	// control that previously needed bespoke JavaScript. Both compose with
	// NavigationLiveIntervalAttr and with each other; NavigationLiveIntervalAttr
	// is no longer required — a live region naming only a signal or an event
	// trigger polls never and refreshes only on that trigger. A signal- or
	// hub-event-triggered refresh is deliberately allowed even while the
	// document is hidden, a navigation is in flight, or the region holds
	// focus or an active pointer: it is a discrete, user-caused trigger, not
	// a background poll, the same distinction package gosx's own
	// RegionIntervalAttr doc comment draws for RegionSignalAttr/
	// RegionEventsAttr. Every
	// trigger kind — interval, signal, event, or the public
	// window.__gosx.live.refresh(element) API — shares one guard: a region
	// already mid-fetch never starts a second, overlapping one.
	NavigationLiveSrcAttr        = "data-gosx-live-src"
	NavigationLiveIntervalAttr   = "data-gosx-live-interval"
	NavigationLiveBindAttr       = "data-gosx-live-bind"
	NavigationLiveFlashClassAttr = "data-gosx-live-flash-class"
	NavigationLiveSignalAttr     = "data-gosx-live-signal"
	NavigationLiveOnAttr         = "data-gosx-live-on"
	// NavigationLiveModeAttr selects event mode: "event" applies a matched
	// gosx:hub:event's own object payload directly to the binds under the
	// same root, with no fetch at all. NavigationLiveSrcAttr becomes
	// optional in that mode — it still serves the public
	// window.__gosx.live.refresh(element) manual-refresh API, but nothing
	// else, since an event-mode-only root never polls and never fetches on
	// its own.
	NavigationLiveModeAttr = "data-gosx-live-mode"
	// NavigationLiveBindAttrAttr sets a named element attribute from a
	// live-bind key, and NavigationLiveBindClassAttr toggles a named class
	// from a boolean live-bind value. Both share NavigationLiveBindAttr's
	// polled or event-mode payload and its "."-separated key grammar; each
	// takes a comma-separated "target:key[,target:key...]" value. An
	// attribute bind is a POSITIVE allowlist: it writes only a data-*
	// attribute (other than a runtime-owned data-gosx-* attribute, or
	// data-csrf-token/data-csrf, which the runtime itself reads for CSRF
	// token resolution), an aria-* attribute,
	// title/value/datetime/disabled/hidden, or href (a relative or
	// http(s) URL only, after stripping every code point <= 0x20 and
	// normalizing every backslash to a forward slash — matching how a
	// browser resolves a URL — so a scheme or an off-site "//" hidden
	// behind embedded whitespace, a control character, or a backslash
	// cannot slip through). hidden and disabled are boolean attributes:
	// a bound value of true or "true" sets the attribute present with an
	// empty value; false, "false", or a JSON null removes it. Every other
	// value type for a boolean target is refused, leaving the attribute
	// untouched. data-gosx-countdown is the single exception to the
	// data-gosx-* refusal, letting a payload retarget a countdown; it is
	// refused whenever the node also declares data-gosx-countdown-then,
	// so a payload can never trigger a revalidation. Other data-* and
	// aria-* names are read only by the consumer's own code; the runtime
	// reads none of them except the refused ones. Every other target —
	// every on* handler, style, srcdoc, src, srcset, poster, ping,
	// background, action, formaction, target, id, name, class, and
	// xlink:href — is refused by omission (a target containing a colon,
	// such as xlink:href, is also unreachable: the "target:key" grammar
	// splits on the first colon, leaving a bare "xlink" target, which the
	// allowlist refuses on its own).
	NavigationLiveBindAttrAttr  = "data-gosx-live-bind-attr"
	NavigationLiveBindClassAttr = "data-gosx-live-bind-class"
	// NavigationFilterAttr, on an input, names the list it filters
	// (gosx#215): an element id, or — when no element has that id — a CSS
	// selector. Each row inside that target (any descendant, not only a
	// direct child) carries NavigationFilterTextAttr with the text to
	// search; the runtime reads this attribute rather than the row's own
	// rendered text, so the server can normalize case and fold in search
	// terms that never render visibly. Filtering is a case-insensitive
	// substring match against the input's trimmed, lower-cased value,
	// debounced 150ms after the last keystroke; an empty input matches
	// every row. A row containing the focused control, or currently under
	// the pointer, is never hidden mid-interaction. NavigationFilterHiddenClass
	// is a class hook the runtime toggles; the application's own CSS
	// decides what a hidden row looks like. NavigationFilterAnnounceAttr
	// (any truthy value) opts an input into an "N of M shown" live-region
	// announcement after every apply. A filter is rebuilt, and its query
	// re-applied, at page boot and after every soft navigation or
	// revalidation swap — the same rescan lifecycle NavigationWatchAttr
	// follows.
	NavigationFilterAttr         = "data-gosx-filter"
	NavigationFilterTextAttr     = "data-gosx-filter-text"
	NavigationFilterAnnounceAttr = "data-gosx-filter-announce"
	NavigationFilterHiddenClass  = "gosx-filter-row--hidden"

	// NavigationHeartbeatAttr, on an element (or on <body> itself),
	// declares a same-origin endpoint the runtime pings with a plain GET,
	// credentials included, on the NavigationHeartbeatIntervalAttr period
	// (gosx#216) — the same whole-second/whole-minute duration grammar
	// NavigationRevalidateIntervalAttr accepts. At most one ping is ever
	// in flight; an interval tick that lands while the previous ping has
	// not settled is skipped outright. Both a network failure and a
	// non-2xx response are silent — presence detection must never
	// surface a console error for a dropped connection.
	//
	// The ping never stops while the tab stays open. A heartbeat that
	// stops entirely while hidden looks the same, from the server, as a
	// closed browser. That ambiguity punishes an engaged visitor whose
	// tab merely lost focus.
	//
	// While the document is visible, the runtime pings on
	// NavigationHeartbeatIntervalAttr. The moment the document is
	// hidden, it switches to the slower
	// NavigationHeartbeatHiddenIntervalAttr period instead of stopping.
	// Every ping the hidden period starts carries
	// NavigationHeartbeatVisibilityHeader with the value "hidden". A
	// ping the visible period starts carries no such header. A server
	// that never reads the header keeps seeing the pings it already
	// sees, at the pace it already sees, while a tab stays visible. A
	// hidden ping is new traffic; no earlier server relied on its
	// absence. The moment the document becomes visible again, the
	// runtime switches back to NavigationHeartbeatIntervalAttr at once.
	//
	// A body-level heartbeat is set through Context.BodyAttrs (gosx#236),
	// not by wrapping the page body in a gosx.El div solely to carry the
	// attribute:
	//
	//	// Before: a div that exists only to carry two attributes, plus a
	//	// CSS rule (display:contents) to keep it out of layout.
	//	heartbeatShell := gosx.El("div", gosx.Attrs(
	//	    gosx.Attr("class", "gosx-heartbeat-shell"),
	//	    gosx.Attr(server.NavigationHeartbeatAttr, "/api/league/version"),
	//	    gosx.Attr(server.NavigationHeartbeatIntervalAttr, "4s"),
	//	), body)
	//	return server.HTMLDocument(ctx.Document(appName, heartbeatShell))
	//
	//	// After: no wrapper, no CSS rule. ctx.BodyAttrs accumulates onto
	//	// the <body> element HTMLDocument already renders.
	//	ctx.BodyAttrs(
	//	    gosx.Attr(server.NavigationHeartbeatAttr, "/api/league/version"),
	//	    gosx.Attr(server.NavigationHeartbeatIntervalAttr, "4s"),
	//	)
	//	return server.HTMLDocument(ctx.Document(appName, body))
	//
	// A page rendered through App.renderPage's default document pipeline
	// (no router.SetLayout call building the document itself) needs only
	// the ctx.BodyAttrs call — App.renderPage reads the accumulated
	// attributes automatically through DocumentContext.BodyAttrs.
	NavigationHeartbeatAttr         = "data-gosx-heartbeat"
	NavigationHeartbeatIntervalAttr = "data-gosx-heartbeat-interval"

	// NavigationHeartbeatHiddenIntervalAttr sets the ping period the
	// runtime uses instead of NavigationHeartbeatIntervalAttr while the
	// document is hidden. It uses the same whole-second or whole-minute
	// grammar. It is optional. Absent, it defaults to 60s on the client.
	// Present with a value the shared duration grammar cannot parse, it
	// disables the whole heartbeat with one console.warn — the same
	// fail-closed behavior NavigationHeartbeatIntervalAttr already has.
	NavigationHeartbeatHiddenIntervalAttr = "data-gosx-heartbeat-hidden-interval"

	// NavigationHeartbeatVisibilityHeader is the request header a hidden
	// heartbeat ping carries, with the value "hidden". A ping the visible
	// period starts carries no such header. Read this header in your own
	// presence handler to tell a backgrounded tab from a closed browser.
	// Without it, both cases produce the same signal: silence.
	NavigationHeartbeatVisibilityHeader = "X-GoSX-Heartbeat-Visibility"
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
	if !sameNavigationCurrentTarget(target, current) {
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

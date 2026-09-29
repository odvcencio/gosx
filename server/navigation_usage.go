package server

import (
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"m31labs.dev/gosx"
)

// needsNavigation preserves EnableNavigation's existing automatic same-origin
// links, GET forms and framework action forms as well as explicit Link/Form
// opt-ins. It also covers the navigation host's standalone behaviors:
// revalidation, heartbeat, countdowns/cues, watchers, live bindings, filters,
// reorder/transfer gestures, toast dismissal and disclosure/modal controls.
//
// Islands, engines, Scene3D and motion can bootstrap independently, but their
// client code can create links/forms or call navigation; hubs additionally use
// navigation.revalidate for refresh bindings. Keep navigation for any active
// page runtime and registered managed/lifecycle scripts to preserve those APIs.
// Deferred content is not known when the head renders, so keep it there too.
// Inert templates, script text and comments do not opt a static page in.
func (s *PageState) needsNavigation(request *http.Request, bodyHTML string) bool {
	if s.runtime != nil {
		if s.runtime.active {
			return true
		}
		for _, entry := range s.runtime.head {
			if entry.script != nil || navigationContentNeedsRuntime(gosx.RenderHTML(entry.node), request) {
				return true
			}
		}
	}
	if s.deferred != nil && len(s.deferred.blocks) != 0 {
		return true
	}
	return navigationContentNeedsRuntime(bodyHTML, request) ||
		navigationContentNeedsRuntime("<body"+gosx.RenderAttrs(s.bodyAttrs)+">", request) ||
		navigationContentNeedsRuntime(gosx.RenderHTML(gosx.Fragment(s.head...)), request)
}

func navigationContentNeedsRuntime(content string, request *http.Request) bool {
	z := html.NewTokenizer(strings.NewReader(content))
	templateDepth := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return false
		case html.EndTagToken:
			if tag, _ := z.TagName(); string(tag) == "template" && templateDepth > 0 {
				templateDepth--
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := z.Token()
			if token.Data == "template" {
				templateDepth++
			}
			if templateDepth > 0 {
				continue
			}
			attrs := make(map[string]string, len(token.Attr))
			for _, attr := range token.Attr {
				attrs[attr.Key] = attr.Val
				switch attr.Key {
				case NavigationRevalidateIntervalAttr, NavigationHeartbeatAttr,
					NavigationCountdownAttr, NavigationCueToggleAttr, NavigationWatchAttr,
					NavigationLiveSrcAttr, NavigationLiveSignalAttr, NavigationLiveHubAttr,
					NavigationLiveOnAttr, NavigationFilterAttr, NavigationReorderAttr,
					NavigationTransferAttr, "data-gosx-toast-host", "data-gosx-toast-dismiss",
					"data-gosx-disclosure", "data-gosx-disclosure-target",
					"data-gosx-disclosure-close", "data-gosx-disclosure-backdrop",
					"data-gosx-island", "data-gosx-engine", "data-gosx-runtime-surface":
					return true
				}
			}
			if _, native := attrs["data-gosx-native"]; native {
				continue
			}
			if _, opted := attrs[NavigationLinkAttr]; opted {
				return true
			}
			if _, opted := attrs[NavigationFormAttr]; opted {
				return true
			}
			if token.Data == "script" && (attrs["data-gosx-script"] == "lifecycle" || attrs["data-gosx-navigation-replay"] != "") {
				return true
			}
			if token.Data == "a" {
				href := strings.TrimSpace(attrs["href"])
				_, download := attrs["download"]
				if href != "" && !strings.HasPrefix(href, "#") && attrs["target"] == "" && !download && navigationSameOriginTarget(href, request) {
					return true
				}
			}
			if token.Data == "form" {
				if managed, exists := attrs["data-gosx-managed"]; exists {
					if gosx.ManagedFormShorthandTruthy(false, managed) {
						return true
					}
					continue
				}
				method := strings.ToUpper(strings.TrimSpace(attrs["method"]))
				if attrs["target"] == "" && navigationSameOriginTarget(attrs["action"], request) &&
					(method == "" || method == http.MethodGet || method == http.MethodPost && navigationFrameworkAction(attrs["action"])) {
					return true
				}
			}
			// Submitters can opt an otherwise native POST form into GET or a
			// framework action without changing the form's own attributes.
			if (token.Data == "button" || token.Data == "input") && attrs["formtarget"] == "" &&
				(strings.EqualFold(attrs["formmethod"], "get") || navigationFrameworkAction(attrs["formaction"])) {
				return true
			}
		}
	}
}

func navigationSameOriginTarget(raw string, request *http.Request) bool {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || target.Scheme != "" && target.Scheme != "http" && target.Scheme != "https" {
		return false
	}
	if target.Host == "" {
		return true
	}
	if request == nil {
		return true // No origin evidence: conservatively preserve enhancement.
	}
	host := request.Host
	if host == "" && request.URL != nil {
		host = request.URL.Host
	}
	// TLS termination may hide the browser's scheme. Compare hosts and keep
	// the runtime for either HTTP scheme; its browser guard checks the origin.
	normalize := func(host string) string {
		host = strings.ToLower(host)
		return strings.TrimSuffix(strings.TrimSuffix(host, ":80"), ":443")
	}
	return normalize(target.Host) == normalize(host)
}

func navigationFrameworkAction(raw string) bool {
	target, err := url.Parse(raw)
	if err != nil {
		return false
	}
	_, name, found := strings.Cut(target.EscapedPath(), "/__actions/")
	name = strings.TrimSuffix(name, "/")
	return found && name != "" && !strings.Contains(name, "/")
}

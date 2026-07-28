package server

import (
	"net/http"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestNavigationBeaconRendersTypedJSONContract(t *testing.T) {
	html := gosx.RenderHTML(NavigationBeacon(NavigationBeaconOptions{
		Name:              "first-party-pageview",
		URL:               "/__internal/attribution/pageview",
		Method:            http.MethodPost,
		Credentials:       "same-origin",
		Keepalive:         true,
		PathField:         "path",
		NavigationIDField: "navigation_id",
	}))

	for _, snippet := range []string{
		`type="application/json"`,
		`data-gosx-navigation-beacon`,
		`"url":"/__internal/attribution/pageview"`,
		`"method":"POST"`,
		`"credentials":"same-origin"`,
		`"keepalive":true`,
		`"pathField":"path"`,
		`"navigationIDField":"navigation_id"`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in %q", snippet, html)
		}
	}
}

func TestPageRuntimeNavigationBeaconRendersInHead(t *testing.T) {
	runtime := NewPageRuntime()
	runtime.NavigationBeacon(NavigationBeaconOptions{
		URL: "/__internal/attribution/pageview",
	})

	html := gosx.RenderHTML(runtime.Head())
	if !strings.Contains(html, `data-gosx-navigation-beacon`) {
		t.Fatalf("expected runtime navigation beacon, got %q", html)
	}
}

func TestNavigationScriptIncludesBeaconDispatcher(t *testing.T) {
	html := gosx.RenderHTML(NavigationScript())

	for _, snippet := range []string{
		`data-gosx-navigation-beacon`,
		`function dispatchNavigationBeacons()`,
		`document.addEventListener("gosx:navigate", dispatchNavigationBeacons)`,
	} {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected %q in navigation runtime", snippet)
		}
	}
}

package server

import (
	_ "embed"
	"encoding/json"
	"strings"

	"m31labs.dev/gosx"
)

//go:embed navigation_runtime.js
var navigationRuntime string

// NavigationBeaconOptions describes a navigation lifecycle beacon owned by the
// GoSX navigation runtime. The runtime sends it after a successful soft
// navigation and skips repeated pathname+search values.
type NavigationBeaconOptions struct {
	Name              string
	URL               string
	Method            string
	Event             string
	ContentType       string
	Credentials       string
	Keepalive         bool
	PathField         string
	NavigationIDField string
}

type navigationBeaconContract struct {
	Name              string `json:"name,omitempty"`
	URL               string `json:"url"`
	Method            string `json:"method,omitempty"`
	Event             string `json:"event,omitempty"`
	ContentType       string `json:"contentType,omitempty"`
	Credentials       string `json:"credentials,omitempty"`
	Keepalive         bool   `json:"keepalive,omitempty"`
	PathField         string `json:"pathField,omitempty"`
	NavigationIDField string `json:"navigationIDField,omitempty"`
}

// NavigationScript returns the inline GoSX page-navigation runtime.
func NavigationScript() gosx.Node {
	return gosx.RawHTML(`<script data-gosx-navigation="true">` + navigationRuntime + `</script>`)
}

// NavigationBeacon renders a typed navigation beacon contract consumed by the
// GoSX navigation runtime.
func NavigationBeacon(opts NavigationBeaconOptions) gosx.Node {
	contract := navigationBeaconContract{
		Name:              strings.TrimSpace(opts.Name),
		URL:               strings.TrimSpace(opts.URL),
		Method:            strings.TrimSpace(opts.Method),
		Event:             strings.TrimSpace(opts.Event),
		ContentType:       strings.TrimSpace(opts.ContentType),
		Credentials:       strings.TrimSpace(opts.Credentials),
		Keepalive:         opts.Keepalive,
		PathField:         strings.TrimSpace(opts.PathField),
		NavigationIDField: strings.TrimSpace(opts.NavigationIDField),
	}
	if contract.URL == "" {
		return gosx.Text("")
	}
	payload, err := json.Marshal(contract)
	if err != nil {
		return gosx.Text("")
	}
	safe := strings.NewReplacer(
		"<", "\\u003c",
		">", "\\u003e",
		"&", "\\u0026",
	).Replace(string(payload))
	return gosx.RawHTML(`<script type="application/json" data-gosx-navigation-beacon>` + safe + `</script>`)
}

// Link renders an anchor tag opted into the GoSX page-navigation runtime.
func Link(href string, args ...any) gosx.Node {
	attrs := gosx.Attrs(
		gosx.Attr("href", href),
		gosx.BoolAttr(NavigationLinkAttr),
		gosx.Attr(NavigationLinkStateAttr, "idle"),
		gosx.Attr(NavigationLinkCurrentPolicyAttr, "auto"),
		gosx.Attr(NavigationLinkPrefetchStateAttr, "idle"),
	)
	attrs = append(attrs, gosx.ProgressiveEnhancementAttrs(gosx.ProgressiveEnhancementOptions{
		Kind:     "navigation",
		Layer:    "bootstrap",
		Fallback: "native-link",
	})...)
	prefixed := append([]any{
		attrs,
	}, args...)
	return gosx.El("a", prefixed...)
}

// Form renders a form tag opted into the GoSX navigation/runtime submission
// layer while preserving native HTML fallback behavior.
func Form(args ...any) gosx.Node {
	prefixed := append([]any{
		gosx.ManagedFormAttrs(gosx.ManagedFormOptions{
			State:    "idle",
			Layer:    "bootstrap",
			Fallback: "native-form",
		}),
	}, args...)
	return gosx.El("form", prefixed...)
}

// HeadOutlet wraps head content in stable markers so the navigation runtime can
// replace managed head nodes during client-side page swaps.
func HeadOutlet(head gosx.Node) gosx.Node {
	return gosx.Fragment(
		gosx.RawHTML(`<meta name="gosx-head-start" content="">`),
		head,
		gosx.RawHTML(`<meta name="gosx-head-end" content="">`),
	)
}

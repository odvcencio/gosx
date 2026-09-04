// Package stripeui provides a server-first Stripe integration for GoSX.
// Hosted Checkout is an ordinary same-origin POST form; Stripe.js is only
// required for Stripe-hosted secure inputs such as Elements and embedded
// Checkout.
package stripeui

import (
	"encoding/json"
	"errors"
	"html"
	"net/url"
	"strings"
	"sync/atomic"
	"unicode"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

const (
	// DefaultStripeJSURL is deliberately not configurable. Stripe.js must be
	// loaded directly from Stripe rather than copied, proxied, or self-hosted.
	DefaultStripeJSURL = "https://js.stripe.com/clover/stripe.js"
	DefaultBridgePath  = "/gosx/stripe-bridge.js"

	ElementPayment                = "payment"
	ElementExpressCheckout        = "expressCheckout"
	ElementAddress                = "address"
	ElementLinkAuthentication     = "linkAuthentication"
	ElementPaymentMethodMessaging = "paymentMethodMessaging"
	ElementCurrencySelector       = "currencySelector"
	ElementTaxID                  = "taxId"
	ElementCard                   = "card"
	ElementCardNumber             = "cardNumber"
	ElementCardExpiry             = "cardExpiry"
	ElementCardCVC                = "cardCvc"
	ElementIBAN                   = "iban"
	ElementIdealBank              = "idealBank"
	ElementAUBankAccount          = "auBankAccount"

	ConfirmPayment = "confirmPayment"
	ConfirmSetup   = "confirmSetup"

	runtimeSurfaceElements = "stripe-elements"
	runtimeSurfaceEmbedded = "stripe-embedded-checkout"
	runtimeSurfaceCheckout = "stripe-checkout"
	runtimeVersion         = "1"
)

var (
	idSeq                   uint64
	errInvalidSessionAction = errors.New("stripeui: session action must be a root-relative same-origin path without a query or fragment")
)

// RuntimeConfig declares the local bridge asset needed by managed Stripe
// surfaces. It does not apply to HostedCheckoutForm, which needs no browser
// runtime at all.
type RuntimeConfig struct {
	BridgePath string
	Preconnect bool
}

// Page is the narrow interface shared by server.Context and route.RouteContext.
type Page interface {
	AddHead(...gosx.Node)
	Runtime() *server.PageRuntime
}

// Require adds Stripe.js from js.stripe.com and the GoSX bridge to a page that
// contains a managed Stripe surface. Hosted Checkout does not call Require.
func Require(page Page, cfg RuntimeConfig) {
	if page == nil {
		return
	}
	if runtime := page.Runtime(); runtime != nil {
		runtime.EnableBootstrap()
	}
	page.AddHead(Head(cfg))
}

// Head renders the assets required by managed Stripe surfaces. Stripe.js is
// always loaded directly from Stripe; only the app-owned bridge path may be
// configured.
func Head(cfg RuntimeConfig) gosx.Node {
	bridge := normalizeBridgePath(cfg.BridgePath)
	if bridge == "" {
		bridge = DefaultBridgePath
	}
	nodes := []gosx.Node{}
	if cfg.Preconnect {
		nodes = append(nodes, gosx.El("link", gosx.Attrs(
			gosx.Attr("rel", "preconnect"),
			gosx.Attr("href", "https://js.stripe.com"),
		)))
	}
	nodes = append(nodes,
		server.ManagedScript(DefaultStripeJSURL, server.ManagedScriptOptions{
			Role: server.ManagedScriptRoleManaged,
		}, gosx.Attrs(gosx.BoolAttr("defer"))),
		server.LifecycleScript(bridge, gosx.Attrs(gosx.BoolAttr("defer"))),
	)
	return gosx.Fragment(nodes...)
}

// BaseProps are shared by rendered Stripe components. Arbitrary attributes
// are intentionally not accepted: security-owned runtime/action attributes
// must not be shadowed by duplicate authored attributes.
type BaseProps struct {
	ID    string
	Class string
}

// RuntimeOptions configure Stripe(publishableKey, options). PublishableKey is
// browser-safe. Secret-bearing option keys are removed before serialization.
type RuntimeOptions struct {
	PublishableKey string
	StripeOptions  map[string]any
}

// HostedCheckoutProps configures the zero-runtime Checkout form. Action is an
// app-owned endpoint that creates a Checkout Session with Stripe's API and
// answers the native POST with a 303 redirect to the returned Checkout URL.
type HostedCheckoutProps struct {
	BaseProps
	Action    string
	CSRFToken string
}

// HostedCheckoutForm renders a native POST form. It never emits Stripe.js,
// bridge metadata, a publishable key, or a Checkout Session value. Invalid
// action paths render a non-submitting group rather than falling back to the
// current document URL.
func HostedCheckoutForm(props HostedCheckoutProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("hosted-checkout"))
	if len(children) == 0 {
		children = []gosx.Node{gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit")), gosx.Text("Checkout"))}
	}
	actionPath := normalizeSessionAction(props.Action)
	if actionPath == "" {
		attrs := baseAttrs(props.BaseProps, "gosx-stripe-hosted-checkout")
		attrs = append(attrs,
			gosx.Attr("id", id),
			gosx.Attr("role", "group"),
			gosx.BoolAttr("data-gosx-stripe-invalid-action"),
		)
		return gosx.El("div", nodeArgs(attrs, children...)...)
	}
	if strings.TrimSpace(props.CSRFToken) != "" {
		children = append([]gosx.Node{gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", "csrf_token"),
			gosx.Attr("value", props.CSRFToken),
		))}, children...)
	}
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-hosted-checkout")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("method", "post"),
		gosx.Attr("action", actionPath),
	)
	return gosx.El("form", nodeArgs(attrs, children...)...)
}

// ValidateSessionAction checks the endpoint contract used by managed Stripe
// surfaces and HostedCheckoutForm. Query strings are intentionally rejected:
// session selection belongs in server-owned routing or POST data, not a
// browser-visible capability URL.
func ValidateSessionAction(path string) error {
	if normalizeSessionAction(path) == "" {
		return errInvalidSessionAction
	}
	return nil
}

// ElementsSurfaceProps configures stripe.elements(...). SessionAction is the
// fixed same-origin POST endpoint that returns {"clientSecret":"..."}.
type ElementsSurfaceProps struct {
	BaseProps
	RuntimeOptions
	SessionAction   string
	ElementsOptions map[string]any
}

// Elements renders a managed provider for PaymentIntent/SetupIntent Elements.
func Elements(props ElementsSurfaceProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("surface"))
	cfg := map[string]any{
		"publishableKey":  normalizePublishableKey(props.PublishableKey),
		"stripeOptions":   props.StripeOptions,
		"sessionAction":   normalizeSessionAction(props.SessionAction),
		"elementsOptions": props.ElementsOptions,
	}
	configID, config := configScript("surface", cfg)
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-surface")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	nodes := append([]gosx.Node{config}, children...)
	return gosx.RuntimeSurface("section", gosx.RuntimeSurfaceOptions{
		Name: runtimeSurfaceElements, Version: runtimeVersion, Fallback: "server",
	}, nodeArgs(attrs, nodes...)...)
}

// ElementProps configures a single stripe.elements().create(type, options)
// mount. Lifecycle events are fixed and redacted by the bridge.
type ElementProps struct {
	BaseProps
	Type    string
	Options map[string]any
}

// Element renders a generic Stripe Element mount. Use Type for newly added
// Stripe Element names without waiting for GoSX to add a typed helper.
func Element(props ElementProps) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("element"))
	typ := firstNonEmpty(props.Type, ElementPayment)
	configID, config := configScript("element", map[string]any{"options": props.Options})
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-element")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("data-gosx-stripe-element", typ),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	return gosx.Fragment(config, gosx.El("div", attrs))
}

func PaymentElement(props ElementProps) gosx.Node {
	props.Type = ElementPayment
	return Element(props)
}

func ExpressCheckoutElement(props ElementProps) gosx.Node {
	props.Type = ElementExpressCheckout
	return Element(props)
}

func AddressElement(props ElementProps) gosx.Node {
	props.Type = ElementAddress
	return Element(props)
}

func LinkAuthenticationElement(props ElementProps) gosx.Node {
	props.Type = ElementLinkAuthentication
	return Element(props)
}

func PaymentMethodMessagingElement(props ElementProps) gosx.Node {
	props.Type = ElementPaymentMethodMessaging
	return Element(props)
}

func CurrencySelectorElement(props ElementProps) gosx.Node {
	props.Type = ElementCurrencySelector
	return Element(props)
}

func TaxIDElement(props ElementProps) gosx.Node {
	props.Type = ElementTaxID
	return Element(props)
}

// ConfirmProps configures a control that calls a fixed Stripe confirmation
// method. ReturnPath must be a root-relative same-origin path and is expanded
// to an absolute URL by the browser bridge.
type ConfirmProps struct {
	BaseProps
	Method     string
	ReturnPath string
	Redirect   string
	SkipSubmit bool
}

// ConfirmForm retains the familiar component name but renders a non-submitting
// group. Without the managed surface runtime it cannot accidentally POST card
// UI state to the app.
func ConfirmForm(props ConfirmProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("confirm"))
	if len(children) == 0 {
		children = []gosx.Node{gosx.El("button", gosx.Attrs(gosx.Attr("type", "button")), gosx.Text("Pay"))}
	}
	cfg := map[string]any{
		"method":     normalizeConfirmMethod(props.Method),
		"returnPath": normalizeReturnPath(props.ReturnPath),
		"redirect":   normalizeRedirectMode(props.Redirect),
	}
	if props.SkipSubmit {
		cfg["submit"] = false
	}
	configID, config := configScript("confirm", cfg)
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-confirm")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("role", "group"),
		gosx.Attr("data-gosx-stripe-confirm", normalizeConfirmMethod(props.Method)),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	nodes := append([]gosx.Node{config}, children...)
	return gosx.El("div", nodeArgs(attrs, nodes...)...)
}

// EmbeddedCheckoutProps configures stripe.initEmbeddedCheckout(...).
type EmbeddedCheckoutProps struct {
	BaseProps
	RuntimeOptions
	SessionAction string
}

func EmbeddedCheckout(props EmbeddedCheckoutProps) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("embedded"))
	configID, config := configScript("embedded", map[string]any{
		"publishableKey": normalizePublishableKey(props.PublishableKey),
		"stripeOptions":  props.StripeOptions,
		"sessionAction":  normalizeSessionAction(props.SessionAction),
	})
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-embedded")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	return gosx.RuntimeSurface("div", gosx.RuntimeSurfaceOptions{
		Name: runtimeSurfaceEmbedded, Version: runtimeVersion, Fallback: "server",
	}, nodeArgs(attrs, config)...)
}

// CheckoutProps configures Stripe's custom Checkout runtime. SessionAction is
// the fixed same-origin POST endpoint that creates the client session.
type CheckoutProps struct {
	BaseProps
	RuntimeOptions
	SessionAction   string
	ElementsOptions map[string]any
}

func Checkout(props CheckoutProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("checkout"))
	configID, config := configScript("checkout", map[string]any{
		"publishableKey":  normalizePublishableKey(props.PublishableKey),
		"stripeOptions":   props.StripeOptions,
		"sessionAction":   normalizeSessionAction(props.SessionAction),
		"elementsOptions": props.ElementsOptions,
	})
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-checkout")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	nodes := append([]gosx.Node{config}, children...)
	return gosx.RuntimeSurface("section", gosx.RuntimeSurfaceOptions{
		Name: runtimeSurfaceCheckout, Version: runtimeVersion, Fallback: "server",
	}, nodeArgs(attrs, nodes...)...)
}

type CheckoutElementProps struct {
	ElementProps
	Create string
}

func CheckoutElement(props CheckoutElementProps) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("checkout-element"))
	typ := firstNonEmpty(props.Type, ElementPayment)
	configID, config := configScript("checkout-element", map[string]any{
		"create": props.Create, "options": props.Options,
	})
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-checkout-element")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("data-gosx-stripe-checkout-element", typ),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	return gosx.Fragment(config, gosx.El("div", attrs))
}

func CheckoutPaymentElement(props CheckoutElementProps) gosx.Node {
	props.Type = ElementPayment
	props.Create = firstNonEmpty(props.Create, "createPaymentElement")
	return CheckoutElement(props)
}

func CheckoutExpressCheckoutElement(props CheckoutElementProps) gosx.Node {
	props.Type = ElementExpressCheckout
	props.Create = firstNonEmpty(props.Create, "createExpressCheckoutElement")
	return CheckoutElement(props)
}

type CheckoutConfirmProps struct {
	BaseProps
}

// CheckoutConfirm renders a button that calls Checkout actions.confirm. Its
// completion event is UX state only; webhooks remain authoritative.
func CheckoutConfirm(props CheckoutConfirmProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("checkout-confirm"))
	if len(children) == 0 {
		children = []gosx.Node{gosx.Text("Pay")}
	}
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-checkout-confirm")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("type", "button"),
		gosx.BoolAttr("data-gosx-stripe-checkout-confirm"),
	)
	return gosx.El("button", nodeArgs(attrs, children...)...)
}

// DesignTokens can be mapped into Stripe's Appearance API variables.
type DesignTokens struct {
	Theme           string
	FontFamily      string
	ColorPrimary    string
	ColorBackground string
	ColorText       string
	ColorDanger     string
	BorderRadius    string
	SpacingUnit     string
}

func AppearanceFromTokens(tokens DesignTokens) map[string]any {
	variables := map[string]any{}
	put := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			variables[key] = strings.TrimSpace(value)
		}
	}
	put("fontFamily", tokens.FontFamily)
	put("colorPrimary", tokens.ColorPrimary)
	put("colorBackground", tokens.ColorBackground)
	put("colorText", tokens.ColorText)
	put("colorDanger", tokens.ColorDanger)
	put("borderRadius", tokens.BorderRadius)
	put("spacingUnit", tokens.SpacingUnit)
	appearance := map[string]any{}
	if strings.TrimSpace(tokens.Theme) != "" {
		appearance["theme"] = strings.TrimSpace(tokens.Theme)
	}
	if len(variables) > 0 {
		appearance["variables"] = variables
	}
	return appearance
}

func configScript(prefix string, value any) (string, gosx.Node) {
	id := nextID(prefix + "-config")
	raw, err := json.Marshal(value)
	var decoded any
	if err == nil {
		err = json.Unmarshal(raw, &decoded)
	}
	data, marshalErr := json.Marshal(sanitizeConfigValue(decoded))
	if err == nil {
		err = marshalErr
	}
	if err != nil {
		data = []byte("{}")
	}
	safe := strings.NewReplacer("<", "\\u003c", ">", "\\u003e", "&", "\\u0026").Replace(string(data))
	htmlID := html.EscapeString(id)
	return id, gosx.RawHTML(`<script id="` + htmlID + `" type="application/json" data-gosx-stripe-config>` + safe + `</script>`)
}

func sanitizeConfigValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for key, item := range typed {
			if forbiddenConfigKey(key) {
				continue
			}
			clean[key] = sanitizeConfigValue(item)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for index, item := range typed {
			clean[index] = sanitizeConfigValue(item)
		}
		return clean
	default:
		return value
	}
}

func forbiddenConfigKey(key string) bool {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
	switch normalized {
	case "clientsecret", "authorization", "headers", "body", "apikey", "secretkey", "accesstoken":
		return true
	default:
		return false
	}
}

func normalizeSessionAction(raw string) string {
	if raw == "" || strings.TrimSpace(raw) != raw || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return ""
	}
	if strings.ContainsAny(raw, "\\?#") || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return ""
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" {
		return ""
	}
	if parsed.Path == "" || !strings.HasPrefix(parsed.Path, "/") || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return raw
}

func normalizeReturnPath(raw string) string {
	return normalizeSessionAction(raw)
}

func normalizeBridgePath(raw string) string {
	return normalizeSessionAction(strings.TrimSpace(raw))
}

func normalizePublishableKey(raw string) string {
	key := strings.TrimSpace(raw)
	if !strings.HasPrefix(key, "pk_") || len(key) > 256 || strings.IndexFunc(key, unicode.IsSpace) >= 0 {
		return ""
	}
	return key
}

func normalizeConfirmMethod(method string) string {
	if strings.TrimSpace(method) == ConfirmSetup {
		return ConfirmSetup
	}
	return ConfirmPayment
}

func normalizeRedirectMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "always", "if_required":
		return strings.TrimSpace(mode)
	default:
		return ""
	}
}

func baseAttrs(props BaseProps, classes ...string) gosx.AttrList {
	class := strings.TrimSpace(strings.Join(append(classes, props.Class), " "))
	attrs := gosx.Attrs()
	if class != "" {
		attrs = append(attrs, gosx.Attr("class", class))
	}
	return attrs
}

func nodeArgs(attrs gosx.AttrList, children ...gosx.Node) []any {
	args := make([]any, 0, 1+len(children))
	if len(attrs) > 0 {
		args = append(args, attrs)
	}
	for _, child := range children {
		args = append(args, child)
	}
	return args
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nextID(prefix string) string {
	return "gosx-stripe-" + prefix + "-" + fmtID(atomic.AddUint64(&idSeq, 1))
}

func fmtID(value uint64) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if value == 0 {
		return "0"
	}
	var buffer [32]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = digits[value%uint64(len(digits))]
		value /= uint64(len(digits))
	}
	return string(buffer[index:])
}

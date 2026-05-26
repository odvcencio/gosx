// Package stripeui provides GoSX-native components for the mandatory
// Stripe.js browser boundary. The app keeps checkout state in Go while this
// package owns the small runtime bridge needed for Stripe-hosted secure inputs.
package stripeui

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"sync/atomic"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

const (
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
)

var idSeq uint64

// RuntimeConfig declares the browser assets needed by Stripe surfaces.
type RuntimeConfig struct {
	StripeJSURL string
	BridgePath  string
	Preconnect  bool
}

// Page is the narrow interface shared by server.Context and route.RouteContext.
type Page interface {
	AddHead(...gosx.Node)
}

// Require adds the Stripe.js direct script and the GoSX Stripe bridge to a page.
func Require(page Page, cfg RuntimeConfig) {
	if page == nil {
		return
	}
	page.AddHead(Head(cfg))
}

// Head renders only the assets needed by pages that use Stripe components.
func Head(cfg RuntimeConfig) gosx.Node {
	stripeJS := strings.TrimSpace(cfg.StripeJSURL)
	if stripeJS == "" {
		stripeJS = DefaultStripeJSURL
	}
	bridge := strings.TrimSpace(cfg.BridgePath)
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
		server.ManagedScript(stripeJS, server.ManagedScriptOptions{
			Role: server.ManagedScriptRoleManaged,
			Load: server.ManagedScriptLoadDOM,
		}, gosx.Attrs(gosx.BoolAttr("defer"))),
		server.LifecycleScript(bridge, gosx.Attrs(gosx.BoolAttr("defer"))),
	)
	return gosx.Fragment(nodes...)
}

// BaseProps are shared by rendered Stripe mount components.
type BaseProps struct {
	ID    string
	Class string
	Attrs gosx.AttrList
}

// FetchRequest describes a server endpoint the bridge can call for a client
// secret or Checkout Session. Body can be any JSON-marshalable value.
type FetchRequest struct {
	URL     string            `json:"url,omitempty"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    any               `json:"body,omitempty"`
}

// RuntimeOptions configure Stripe(publishableKey, options).
type RuntimeOptions struct {
	PublishableKey string         `json:"publishableKey,omitempty"`
	StripeJSURL    string         `json:"stripeJS,omitempty"`
	StripeOptions  map[string]any `json:"stripeOptions,omitempty"`
}

// ElementsSurfaceProps configures stripe.elements(...).
type ElementsSurfaceProps struct {
	BaseProps
	RuntimeOptions
	ClientSecret        string         `json:"clientSecret,omitempty"`
	ClientSecretRequest *FetchRequest  `json:"clientSecretRequest,omitempty"`
	ElementsOptions     map[string]any `json:"elementsOptions,omitempty"`
}

// Elements renders a provider for PaymentIntent/SetupIntent Elements.
func Elements(props ElementsSurfaceProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("surface"))
	cfg := map[string]any{
		"publishableKey":      props.PublishableKey,
		"stripeJS":            firstNonEmpty(props.StripeJSURL, DefaultStripeJSURL),
		"stripeOptions":       props.StripeOptions,
		"clientSecret":        props.ClientSecret,
		"clientSecretRequest": props.ClientSecretRequest,
		"elementsOptions":     props.ElementsOptions,
	}
	configID, config := configScript("surface", cfg)
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-surface")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.BoolAttr("data-gosx-stripe-surface"),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	nodes := append([]gosx.Node{config}, children...)
	return gosx.El("section", nodeArgs(attrs, nodes...)...)
}

// ElementProps configures a single stripe.elements().create(type, options) mount.
type ElementProps struct {
	BaseProps
	Type    string         `json:"type,omitempty"`
	Options map[string]any `json:"options,omitempty"`
	Events  []string       `json:"events,omitempty"`
}

// Element renders a generic Stripe Element mount. Use Type for newly added
// Stripe Element names without waiting for GoSX to add a typed helper.
func Element(props ElementProps) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("element"))
	typ := firstNonEmpty(props.Type, ElementPayment)
	cfg := map[string]any{
		"options": props.Options,
		"events":  props.Events,
	}
	configID, config := configScript("element", cfg)
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

// ConfirmProps configures a form that calls stripe.confirmPayment,
// stripe.confirmSetup, or another Stripe.js confirmation method.
type ConfirmProps struct {
	BaseProps
	Method        string         `json:"method,omitempty"`
	ClientSecret  string         `json:"clientSecret,omitempty"`
	ReturnURL     string         `json:"returnUrl,omitempty"`
	Redirect      string         `json:"redirect,omitempty"`
	Params        map[string]any `json:"params,omitempty"`
	ConfirmParams map[string]any `json:"confirmParams,omitempty"`
	SkipSubmit    bool           `json:"skipSubmit,omitempty"`
}

// ConfirmForm renders a server-authored form whose submit is handled by the
// Stripe bridge. Without JavaScript, it remains inert instead of posting card
// details to the server.
func ConfirmForm(props ConfirmProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("confirm"))
	if len(children) == 0 {
		children = []gosx.Node{gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit")), gosx.Text("Pay"))}
	}
	cfg := map[string]any{
		"method":        firstNonEmpty(props.Method, ConfirmPayment),
		"clientSecret":  props.ClientSecret,
		"returnUrl":     props.ReturnURL,
		"redirect":      props.Redirect,
		"params":        props.Params,
		"confirmParams": props.ConfirmParams,
	}
	if props.SkipSubmit {
		cfg["submit"] = false
	}
	configID, config := configScript("confirm", cfg)
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-confirm")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("method", "post"),
		gosx.Attr("data-gosx-stripe-confirm", firstNonEmpty(props.Method, ConfirmPayment)),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	nodes := append([]gosx.Node{config}, children...)
	return gosx.El("form", nodeArgs(attrs, nodes...)...)
}

// EmbeddedCheckoutProps configures stripe.initEmbeddedCheckout(...).
type EmbeddedCheckoutProps struct {
	BaseProps
	RuntimeOptions
	ClientSecret        string         `json:"clientSecret,omitempty"`
	ClientSecretRequest *FetchRequest  `json:"clientSecretRequest,omitempty"`
	Init                map[string]any `json:"init,omitempty"`
}

func EmbeddedCheckout(props EmbeddedCheckoutProps) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("embedded"))
	cfg := map[string]any{
		"publishableKey":      props.PublishableKey,
		"stripeJS":            firstNonEmpty(props.StripeJSURL, DefaultStripeJSURL),
		"stripeOptions":       props.StripeOptions,
		"clientSecret":        props.ClientSecret,
		"clientSecretRequest": props.ClientSecretRequest,
		"init":                props.Init,
	}
	configID, config := configScript("embedded", cfg)
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-embedded")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.BoolAttr("data-gosx-stripe-embedded-checkout"),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	return gosx.Fragment(config, gosx.El("div", attrs))
}

// CheckoutProps configures Stripe's Checkout Sessions custom checkout runtime
// via stripe.initCheckout(...).
type CheckoutProps struct {
	BaseProps
	RuntimeOptions
	ClientSecret        string         `json:"clientSecret,omitempty"`
	ClientSecretRequest *FetchRequest  `json:"clientSecretRequest,omitempty"`
	Init                map[string]any `json:"init,omitempty"`
	ElementsOptions     map[string]any `json:"elementsOptions,omitempty"`
}

func Checkout(props CheckoutProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("checkout"))
	cfg := map[string]any{
		"publishableKey":      props.PublishableKey,
		"stripeJS":            firstNonEmpty(props.StripeJSURL, DefaultStripeJSURL),
		"stripeOptions":       props.StripeOptions,
		"clientSecret":        props.ClientSecret,
		"clientSecretRequest": props.ClientSecretRequest,
		"init":                props.Init,
		"elementsOptions":     props.ElementsOptions,
	}
	configID, config := configScript("checkout", cfg)
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-checkout")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.BoolAttr("data-gosx-stripe-checkout"),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	nodes := append([]gosx.Node{config}, children...)
	return gosx.El("section", nodeArgs(attrs, nodes...)...)
}

// CheckoutElementProps configures a Checkout object element factory such as
// createPaymentElement or createExpressCheckoutElement.
type CheckoutElementProps struct {
	ElementProps
	Create string `json:"create,omitempty"`
}

func CheckoutElement(props CheckoutElementProps) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("checkout-element"))
	typ := firstNonEmpty(props.Type, ElementPayment)
	cfg := map[string]any{
		"create":  props.Create,
		"options": props.Options,
		"events":  props.Events,
	}
	configID, config := configScript("checkout-element", cfg)
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

// CheckoutConfirm renders a button or form that calls Checkout actions.confirm.
func CheckoutConfirm(props ConfirmProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("checkout-confirm"))
	if len(children) == 0 {
		children = []gosx.Node{gosx.Text("Pay")}
	}
	cfg := map[string]any{"params": props.Params}
	configID, config := configScript("checkout-confirm", cfg)
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-checkout-confirm")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("type", "button"),
		gosx.BoolAttr("data-gosx-stripe-checkout-confirm"),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	nodes := append([]gosx.Node{config}, gosx.El("button", nodeArgs(attrs, children...)...))
	return gosx.Fragment(nodes...)
}

// RedirectCheckoutProps configures stripe.redirectToCheckout or direct Session
// URL navigation for hosted Checkout.
type RedirectCheckoutProps struct {
	BaseProps
	RuntimeOptions
	SessionID      string        `json:"sessionId,omitempty"`
	URL            string        `json:"url,omitempty"`
	SessionRequest *FetchRequest `json:"sessionRequest,omitempty"`
}

func RedirectCheckoutForm(props RedirectCheckoutProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("redirect"))
	if len(children) == 0 {
		children = []gosx.Node{gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit")), gosx.Text("Checkout"))}
	}
	cfg := map[string]any{
		"publishableKey": props.PublishableKey,
		"stripeJS":       firstNonEmpty(props.StripeJSURL, DefaultStripeJSURL),
		"stripeOptions":  props.StripeOptions,
		"sessionId":      props.SessionID,
		"url":            props.URL,
		"sessionRequest": props.SessionRequest,
	}
	configID, config := configScript("redirect", cfg)
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-redirect")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("method", "post"),
		gosx.BoolAttr("data-gosx-stripe-redirect"),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	nodes := append([]gosx.Node{config}, children...)
	return gosx.El("form", nodeArgs(attrs, nodes...)...)
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
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte("{}")
	}
	safe := strings.NewReplacer("<", "\\u003c", ">", "\\u003e", "&", "\\u0026").Replace(string(data))
	htmlID := html.EscapeString(id)
	return id, gosx.RawHTML(`<script id="` + htmlID + `" type="application/json" data-gosx-stripe-config>` + safe + `</script>`)
}

func baseAttrs(props BaseProps, classes ...string) gosx.AttrList {
	class := strings.TrimSpace(strings.Join(append(classes, props.Class), " "))
	attrs := gosx.Attrs()
	if class != "" {
		attrs = append(attrs, gosx.Attr("class", class))
	}
	attrs = append(attrs, props.Attrs...)
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
	prefix = strings.Trim(strings.ToLower(prefix), "-")
	if prefix == "" {
		prefix = "stripe"
	}
	return fmt.Sprintf("gosx-stripe-%s-%d", prefix, atomic.AddUint64(&idSeq, 1))
}

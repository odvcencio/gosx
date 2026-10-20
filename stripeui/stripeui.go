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

	ConfirmPayment ConfirmMethod = "confirmPayment"
	ConfirmSetup   ConfirmMethod = "confirmSetup"

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
	Runtime() *server.PageRuntime
}

// Require adds Stripe.js from js.stripe.com and the GoSX bridge to a page that
// contains a managed Stripe surface. Hosted Checkout does not call Require.
func Require(page Page, cfg RuntimeConfig) {
	if page == nil {
		return
	}
	runtime := page.Runtime()
	if runtime == nil {
		return
	}
	runtime.EnableBootstrap()
	bridge := normalizeBridgePath(cfg.BridgePath)
	if bridge == "" {
		bridge = DefaultBridgePath
	}
	if cfg.Preconnect {
		runtime.AddHead(gosx.El("link", gosx.Attrs(
			gosx.Attr("rel", "preconnect"),
			gosx.Attr("href", "https://js.stripe.com"),
		)))
	}
	runtime.ManagedScript(DefaultStripeJSURL, server.ManagedScriptOptions{
		Role: server.ManagedScriptRoleManaged,
	}, gosx.Attrs(gosx.BoolAttr("defer")))
	runtime.LifecycleScript(bridge, gosx.Attrs(gosx.BoolAttr("defer")))
}

// BaseProps are shared by rendered Stripe components. Arbitrary attributes
// are intentionally not accepted: security-owned runtime/action attributes
// must not be shadowed by duplicate authored attributes.
type BaseProps struct {
	ID    string
	Class string
}

// RuntimeOptions contain the one provider value permitted in SSR HTML.
// Secret keys are rejected by the pk_ prefix check during rendering.
type RuntimeOptions struct {
	PublishableKey string
}

type LoaderBehavior string

const (
	LoaderAuto   LoaderBehavior = "auto"
	LoaderAlways LoaderBehavior = "always"
	LoaderNever  LoaderBehavior = "never"
)

type AppearanceTheme string

const (
	AppearanceStripe AppearanceTheme = "stripe"
	AppearanceNight  AppearanceTheme = "night"
	AppearanceFlat   AppearanceTheme = "flat"
)

type AppearanceLabels string

const (
	LabelsFloating AppearanceLabels = "floating"
	LabelsAbove    AppearanceLabels = "above"
)

// AppearanceVariables are the allowlisted Stripe Appearance values that GoSX
// can derive from a design system. There is intentionally no arbitrary rules
// or key/value escape hatch.
type AppearanceVariables struct {
	FontFamily      string `json:"fontFamily,omitempty"`
	ColorPrimary    string `json:"colorPrimary,omitempty"`
	ColorBackground string `json:"colorBackground,omitempty"`
	ColorText       string `json:"colorText,omitempty"`
	ColorDanger     string `json:"colorDanger,omitempty"`
	BorderRadius    string `json:"borderRadius,omitempty"`
	SpacingUnit     string `json:"spacingUnit,omitempty"`
}

type Appearance struct {
	Theme     AppearanceTheme      `json:"theme,omitempty"`
	Labels    AppearanceLabels     `json:"labels,omitempty"`
	Variables *AppearanceVariables `json:"variables,omitempty"`
}

// ElementsOptions is the typed subset of provider-level Elements options that
// is safe to serialize into server HTML.
type ElementsOptions struct {
	Appearance *Appearance    `json:"appearance,omitempty"`
	Loader     LoaderBehavior `json:"loader,omitempty"`
}

type ElementLayout string

const (
	LayoutAuto      ElementLayout = "auto"
	LayoutTabs      ElementLayout = "tabs"
	LayoutAccordion ElementLayout = "accordion"
)

type AddressMode string

const (
	AddressBilling  AddressMode = "billing"
	AddressShipping AddressMode = "shipping"
)

// ElementOptions is shared by GoSX's managed element mounts. Fields that do
// not apply to a particular Stripe element are ignored by Stripe; arbitrary
// provider objects cannot be authored through this contract.
type ElementOptions struct {
	Layout   ElementLayout `json:"layout,omitempty"`
	Mode     AddressMode   `json:"mode,omitempty"`
	ReadOnly bool          `json:"readOnly,omitempty"`
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
	ElementsOptions ElementsOptions
}

// Elements renders a managed provider for PaymentIntent/SetupIntent Elements.
func Elements(props ElementsSurfaceProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("surface"))
	cfg := surfaceConfig{
		PublishableKey:  normalizePublishableKey(props.PublishableKey),
		SessionAction:   normalizeSessionAction(props.SessionAction),
		ElementsOptions: normalizeElementsOptions(props.ElementsOptions),
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
	Options ElementOptions
}

// Element renders a generic Stripe Element mount. Use Type for newly added
// Stripe Element names without waiting for GoSX to add a typed helper.
func Element(props ElementProps) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("element"))
	typ := firstNonEmpty(props.Type, ElementPayment)
	configID, config := configScript("element", elementConfig{Options: normalizeElementOptions(props.Options)})
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
	Method     ConfirmMethod
	ReturnPath string
	Redirect   RedirectBehavior
	SkipSubmit bool
}

type ConfirmMethod string

type RedirectBehavior string

const (
	RedirectAlways     RedirectBehavior = "always"
	RedirectIfRequired RedirectBehavior = "if_required"
)

// ConfirmButton renders the only interactive confirmation control. The
// listener is scoped to this type=button element, so sibling fields, links,
// and controls inside the Elements surface cannot trigger confirmation.
func ConfirmButton(props ConfirmProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("confirm"))
	if len(children) == 0 {
		children = []gosx.Node{gosx.Text("Pay")}
	}
	configID, config := configScript("confirm", confirmConfig{
		Method:     normalizeConfirmMethod(props.Method),
		ReturnPath: normalizeReturnPath(props.ReturnPath),
		Redirect:   normalizeRedirectMode(props.Redirect),
		SkipSubmit: props.SkipSubmit,
	})
	attrs := baseAttrs(props.BaseProps, "gosx-stripe-confirm")
	attrs = append(attrs,
		gosx.Attr("id", id),
		gosx.Attr("type", "button"),
		gosx.Attr("aria-busy", "false"),
		gosx.BoolAttr("data-gosx-stripe-confirm-control"),
		gosx.Attr("data-gosx-stripe-config-id", configID),
	)
	return gosx.Fragment(config, gosx.El("button", nodeArgs(attrs, children...)...))
}

// ConfirmForm is retained as a pre-1.0 migration alias. Despite its old name,
// it renders exactly the explicit accessible button returned by ConfirmButton;
// it never marks a form or role=group as a confirmation listener target.
func ConfirmForm(props ConfirmProps, children ...gosx.Node) gosx.Node {
	return ConfirmButton(props, children...)
}

// EmbeddedCheckoutProps configures stripe.initEmbeddedCheckout(...).
type EmbeddedCheckoutProps struct {
	BaseProps
	RuntimeOptions
	SessionAction string
}

func EmbeddedCheckout(props EmbeddedCheckoutProps) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("embedded"))
	configID, config := configScript("embedded", surfaceConfig{
		PublishableKey: normalizePublishableKey(props.PublishableKey),
		SessionAction:  normalizeSessionAction(props.SessionAction),
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
	ElementsOptions ElementsOptions
}

func Checkout(props CheckoutProps, children ...gosx.Node) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("checkout"))
	configID, config := configScript("checkout", surfaceConfig{
		PublishableKey:  normalizePublishableKey(props.PublishableKey),
		SessionAction:   normalizeSessionAction(props.SessionAction),
		ElementsOptions: normalizeElementsOptions(props.ElementsOptions),
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
}

func CheckoutElement(props CheckoutElementProps) gosx.Node {
	id := firstNonEmpty(props.ID, nextID("checkout-element"))
	typ := firstNonEmpty(props.Type, ElementPayment)
	configID, config := configScript("checkout-element", elementConfig{Options: normalizeElementOptions(props.Options)})
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
	return CheckoutElement(props)
}

func CheckoutExpressCheckoutElement(props CheckoutElementProps) gosx.Node {
	props.Type = ElementExpressCheckout
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
	Theme           AppearanceTheme
	Labels          AppearanceLabels
	FontFamily      string
	ColorPrimary    string
	ColorBackground string
	ColorText       string
	ColorDanger     string
	BorderRadius    string
	SpacingUnit     string
}

func AppearanceFromTokens(tokens DesignTokens) *Appearance {
	variables := AppearanceVariables{
		FontFamily:      boundedOptionValue(tokens.FontFamily),
		ColorPrimary:    boundedOptionValue(tokens.ColorPrimary),
		ColorBackground: boundedOptionValue(tokens.ColorBackground),
		ColorText:       boundedOptionValue(tokens.ColorText),
		ColorDanger:     boundedOptionValue(tokens.ColorDanger),
		BorderRadius:    boundedOptionValue(tokens.BorderRadius),
		SpacingUnit:     boundedOptionValue(tokens.SpacingUnit),
	}
	appearance := &Appearance{
		Theme:  normalizeAppearanceTheme(tokens.Theme),
		Labels: normalizeAppearanceLabels(tokens.Labels),
	}
	if variables != (AppearanceVariables{}) {
		appearance.Variables = &variables
	}
	if appearance.Theme == "" && appearance.Labels == "" && appearance.Variables == nil {
		return nil
	}
	return appearance
}

type surfaceConfig struct {
	PublishableKey  string           `json:"publishableKey"`
	SessionAction   string           `json:"sessionAction"`
	ElementsOptions *ElementsOptions `json:"elementsOptions,omitempty"`
}

type elementConfig struct {
	Options ElementOptions `json:"options"`
}

type confirmConfig struct {
	Method     ConfirmMethod    `json:"method"`
	ReturnPath string           `json:"returnPath,omitempty"`
	Redirect   RedirectBehavior `json:"redirect,omitempty"`
	SkipSubmit bool             `json:"skipSubmit,omitempty"`
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

func normalizeElementsOptions(options ElementsOptions) *ElementsOptions {
	normalized := ElementsOptions{
		Appearance: normalizeAppearance(options.Appearance),
		Loader:     normalizeLoader(options.Loader),
	}
	if normalized.Appearance == nil && normalized.Loader == "" {
		return nil
	}
	return &normalized
}

func normalizeAppearance(appearance *Appearance) *Appearance {
	if appearance == nil {
		return nil
	}
	normalized := &Appearance{
		Theme:  normalizeAppearanceTheme(appearance.Theme),
		Labels: normalizeAppearanceLabels(appearance.Labels),
	}
	if appearance.Variables != nil {
		variables := AppearanceVariables{
			FontFamily:      boundedOptionValue(appearance.Variables.FontFamily),
			ColorPrimary:    boundedOptionValue(appearance.Variables.ColorPrimary),
			ColorBackground: boundedOptionValue(appearance.Variables.ColorBackground),
			ColorText:       boundedOptionValue(appearance.Variables.ColorText),
			ColorDanger:     boundedOptionValue(appearance.Variables.ColorDanger),
			BorderRadius:    boundedOptionValue(appearance.Variables.BorderRadius),
			SpacingUnit:     boundedOptionValue(appearance.Variables.SpacingUnit),
		}
		if variables != (AppearanceVariables{}) {
			normalized.Variables = &variables
		}
	}
	if normalized.Theme == "" && normalized.Labels == "" && normalized.Variables == nil {
		return nil
	}
	return normalized
}

func normalizeLoader(loader LoaderBehavior) LoaderBehavior {
	switch loader {
	case LoaderAuto, LoaderAlways, LoaderNever:
		return loader
	default:
		return ""
	}
}

func normalizeAppearanceTheme(theme AppearanceTheme) AppearanceTheme {
	switch theme {
	case AppearanceStripe, AppearanceNight, AppearanceFlat:
		return theme
	default:
		return ""
	}
}

func normalizeAppearanceLabels(labels AppearanceLabels) AppearanceLabels {
	switch labels {
	case LabelsFloating, LabelsAbove:
		return labels
	default:
		return ""
	}
}

func normalizeElementOptions(options ElementOptions) ElementOptions {
	normalized := ElementOptions{ReadOnly: options.ReadOnly}
	switch options.Layout {
	case LayoutAuto, LayoutTabs, LayoutAccordion:
		normalized.Layout = options.Layout
	}
	switch options.Mode {
	case AddressBilling, AddressShipping:
		normalized.Mode = options.Mode
	}
	return normalized
}

func boundedOptionValue(raw string) string {
	value := strings.TrimSpace(raw)
	if len(value) > 256 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ""
	}
	return value
}

func normalizeConfirmMethod(method ConfirmMethod) ConfirmMethod {
	if method == ConfirmSetup {
		return ConfirmSetup
	}
	return ConfirmPayment
}

func normalizeRedirectMode(mode RedirectBehavior) RedirectBehavior {
	switch mode {
	case RedirectAlways, RedirectIfRequired:
		return mode
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

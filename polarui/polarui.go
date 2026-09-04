// Package polarui provides a server-first Polar hosted-checkout integration.
//
// The package deliberately has no browser runtime. CheckoutForm submits an
// opaque offer identifier to an application-owned handler, which resolves all
// Polar product and customer data on the server before redirecting the browser
// to an explicitly trusted Polar checkout origin.
package polarui

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"m31labs.dev/gosx"
)

const (
	// OfferFieldName is the only application datum CheckoutForm submits.
	OfferFieldName = "offer_id"
	// CSRFFieldName matches the native GoSX session synchronizer-token field.
	CSRFFieldName = "csrf_token"

	// MaxOfferIDBytes bounds the opaque offer lookup key.
	MaxOfferIDBytes = 128
	// MaxCSRFTokenBytes bounds the rendered synchronizer token.
	MaxCSRFTokenBytes = 1024
)

var (
	errInvalidAction  = errors.New("polarui: checkout action must be a root-relative path without query or fragment")
	errInvalidOfferID = errors.New("polarui: invalid opaque offer id")
)

// CheckoutFormProps configures a native hosted-checkout POST form. Action is
// an application route, OfferID is an opaque application-owned lookup key, and
// CSRFToken should normally come from session.Token(r).
type CheckoutFormProps struct {
	ID        string
	Class     string
	Action    string
	OfferID   string
	CSRFToken string
}

// CheckoutForm renders a native POST form. Invalid or incomplete security
// inputs fail closed to a disabled group and are not copied into the DOM.
func CheckoutForm(props CheckoutFormProps, children ...gosx.Node) gosx.Node {
	if ValidateCheckoutAction(props.Action) != nil || ValidateOfferID(props.OfferID) != nil || !validCSRFToken(props.CSRFToken) {
		attrs := gosx.Attrs(
			gosx.Attr("role", "group"),
			gosx.Attr("aria-disabled", "true"),
			gosx.BoolAttr("data-gosx-polar-checkout-invalid"),
		)
		if props.ID != "" {
			attrs = append(attrs, gosx.Attr("id", props.ID))
		}
		if props.Class != "" {
			attrs = append(attrs, gosx.Attr("class", props.Class))
		}
		return gosx.El("div", attrs,
			gosx.El("button", gosx.Attrs(
				gosx.Attr("type", "button"),
				gosx.BoolAttr("disabled"),
			), gosx.Text("Checkout unavailable")),
		)
	}

	if len(children) == 0 {
		children = []gosx.Node{
			gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit")), gosx.Text("Checkout")),
		}
	}
	attrs := gosx.Attrs(
		gosx.Attr("method", "post"),
		gosx.Attr("action", props.Action),
		gosx.BoolAttr("data-gosx-native"),
		gosx.BoolAttr("data-gosx-polar-checkout"),
	)
	if props.ID != "" {
		attrs = append(attrs, gosx.Attr("id", props.ID))
	}
	if props.Class != "" {
		attrs = append(attrs, gosx.Attr("class", props.Class))
	}
	nodes := []gosx.Node{
		gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", OfferFieldName),
			gosx.Attr("value", props.OfferID),
		)),
		gosx.El("input", gosx.Attrs(
			gosx.Attr("type", "hidden"),
			gosx.Attr("name", CSRFFieldName),
			gosx.Attr("value", props.CSRFToken),
		)),
	}
	nodes = append(nodes, children...)
	return gosx.El("form", nodeArgs(attrs, nodes...)...)
}

// ValidateCheckoutAction rejects absolute, scheme-relative, query-bearing,
// fragment-bearing, encoded, or control-containing form targets.
func ValidateCheckoutAction(action string) error {
	if action == "" || len(action) > 2048 || !strings.HasPrefix(action, "/") || strings.HasPrefix(action, "//") {
		return errInvalidAction
	}
	if strings.ContainsAny(action, "\\\r\n\t?#") {
		return errInvalidAction
	}
	for _, r := range action {
		if r < 0x20 || r == 0x7f {
			return errInvalidAction
		}
	}
	u, err := url.ParseRequestURI(action)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return errInvalidAction
	}
	return nil
}

// ValidateOfferID checks the opaque server lookup key shared by the component
// and polaradapter handler.
func ValidateOfferID(offerID string) error {
	if offerID == "" || len(offerID) > MaxOfferIDBytes || !utf8.ValidString(offerID) {
		return errInvalidOfferID
	}
	for _, r := range offerID {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-", r)) {
			return errInvalidOfferID
		}
	}
	return nil
}

func validCSRFToken(token string) bool {
	if token == "" || len(token) > MaxCSRFTokenBytes || !utf8.ValidString(token) {
		return false
	}
	for _, r := range token {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func nodeArgs(attrs gosx.AttrList, children ...gosx.Node) []any {
	args := make([]any, 0, 1+len(children))
	args = append(args, attrs)
	for _, child := range children {
		args = append(args, child)
	}
	return args
}

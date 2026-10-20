// Package polaradapter provides the server-only Polar hosted-checkout client
// and native POST/303 handler for polarui forms.
package polaradapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"m31labs.dev/gosx/polarui"
)

const (
	productionCheckoutEndpoint = "https://api.polar.sh/v1/checkouts/"
	sandboxCheckoutEndpoint    = "https://sandbox-api.polar.sh/v1/checkouts/"

	defaultRequestTimeout   = 10 * time.Second
	maxRequestTimeout       = 30 * time.Second
	maxFormBytes            = 8 << 10
	maxProviderRequestBytes = 16 << 10
	maxProviderBodyBytes    = 64 << 10
	maxProviderURLBytes     = 2083
)

var (
	// ErrOfferUnavailable lets a resolver reject an unknown or currently
	// unavailable opaque offer without exposing its internal reason.
	ErrOfferUnavailable = errors.New("polaradapter: offer unavailable")

	errInvalidConfiguration = errors.New("polaradapter: invalid configuration")
	errInvalidIntent        = errors.New("polaradapter: invalid checkout intent")
	errProvider             = errors.New("polaradapter: provider request failed")
	errProviderResponse     = errors.New("polaradapter: invalid provider response")
)

// Environment selects Polar's fixed API origin. It is intentionally not a
// caller-supplied URL so the private token cannot be redirected elsewhere.
type Environment uint8

const (
	Production Environment = iota + 1
	Sandbox
)

func (e Environment) String() string {
	switch e {
	case Production:
		return "production"
	case Sandbox:
		return "sandbox"
	default:
		return "invalid"
	}
}

// Locale is Polar's hosted-checkout locale allowlist. Automatic leaves locale
// detection to the hosted checkout.
type Locale uint8

const (
	LocaleAutomatic Locale = iota
	LocaleEnglish
	LocaleDutch
	LocaleSpanish
	LocaleFrench
	LocaleSwedish
	LocaleGerman
	LocaleHungarian
	LocaleItalian
	LocalePortugueseBrazil
	LocalePortuguesePortugal
	LocaleKorean
)

// BillingIntent declares whether Polar should collect individual or business
// billing details. A business checkout asks Polar for the full billing name and
// address; no billing data is accepted from the browser POST.
type BillingIntent uint8

const (
	BillingIndividual BillingIntent = iota
	BillingBusiness
)

// Customer identifies or prefills the customer using server-owned data.
// PolarID and ExternalID are mutually exclusive.
type Customer struct {
	PolarID    string
	ExternalID string
	Name       string
	Email      string
}

// CheckoutIntent is the typed, server-owned request returned by OfferResolver.
// SuccessPath is required; ReturnPath is optional. Both are resolved against
// ClientOptions.PublicOrigin, never the incoming request Host.
type CheckoutIntent struct {
	ProductIDs  []string
	Customer    Customer
	Locale      Locale
	Billing     BillingIntent
	SuccessPath string
	ReturnPath  string
}

// OfferResolver turns the browser's opaque offer identifier into a complete,
// server-owned Polar checkout intent. Authentication data can be read from ctx.
type OfferResolver interface {
	ResolvePolarOffer(context.Context, string) (CheckoutIntent, error)
}

// OfferResolverFunc adapts a function to OfferResolver.
type OfferResolverFunc func(context.Context, string) (CheckoutIntent, error)

func (f OfferResolverFunc) ResolvePolarOffer(ctx context.Context, offerID string) (CheckoutIntent, error) {
	if f == nil {
		return CheckoutIntent{}, errInvalidConfiguration
	}
	return f(ctx, offerID)
}

// CSRFProtector is an explicit middleware dependency. session.Manager.Protect
// satisfies this contract directly.
type CSRFProtector func(http.Handler) http.Handler

// ClientIPFunc returns an address only when the application has established a
// trusted proxy/client-IP boundary. The adapter never reads forwarding headers.
type ClientIPFunc func(*http.Request) netip.Addr

// ClientOptions configures Polar API access. OrganizationAccessToken remains
// server-side. AllowedCheckoutOrigins must contain exact HTTPS origins returned
// by the organization's Polar environment.
type ClientOptions struct {
	OrganizationAccessToken string `json:"-"`
	Environment             Environment
	HTTPClient              *http.Client
	PublicOrigin            string
	AllowedCheckoutOrigins  []string
	Timeout                 time.Duration
}

// String redacts the organization access token.
func (o ClientOptions) String() string {
	return fmt.Sprintf("polaradapter.ClientOptions{Environment:%s, OrganizationAccessToken:<redacted>}", o.Environment)
}

// GoString redacts the organization access token for %#v formatting too.
func (o ClientOptions) GoString() string { return o.String() }

// Client is a bounded direct client for Polar's checkout-session endpoint.
type Client struct {
	environment             Environment
	httpClient              *http.Client
	organizationAccessToken string
	publicOrigin            *url.URL
	allowedCheckoutOrigins  map[string]struct{}
	timeout                 time.Duration
	checkoutEndpoint        string
}

// NewClient validates configuration and copies the supplied HTTP client before
// enforcing a no-redirect policy.
func NewClient(options ClientOptions) (*Client, error) {
	token := options.OrganizationAccessToken
	if token == "" || len(token) > 4096 || strings.TrimSpace(token) != token || hasControl(token) {
		return nil, fmt.Errorf("%w: organization access token", errInvalidConfiguration)
	}

	var endpoint string
	switch options.Environment {
	case Production:
		endpoint = productionCheckoutEndpoint
	case Sandbox:
		endpoint = sandboxCheckoutEndpoint
	default:
		return nil, fmt.Errorf("%w: environment", errInvalidConfiguration)
	}

	publicOrigin, _, err := parseHTTPSOrigin(options.PublicOrigin)
	if err != nil {
		return nil, fmt.Errorf("%w: public origin", errInvalidConfiguration)
	}
	if len(options.AllowedCheckoutOrigins) == 0 || len(options.AllowedCheckoutOrigins) > 16 {
		return nil, fmt.Errorf("%w: allowed checkout origins", errInvalidConfiguration)
	}
	allowed := make(map[string]struct{}, len(options.AllowedCheckoutOrigins))
	for _, raw := range options.AllowedCheckoutOrigins {
		_, origin, parseErr := parseHTTPSOrigin(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: allowed checkout origin", errInvalidConfiguration)
		}
		if _, duplicate := allowed[origin]; duplicate {
			return nil, fmt.Errorf("%w: duplicate checkout origin", errInvalidConfiguration)
		}
		allowed[origin] = struct{}{}
	}

	timeout := options.Timeout
	if timeout == 0 {
		timeout = defaultRequestTimeout
	}
	if timeout < 0 || timeout > maxRequestTimeout {
		return nil, fmt.Errorf("%w: timeout", errInvalidConfiguration)
	}

	baseHTTPClient := options.HTTPClient
	if baseHTTPClient == nil {
		baseHTTPClient = http.DefaultClient
	}
	httpClient := *baseHTTPClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &Client{
		environment:             options.Environment,
		httpClient:              &httpClient,
		organizationAccessToken: token,
		publicOrigin:            publicOrigin,
		allowedCheckoutOrigins:  allowed,
		timeout:                 timeout,
		checkoutEndpoint:        endpoint,
	}, nil
}

// String intentionally exposes no credential or checkout URL.
func (c *Client) String() string {
	if c == nil {
		return "polaradapter.Client<nil>"
	}
	return fmt.Sprintf("polaradapter.Client{Environment:%s, OrganizationAccessToken:<redacted>}", c.environment)
}

// GoString redacts the organization access token for %#v formatting too.
func (c *Client) GoString() string { return c.String() }

// CheckoutHandlerOptions configures the native POST/303 adapter.
type CheckoutHandlerOptions struct {
	Client        *Client
	Resolver      OfferResolver
	CSRFProtector CSRFProtector
	ClientIP      ClientIPFunc
}

// NewCheckoutHandler constructs a bounded native checkout handler. A CSRF
// protector is mandatory; pass sessionManager.Protect and mount the returned
// handler below sessionManager.Middleware.
func NewCheckoutHandler(options CheckoutHandlerOptions) (http.Handler, error) {
	if !options.Client.configured() || !resolverConfigured(options.Resolver) || options.CSRFProtector == nil {
		return nil, fmt.Errorf("%w: checkout handler dependencies", errInvalidConfiguration)
	}
	core := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offerID, _ := r.Context().Value(offerContextKey{}).(string)
		intent, err := options.Resolver.ResolvePolarOffer(r.Context(), offerID)
		if err != nil {
			if errors.Is(err, ErrOfferUnavailable) {
				writeFixedError(w, http.StatusBadRequest, "checkout unavailable")
				return
			}
			writeFixedError(w, http.StatusInternalServerError, "checkout unavailable")
			return
		}

		var clientIP netip.Addr
		if options.ClientIP != nil {
			clientIP = options.ClientIP(r)
			if !clientIP.IsValid() || clientIP.Zone() != "" {
				clientIP = netip.Addr{}
			} else {
				clientIP = clientIP.Unmap()
			}
		}

		checkoutURL, err := options.Client.createCheckout(r.Context(), intent, clientIP)
		if err != nil {
			switch {
			case errors.Is(err, errInvalidIntent):
				writeFixedError(w, http.StatusInternalServerError, "checkout unavailable")
			case errors.Is(err, context.Canceled):
				writeFixedError(w, http.StatusRequestTimeout, "checkout request canceled")
			case errors.Is(err, context.DeadlineExceeded):
				writeFixedError(w, http.StatusGatewayTimeout, "checkout provider timed out")
			default:
				writeFixedError(w, http.StatusBadGateway, "checkout provider unavailable")
			}
			return
		}

		w.Header().Set("Location", checkoutURL)
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusSeeOther)
	})
	protected := options.CSRFProtector(core)
	if protected == nil {
		return nil, fmt.Errorf("%w: csrf protector returned nil", errInvalidConfiguration)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeFixedError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := prepareCheckoutForm(w, r); err != nil {
			var maxErr *http.MaxBytesError
			switch {
			case errors.As(err, &maxErr):
				writeFixedError(w, http.StatusRequestEntityTooLarge, "checkout form too large")
			case errors.Is(err, errUnsupportedFormMedia):
				writeFixedError(w, http.StatusUnsupportedMediaType, "unsupported checkout form")
			default:
				writeFixedError(w, http.StatusBadRequest, "invalid checkout form")
			}
			return
		}
		offerID := r.PostForm.Get(polarui.OfferFieldName)
		protected.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), offerContextKey{}, offerID)))
	}), nil
}

type offerContextKey struct{}

var errUnsupportedFormMedia = errors.New("polaradapter: unsupported form media")

func prepareCheckoutForm(w http.ResponseWriter, r *http.Request) error {
	if r.URL == nil || r.URL.ForceQuery || r.URL.RawQuery != "" || r.URL.Fragment != "" {
		return errors.New("polaradapter: request query or fragment")
	}
	if r.ContentLength > maxFormBytes {
		return &http.MaxBytesError{Limit: maxFormBytes}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return errUnsupportedFormMedia
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		return err
	}
	for key, values := range r.PostForm {
		switch key {
		case polarui.OfferFieldName:
			if len(values) != 1 || polarui.ValidateOfferID(values[0]) != nil {
				return errors.New("polaradapter: invalid offer field")
			}
		case polarui.CSRFFieldName:
			if len(values) != 1 || !validCSRFToken(values[0]) {
				return errors.New("polaradapter: invalid csrf field")
			}
		default:
			return errors.New("polaradapter: unknown form field")
		}
	}
	if values := r.PostForm[polarui.OfferFieldName]; len(values) != 1 {
		return errors.New("polaradapter: missing offer field")
	}
	return nil
}

func (c *Client) createCheckout(ctx context.Context, intent CheckoutIntent, clientIP netip.Addr) (string, error) {
	requestBody, err := c.checkoutRequest(intent, clientIP)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return "", errInvalidIntent
	}
	if len(body) > maxProviderRequestBytes {
		return "", errInvalidIntent
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.checkoutEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", errProvider
	}
	req.Header.Set("Authorization", "Bearer "+c.organizationAccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if cause := context.Cause(requestCtx); cause != nil {
			return "", cause
		}
		return "", errProvider
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxProviderBodyBytes+1))
	if readErr != nil {
		return "", errProviderResponse
	}
	if len(responseBody) > maxProviderBodyBytes {
		return "", errProviderResponse
	}
	if resp.StatusCode != http.StatusCreated {
		return "", errProvider
	}
	mediaType, _, mediaErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "application/json" {
		return "", errProviderResponse
	}
	var result struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil || !validUUID4(result.ID) {
		return "", errProviderResponse
	}
	if len(result.URL) == 0 || len(result.URL) > maxProviderURLBytes {
		return "", errProviderResponse
	}
	checkoutURL, origin, err := parseCheckoutURL(result.URL)
	if err != nil {
		return "", errProviderResponse
	}
	if _, ok := c.allowedCheckoutOrigins[origin]; !ok {
		return "", errProviderResponse
	}
	return checkoutURL, nil
}

type checkoutCreateRequest struct {
	Products           []string `json:"products"`
	CustomerID         string   `json:"customer_id,omitempty"`
	ExternalCustomerID string   `json:"external_customer_id,omitempty"`
	CustomerName       string   `json:"customer_name,omitempty"`
	CustomerEmail      string   `json:"customer_email,omitempty"`
	CustomerIPAddress  string   `json:"customer_ip_address,omitempty"`
	Locale             string   `json:"locale,omitempty"`
	IsBusinessCustomer bool     `json:"is_business_customer"`
	SuccessURL         string   `json:"success_url"`
	ReturnURL          string   `json:"return_url,omitempty"`
}

func (c *Client) checkoutRequest(intent CheckoutIntent, clientIP netip.Addr) (checkoutCreateRequest, error) {
	if len(intent.ProductIDs) == 0 || len(intent.ProductIDs) > 16 {
		return checkoutCreateRequest{}, errInvalidIntent
	}
	products := make([]string, len(intent.ProductIDs))
	for i, productID := range intent.ProductIDs {
		if !validUUID4(productID) {
			return checkoutCreateRequest{}, errInvalidIntent
		}
		products[i] = productID
	}
	if intent.Customer.PolarID != "" && intent.Customer.ExternalID != "" {
		return checkoutCreateRequest{}, errInvalidIntent
	}
	if intent.Customer.PolarID != "" && !validUUID4(intent.Customer.PolarID) {
		return checkoutCreateRequest{}, errInvalidIntent
	}
	if !validOptionalText(intent.Customer.ExternalID, 255) || !validOptionalText(intent.Customer.Name, 254) || !validOptionalText(intent.Customer.Email, 320) {
		return checkoutCreateRequest{}, errInvalidIntent
	}
	locale, ok := intent.Locale.apiValue()
	if !ok {
		return checkoutCreateRequest{}, errInvalidIntent
	}
	var business bool
	switch intent.Billing {
	case BillingIndividual:
	case BillingBusiness:
		business = true
	default:
		return checkoutCreateRequest{}, errInvalidIntent
	}
	successURL, err := c.resolvePublicPath(intent.SuccessPath, false)
	if err != nil {
		return checkoutCreateRequest{}, errInvalidIntent
	}
	returnURL, err := c.resolvePublicPath(intent.ReturnPath, true)
	if err != nil {
		return checkoutCreateRequest{}, errInvalidIntent
	}
	request := checkoutCreateRequest{
		Products:           products,
		CustomerID:         intent.Customer.PolarID,
		ExternalCustomerID: intent.Customer.ExternalID,
		CustomerName:       intent.Customer.Name,
		CustomerEmail:      intent.Customer.Email,
		Locale:             locale,
		IsBusinessCustomer: business,
		SuccessURL:         successURL,
		ReturnURL:          returnURL,
	}
	if clientIP.IsValid() && clientIP.Zone() == "" {
		request.CustomerIPAddress = clientIP.Unmap().String()
	}
	return request, nil
}

func (c *Client) resolvePublicPath(raw string, optional bool) (string, error) {
	if raw == "" && optional {
		return "", nil
	}
	if raw == "" || len(raw) > maxProviderURLBytes || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n\t#") {
		return "", errInvalidIntent
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" {
		return "", errInvalidIntent
	}
	resolved := c.publicOrigin.ResolveReference(u).String()
	if len(resolved) > maxProviderURLBytes {
		return "", errInvalidIntent
	}
	return resolved, nil
}

func (l Locale) apiValue() (string, bool) {
	switch l {
	case LocaleAutomatic:
		return "", true
	case LocaleEnglish:
		return "en", true
	case LocaleDutch:
		return "nl", true
	case LocaleSpanish:
		return "es", true
	case LocaleFrench:
		return "fr", true
	case LocaleSwedish:
		return "sv", true
	case LocaleGerman:
		return "de", true
	case LocaleHungarian:
		return "hu", true
	case LocaleItalian:
		return "it", true
	case LocalePortugueseBrazil:
		return "pt", true
	case LocalePortuguesePortugal:
		return "pt-PT", true
	case LocaleKorean:
		return "ko", true
	default:
		return "", false
	}
}

func (c *Client) configured() bool {
	return c != nil && c.httpClient != nil && c.organizationAccessToken != "" && c.publicOrigin != nil &&
		len(c.allowedCheckoutOrigins) > 0 && c.timeout > 0 && c.timeout <= maxRequestTimeout && c.checkoutEndpoint != ""
}

func resolverConfigured(resolver OfferResolver) bool {
	if resolver == nil {
		return false
	}
	value := reflect.ValueOf(resolver)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func parseHTTPSOrigin(raw string) (*url.URL, string, error) {
	if raw == "" || len(raw) > maxProviderURLBytes || strings.TrimSpace(raw) != raw || hasControl(raw) {
		return nil, "", errInvalidConfiguration
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return nil, "", errInvalidConfiguration
	}
	if u.Hostname() == "" {
		return nil, "", errInvalidConfiguration
	}
	u.Scheme = "https"
	u.Host = strings.ToLower(u.Host)
	u.Path = ""
	origin := u.Scheme + "://" + u.Host
	return u, origin, nil
}

func parseCheckoutURL(raw string) (string, string, error) {
	if strings.TrimSpace(raw) != raw || hasControl(raw) || strings.Contains(raw, "\\") {
		return "", "", errProviderResponse
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.Hostname() == "" {
		return "", "", errProviderResponse
	}
	u.Scheme = "https"
	u.Host = strings.ToLower(u.Host)
	return u.String(), u.Scheme + "://" + u.Host, nil
}

func validUUID4(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value[14] != '4' {
		return false
	}
	variant := value[19]
	if variant != '8' && variant != '9' && variant != 'a' && variant != 'b' && variant != 'A' && variant != 'B' {
		return false
	}
	for i := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		c := value[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func validOptionalText(value string, max int) bool {
	return len(value) <= max && utf8.ValidString(value) && !hasControl(value)
}

func validCSRFToken(token string) bool {
	if token == "" || len(token) > polarui.MaxCSRFTokenBytes || !utf8.ValidString(token) {
		return false
	}
	for _, r := range token {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func writeFixedError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, message+"\n")
}

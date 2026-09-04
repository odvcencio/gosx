package polaradapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/polarui"
	"m31labs.dev/gosx/session"
)

const (
	testProductID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testCheckoutID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	testCustomerID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	testToken      = "polar_oat_private_do_not_log"
)

func TestNewClientRejectsUnsafeConfiguration(t *testing.T) {
	valid := ClientOptions{
		OrganizationAccessToken: testToken,
		Environment:             Production,
		PublicOrigin:            "https://app.example.test",
		AllowedCheckoutOrigins:  []string{"https://checkout.example.test"},
	}
	tests := []struct {
		name   string
		mutate func(*ClientOptions)
	}{
		{name: "missing token", mutate: func(o *ClientOptions) { o.OrganizationAccessToken = "" }},
		{name: "token whitespace", mutate: func(o *ClientOptions) { o.OrganizationAccessToken = " secret" }},
		{name: "token control", mutate: func(o *ClientOptions) { o.OrganizationAccessToken = "secret\nvalue" }},
		{name: "invalid environment", mutate: func(o *ClientOptions) { o.Environment = 99 }},
		{name: "public origin http", mutate: func(o *ClientOptions) { o.PublicOrigin = "http://app.example.test" }},
		{name: "public origin userinfo", mutate: func(o *ClientOptions) { o.PublicOrigin = "https://user@app.example.test" }},
		{name: "public origin path", mutate: func(o *ClientOptions) { o.PublicOrigin = "https://app.example.test/base" }},
		{name: "public origin query", mutate: func(o *ClientOptions) { o.PublicOrigin = "https://app.example.test?x=1" }},
		{name: "no checkout origins", mutate: func(o *ClientOptions) { o.AllowedCheckoutOrigins = nil }},
		{name: "checkout origin http", mutate: func(o *ClientOptions) { o.AllowedCheckoutOrigins = []string{"http://checkout.example.test"} }},
		{name: "checkout origin userinfo", mutate: func(o *ClientOptions) { o.AllowedCheckoutOrigins = []string{"https://user@checkout.example.test"} }},
		{name: "checkout origin path", mutate: func(o *ClientOptions) { o.AllowedCheckoutOrigins = []string{"https://checkout.example.test/session"} }},
		{name: "duplicate checkout origin", mutate: func(o *ClientOptions) {
			o.AllowedCheckoutOrigins = []string{"https://checkout.example.test", "https://CHECKOUT.EXAMPLE.TEST/"}
		}},
		{name: "negative timeout", mutate: func(o *ClientOptions) { o.Timeout = -time.Second }},
		{name: "unbounded timeout", mutate: func(o *ClientOptions) { o.Timeout = maxRequestTimeout + time.Nanosecond }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := valid
			options.AllowedCheckoutOrigins = append([]string(nil), valid.AllowedCheckoutOrigins...)
			test.mutate(&options)
			if client, err := NewClient(options); err == nil || client != nil {
				t.Fatalf("NewClient() = (%v, %v), want a closed configuration error", client, err)
			} else if strings.Contains(err.Error(), testToken) {
				t.Fatalf("configuration error leaked token: %v", err)
			}
		})
	}
}

func TestClientAndOptionsFormattingRedactsToken(t *testing.T) {
	options := ClientOptions{
		OrganizationAccessToken: testToken,
		Environment:             Production,
		PublicOrigin:            "https://app.example.test",
		AllowedCheckoutOrigins:  []string{"https://checkout.example.test"},
	}
	client, err := NewClient(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{options, &options, client} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			formatted := fmt.Sprintf(format, value)
			if strings.Contains(formatted, testToken) {
				t.Fatalf("format %s leaked token in %q", format, formatted)
			}
			if !strings.Contains(formatted, "<redacted>") {
				t.Fatalf("format %s did not identify redaction in %q", format, formatted)
			}
		}
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), testToken) || strings.Contains(string(encoded), "OrganizationAccessToken") {
		t.Fatalf("JSON encoding leaked the token field: %s", encoded)
	}
}

func TestPublicProviderInputsHaveNoGenericEscapeHatches(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(Customer{}),
		reflect.TypeOf(CheckoutIntent{}),
		reflect.TypeOf(ClientOptions{}),
	} {
		for index := 0; index < typ.NumField(); index++ {
			field := typ.Field(index)
			if field.Type.Kind() == reflect.Map || field.Type.Kind() == reflect.Interface {
				t.Fatalf("generic field %s remains on %s", field.Name, typ.Name())
			}
		}
	}
}

func TestClientUsesOnlyFixedEnvironmentEndpoints(t *testing.T) {
	tests := []struct {
		name        string
		environment Environment
		endpoint    string
	}{
		{name: "production", environment: Production, endpoint: productionCheckoutEndpoint},
		{name: "sandbox", environment: Sandbox, endpoint: sandboxCheckoutEndpoint},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var originalURL string
			client := newTestClient(t, test.environment, func(request *http.Request) {
				originalURL = request.URL.String()
			}, successProvider("https://checkout.example.test/session"))
			handler := mustCheckoutHandler(t, client, validCheckoutIntent(), nil)
			response := serveCheckout(handler, "starter", "csrf", nil)
			if response.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, body=%q", response.Code, response.Body.String())
			}
			if originalURL != test.endpoint {
				t.Fatalf("provider URL = %q, want fixed %q", originalURL, test.endpoint)
			}
		})
	}
}

func TestNewCheckoutHandlerRequiresExplicitDependencies(t *testing.T) {
	client := newTestClient(t, Production, nil, successProvider("https://checkout.example.test/session"))
	resolver := OfferResolverFunc(func(context.Context, string) (CheckoutIntent, error) {
		return validCheckoutIntent(), nil
	})
	valid := CheckoutHandlerOptions{Client: client, Resolver: resolver, CSRFProtector: allowCSRF}
	tests := []struct {
		name   string
		mutate func(*CheckoutHandlerOptions)
	}{
		{name: "client", mutate: func(o *CheckoutHandlerOptions) { o.Client = nil }},
		{name: "zero client", mutate: func(o *CheckoutHandlerOptions) { o.Client = &Client{} }},
		{name: "resolver", mutate: func(o *CheckoutHandlerOptions) { o.Resolver = nil }},
		{name: "typed nil resolver", mutate: func(o *CheckoutHandlerOptions) { o.Resolver = OfferResolverFunc(nil) }},
		{name: "csrf", mutate: func(o *CheckoutHandlerOptions) { o.CSRFProtector = nil }},
		{name: "csrf returns nil", mutate: func(o *CheckoutHandlerOptions) { o.CSRFProtector = func(http.Handler) http.Handler { return nil } }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := valid
			test.mutate(&options)
			if handler, err := NewCheckoutHandler(options); err == nil || handler != nil {
				t.Fatalf("NewCheckoutHandler() = (%v, %v), want error", handler, err)
			}
		})
	}
}

func TestCheckoutHandlerRejectsMethodAndMalformedFormsBeforeResolution(t *testing.T) {
	var providerCalls atomic.Int32
	client := newTestClient(t, Production, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		successProvider("https://checkout.example.test/session").ServeHTTP(w, r)
	}))
	tests := []struct {
		name          string
		method        string
		target        string
		body          string
		contentType   string
		contentLength int64
		wantStatus    int
	}{
		{name: "method", method: http.MethodGet, target: "/checkout", wantStatus: http.StatusMethodNotAllowed},
		{name: "query", method: http.MethodPost, target: "/checkout?offer_id=starter", body: "offer_id=starter", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest},
		{name: "empty query", method: http.MethodPost, target: "/checkout?", body: "offer_id=starter", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest},
		{name: "missing content type", method: http.MethodPost, target: "/checkout", body: "offer_id=starter", wantStatus: http.StatusUnsupportedMediaType},
		{name: "json", method: http.MethodPost, target: "/checkout", body: `{}`, contentType: "application/json", wantStatus: http.StatusUnsupportedMediaType},
		{name: "missing offer", method: http.MethodPost, target: "/checkout", body: "csrf_token=csrf", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest},
		{name: "duplicate offer", method: http.MethodPost, target: "/checkout", body: "offer_id=one&offer_id=two", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest},
		{name: "unknown action field", method: http.MethodPost, target: "/checkout", body: "offer_id=starter&action=https%3A%2F%2Fevil.example", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest},
		{name: "invalid offer whitespace", method: http.MethodPost, target: "/checkout", body: "offer_id=starter+plan", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest},
		{name: "invalid offer slash", method: http.MethodPost, target: "/checkout", body: "offer_id=starter%2Fplan", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest},
		{name: "oversized offer", method: http.MethodPost, target: "/checkout", body: "offer_id=" + strings.Repeat("a", polarui.MaxOfferIDBytes+1), contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest},
		{name: "duplicate csrf", method: http.MethodPost, target: "/checkout", body: "offer_id=starter&csrf_token=one&csrf_token=two", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest},
		{name: "invalid csrf", method: http.MethodPost, target: "/checkout", body: "offer_id=starter&csrf_token=bad+token", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusBadRequest},
		{name: "declared oversized", method: http.MethodPost, target: "/checkout", body: "offer_id=starter", contentType: "application/x-www-form-urlencoded", contentLength: maxFormBytes + 1, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "streamed oversized", method: http.MethodPost, target: "/checkout", body: "offer_id=" + strings.Repeat("a", maxFormBytes), contentType: "application/x-www-form-urlencoded", contentLength: -1, wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var resolverCalls atomic.Int32
			var csrfCalls atomic.Int32
			handler, err := NewCheckoutHandler(CheckoutHandlerOptions{
				Client: client,
				Resolver: OfferResolverFunc(func(context.Context, string) (CheckoutIntent, error) {
					resolverCalls.Add(1)
					return validCheckoutIntent(), nil
				}),
				CSRFProtector: func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						csrfCalls.Add(1)
						next.ServeHTTP(w, r)
					})
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(test.method, "https://app.example.test"+test.target, strings.NewReader(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			if test.contentLength != 0 {
				request.ContentLength = test.contentLength
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, test.wantStatus, response.Body.String())
			}
			if resolverCalls.Load() != 0 {
				t.Fatalf("resolver called %d times", resolverCalls.Load())
			}
			if csrfCalls.Load() != 0 {
				t.Fatalf("csrf middleware called %d times before form validation", csrfCalls.Load())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("missing no-store on failure: %v", response.Header())
			}
		})
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("provider called %d times for rejected requests", providerCalls.Load())
	}
}

func TestCheckoutHandlerRequiresCSRFBeforeResolver(t *testing.T) {
	var resolverCalls atomic.Int32
	client := newTestClient(t, Production, nil, successProvider("https://checkout.example.test/session"))
	handler, err := NewCheckoutHandler(CheckoutHandlerOptions{
		Client: client,
		Resolver: OfferResolverFunc(func(context.Context, string) (CheckoutIntent, error) {
			resolverCalls.Add(1)
			return validCheckoutIntent(), nil
		}),
		CSRFProtector: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.PostForm.Get(polarui.CSRFFieldName) != "expected" {
					http.Error(w, "csrf rejected", http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := serveCheckout(handler, "starter", "wrong", nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
	if resolverCalls.Load() != 0 {
		t.Fatalf("resolver ran before CSRF validation")
	}
}

func TestCheckoutHandlerUsesSessionProtectConvention(t *testing.T) {
	client := newTestClient(t, Production, nil, successProvider("https://checkout.example.test/session"))
	manager := session.MustNew("a sufficiently long test session secret", session.Options{AllowInsecure: true})
	handler, err := NewCheckoutHandler(CheckoutHandlerOptions{
		Client: client,
		Resolver: OfferResolverFunc(func(context.Context, string) (CheckoutIntent, error) {
			return validCheckoutIntent(), nil
		}),
		CSRFProtector: manager.Protect,
	})
	if err != nil {
		t.Fatal(err)
	}

	tokenResponse := httptest.NewRecorder()
	tokenRequest := httptest.NewRequest(http.MethodGet, "http://app.example.test/token", nil)
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-CSRF", manager.Token(r))
	})).ServeHTTP(tokenResponse, tokenRequest)
	token := tokenResponse.Header().Get("X-Test-CSRF")
	cookies := tokenResponse.Result().Cookies()
	if token == "" || len(cookies) != 1 {
		t.Fatalf("failed to establish session token: token=%q cookies=%d", token, len(cookies))
	}

	post := httptest.NewRequest(http.MethodPost, "http://app.example.test/checkout", strings.NewReader(url.Values{
		polarui.OfferFieldName: {"starter"},
		polarui.CSRFFieldName:  {token},
	}.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookies[0])
	response := httptest.NewRecorder()
	manager.Middleware(handler).ServeHTTP(response, post)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("session-protected checkout status = %d, body=%q", response.Code, response.Body.String())
	}

	missing := httptest.NewRequest(http.MethodPost, "http://app.example.test/checkout", strings.NewReader(url.Values{
		polarui.OfferFieldName: {"starter"},
	}.Encode()))
	missing.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	missing.AddCookie(cookies[0])
	missingResponse := httptest.NewRecorder()
	manager.Middleware(handler).ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", missingResponse.Code)
	}
}

func TestCheckoutHandlerCreatesBoundedServerOwnedCheckoutAndRedirects(t *testing.T) {
	var captured checkoutCreateRequest
	var capturedBody string
	var authorization string
	client := newTestClient(t, Sandbox, func(original *http.Request) {
		if original.URL.String() != sandboxCheckoutEndpoint {
			t.Errorf("provider endpoint = %q, want %q", original.URL, sandboxCheckoutEndpoint)
		}
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		if r.Method != http.MethodPost || r.URL.Path != "/v1/checkouts/" {
			t.Errorf("provider request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("provider headers = %v", r.Header)
		}
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("read provider request: %v", readErr)
		}
		capturedBody = string(body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testCheckoutID+`","url":"https://checkout.example.test/session?id=public","client_secret":"provider_secret_must_not_be_retained","metadata":{"ignored":true}}`)
	}))

	var resolvedOffer string
	handler, err := NewCheckoutHandler(CheckoutHandlerOptions{
		Client: client,
		Resolver: OfferResolverFunc(func(_ context.Context, offerID string) (CheckoutIntent, error) {
			resolvedOffer = offerID
			return CheckoutIntent{
				ProductIDs: []string{testProductID},
				Customer: Customer{
					ExternalID: "account_42",
					Name:       "Ada Lovelace",
					Email:      "ada@example.test",
				},
				Locale:      LocaleFrench,
				Billing:     BillingBusiness,
				SuccessPath: "/billing/success?checkout_id={CHECKOUT_ID}",
				ReturnPath:  "/pricing",
			}, nil
		}),
		CSRFProtector: allowCSRF,
		ClientIP: func(r *http.Request) netip.Addr {
			if r.Host != "attacker.example" {
				t.Errorf("trusted client-IP callback did not receive original request")
			}
			return netip.MustParseAddr("::ffff:203.0.113.7")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := serveCheckout(handler, "annual_pro", "csrf", func(r *http.Request) {
		r.Host = "attacker.example"
	})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body=%q", response.Code, response.Body.String())
	}
	if response.Body.Len() != 0 {
		t.Fatalf("303 body = %q, want empty", response.Body.String())
	}
	if response.Header().Get("Location") != "https://checkout.example.test/session?id=public" || response.Header().Get("Content-Length") != "0" {
		t.Fatalf("redirect headers = %v", response.Header())
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("cache headers = %v", response.Header())
	}
	if resolvedOffer != "annual_pro" {
		t.Fatalf("resolver offer = %q", resolvedOffer)
	}
	if authorization != "Bearer "+testToken {
		t.Fatalf("authorization = %q", authorization)
	}
	if len(capturedBody) > maxProviderRequestBytes || strings.Contains(capturedBody, "annual_pro") || strings.Contains(capturedBody, "attacker.example") {
		t.Fatalf("provider body was not bounded/server-owned: %q", capturedBody)
	}
	if got := captured.Products; len(got) != 1 || got[0] != testProductID {
		t.Fatalf("products = %v", got)
	}
	if captured.CustomerID != "" || captured.ExternalCustomerID != "account_42" || captured.CustomerName != "Ada Lovelace" || captured.CustomerEmail != "ada@example.test" {
		t.Fatalf("customer fields = %+v", captured)
	}
	if captured.Locale != "fr" || !captured.IsBusinessCustomer || captured.CustomerIPAddress != "203.0.113.7" {
		t.Fatalf("locale/billing/ip = %+v", captured)
	}
	if captured.SuccessURL != "https://app.example.test/billing/success?checkout_id={CHECKOUT_ID}" || captured.ReturnURL != "https://app.example.test/pricing" {
		t.Fatalf("canonical redirects = success %q return %q", captured.SuccessURL, captured.ReturnURL)
	}
	encoded, _ := json.Marshal(captured)
	if strings.Contains(string(encoded), "attacker.example") {
		t.Fatalf("host header influenced provider request: %s", encoded)
	}
}

func TestCheckoutHandlerOmitsUntrustedOrInvalidClientIP(t *testing.T) {
	tests := []struct {
		name     string
		callback ClientIPFunc
	}{
		{name: "no callback"},
		{name: "invalid address", callback: func(*http.Request) netip.Addr { return netip.Addr{} }},
		{name: "zoned address", callback: func(*http.Request) netip.Addr { return netip.MustParseAddr("fe80::1%eth0") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var captured checkoutCreateRequest
			client := newTestClient(t, Production, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&captured)
				successProvider("https://checkout.example.test/session").ServeHTTP(w, r)
			}))
			handler := mustCheckoutHandler(t, client, validCheckoutIntent(), test.callback)
			response := serveCheckout(handler, "starter", "csrf", func(r *http.Request) {
				r.Header.Set("Forwarded", "for=198.51.100.8")
				r.Header.Set("X-Forwarded-For", "198.51.100.9")
				r.Header.Set("CF-Connecting-IP", "198.51.100.10")
				r.RemoteAddr = "198.51.100.11:1234"
			})
			if response.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, body=%q", response.Code, response.Body.String())
			}
			if captured.CustomerIPAddress != "" {
				t.Fatalf("ambient client IP leaked to provider: %q", captured.CustomerIPAddress)
			}
		})
	}
}

func TestCheckoutHandlerRejectsProviderFailuresWithoutLeakingBodies(t *testing.T) {
	secretBody := "provider diagnostic containing " + testToken + " and provider_secret"
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{name: "status", status: http.StatusUnprocessableEntity, contentType: "application/json", body: `{"detail":"` + secretBody + `"}`},
		{name: "redirect", status: http.StatusFound, contentType: "text/html", body: secretBody},
		{name: "wrong media", status: http.StatusCreated, contentType: "text/plain", body: `{"id":"` + testCheckoutID + `","url":"https://checkout.example.test/session"}`},
		{name: "malformed", status: http.StatusCreated, contentType: "application/json", body: `{`},
		{name: "oversized", status: http.StatusCreated, contentType: "application/json", body: strings.Repeat("x", maxProviderBodyBytes+1)},
		{name: "missing id", status: http.StatusCreated, contentType: "application/json", body: `{"url":"https://checkout.example.test/session"}`},
		{name: "bad id", status: http.StatusCreated, contentType: "application/json", body: `{"id":"not-a-uuid","url":"https://checkout.example.test/session"}`},
		{name: "missing url", status: http.StatusCreated, contentType: "application/json", body: `{"id":"` + testCheckoutID + `"}`},
		{name: "http url", status: http.StatusCreated, contentType: "application/json", body: `{"id":"` + testCheckoutID + `","url":"http://checkout.example.test/session"}`},
		{name: "userinfo", status: http.StatusCreated, contentType: "application/json", body: `{"id":"` + testCheckoutID + `","url":"https://user:password@checkout.example.test/session"}`},
		{name: "wrong origin", status: http.StatusCreated, contentType: "application/json", body: `{"id":"` + testCheckoutID + `","url":"https://evil.example/session"}`},
		{name: "origin suffix", status: http.StatusCreated, contentType: "application/json", body: `{"id":"` + testCheckoutID + `","url":"https://checkout.example.test.evil.example/session"}`},
		{name: "fragment", status: http.StatusCreated, contentType: "application/json", body: `{"id":"` + testCheckoutID + `","url":"https://checkout.example.test/session#secret"}`},
		{name: "backslash", status: http.StatusCreated, contentType: "application/json", body: `{"id":"` + testCheckoutID + `","url":"https://checkout.example.test\\@evil.example/session"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var attackerCalls atomic.Int32
			client := newTestClient(t, Production, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attackerCalls.Add(1)
				if r.URL.Path == "/steal" {
					w.WriteHeader(http.StatusOK)
					return
				}
				if test.name == "redirect" {
					w.Header().Set("Location", "/steal")
				}
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			handler := mustCheckoutHandler(t, client, validCheckoutIntent(), nil)
			response := serveCheckout(handler, "starter", "csrf", nil)
			if response.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502; body=%q", response.Code, response.Body.String())
			}
			if response.Header().Get("Location") != "" {
				t.Fatalf("failure supplied redirect: %q", response.Header().Get("Location"))
			}
			for _, forbidden := range []string{secretBody, testToken, "provider_secret", test.body} {
				if forbidden != "" && strings.Contains(response.Body.String(), forbidden) {
					t.Fatalf("provider data leaked in response %q", response.Body.String())
				}
			}
			if attackerCalls.Load() != 1 {
				t.Fatalf("provider calls = %d, want exactly one (redirects disabled)", attackerCalls.Load())
			}
		})
	}
}

func TestCheckoutHandlerTimeoutAndCancellation(t *testing.T) {
	t.Run("provider timeout", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		defer close(release)
		client := newTestClientWithTimeout(t, 20*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}))
		handler := mustCheckoutHandler(t, client, validCheckoutIntent(), nil)
		response := serveCheckout(handler, "starter", "csrf", nil)
		if response.Code != http.StatusGatewayTimeout {
			t.Fatalf("status = %d, want 504; body=%q", response.Code, response.Body.String())
		}
		select {
		case <-started:
		default:
			t.Fatal("provider request never started")
		}
	})

	t.Run("request cancellation", func(t *testing.T) {
		var providerCalls atomic.Int32
		client := newTestClient(t, Production, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			providerCalls.Add(1)
			successProvider("https://checkout.example.test/session").ServeHTTP(w, r)
		}))
		handler := mustCheckoutHandler(t, client, validCheckoutIntent(), nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		response := serveCheckout(handler, "starter", "csrf", func(r *http.Request) {
			*r = *r.WithContext(ctx)
		})
		if response.Code != http.StatusRequestTimeout {
			t.Fatalf("status = %d, want 408; body=%q", response.Code, response.Body.String())
		}
		if providerCalls.Load() != 0 {
			t.Fatalf("provider called after cancellation")
		}
	})
}

func TestCheckoutHandlerClassifiesResolverAndIntentFailures(t *testing.T) {
	client := newTestClient(t, Production, nil, successProvider("https://checkout.example.test/session"))
	tests := []struct {
		name       string
		intent     CheckoutIntent
		resolveErr error
		wantStatus int
	}{
		{name: "unknown offer", resolveErr: ErrOfferUnavailable, wantStatus: http.StatusBadRequest},
		{name: "resolver failure", resolveErr: errors.New("database includes private detail"), wantStatus: http.StatusInternalServerError},
		{name: "absolute success", intent: CheckoutIntent{ProductIDs: []string{testProductID}, SuccessPath: "https://evil.example/success"}, wantStatus: http.StatusInternalServerError},
		{name: "scheme-relative return", intent: CheckoutIntent{ProductIDs: []string{testProductID}, SuccessPath: "/success", ReturnPath: "//evil.example"}, wantStatus: http.StatusInternalServerError},
		{name: "bad product", intent: CheckoutIntent{ProductIDs: []string{"product-from-browser"}, SuccessPath: "/success"}, wantStatus: http.StatusInternalServerError},
		{name: "conflicting customer IDs", intent: CheckoutIntent{ProductIDs: []string{testProductID}, Customer: Customer{PolarID: testCustomerID, ExternalID: "external"}, SuccessPath: "/success"}, wantStatus: http.StatusInternalServerError},
		{name: "invalid locale", intent: CheckoutIntent{ProductIDs: []string{testProductID}, Locale: 99, SuccessPath: "/success"}, wantStatus: http.StatusInternalServerError},
		{name: "invalid billing", intent: CheckoutIntent{ProductIDs: []string{testProductID}, Billing: 99, SuccessPath: "/success"}, wantStatus: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent := test.intent
			if test.resolveErr == nil && len(intent.ProductIDs) == 0 {
				intent = validCheckoutIntent()
			}
			handler, err := NewCheckoutHandler(CheckoutHandlerOptions{
				Client: client,
				Resolver: OfferResolverFunc(func(context.Context, string) (CheckoutIntent, error) {
					return intent, test.resolveErr
				}),
				CSRFProtector: allowCSRF,
			})
			if err != nil {
				t.Fatal(err)
			}
			response := serveCheckout(handler, "starter", "csrf", nil)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, test.wantStatus, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "database includes private detail") || response.Header().Get("Location") != "" {
				t.Fatalf("failure leaked detail or redirect: headers=%v body=%q", response.Header(), response.Body.String())
			}
		})
	}
}

func validCheckoutIntent() CheckoutIntent {
	return CheckoutIntent{
		ProductIDs:  []string{testProductID},
		SuccessPath: "/success",
		ReturnPath:  "/pricing",
	}
}

func allowCSRF(next http.Handler) http.Handler { return next }

func successProvider(checkoutURL string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testCheckoutID+`","url":"`+checkoutURL+`"}`)
	})
}

func serveCheckout(handler http.Handler, offerID, csrfToken string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	values := url.Values{polarui.OfferFieldName: {offerID}}
	if csrfToken != "" {
		values.Set(polarui.CSRFFieldName, csrfToken)
	}
	request := httptest.NewRequest(http.MethodPost, "https://app.example.test/checkout", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if mutate != nil {
		mutate(request)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func mustCheckoutHandler(t *testing.T, client *Client, intent CheckoutIntent, clientIP ClientIPFunc) http.Handler {
	t.Helper()
	handler, err := NewCheckoutHandler(CheckoutHandlerOptions{
		Client: client,
		Resolver: OfferResolverFunc(func(context.Context, string) (CheckoutIntent, error) {
			return intent, nil
		}),
		CSRFProtector: allowCSRF,
		ClientIP:      clientIP,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func newTestClient(t *testing.T, environment Environment, inspect func(*http.Request), provider http.Handler) *Client {
	t.Helper()
	return newTestClientOptions(t, environment, defaultRequestTimeout, inspect, provider)
}

func newTestClientWithTimeout(t *testing.T, timeout time.Duration, provider http.Handler) *Client {
	t.Helper()
	return newTestClientOptions(t, Production, timeout, nil, provider)
}

func newTestClientOptions(t *testing.T, environment Environment, timeout time.Duration, inspect func(*http.Request), provider http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(provider)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := &rewriteTransport{
		target:  target,
		base:    server.Client().Transport,
		inspect: inspect,
	}
	client, err := NewClient(ClientOptions{
		OrganizationAccessToken: testToken,
		Environment:             environment,
		HTTPClient:              &http.Client{Transport: transport},
		PublicOrigin:            "https://app.example.test",
		AllowedCheckoutOrigins:  []string{"https://checkout.example.test"},
		Timeout:                 timeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

type rewriteTransport struct {
	target  *url.URL
	base    http.RoundTripper
	inspect func(*http.Request)
	mu      sync.Mutex
}

func (t *rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if t.inspect != nil {
		t.mu.Lock()
		t.inspect(request)
		t.mu.Unlock()
	}
	clone := request.Clone(request.Context())
	clonedURL := *request.URL
	clonedURL.Scheme = t.target.Scheme
	clonedURL.Host = t.target.Host
	clone.URL = &clonedURL
	clone.Host = t.target.Host
	return t.base.RoundTrip(clone)
}

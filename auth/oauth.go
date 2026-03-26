package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/odvcencio/gosx/session"
)

var (
	ErrOAuthProviderNotFound = fmt.Errorf("oauth provider not found")
	ErrOAuthStateInvalid     = fmt.Errorf("oauth state is invalid")
	ErrOAuthStateExpired     = fmt.Errorf("oauth state expired")
	ErrOAuthCodeMissing      = fmt.Errorf("oauth code is missing")
)

// OAuthToken is the exchanged token payload returned by an OAuth provider.
type OAuthToken struct {
	AccessToken  string         `json:"access_token"`
	TokenType    string         `json:"token_type,omitempty"`
	RefreshToken string         `json:"refresh_token,omitempty"`
	IDToken      string         `json:"id_token,omitempty"`
	Scope        string         `json:"scope,omitempty"`
	ExpiresIn    int            `json:"expires_in,omitempty"`
	Raw          map[string]any `json:"raw,omitempty"`
}

// OAuthUserResolver resolves the signed-in user from a provider token.
type OAuthUserResolver interface {
	ResolveOAuthUser(context.Context, OAuthProvider, *http.Client, OAuthToken) (User, error)
}

// OAuthUserResolverFunc adapts a function into an OAuthUserResolver.
type OAuthUserResolverFunc func(context.Context, OAuthProvider, *http.Client, OAuthToken) (User, error)

func (fn OAuthUserResolverFunc) ResolveOAuthUser(ctx context.Context, provider OAuthProvider, client *http.Client, token OAuthToken) (User, error) {
	if fn == nil {
		return User{}, nil
	}
	return fn(ctx, provider, client, token)
}

// OAuthProvider configures a single OAuth provider.
type OAuthProvider struct {
	Name         string
	ClientID     string
	ClientSecret string
	AuthorizeURL string
	TokenURL     string
	RedirectURL  string
	UserInfoURL  string
	Scopes       []string
	AuthParams   map[string]string
	Resolver     OAuthUserResolver
}

// OAuthOptions configures the built-in OAuth flow.
type OAuthOptions struct {
	Providers   []OAuthProvider
	SessionKey  string
	TTL         time.Duration
	SuccessPath string
	FailurePath string
	FlashKey    string
	HTTPClient  *http.Client
	Now         func() time.Time
}

// OAuth orchestrates provider-based OAuth sign-ins.
type OAuth struct {
	manager     *Manager
	sessionKey  string
	ttl         time.Duration
	successPath string
	failurePath string
	flashKey    string
	httpClient  *http.Client
	now         func() time.Time
	providers   map[string]OAuthProvider
}

type oauthState struct {
	Provider  string    `json:"provider"`
	State     string    `json:"state"`
	Verifier  string    `json:"verifier"`
	Next      string    `json:"next,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// NewOAuth creates a batteries-included OAuth flow.
func NewOAuth(manager *Manager, opts OAuthOptions) *OAuth {
	if opts.SessionKey == "" {
		opts.SessionKey = "auth.oauth"
	}
	if opts.TTL == 0 {
		opts.TTL = 15 * time.Minute
	}
	if opts.SuccessPath == "" {
		opts.SuccessPath = "/"
	}
	if opts.FailurePath == "" {
		opts.FailurePath = "/"
	}
	if opts.FlashKey == "" {
		opts.FlashKey = "auth.oauth"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	providers := make(map[string]OAuthProvider, len(opts.Providers))
	for _, provider := range opts.Providers {
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			continue
		}
		provider.Name = name
		providers[name] = provider
	}
	return &OAuth{
		manager:     manager,
		sessionKey:  opts.SessionKey,
		ttl:         opts.TTL,
		successPath: opts.SuccessPath,
		failurePath: opts.FailurePath,
		flashKey:    opts.FlashKey,
		httpClient:  opts.HTTPClient,
		now:         opts.Now,
		providers:   providers,
	}
}

// OAuth creates an OAuth manager bound to the auth manager.
func (m *Manager) OAuth(opts OAuthOptions) *OAuth {
	return NewOAuth(m, opts)
}

// Begin starts an OAuth flow and returns the provider redirect URL.
func (o *OAuth) Begin(r *http.Request, providerName string, next string) (string, error) {
	provider, err := o.provider(providerName)
	if err != nil {
		return "", err
	}
	state, err := randomOAuthString(32)
	if err != nil {
		return "", err
	}
	verifier, err := randomOAuthString(48)
	if err != nil {
		return "", err
	}
	if err := o.saveState(r, oauthState{
		Provider:  provider.Name,
		State:     state,
		Verifier:  verifier,
		Next:      sanitizeRedirectTarget(next),
		ExpiresAt: o.now().Add(o.ttl),
	}); err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", provider.ClientID)
	values.Set("redirect_uri", provider.RedirectURL)
	values.Set("state", state)
	values.Set("code_challenge", oauthCodeChallenge(verifier))
	values.Set("code_challenge_method", "S256")
	if scopes := strings.TrimSpace(strings.Join(provider.Scopes, " ")); scopes != "" {
		values.Set("scope", scopes)
	}
	for key, value := range provider.AuthParams {
		if strings.TrimSpace(key) == "" || value == "" {
			continue
		}
		values.Set(key, value)
	}
	target := provider.AuthorizeURL
	if strings.Contains(target, "?") {
		target += "&" + values.Encode()
	} else {
		target += "?" + values.Encode()
	}
	return target, nil
}

// Callback completes the OAuth flow, signs the user in, and returns the user.
func (o *OAuth) Callback(r *http.Request, providerName string) (User, string, error) {
	provider, err := o.provider(providerName)
	if err != nil {
		return User{}, "", err
	}
	state, err := o.consumeState(r, provider.Name, r.URL.Query().Get("state"))
	if err != nil {
		return User{}, "", err
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		return User{}, "", ErrOAuthCodeMissing
	}
	token, err := exchangeOAuthCode(r.Context(), o.httpClient, provider, code, state.Verifier)
	if err != nil {
		return User{}, "", err
	}
	user, err := resolveOAuthUser(r.Context(), o.httpClient, provider, token)
	if err != nil {
		return User{}, "", err
	}
	if o.manager == nil || !o.manager.SignIn(r, user) {
		return User{}, "", fmt.Errorf("session middleware required before oauth callback")
	}
	target := state.Next
	if target == "" {
		target = o.successPath
	}
	return user, target, nil
}

// BeginHandler redirects to the selected provider's authorize URL.
func (o *OAuth) BeginHandler(providerName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, err := o.Begin(r, providerName, r.URL.Query().Get("next"))
		if err != nil {
			writeOAuthError(w, r, http.StatusBadRequest, err)
			return
		}
		if requestWantsJSON(r) {
			writeOAuthJSON(w, http.StatusOK, map[string]any{
				"ok":       true,
				"provider": providerName,
				"url":      target,
			})
			return
		}
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	})
}

// CallbackHandler consumes an OAuth callback and signs the user in.
func (o *OAuth) CallbackHandler(providerName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, target, err := o.Callback(r, providerName)
		if err != nil {
			if !requestWantsJSON(r) {
				addMagicLinkFlash(r, o.flashKey, map[string]any{
					"status":   "error",
					"provider": providerName,
					"error":    err.Error(),
				})
				http.Redirect(w, r, o.failurePath, http.StatusSeeOther)
				return
			}
			writeOAuthError(w, r, http.StatusUnauthorized, err)
			return
		}
		addMagicLinkFlash(r, o.flashKey, map[string]any{
			"status":   "signed_in",
			"provider": providerName,
			"email":    user.Email,
		})
		if requestWantsJSON(r) {
			writeOAuthJSON(w, http.StatusOK, map[string]any{
				"ok":     true,
				"user":   user,
				"target": target,
			})
			return
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	})
}

// GoogleProvider returns a preconfigured Google OAuth provider.
func GoogleProvider(clientID, clientSecret, redirectURL string) OAuthProvider {
	return OAuthProvider{
		Name:         "google",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		RedirectURL:  redirectURL,
		UserInfoURL:  "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes:       []string{"openid", "email", "profile"},
	}
}

// GitHubProvider returns a preconfigured GitHub OAuth provider.
func GitHubProvider(clientID, clientSecret, redirectURL string) OAuthProvider {
	return OAuthProvider{
		Name:         "github",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AuthorizeURL: "https://github.com/login/oauth/authorize",
		TokenURL:     "https://github.com/login/oauth/access_token",
		RedirectURL:  redirectURL,
		UserInfoURL:  "https://api.github.com/user",
		Scopes:       []string{"read:user", "user:email"},
		Resolver:     githubOAuthResolver(),
	}
}

func (o *OAuth) provider(name string) (OAuthProvider, error) {
	if o == nil {
		return OAuthProvider{}, ErrOAuthProviderNotFound
	}
	name = strings.TrimSpace(name)
	provider, ok := o.providers[name]
	if !ok {
		return OAuthProvider{}, ErrOAuthProviderNotFound
	}
	return provider, nil
}

func (o *OAuth) saveState(r *http.Request, state oauthState) error {
	store := session.Current(r)
	if store == nil {
		return fmt.Errorf("session middleware required before oauth")
	}
	store.Set(o.sessionKey, state)
	return nil
}

func (o *OAuth) consumeState(r *http.Request, providerName, stateParam string) (oauthState, error) {
	store := session.Current(r)
	if store == nil {
		return oauthState{}, fmt.Errorf("session middleware required before oauth")
	}
	var state oauthState
	if !store.Decode(o.sessionKey, &state) {
		return oauthState{}, ErrOAuthStateInvalid
	}
	store.Delete(o.sessionKey)
	if state.Provider != providerName || strings.TrimSpace(state.State) == "" || strings.TrimSpace(state.State) != strings.TrimSpace(stateParam) {
		return oauthState{}, ErrOAuthStateInvalid
	}
	if !state.ExpiresAt.IsZero() && o.now().After(state.ExpiresAt) {
		return oauthState{}, ErrOAuthStateExpired
	}
	return state, nil
}

func exchangeOAuthCode(ctx context.Context, client *http.Client, provider OAuthProvider, code, verifier string) (OAuthToken, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", provider.RedirectURL)
	values.Set("client_id", provider.ClientID)
	values.Set("client_secret", provider.ClientSecret)
	values.Set("code_verifier", verifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return OAuthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return OAuthToken{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return OAuthToken{}, fmt.Errorf("oauth token exchange failed: %s", strings.TrimSpace(string(body)))
	}
	return decodeOAuthToken(res)
}

func decodeOAuthToken(res *http.Response) (OAuthToken, error) {
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return OAuthToken{}, err
	}
	var payload map[string]any
	contentType := res.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") || strings.HasPrefix(strings.TrimSpace(string(body)), "{") {
		if err := json.Unmarshal(body, &payload); err != nil {
			return OAuthToken{}, err
		}
	} else {
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return OAuthToken{}, err
		}
		payload = make(map[string]any, len(values))
		for key, valuesForKey := range values {
			if len(valuesForKey) > 0 {
				payload[key] = valuesForKey[0]
			}
		}
	}
	token := OAuthToken{
		AccessToken:  stringValue(payload["access_token"]),
		TokenType:    stringValue(payload["token_type"]),
		RefreshToken: stringValue(payload["refresh_token"]),
		IDToken:      stringValue(payload["id_token"]),
		Scope:        stringValue(payload["scope"]),
		ExpiresIn:    intValue(payload["expires_in"]),
		Raw:          payload,
	}
	if token.AccessToken == "" {
		return OAuthToken{}, fmt.Errorf("oauth token exchange returned no access token")
	}
	return token, nil
}

func resolveOAuthUser(ctx context.Context, client *http.Client, provider OAuthProvider, token OAuthToken) (User, error) {
	if provider.Resolver != nil {
		user, err := provider.Resolver.ResolveOAuthUser(ctx, provider, client, token)
		return normalizeOAuthUser(provider.Name, user, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.UserInfoURL, nil)
	if err != nil {
		return User{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return User{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return User{}, fmt.Errorf("oauth userinfo failed: %s", strings.TrimSpace(string(body)))
	}
	var payload map[string]any
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return User{}, err
	}
	return normalizeOAuthUser(provider.Name, userFromOAuthPayload(provider.Name, payload), nil)
}

func userFromOAuthPayload(provider string, payload map[string]any) User {
	id := stringValue(payload["sub"])
	if id == "" {
		id = stringValue(payload["id"])
	}
	email := strings.ToLower(strings.TrimSpace(stringValue(payload["email"])))
	name := strings.TrimSpace(stringValue(payload["name"]))
	if name == "" {
		name = strings.TrimSpace(stringValue(payload["preferred_username"]))
	}
	if name == "" {
		name = strings.TrimSpace(stringValue(payload["login"]))
	}
	if name == "" {
		name = email
	}
	if id == "" {
		id = email
	}
	meta := map[string]any{
		"provider": provider,
	}
	if avatar := stringValue(payload["picture"]); avatar != "" {
		meta["avatar"] = avatar
	}
	if avatar := stringValue(payload["avatar_url"]); avatar != "" {
		meta["avatar"] = avatar
	}
	if profile := stringValue(payload["html_url"]); profile != "" {
		meta["profile"] = profile
	}
	return User{
		ID:    provider + ":" + id,
		Email: email,
		Name:  name,
		Meta:  meta,
	}
}

func normalizeOAuthUser(provider string, user User, err error) (User, error) {
	if err != nil {
		return User{}, err
	}
	user.ID = strings.TrimSpace(user.ID)
	user.Email = strings.TrimSpace(strings.ToLower(user.Email))
	user.Name = strings.TrimSpace(user.Name)
	if user.ID == "" {
		if user.Email != "" {
			user.ID = provider + ":" + user.Email
		} else {
			return User{}, fmt.Errorf("oauth user resolver returned no id")
		}
	}
	if user.Meta == nil {
		user.Meta = map[string]any{}
	}
	if _, ok := user.Meta["provider"]; !ok {
		user.Meta["provider"] = provider
	}
	return user, nil
}

func githubOAuthResolver() OAuthUserResolver {
	return OAuthUserResolverFunc(func(ctx context.Context, provider OAuthProvider, client *http.Client, token OAuthToken) (User, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.UserInfoURL, nil)
		if err != nil {
			return User{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		req.Header.Set("Accept", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return User{}, err
		}
		defer res.Body.Close()
		if res.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
			return User{}, fmt.Errorf("github userinfo failed: %s", strings.TrimSpace(string(body)))
		}
		var payload map[string]any
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			return User{}, err
		}
		email := strings.ToLower(strings.TrimSpace(stringValue(payload["email"])))
		if email == "" {
			email = fetchGitHubPrimaryEmail(ctx, client, token.AccessToken)
		}
		payload["email"] = email
		return userFromOAuthPayload(provider.Name, payload), nil
	})
}

func fetchGitHubPrimaryEmail(ctx context.Context, client *http.Client, accessToken string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return ""
	}
	var payload []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return ""
	}
	for _, item := range payload {
		if boolValue(item["primary"]) {
			return strings.ToLower(strings.TrimSpace(stringValue(item["email"])))
		}
	}
	if len(payload) > 0 {
		return strings.ToLower(strings.TrimSpace(stringValue(payload[0]["email"])))
	}
	return ""
}

func randomOAuthString(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func oauthCodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func writeOAuthJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeOAuthError(w http.ResponseWriter, r *http.Request, status int, err error) {
	if requestWantsJSON(r) {
		writeOAuthJSON(w, status, map[string]any{
			"error": err.Error(),
		})
		return
	}
	http.Error(w, err.Error(), status)
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	}
	return 0
}

func boolValue(value any) bool {
	typed, _ := value.(bool)
	return typed
}

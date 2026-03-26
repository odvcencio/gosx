package auth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/gosx/session"
)

var (
	ErrWebAuthnChallengeInvalid   = errors.New("webauthn challenge is invalid")
	ErrWebAuthnChallengeExpired   = errors.New("webauthn challenge expired")
	ErrWebAuthnCredentialNotFound = errors.New("webauthn credential not found")
	ErrWebAuthnVerificationFailed = errors.New("webauthn verification failed")
	ErrWebAuthnCounterInvalid     = errors.New("webauthn counter is invalid")
)

type webAuthnStateKind string

const (
	webAuthnStateRegister webAuthnStateKind = "register"
	webAuthnStateLogin    webAuthnStateKind = "login"
)

// WebAuthnCredential is a stored passkey/public-key credential bound to a user.
type WebAuthnCredential struct {
	ID         string    `json:"id"`
	User       User      `json:"user"`
	PublicKey  []byte    `json:"publicKey"`
	Algorithm  int       `json:"algorithm"`
	SignCount  uint32    `json:"signCount"`
	Transports []string  `json:"transports,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
	LastUsedAt time.Time `json:"lastUsedAt,omitempty"`
}

// WebAuthnStore persists registered credentials.
type WebAuthnStore interface {
	SaveCredential(WebAuthnCredential) error
	Credential(string) (WebAuthnCredential, error)
	Credentials(string) ([]WebAuthnCredential, error)
	UpdateCounter(string, uint32, time.Time) error
}

// WebAuthnResolver resolves a user identifier into the user that should be
// offered for passkey authentication.
type WebAuthnResolver interface {
	ResolveWebAuthn(context.Context, string) (User, error)
}

// WebAuthnResolverFunc adapts a function into a WebAuthnResolver.
type WebAuthnResolverFunc func(context.Context, string) (User, error)

func (fn WebAuthnResolverFunc) ResolveWebAuthn(ctx context.Context, value string) (User, error) {
	if fn == nil {
		return User{}, nil
	}
	return fn(ctx, value)
}

// MemoryWebAuthnStore keeps credentials in memory.
type MemoryWebAuthnStore struct {
	mu          sync.Mutex
	credentials map[string]WebAuthnCredential
	userIndex   map[string][]string
}

// NewMemoryWebAuthnStore creates an in-memory passkey store.
func NewMemoryWebAuthnStore() *MemoryWebAuthnStore {
	return &MemoryWebAuthnStore{
		credentials: make(map[string]WebAuthnCredential),
		userIndex:   make(map[string][]string),
	}
}

// SaveCredential stores or replaces a credential.
func (s *MemoryWebAuthnStore) SaveCredential(credential WebAuthnCredential) error {
	if s == nil {
		return fmt.Errorf("webauthn store is nil")
	}
	if strings.TrimSpace(credential.ID) == "" {
		return ErrWebAuthnCredentialNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.credentials == nil {
		s.credentials = make(map[string]WebAuthnCredential)
	}
	if s.userIndex == nil {
		s.userIndex = make(map[string][]string)
	}
	credential.ID = normalizeCredentialID(credential.ID)
	s.credentials[credential.ID] = credential
	if credential.User.ID != "" && !containsString(s.userIndex[credential.User.ID], credential.ID) {
		s.userIndex[credential.User.ID] = append(s.userIndex[credential.User.ID], credential.ID)
	}
	return nil
}

// Credential loads a credential by ID.
func (s *MemoryWebAuthnStore) Credential(id string) (WebAuthnCredential, error) {
	if s == nil {
		return WebAuthnCredential{}, ErrWebAuthnCredentialNotFound
	}
	id = normalizeCredentialID(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	credential, ok := s.credentials[id]
	if !ok {
		return WebAuthnCredential{}, ErrWebAuthnCredentialNotFound
	}
	return credential, nil
}

// Credentials loads all credentials for the provided user ID.
func (s *MemoryWebAuthnStore) Credentials(userID string) ([]WebAuthnCredential, error) {
	if s == nil {
		return nil, nil
	}
	userID = strings.TrimSpace(userID)
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.userIndex[userID]
	out := make([]WebAuthnCredential, 0, len(ids))
	for _, id := range ids {
		if credential, ok := s.credentials[id]; ok {
			out = append(out, credential)
		}
	}
	return out, nil
}

// UpdateCounter updates the signature counter for a credential.
func (s *MemoryWebAuthnStore) UpdateCounter(id string, signCount uint32, usedAt time.Time) error {
	if s == nil {
		return ErrWebAuthnCredentialNotFound
	}
	id = normalizeCredentialID(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	credential, ok := s.credentials[id]
	if !ok {
		return ErrWebAuthnCredentialNotFound
	}
	credential.SignCount = signCount
	credential.LastUsedAt = usedAt
	s.credentials[id] = credential
	return nil
}

// WebAuthnOptions configures the built-in WebAuthn/passkey flow.
type WebAuthnOptions struct {
	RPID             string
	RPName           string
	Origin           string
	TTL              time.Duration
	SessionKey       string
	SuccessPath      string
	FailurePath      string
	FlashKey         string
	UserVerification string
	Store            WebAuthnStore
	Resolver         WebAuthnResolver
	Now              func() time.Time
}

// WebAuthn drives session-backed registration and authentication ceremonies.
type WebAuthn struct {
	manager          *Manager
	rpID             string
	rpName           string
	origin           string
	ttl              time.Duration
	sessionKey       string
	successPath      string
	failurePath      string
	flashKey         string
	userVerification string
	store            WebAuthnStore
	resolver         WebAuthnResolver
	now              func() time.Time
}

type webAuthnState struct {
	Kind      webAuthnStateKind `json:"kind"`
	Challenge string            `json:"challenge"`
	User      User              `json:"user"`
	Allowed   []string          `json:"allowed,omitempty"`
	Next      string            `json:"next,omitempty"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

// WebAuthnCredentialDescriptor identifies a browser credential.
type WebAuthnCredentialDescriptor struct {
	Type       string   `json:"type"`
	ID         string   `json:"id"`
	Transports []string `json:"transports,omitempty"`
}

// WebAuthnCreationOptions is the JSON shape expected by browser helpers for
// navigator.credentials.create.
type WebAuthnCreationOptions struct {
	Challenge              string                             `json:"challenge"`
	RP                     WebAuthnRPEntity                   `json:"rp"`
	User                   WebAuthnUserEntity                 `json:"user"`
	PubKeyCredParams       []WebAuthnPublicKeyCredentialParam `json:"pubKeyCredParams"`
	Timeout                int                                `json:"timeout,omitempty"`
	Attestation            string                             `json:"attestation,omitempty"`
	AuthenticatorSelection WebAuthnAuthenticatorSelection     `json:"authenticatorSelection,omitempty"`
	ExcludeCredentials     []WebAuthnCredentialDescriptor     `json:"excludeCredentials,omitempty"`
}

// WebAuthnRequestOptions is the JSON shape expected by browser helpers for
// navigator.credentials.get.
type WebAuthnRequestOptions struct {
	Challenge        string                         `json:"challenge"`
	RPID             string                         `json:"rpId,omitempty"`
	Timeout          int                            `json:"timeout,omitempty"`
	UserVerification string                         `json:"userVerification,omitempty"`
	AllowCredentials []WebAuthnCredentialDescriptor `json:"allowCredentials,omitempty"`
}

// WebAuthnRPEntity describes the relying party.
type WebAuthnRPEntity struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// WebAuthnUserEntity describes the registering user.
type WebAuthnUserEntity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// WebAuthnPublicKeyCredentialParam describes a supported credential algorithm.
type WebAuthnPublicKeyCredentialParam struct {
	Type string `json:"type"`
	Alg  int    `json:"alg"`
}

// WebAuthnAuthenticatorSelection configures browser authenticator selection.
type WebAuthnAuthenticatorSelection struct {
	ResidentKey      string `json:"residentKey,omitempty"`
	UserVerification string `json:"userVerification,omitempty"`
}

// WebAuthnRegistrationResponse is the browser JSON payload returned from
// navigator.credentials.create.
type WebAuthnRegistrationResponse struct {
	ID       string `json:"id"`
	RawID    string `json:"rawId"`
	Type     string `json:"type"`
	Response struct {
		ClientDataJSON     string   `json:"clientDataJSON"`
		AuthenticatorData  string   `json:"authenticatorData"`
		PublicKey          string   `json:"publicKey"`
		PublicKeyAlgorithm int      `json:"publicKeyAlgorithm"`
		Transports         []string `json:"transports,omitempty"`
	} `json:"response"`
}

// WebAuthnAuthenticationResponse is the browser JSON payload returned from
// navigator.credentials.get.
type WebAuthnAuthenticationResponse struct {
	ID       string `json:"id"`
	RawID    string `json:"rawId"`
	Type     string `json:"type"`
	Response struct {
		ClientDataJSON    string `json:"clientDataJSON"`
		AuthenticatorData string `json:"authenticatorData"`
		Signature         string `json:"signature"`
		UserHandle        string `json:"userHandle,omitempty"`
	} `json:"response"`
}

type webAuthnClientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

type parsedAuthenticatorData struct {
	RPIDHash  [32]byte
	Flags     byte
	SignCount uint32
}

// NewWebAuthn creates a batteries-included WebAuthn/passkey flow for a manager.
func NewWebAuthn(manager *Manager, opts WebAuthnOptions) *WebAuthn {
	if opts.TTL == 0 {
		opts.TTL = 10 * time.Minute
	}
	if opts.SessionKey == "" {
		opts.SessionKey = "auth.webauthn"
	}
	if opts.SuccessPath == "" {
		opts.SuccessPath = "/"
	}
	if opts.FailurePath == "" {
		opts.FailurePath = "/"
	}
	if opts.FlashKey == "" {
		opts.FlashKey = "auth.webauthn"
	}
	if opts.UserVerification == "" {
		opts.UserVerification = "preferred"
	}
	if opts.Store == nil {
		opts.Store = NewMemoryWebAuthnStore()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &WebAuthn{
		manager:          manager,
		rpID:             strings.TrimSpace(opts.RPID),
		rpName:           strings.TrimSpace(opts.RPName),
		origin:           strings.TrimRight(strings.TrimSpace(opts.Origin), "/"),
		ttl:              opts.TTL,
		sessionKey:       opts.SessionKey,
		successPath:      opts.SuccessPath,
		failurePath:      opts.FailurePath,
		flashKey:         opts.FlashKey,
		userVerification: opts.UserVerification,
		store:            opts.Store,
		resolver:         opts.Resolver,
		now:              opts.Now,
	}
}

// WebAuthn creates a passkey manager bound to the auth manager.
func (m *Manager) WebAuthn(opts WebAuthnOptions) *WebAuthn {
	return NewWebAuthn(m, opts)
}

// BeginRegistration starts a registration ceremony for a user.
func (w *WebAuthn) BeginRegistration(r *http.Request, user User, next string) (WebAuthnCreationOptions, error) {
	if w == nil {
		return WebAuthnCreationOptions{}, fmt.Errorf("webauthn manager is nil")
	}
	user = normalizeWebAuthnUser(user)
	if user.ID == "" {
		return WebAuthnCreationOptions{}, fmt.Errorf("webauthn registration requires a user id")
	}
	creds, err := w.store.Credentials(user.ID)
	if err != nil {
		return WebAuthnCreationOptions{}, err
	}
	challenge, err := randomChallenge()
	if err != nil {
		return WebAuthnCreationOptions{}, err
	}
	if err := w.saveState(r, webAuthnState{
		Kind:      webAuthnStateRegister,
		Challenge: challenge,
		User:      user,
		Next:      sanitizeRedirectTarget(next),
		ExpiresAt: w.now().Add(w.ttl),
	}); err != nil {
		return WebAuthnCreationOptions{}, err
	}
	options := WebAuthnCreationOptions{
		Challenge: challenge,
		RP: WebAuthnRPEntity{
			ID:   w.effectiveRPID(r),
			Name: w.effectiveRPName(r),
		},
		User: WebAuthnUserEntity{
			ID:          encodeWebAuthnBytes([]byte(user.ID)),
			Name:        webAuthnUserName(user),
			DisplayName: webAuthnDisplayName(user),
		},
		PubKeyCredParams: []WebAuthnPublicKeyCredentialParam{
			{Type: "public-key", Alg: -7},
			{Type: "public-key", Alg: -257},
		},
		Timeout:     int(w.ttl / time.Millisecond),
		Attestation: "none",
		AuthenticatorSelection: WebAuthnAuthenticatorSelection{
			ResidentKey:      "preferred",
			UserVerification: w.userVerification,
		},
		ExcludeCredentials: make([]WebAuthnCredentialDescriptor, 0, len(creds)),
	}
	for _, credential := range creds {
		options.ExcludeCredentials = append(options.ExcludeCredentials, WebAuthnCredentialDescriptor{
			Type:       "public-key",
			ID:         credential.ID,
			Transports: append([]string(nil), credential.Transports...),
		})
	}
	return options, nil
}

// FinishRegistration verifies and stores a new passkey credential.
func (w *WebAuthn) FinishRegistration(r *http.Request, payload WebAuthnRegistrationResponse) (WebAuthnCredential, string, error) {
	state, err := w.consumeState(r, webAuthnStateRegister)
	if err != nil {
		return WebAuthnCredential{}, "", err
	}
	if payload.Type != "" && payload.Type != "public-key" {
		return WebAuthnCredential{}, "", ErrWebAuthnVerificationFailed
	}
	clientData, err := parseClientData(payload.Response.ClientDataJSON)
	if err != nil {
		return WebAuthnCredential{}, "", err
	}
	if clientData.Type != "webauthn.create" {
		return WebAuthnCredential{}, "", ErrWebAuthnVerificationFailed
	}
	if !equalWebAuthnChallenge(clientData.Challenge, state.Challenge) {
		return WebAuthnCredential{}, "", ErrWebAuthnChallengeInvalid
	}
	if clientData.Origin != w.effectiveOrigin(r) {
		return WebAuthnCredential{}, "", ErrWebAuthnVerificationFailed
	}
	authData, err := decodeAuthenticatorData(payload.Response.AuthenticatorData)
	if err != nil {
		return WebAuthnCredential{}, "", err
	}
	if err := verifyAuthenticatorData(authData, w.effectiveRPID(r), w.userVerification); err != nil {
		return WebAuthnCredential{}, "", err
	}
	publicKey, err := decodeWebAuthnBytes(payload.Response.PublicKey)
	if err != nil || len(publicKey) == 0 {
		return WebAuthnCredential{}, "", ErrWebAuthnVerificationFailed
	}
	if _, err := x509.ParsePKIXPublicKey(publicKey); err != nil {
		return WebAuthnCredential{}, "", ErrWebAuthnVerificationFailed
	}
	id, err := resolveCredentialID(payload.ID, payload.RawID)
	if err != nil {
		return WebAuthnCredential{}, "", err
	}
	credential := WebAuthnCredential{
		ID:         id,
		User:       state.User,
		PublicKey:  append([]byte(nil), publicKey...),
		Algorithm:  payload.Response.PublicKeyAlgorithm,
		SignCount:  authData.SignCount,
		Transports: append([]string(nil), payload.Response.Transports...),
		CreatedAt:  w.now(),
		LastUsedAt: w.now(),
	}
	if err := w.store.SaveCredential(credential); err != nil {
		return WebAuthnCredential{}, "", err
	}
	if w.manager != nil {
		_ = w.manager.SignIn(r, state.User)
	}
	target := state.Next
	if target == "" {
		target = w.successPath
	}
	return credential, target, nil
}

// BeginAuthentication starts a passkey authentication ceremony.
func (w *WebAuthn) BeginAuthentication(r *http.Request, login string, next string) (WebAuthnRequestOptions, error) {
	if w == nil {
		return WebAuthnRequestOptions{}, fmt.Errorf("webauthn manager is nil")
	}
	user, err := w.resolveUser(r.Context(), login)
	if err != nil {
		return WebAuthnRequestOptions{}, err
	}
	var credentials []WebAuthnCredential
	if user.ID != "" {
		credentials, err = w.store.Credentials(user.ID)
		if err != nil {
			return WebAuthnRequestOptions{}, err
		}
		if len(credentials) == 0 {
			return WebAuthnRequestOptions{}, ErrWebAuthnCredentialNotFound
		}
	}

	challenge, err := randomChallenge()
	if err != nil {
		return WebAuthnRequestOptions{}, err
	}
	state := webAuthnState{
		Kind:      webAuthnStateLogin,
		Challenge: challenge,
		User:      user,
		Next:      sanitizeRedirectTarget(next),
		ExpiresAt: w.now().Add(w.ttl),
	}
	options := WebAuthnRequestOptions{
		Challenge:        challenge,
		RPID:             w.effectiveRPID(r),
		Timeout:          int(w.ttl / time.Millisecond),
		UserVerification: w.userVerification,
		AllowCredentials: make([]WebAuthnCredentialDescriptor, 0, len(credentials)),
	}
	for _, credential := range credentials {
		options.AllowCredentials = append(options.AllowCredentials, WebAuthnCredentialDescriptor{
			Type:       "public-key",
			ID:         credential.ID,
			Transports: append([]string(nil), credential.Transports...),
		})
		state.Allowed = append(state.Allowed, credential.ID)
	}
	if err := w.saveState(r, state); err != nil {
		return WebAuthnRequestOptions{}, err
	}
	return options, nil
}

// FinishAuthentication verifies a passkey assertion and signs the user in.
func (w *WebAuthn) FinishAuthentication(r *http.Request, payload WebAuthnAuthenticationResponse) (User, string, error) {
	state, err := w.consumeState(r, webAuthnStateLogin)
	if err != nil {
		return User{}, "", err
	}
	if payload.Type != "" && payload.Type != "public-key" {
		return User{}, "", ErrWebAuthnVerificationFailed
	}
	clientDataJSON, err := decodeWebAuthnBytes(payload.Response.ClientDataJSON)
	if err != nil {
		return User{}, "", ErrWebAuthnVerificationFailed
	}
	var clientData webAuthnClientData
	if err := json.Unmarshal(clientDataJSON, &clientData); err != nil {
		return User{}, "", ErrWebAuthnVerificationFailed
	}
	if clientData.Type != "webauthn.get" {
		return User{}, "", ErrWebAuthnVerificationFailed
	}
	if !equalWebAuthnChallenge(clientData.Challenge, state.Challenge) {
		return User{}, "", ErrWebAuthnChallengeInvalid
	}
	if clientData.Origin != w.effectiveOrigin(r) {
		return User{}, "", ErrWebAuthnVerificationFailed
	}
	authDataBytes, err := decodeWebAuthnBytes(payload.Response.AuthenticatorData)
	if err != nil {
		return User{}, "", ErrWebAuthnVerificationFailed
	}
	authData, err := parseAuthenticatorData(authDataBytes)
	if err != nil {
		return User{}, "", err
	}
	if err := verifyAuthenticatorData(authData, w.effectiveRPID(r), w.userVerification); err != nil {
		return User{}, "", err
	}
	credentialID, err := resolveCredentialID(payload.ID, payload.RawID)
	if err != nil {
		return User{}, "", err
	}
	if len(state.Allowed) > 0 && !containsString(state.Allowed, credentialID) {
		return User{}, "", ErrWebAuthnCredentialNotFound
	}
	credential, err := w.store.Credential(credentialID)
	if err != nil {
		return User{}, "", err
	}
	if state.User.ID != "" && credential.User.ID != state.User.ID {
		return User{}, "", ErrWebAuthnCredentialNotFound
	}
	signature, err := decodeWebAuthnBytes(payload.Response.Signature)
	if err != nil {
		return User{}, "", ErrWebAuthnVerificationFailed
	}
	clientHash := sha256.Sum256(clientDataJSON)
	signed := append(append([]byte(nil), authDataBytes...), clientHash[:]...)
	if err := verifyWebAuthnSignature(credential.PublicKey, credential.Algorithm, signed, signature); err != nil {
		return User{}, "", err
	}
	if authData.SignCount > 0 && credential.SignCount > 0 && authData.SignCount <= credential.SignCount {
		return User{}, "", ErrWebAuthnCounterInvalid
	}
	if err := w.store.UpdateCounter(credential.ID, authData.SignCount, w.now()); err != nil {
		return User{}, "", err
	}
	if w.manager == nil || !w.manager.SignIn(r, credential.User) {
		return User{}, "", fmt.Errorf("session middleware required before webauthn authentication")
	}
	target := state.Next
	if target == "" {
		target = w.successPath
	}
	return credential.User, target, nil
}

// RegisterOptionsHandler returns creation options for navigator.credentials.create.
func (w *WebAuthn) RegisterOptionsHandler() http.Handler {
	return http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		payload, err := readWebAuthnUserRequest(r)
		if err != nil {
			writeWebAuthnError(wr, http.StatusBadRequest, err)
			return
		}
		user, ok := Current(r)
		if !ok {
			user = payload.User
		}
		options, err := w.BeginRegistration(r, user, payload.Next)
		if err != nil {
			writeWebAuthnError(wr, http.StatusBadRequest, err)
			return
		}
		writeWebAuthnJSON(wr, http.StatusOK, map[string]any{
			"ok":      true,
			"options": options,
		})
	})
}

// RegisterHandler verifies a navigator.credentials.create payload.
func (w *WebAuthn) RegisterHandler() http.Handler {
	return http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		var payload WebAuthnRegistrationResponse
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeWebAuthnError(wr, http.StatusBadRequest, ErrWebAuthnVerificationFailed)
			return
		}
		credential, target, err := w.FinishRegistration(r, payload)
		if err != nil {
			writeWebAuthnError(wr, http.StatusUnauthorized, err)
			return
		}
		addMagicLinkFlash(r, w.flashKey, map[string]any{
			"status": "registered",
			"id":     credential.ID,
		})
		writeWebAuthnJSON(wr, http.StatusOK, map[string]any{
			"ok":         true,
			"credential": credential,
			"target":     target,
		})
	})
}

// LoginOptionsHandler returns request options for navigator.credentials.get.
func (w *WebAuthn) LoginOptionsHandler() http.Handler {
	return http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		payload, err := readWebAuthnLoginRequest(r)
		if err != nil {
			writeWebAuthnError(wr, http.StatusBadRequest, err)
			return
		}
		options, err := w.BeginAuthentication(r, payload.Login, payload.Next)
		if err != nil {
			writeWebAuthnError(wr, http.StatusBadRequest, err)
			return
		}
		writeWebAuthnJSON(wr, http.StatusOK, map[string]any{
			"ok":      true,
			"options": options,
		})
	})
}

// LoginHandler verifies a navigator.credentials.get payload and signs the user in.
func (w *WebAuthn) LoginHandler() http.Handler {
	return http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		var payload WebAuthnAuthenticationResponse
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeWebAuthnError(wr, http.StatusBadRequest, ErrWebAuthnVerificationFailed)
			return
		}
		user, target, err := w.FinishAuthentication(r, payload)
		if err != nil {
			writeWebAuthnError(wr, http.StatusUnauthorized, err)
			return
		}
		addMagicLinkFlash(r, w.flashKey, map[string]any{
			"status": "signed_in",
			"email":  user.Email,
		})
		writeWebAuthnJSON(wr, http.StatusOK, map[string]any{
			"ok":     true,
			"user":   user,
			"target": target,
		})
	})
}

func (w *WebAuthn) saveState(r *http.Request, state webAuthnState) error {
	store := session.Current(r)
	if store == nil {
		return fmt.Errorf("session middleware required before webauthn")
	}
	store.Set(w.sessionKey, state)
	return nil
}

func (w *WebAuthn) consumeState(r *http.Request, kind webAuthnStateKind) (webAuthnState, error) {
	store := session.Current(r)
	if store == nil {
		return webAuthnState{}, fmt.Errorf("session middleware required before webauthn")
	}
	var state webAuthnState
	if !store.Decode(w.sessionKey, &state) {
		return webAuthnState{}, ErrWebAuthnChallengeInvalid
	}
	store.Delete(w.sessionKey)
	if state.Kind != kind {
		return webAuthnState{}, ErrWebAuthnChallengeInvalid
	}
	if !state.ExpiresAt.IsZero() && w.now().After(state.ExpiresAt) {
		return webAuthnState{}, ErrWebAuthnChallengeExpired
	}
	return state, nil
}

func (w *WebAuthn) resolveUser(ctx context.Context, login string) (User, error) {
	login = strings.TrimSpace(login)
	if login == "" {
		return User{}, nil
	}
	if w != nil && w.resolver != nil {
		user, err := w.resolver.ResolveWebAuthn(ctx, login)
		return normalizeWebAuthnUserWithFallback(login, user, err)
	}
	return normalizeWebAuthnUser(User{ID: login, Email: login}), nil
}

func (w *WebAuthn) effectiveRPID(r *http.Request) string {
	if w != nil && strings.TrimSpace(w.rpID) != "" {
		return strings.TrimSpace(w.rpID)
	}
	host := ""
	if r != nil {
		host = r.Host
	}
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return host
}

func (w *WebAuthn) effectiveRPName(r *http.Request) string {
	if w != nil && strings.TrimSpace(w.rpName) != "" {
		return strings.TrimSpace(w.rpName)
	}
	if host := w.effectiveRPID(r); host != "" {
		return host
	}
	return "GoSX"
}

func (w *WebAuthn) effectiveOrigin(r *http.Request) string {
	if w != nil && strings.TrimSpace(w.origin) != "" {
		return strings.TrimRight(w.origin, "/")
	}
	return requestOrigin(r)
}

func normalizeWebAuthnUserWithFallback(login string, user User, err error) (User, error) {
	if err != nil {
		return User{}, err
	}
	if user.ID == "" {
		user.ID = login
	}
	if user.Email == "" && strings.Contains(login, "@") {
		user.Email = login
	}
	return normalizeWebAuthnUser(user), nil
}

func normalizeWebAuthnUser(user User) User {
	user.ID = strings.TrimSpace(user.ID)
	user.Email = strings.TrimSpace(strings.ToLower(user.Email))
	user.Name = strings.TrimSpace(user.Name)
	if user.ID == "" {
		user.ID = user.Email
	}
	if user.Email == "" && strings.Contains(user.ID, "@") {
		user.Email = strings.ToLower(user.ID)
	}
	return user
}

func webAuthnUserName(user User) string {
	if user.Email != "" {
		return user.Email
	}
	if user.Name != "" {
		return user.Name
	}
	return user.ID
}

func webAuthnDisplayName(user User) string {
	if user.Name != "" {
		return user.Name
	}
	if user.Email != "" {
		return user.Email
	}
	return user.ID
}

func randomChallenge() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return encodeWebAuthnBytes(buf), nil
}

func resolveCredentialID(id, rawID string) (string, error) {
	if rawID != "" {
		bytes, err := decodeWebAuthnBytes(rawID)
		if err != nil {
			return "", ErrWebAuthnVerificationFailed
		}
		return encodeWebAuthnBytes(bytes), nil
	}
	id = normalizeCredentialID(id)
	if id == "" {
		return "", ErrWebAuthnCredentialNotFound
	}
	return id, nil
}

func normalizeCredentialID(id string) string {
	return strings.TrimSpace(id)
}

func parseClientData(encoded string) (webAuthnClientData, error) {
	bytes, err := decodeWebAuthnBytes(encoded)
	if err != nil {
		return webAuthnClientData{}, ErrWebAuthnVerificationFailed
	}
	var data webAuthnClientData
	if err := json.Unmarshal(bytes, &data); err != nil {
		return webAuthnClientData{}, ErrWebAuthnVerificationFailed
	}
	return data, nil
}

func decodeAuthenticatorData(encoded string) (parsedAuthenticatorData, error) {
	raw, err := decodeWebAuthnBytes(encoded)
	if err != nil {
		return parsedAuthenticatorData{}, ErrWebAuthnVerificationFailed
	}
	return parseAuthenticatorData(raw)
}

func parseAuthenticatorData(raw []byte) (parsedAuthenticatorData, error) {
	if len(raw) < 37 {
		return parsedAuthenticatorData{}, ErrWebAuthnVerificationFailed
	}
	var rpIDHash [32]byte
	copy(rpIDHash[:], raw[:32])
	return parsedAuthenticatorData{
		RPIDHash:  rpIDHash,
		Flags:     raw[32],
		SignCount: binary.BigEndian.Uint32(raw[33:37]),
	}, nil
}

func verifyAuthenticatorData(data parsedAuthenticatorData, rpID string, userVerification string) error {
	sum := sha256.Sum256([]byte(rpID))
	if !bytes.Equal(data.RPIDHash[:], sum[:]) {
		return ErrWebAuthnVerificationFailed
	}
	if data.Flags&0x01 == 0 {
		return ErrWebAuthnVerificationFailed
	}
	if userVerification == "required" && data.Flags&0x04 == 0 {
		return ErrWebAuthnVerificationFailed
	}
	return nil
}

func verifyWebAuthnSignature(publicKeyDER []byte, algorithm int, signed, signature []byte) error {
	publicKey, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return ErrWebAuthnVerificationFailed
	}
	hash := sha256.Sum256(signed)
	switch key := publicKey.(type) {
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(key, hash[:], signature) {
			return ErrWebAuthnVerificationFailed
		}
		return nil
	case *rsa.PublicKey:
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, hash[:], signature); err != nil {
			return ErrWebAuthnVerificationFailed
		}
		return nil
	case ed25519.PublicKey:
		if !ed25519.Verify(key, signed, signature) {
			return ErrWebAuthnVerificationFailed
		}
		return nil
	default:
		_ = algorithm
		return ErrWebAuthnVerificationFailed
	}
}

func writeWebAuthnJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeWebAuthnError(w http.ResponseWriter, status int, err error) {
	writeWebAuthnJSON(w, status, map[string]any{
		"error": err.Error(),
	})
}

type webAuthnUserRequest struct {
	User User   `json:"user"`
	Next string `json:"next"`
}

type webAuthnLoginRequest struct {
	Login string `json:"login"`
	Next  string `json:"next"`
}

func readWebAuthnUserRequest(r *http.Request) (webAuthnUserRequest, error) {
	if r == nil || r.Body == nil {
		return webAuthnUserRequest{}, nil
	}
	var payload webAuthnUserRequest
	if requestWantsJSON(r) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return webAuthnUserRequest{}, fmt.Errorf("invalid webauthn registration request")
		}
		return payload, nil
	}
	if err := r.ParseForm(); err != nil {
		return webAuthnUserRequest{}, err
	}
	payload.User = User{
		ID:    r.Form.Get("id"),
		Email: r.Form.Get("email"),
		Name:  r.Form.Get("name"),
	}
	payload.Next = r.Form.Get("next")
	return payload, nil
}

func readWebAuthnLoginRequest(r *http.Request) (webAuthnLoginRequest, error) {
	if r == nil || r.Body == nil {
		return webAuthnLoginRequest{}, nil
	}
	var payload webAuthnLoginRequest
	if requestWantsJSON(r) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return webAuthnLoginRequest{}, fmt.Errorf("invalid webauthn login request")
		}
		return payload, nil
	}
	if err := r.ParseForm(); err != nil {
		return webAuthnLoginRequest{}, err
	}
	payload.Login = r.Form.Get("login")
	payload.Next = r.Form.Get("next")
	return payload, nil
}

func encodeWebAuthnBytes(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func decodeWebAuthnBytes(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func equalWebAuthnChallenge(a, b string) bool {
	aBytes, errA := decodeWebAuthnBytes(a)
	bBytes, errB := decodeWebAuthnBytes(b)
	if errA == nil && errB == nil {
		return bytes.Equal(aBytes, bBytes)
	}
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

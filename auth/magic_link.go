package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/gosx/session"
)

var (
	ErrMagicLinkInvalid = errors.New("magic link is invalid")
	ErrMagicLinkExpired = errors.New("magic link expired")
)

// MagicLinkToken is a persisted sign-in token awaiting consumption.
type MagicLinkToken struct {
	Token     string    `json:"token"`
	Email     string    `json:"email"`
	User      User      `json:"user"`
	Next      string    `json:"next,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// MagicLinkDelivery is the generated delivery payload for a magic link.
type MagicLinkDelivery struct {
	Email     string    `json:"email"`
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	User      User      `json:"user"`
	Next      string    `json:"next,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// MagicLinkStore persists issued magic-link tokens until they are consumed.
type MagicLinkStore interface {
	Save(MagicLinkToken) error
	Consume(token string, now time.Time) (MagicLinkToken, error)
}

// MagicLinkSender delivers a generated magic link.
type MagicLinkSender interface {
	SendMagicLink(context.Context, MagicLinkDelivery) error
}

// MagicLinkSenderFunc adapts a function into a sender.
type MagicLinkSenderFunc func(context.Context, MagicLinkDelivery) error

func (fn MagicLinkSenderFunc) SendMagicLink(ctx context.Context, delivery MagicLinkDelivery) error {
	if fn == nil {
		return nil
	}
	return fn(ctx, delivery)
}

// MagicLinkResolver resolves the user that should be signed in for an email.
type MagicLinkResolver interface {
	ResolveMagicLink(context.Context, string) (User, error)
}

// MagicLinkResolverFunc adapts a function into a resolver.
type MagicLinkResolverFunc func(context.Context, string) (User, error)

func (fn MagicLinkResolverFunc) ResolveMagicLink(ctx context.Context, email string) (User, error) {
	if fn == nil {
		return User{}, nil
	}
	return fn(ctx, email)
}

// MemoryMagicLinkStore keeps tokens in memory.
type MemoryMagicLinkStore struct {
	mu     sync.Mutex
	tokens map[string]MagicLinkToken
}

// NewMemoryMagicLinkStore creates an in-memory magic-link token store.
func NewMemoryMagicLinkStore() *MemoryMagicLinkStore {
	return &MemoryMagicLinkStore{
		tokens: make(map[string]MagicLinkToken),
	}
}

// Save stores or replaces a token.
func (s *MemoryMagicLinkStore) Save(token MagicLinkToken) error {
	if s == nil {
		return fmt.Errorf("magic link store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens == nil {
		s.tokens = make(map[string]MagicLinkToken)
	}
	s.tokens[token.Token] = token
	return nil
}

// Consume validates and removes a token from the store.
func (s *MemoryMagicLinkStore) Consume(token string, now time.Time) (MagicLinkToken, error) {
	if s == nil {
		return MagicLinkToken{}, ErrMagicLinkInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tokens[token]
	if !ok {
		return MagicLinkToken{}, ErrMagicLinkInvalid
	}
	delete(s.tokens, token)
	if !entry.ExpiresAt.IsZero() && now.After(entry.ExpiresAt) {
		return MagicLinkToken{}, ErrMagicLinkExpired
	}
	return entry, nil
}

// MagicLinkOptions configures the built-in magic-link auth flow.
type MagicLinkOptions struct {
	Path        string
	BaseURL     string
	TTL         time.Duration
	SuccessPath string
	FailurePath string
	FlashKey    string
	Sender      MagicLinkSender
	Store       MagicLinkStore
	Resolver    MagicLinkResolver
	Now         func() time.Time
}

// MagicLinks issues and consumes session-backed magic-link sign-ins.
type MagicLinks struct {
	manager     *Manager
	path        string
	baseURL     string
	ttl         time.Duration
	successPath string
	failurePath string
	flashKey    string
	sender      MagicLinkSender
	store       MagicLinkStore
	resolver    MagicLinkResolver
	now         func() time.Time
}

// NewMagicLinks creates a batteries-included magic-link flow for a manager.
func NewMagicLinks(manager *Manager, opts MagicLinkOptions) *MagicLinks {
	if opts.Path == "" {
		opts.Path = "/auth/magic-link"
	}
	if opts.TTL == 0 {
		opts.TTL = 20 * time.Minute
	}
	if opts.SuccessPath == "" {
		opts.SuccessPath = "/"
	}
	if opts.FailurePath == "" {
		opts.FailurePath = opts.Path
	}
	if opts.FlashKey == "" {
		opts.FlashKey = "auth.magic_link"
	}
	if opts.Store == nil {
		opts.Store = NewMemoryMagicLinkStore()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &MagicLinks{
		manager:     manager,
		path:        opts.Path,
		baseURL:     strings.TrimRight(opts.BaseURL, "/"),
		ttl:         opts.TTL,
		successPath: opts.SuccessPath,
		failurePath: opts.FailurePath,
		flashKey:    opts.FlashKey,
		sender:      opts.Sender,
		store:       opts.Store,
		resolver:    opts.Resolver,
		now:         opts.Now,
	}
}

// MagicLinks creates a magic-link manager bound to the auth manager.
func (m *Manager) MagicLinks(opts MagicLinkOptions) *MagicLinks {
	return NewMagicLinks(m, opts)
}

// Issue creates and stores a magic link without sending it.
func (m *MagicLinks) Issue(r *http.Request, email string, next string) (MagicLinkDelivery, error) {
	if m == nil {
		return MagicLinkDelivery{}, fmt.Errorf("magic links manager is nil")
	}
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return MagicLinkDelivery{}, fmt.Errorf("email is required")
	}

	user, err := m.resolveUser(r.Context(), email)
	if err != nil {
		return MagicLinkDelivery{}, err
	}
	if user.Email == "" {
		user.Email = email
	}
	if user.ID == "" {
		user.ID = email
	}

	token, err := randomMagicLinkToken()
	if err != nil {
		return MagicLinkDelivery{}, err
	}
	expiresAt := m.now().Add(m.ttl)
	record := MagicLinkToken{
		Token:     token,
		Email:     email,
		User:      user,
		Next:      sanitizeRedirectTarget(next),
		ExpiresAt: expiresAt,
	}
	if err := m.store.Save(record); err != nil {
		return MagicLinkDelivery{}, err
	}

	delivery := MagicLinkDelivery{
		Email:     email,
		Token:     token,
		URL:       m.callbackURL(r, token),
		User:      user,
		Next:      record.Next,
		ExpiresAt: expiresAt,
	}
	return delivery, nil
}

// Send issues and delivers a magic link.
func (m *MagicLinks) Send(r *http.Request, email string, next string) (MagicLinkDelivery, error) {
	delivery, err := m.Issue(r, email, next)
	if err != nil {
		return MagicLinkDelivery{}, err
	}
	if m.sender == nil {
		return delivery, nil
	}
	if err := m.sender.SendMagicLink(r.Context(), delivery); err != nil {
		return MagicLinkDelivery{}, err
	}
	return delivery, nil
}

// Consume validates a token and signs the matching user into the bound manager.
func (m *MagicLinks) Consume(r *http.Request, token string) (User, string, error) {
	if m == nil {
		return User{}, "", ErrMagicLinkInvalid
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return User{}, "", ErrMagicLinkInvalid
	}
	record, err := m.store.Consume(token, m.now())
	if err != nil {
		return User{}, "", err
	}
	if m.manager == nil || !m.manager.SignIn(r, record.User) {
		return User{}, "", fmt.Errorf("session middleware required before magic link consume")
	}
	target := sanitizeRedirectTarget(record.Next)
	if target == "" {
		target = m.successPath
	}
	return record.User, target, nil
}

// RequestHandler accepts an email, issues a magic link, and sends it.
func (m *MagicLinks) RequestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email, next, err := readMagicLinkRequest(r)
		if err != nil {
			writeMagicLinkError(w, r, http.StatusBadRequest, err)
			return
		}
		delivery, err := m.Send(r, email, next)
		if err != nil {
			writeMagicLinkError(w, r, http.StatusBadRequest, err)
			return
		}

		if requestWantsJSON(r) {
			payload := map[string]any{
				"ok":        true,
				"email":     delivery.Email,
				"expiresAt": delivery.ExpiresAt,
			}
			if m.sender == nil {
				payload["url"] = delivery.URL
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(payload)
			return
		}

		flash := map[string]any{
			"status": "sent",
			"email":  delivery.Email,
		}
		if m.sender == nil {
			flash["url"] = delivery.URL
		}
		addMagicLinkFlash(r, m.flashKey, flash)
		http.Redirect(w, r, redirectBackTarget(r, m.failurePath), http.StatusSeeOther)
	})
}

// CallbackHandler consumes a token from the query string and signs in the user.
func (m *MagicLinks) CallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, target, err := m.Consume(r, r.URL.Query().Get("token"))
		if err != nil {
			if requestWantsJSON(r) {
				writeMagicLinkError(w, r, http.StatusUnauthorized, err)
				return
			}
			addMagicLinkFlash(r, m.flashKey, map[string]any{
				"status": "error",
				"error":  err.Error(),
			})
			http.Redirect(w, r, m.failurePath, http.StatusSeeOther)
			return
		}

		if requestWantsJSON(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"target": target,
				"user":   user,
			})
			return
		}

		addMagicLinkFlash(r, m.flashKey, map[string]any{
			"status": "signed_in",
			"email":  user.Email,
		})
		http.Redirect(w, r, target, http.StatusSeeOther)
	})
}

func (m *MagicLinks) resolveUser(ctx context.Context, email string) (User, error) {
	if m != nil && m.resolver != nil {
		return m.resolver.ResolveMagicLink(ctx, email)
	}
	return User{
		ID:    email,
		Email: email,
	}, nil
}

func (m *MagicLinks) callbackURL(r *http.Request, token string) string {
	path := m.path + "?token=" + url.QueryEscape(token)
	base := m.baseURL
	if base == "" {
		base = requestOrigin(r)
	}
	if base == "" {
		return path
	}
	return base + path
}

func readMagicLinkRequest(r *http.Request) (string, string, error) {
	if requestWantsJSON(r) {
		var payload struct {
			Email string `json:"email"`
			Next  string `json:"next"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return "", "", fmt.Errorf("invalid magic link request payload")
		}
		return payload.Email, payload.Next, nil
	}
	if err := r.ParseForm(); err != nil {
		return "", "", err
	}
	return r.Form.Get("email"), r.Form.Get("next"), nil
}

func writeMagicLinkError(w http.ResponseWriter, r *http.Request, status int, err error) {
	if requestWantsJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": err.Error(),
		})
		return
	}
	http.Error(w, err.Error(), status)
}

func addMagicLinkFlash(r *http.Request, key string, value any) {
	if store := session.Current(r); store != nil {
		store.AddFlash(key, value)
	}
}

func redirectBackTarget(r *http.Request, fallback string) string {
	if r != nil {
		if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
			return referer
		}
		if r.URL != nil && r.URL.Path != "" {
			return r.URL.Path
		}
	}
	if fallback == "" {
		return "/"
	}
	return fallback
}

func sanitizeRedirectTarget(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "//") {
		return ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		return ""
	}
	return value
}

func requestOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "http"
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func randomMagicLinkToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

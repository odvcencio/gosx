package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/odvcencio/gosx/session"
)

type contextKey string

const userContextKey contextKey = "gosx.auth.user"

// User is the default session-backed identity payload.
type User struct {
	ID    string         `json:"id"`
	Email string         `json:"email,omitempty"`
	Name  string         `json:"name,omitempty"`
	Roles []string       `json:"roles,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

// Options configures a session-backed auth manager.
type Options struct {
	SessionKey string
	LoginPath  string
}

// Manager loads and guards the current user from the session store.
type Manager struct {
	sessions   *session.Manager
	sessionKey string
	loginPath  string
}

// New creates a session-backed auth manager.
func New(sessions *session.Manager, opts Options) *Manager {
	if opts.SessionKey == "" {
		opts.SessionKey = "gosx.user"
	}
	if opts.LoginPath == "" {
		opts.LoginPath = "/login"
	}
	return &Manager{
		sessions:   sessions,
		sessionKey: opts.SessionKey,
		loginPath:  opts.LoginPath,
	}
}

// Middleware resolves the current user once and stores it on the request
// context for downstream handlers and templates.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, ok := m.Current(r); ok {
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Current returns the current authenticated user from request context when
// available.
func Current(r *http.Request) (User, bool) {
	if r == nil {
		return User{}, false
	}
	user, ok := r.Context().Value(userContextKey).(User)
	return user, ok
}

// Current returns the current authenticated user, if present.
func (m *Manager) Current(r *http.Request) (User, bool) {
	if user, ok := Current(r); ok {
		return user, true
	}
	if m == nil || m.sessions == nil {
		return User{}, false
	}
	store := m.sessions.Get(r)
	if store == nil {
		return User{}, false
	}
	var user User
	if !store.Decode(m.sessionKey, &user) {
		return User{}, false
	}
	return user, user.ID != ""
}

// SignIn stores the authenticated user in the session.
func (m *Manager) SignIn(r *http.Request, user User) bool {
	if m == nil || m.sessions == nil {
		return false
	}
	store := m.sessions.Get(r)
	if store == nil {
		return false
	}
	store.Set(m.sessionKey, user)
	return true
}

// SignOut removes the authenticated user from the session.
func (m *Manager) SignOut(r *http.Request) {
	if m == nil || m.sessions == nil {
		return
	}
	store := m.sessions.Get(r)
	if store == nil {
		return
	}
	store.Delete(m.sessionKey)
}

// Require blocks unauthenticated requests.
func (m *Manager) Require(next http.Handler) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := m.Current(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		m.unauthorized(w, r)
	})
}

// RequireRole blocks users who do not have the requested role.
func (m *Manager) RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := m.Current(r)
			if !ok || !hasRole(user, role) {
				m.unauthorized(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *Manager) unauthorized(w http.ResponseWriter, r *http.Request) {
	if requestWantsJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "authentication required",
		})
		return
	}

	target := m.loginPath
	if r != nil && r.URL != nil {
		values := url.Values{}
		values.Set("next", r.URL.RequestURI())
		if strings.Contains(target, "?") {
			target += "&" + values.Encode()
		} else {
			target += "?" + values.Encode()
		}
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func hasRole(user User, role string) bool {
	for _, candidate := range user.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func requestWantsJSON(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := r.Header.Get("Accept")
	contentType := r.Header.Get("Content-Type")
	return strings.Contains(accept, "application/json") || strings.HasPrefix(contentType, "application/json")
}

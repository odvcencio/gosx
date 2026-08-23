package session

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const (
	storeContextKey  contextKey = "gosx.session.store"
	defaultFlashKey             = "__gosx_flash"
	defaultCSRFKey              = "__gosx_csrf"
	defaultCSRFField            = "csrf_token"
	hostCookiePrefix            = "__Host-"
)

const (
	// DefaultMaxAge is the session lifetime that New applies when
	// Options.MaxAge is zero. The manager stamps every cookie with the
	// issuance time and rejects a cookie that is older than this limit.
	DefaultMaxAge = 30 * 24 * time.Hour

	// DefaultLegacyCookieGrace is the window in which the manager still
	// accepts a cookie that carries no issuance timestamp. The window starts
	// when the process creates the manager.
	DefaultLegacyCookieGrace = 30 * 24 * time.Hour

	// MaxCookieSize is the per-cookie byte budget from RFC 6265. Browsers may
	// drop a larger cookie without a report, so the manager refuses to write
	// one.
	MaxCookieSize = 4096

	// clockSkewTolerance accepts a cookie stamped a short time in the future.
	// Two servers behind one load balancer can disagree by a few minutes.
	clockSkewTolerance = 5 * time.Minute
)

var (
	// ErrSessionExpired reports a cookie that is older than the configured
	// max age. The manager drops the session and starts an empty one.
	ErrSessionExpired = errors.New("session cookie expired")

	// ErrSessionTimestampInFuture reports a cookie stamped further ahead than
	// the clock skew tolerance allows.
	ErrSessionTimestampInFuture = errors.New("session cookie issued in the future")

	// ErrSessionTooLarge reports a session that exceeds MaxCookieSize. The
	// manager refuses to write the cookie instead of losing the write later.
	ErrSessionTooLarge = errors.New("session cookie exceeds the browser size limit")

	// ErrInvalidCookie reports session cookie options or serialized state that
	// cannot produce a valid Set-Cookie field. New rejects invalid static
	// options; Commit repeats the validation at its transaction boundary.
	ErrInvalidCookie = errors.New("invalid session cookie")

	// ErrSessionCommitted reports a mutation attempted after the request's
	// session was durably committed. A successful commit is terminal: allowing
	// later changes would recreate the false-success window Commit closes.
	ErrSessionCommitted = errors.New("session is already committed")

	// ErrSessionMiddlewareRequired reports an explicit commit without the
	// request-scoped Store and writer installed by Manager.Middleware.
	ErrSessionMiddlewareRequired = errors.New("session middleware is required")

	// ErrSessionWriterMismatch reports a Store committed through a response
	// writer that does not belong to the same middleware invocation.
	ErrSessionWriterMismatch = errors.New("response writer does not match the session request")

	// ErrSessionCommitRequired reports a dirty session whose connection was
	// about to be hijacked before its cookie was explicitly committed.
	ErrSessionCommitRequired = errors.New("commit the session before hijacking the response")
)

// Options configures a cookie-backed session manager.
type Options struct {
	CookieName string
	Path       string
	Domain     string

	// MaxAge limits both the browser cookie lifetime and the server-side
	// session lifetime. New applies DefaultMaxAge when MaxAge is zero.
	MaxAge time.Duration

	// Secure marks the cookie Secure, so a browser sends it over HTTPS only.
	// New sets Secure to true unless AllowInsecure is true.
	Secure bool

	// AllowInsecure clears the Secure flag for local HTTP development. Set it
	// only when you serve plain HTTP on purpose.
	AllowInsecure bool

	HTTPOnly        bool
	SameSite        http.SameSite
	Encrypt         bool
	PreviousSecrets []string

	// LegacyCookieGrace sets how long the manager accepts a cookie that
	// carries no issuance timestamp. New applies DefaultLegacyCookieGrace
	// when the value is zero. Give a negative value to reject every
	// timestamp-free cookie at once.
	LegacyCookieGrace time.Duration

	// OnError receives cookie write failures, such as an oversized session.
	// The manager logs the failure when OnError is nil.
	OnError func(error)
}

// Manager loads and persists signed cookie sessions.
type Manager struct {
	secret          []byte
	previousSecrets [][]byte
	opts            Options
	now             func() time.Time
	startedAt       time.Time
}

type sessionEnvelope struct {
	Values  map[string]any   `json:"values,omitempty"`
	Flashes map[string][]any `json:"flashes,omitempty"`

	// IssuedAt is the Unix millisecond at which the manager signed the
	// envelope. The value sits inside the signed and encrypted payload, so a
	// client cannot change it. decodeCookie compares it against
	// Options.MaxAge. Zero marks a cookie from a release that did not stamp
	// the envelope.
	IssuedAt int64 `json:"iat_ms,omitempty"`
}

// Store holds request-scoped session state.
type Store struct {
	manager         *Manager
	values          map[string]any
	incomingFlashes map[string][]any
	outgoingFlashes map[string][]any
	dirty           bool
	destroyed       bool
	writeErr        error

	// revision advances on every accepted mutation. Commit failures are
	// cached by revision so an unchanged retry neither re-encodes nor reports
	// the same error twice; changing the state permits a fresh attempt.
	revision        uint64
	attempted       bool
	attemptRevision uint64
	attemptErr      error
	sealed          bool
	terminal        bool
	response        *responseWriter
}

// New creates a new cookie-backed session manager.
func New(secret string, opts Options) (*Manager, error) {
	if len(secret) < 16 {
		return nil, fmt.Errorf("session secret must be at least 16 bytes")
	}
	if opts.CookieName == "" {
		opts.CookieName = "gosx_session"
	}
	if opts.Path == "" {
		opts.Path = "/"
	}
	if opts.MaxAge == 0 {
		opts.MaxAge = DefaultMaxAge
	}
	if opts.MaxAge < 0 {
		return nil, fmt.Errorf("session max age must not be negative")
	}
	if opts.LegacyCookieGrace == 0 {
		opts.LegacyCookieGrace = DefaultLegacyCookieGrace
	}
	// Default to a Secure cookie. A session cookie sent over plain HTTP is
	// readable by any network attacker, so the safe value must be the
	// default. Set AllowInsecure for local HTTP development.
	if !opts.Secure && !opts.AllowInsecure {
		opts.Secure = true
	}
	if opts.HTTPOnly == false {
		opts.HTTPOnly = true
	}
	if opts.SameSite == 0 {
		opts.SameSite = http.SameSiteLaxMode
	}
	if err := validateHostPrefix(opts); err != nil {
		return nil, err
	}
	if err := validateCookieOptions(opts); err != nil {
		return nil, err
	}
	return &Manager{
		secret:          []byte(secret),
		previousSecrets: normalizePreviousSecrets(opts.PreviousSecrets),
		opts:            opts,
		now:             time.Now,
		startedAt:       time.Now(),
	}, nil
}

func validateCookieOptions(opts Options) error {
	cookie := &http.Cookie{
		Name:     opts.CookieName,
		Value:    "validation",
		Path:     opts.Path,
		Domain:   opts.Domain,
		Secure:   opts.Secure,
		HttpOnly: opts.HTTPOnly,
		SameSite: opts.SameSite,
	}
	if err := cookie.Valid(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCookie, err)
	}
	if cookie.String() == "" {
		return fmt.Errorf("%w: cookie options serialize to an empty header", ErrInvalidCookie)
	}
	return nil
}

// validateHostPrefix enforces the browser rules for the __Host- cookie name
// prefix. A browser rejects such a cookie unless it is Secure, has the path
// "/", and carries no Domain attribute.
func validateHostPrefix(opts Options) error {
	if !strings.HasPrefix(opts.CookieName, hostCookiePrefix) {
		return nil
	}
	if !opts.Secure {
		return fmt.Errorf("%s cookie name requires a secure cookie", hostCookiePrefix)
	}
	if opts.Domain != "" {
		return fmt.Errorf("%s cookie name must not set a domain", hostCookiePrefix)
	}
	if opts.Path != "/" {
		return fmt.Errorf("%s cookie name requires the path /", hostCookiePrefix)
	}
	return nil
}

// MustNew creates a new session manager.
//
// Deprecated: use New and handle the returned error. MustNew returns nil when
// configuration is invalid.
func MustNew(secret string, opts Options) *Manager {
	manager, err := New(secret, opts)
	if err != nil {
		return nil
	}
	return manager
}

// Middleware loads the session store and persists changes back to the cookie.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	if m == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "session manager is not configured", http.StatusInternalServerError)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := m.load(r)
		ctx := context.WithValue(r.Context(), storeContextKey, store)
		core := &responseWriter{
			ResponseWriter: w,
			store:          store,
		}
		store.response = core
		next.ServeHTTP(wrapResponseWriter(core), r.WithContext(ctx))
		core.finish()
	})
}

// Protect enforces CSRF validation on unsafe requests.
func (m *Manager) Protect(next http.Handler) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	if m == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "session manager is not configured", http.StatusInternalServerError)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !csrfProtectedMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		// Managed actions own one bounded parser and one authoritative CSRF
		// decision. Only a concrete framework handler that implements the
		// capability below may opt out of this middleware's body parser. A path
		// prefix alone is never sufficient: an app-authored handler mounted at
		// /gosx/action/ remains protected.
		if isRegisteredManagedAction(next, r) {
			next.ServeHTTP(w, r)
			return
		}
		store := m.Get(r)
		if store == nil {
			http.Error(w, "session middleware required before csrf protection", http.StatusInternalServerError)
			return
		}
		expected := store.ensureCSRFToken()
		actual := r.Header.Get("X-CSRF-Token")
		if actual == "" && !requestWantsJSON(r) {
			// FormValue parses multipart/form-data (via ParseMultipartForm)
			// as well as urlencoded bodies and query params, so it reads the
			// csrf_token whether the form was submitted as multipart (e.g. the
			// studio workbench's FormData fetch) or urlencoded. The parsed form
			// is cached on the request, so downstream handlers reusing it are
			// unaffected.
			actual = r.FormValue(defaultCSRFField)
		}
		if !constantTimeSessionStringEqual(expected, actual) {
			writeCSRFFailure(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isRegisteredManagedAction(handler http.Handler, req *http.Request) bool {
	// A capability is an authorization-bearing signal.  Only a wrapper that
	// explicitly opts into preserving it may be traversed; an arbitrary
	// Unwrap method is not evidence that the wrapper delegates every request.
	for depth := 0; depth < 32 && handler != nil; depth++ {
		preserver, ok := handler.(interface {
			PreservesManagedActionCapability() bool
		})
		if !ok || !preserver.PreservesManagedActionCapability() {
			return false
		}
		if capability, ok := handler.(interface {
			IsManagedActionRequest(*http.Request) bool
		}); ok && capability.IsManagedActionRequest(req) {
			return true
		}
		unwrapper, ok := handler.(interface{ Unwrap() http.Handler })
		if !ok {
			return false
		}
		inner := unwrapper.Unwrap()
		if inner == nil {
			return false
		}
		handler = inner
	}
	return false
}

// Get returns the request-scoped store for the manager.
func (m *Manager) Get(r *http.Request) *Store {
	if m == nil {
		return nil
	}
	store := Current(r)
	if store == nil || store.manager != m {
		return nil
	}
	return store
}

// Token returns the request CSRF token, generating one if needed.
func (m *Manager) Token(r *http.Request) string {
	store := m.Get(r)
	if store == nil {
		return ""
	}
	return store.ensureCSRFToken()
}

// Current returns the request-scoped session store loaded by Middleware.
func Current(r *http.Request) *Store {
	if r == nil {
		return nil
	}
	store, _ := r.Context().Value(storeContextKey).(*Store)
	return store
}

// Commit synchronously persists and seals the current request's session.
// Call it after the final session mutation and before writing a status or body
// whenever the response claims persistence-dependent success.
func Commit(w http.ResponseWriter, r *http.Request) error {
	store := Current(r)
	if store == nil {
		return ErrSessionMiddlewareRequired
	}
	return store.Commit(w)
}

// Token returns the request CSRF token from the current session store.
func Token(r *http.Request) string {
	store := Current(r)
	if store == nil {
		return ""
	}
	return store.ensureCSRFToken()
}

// Values returns a shallow copy of the current session values.
func Values(r *http.Request) map[string]any {
	store := Current(r)
	if store == nil {
		return map[string]any{}
	}
	return store.Values()
}

// FlashValues returns the flashes loaded for the current request.
func FlashValues(r *http.Request) map[string][]any {
	store := Current(r)
	if store == nil {
		return map[string][]any{}
	}
	return store.AllFlashes()
}

// AddFlash appends a flash value that will be available on the next request.
func AddFlash(r *http.Request, key string, value any) error {
	store := Current(r)
	if store == nil {
		return ErrSessionMiddlewareRequired
	}
	return store.AddFlash(key, value)
}

// Destroy marks the current request session for deletion.
func Destroy(r *http.Request) error {
	store := Current(r)
	if store == nil {
		return ErrSessionMiddlewareRequired
	}
	return store.Destroy()
}

// Value returns a session value by key.
func (s *Store) Value(key string) any {
	if s == nil {
		return nil
	}
	return s.values[key]
}

// String returns a string session value by key.
func (s *Store) String(key string) string {
	if s == nil {
		return ""
	}
	value, _ := s.values[key].(string)
	return value
}

// Decode unmarshals a stored session value into dst.
//
// It reports whether dst was populated. False covers every failure without
// separating them: a nil store, a nil dst, a key that was never set, a value
// that will not marshal, and a value whose shape does not fit dst. A caller
// that needs to tell "absent" from "present but wrong shape" must ask through
// another method first; this one collapses both to false.
//
// The round trip goes through JSON, so dst receives what json.Unmarshal would
// produce and not the stored Go value. A value stored as a struct and decoded
// into a map arrives as a map, and unexported fields do not survive.
//
// On a shape mismatch dst may already hold partially decoded fields, because
// json.Unmarshal writes as it goes. Treat dst as undefined when this returns
// false.
func (s *Store) Decode(key string, dst any) bool {
	if s == nil || dst == nil {
		return false
	}
	value, ok := s.values[key]
	if !ok {
		return false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, dst) == nil
}

// Commit synchronously persists and seals this request's session. The supplied
// writer must be the writer from the same Manager.Middleware invocation, or a
// wrapper whose Unwrap chain reaches it. Commit never writes a response status.
func (s *Store) Commit(w http.ResponseWriter) error {
	if s == nil {
		return ErrSessionMiddlewareRequired
	}
	if !writerContains(w, s.response) {
		return ErrSessionWriterMismatch
	}
	return s.attemptCommit(w)
}

func (s *Store) beginMutation() error {
	if s == nil {
		return ErrSessionMiddlewareRequired
	}
	if s.sealed || s.terminal {
		return ErrSessionCommitted
	}
	s.revision++
	return nil
}

// Set stores a session value.
func (s *Store) Set(key string, value any) error {
	if err := s.beginMutation(); err != nil {
		return err
	}
	if s.values == nil {
		s.values = make(map[string]any)
	}
	s.values[key] = value
	s.dirty = true
	return nil
}

// Delete removes a session value.
func (s *Store) Delete(key string) error {
	if err := s.beginMutation(); err != nil {
		return err
	}
	if s.values != nil {
		delete(s.values, key)
	}
	s.dirty = true
	return nil
}

// Values returns a shallow copy of the store values.
func (s *Store) Values() map[string]any {
	if s == nil || len(s.values) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(s.values))
	for key, value := range s.values {
		out[key] = value
	}
	return out
}

// AddFlash appends a flash value for the next request.
func (s *Store) AddFlash(key string, value any) error {
	if err := s.beginMutation(); err != nil {
		return err
	}
	if key == "" {
		key = defaultFlashKey
	}
	if s.outgoingFlashes == nil {
		s.outgoingFlashes = make(map[string][]any)
	}
	s.outgoingFlashes[key] = append(s.outgoingFlashes[key], value)
	s.dirty = true
	return nil
}

// Flashes returns the flash values loaded for this request.
func (s *Store) Flashes(key string) []any {
	if s == nil {
		return nil
	}
	values := s.incomingFlashes[key]
	if len(values) == 0 {
		return nil
	}
	out := make([]any, len(values))
	copy(out, values)
	return out
}

// AllFlashes returns all flash values loaded for this request.
func (s *Store) AllFlashes() map[string][]any {
	if s == nil || len(s.incomingFlashes) == 0 {
		return map[string][]any{}
	}
	out := make(map[string][]any, len(s.incomingFlashes))
	for key, values := range s.incomingFlashes {
		cp := make([]any, len(values))
		copy(cp, values)
		out[key] = cp
	}
	return out
}

// Destroy deletes the current session cookie.
func (s *Store) Destroy() error {
	if err := s.beginMutation(); err != nil {
		return err
	}
	s.values = map[string]any{}
	s.incomingFlashes = map[string][]any{}
	s.outgoingFlashes = map[string][]any{}
	s.dirty = true
	s.destroyed = true
	return nil
}

func (s *Store) ensureCSRFToken() string {
	if s == nil {
		return ""
	}
	if token, ok := s.values[defaultCSRFKey].(string); ok && token != "" {
		return token
	}
	token := randomToken(32)
	if err := s.Set(defaultCSRFKey, token); err != nil {
		return ""
	}
	return token
}

func (m *Manager) load(r *http.Request) *Store {
	store := &Store{
		manager:         m,
		values:          make(map[string]any),
		incomingFlashes: make(map[string][]any),
		outgoingFlashes: make(map[string][]any),
	}

	if r == nil {
		return store
	}
	cookie, err := r.Cookie(m.opts.CookieName)
	if err != nil || cookie.Value == "" {
		return store
	}

	envelope, refresh, err := m.decodeCookie(cookie.Value)
	if err != nil {
		return store
	}
	store.values = envelope.Values
	if store.values == nil {
		store.values = make(map[string]any)
	}
	store.incomingFlashes = envelope.Flashes
	if store.incomingFlashes == nil {
		store.incomingFlashes = make(map[string][]any)
	}
	if len(store.incomingFlashes) > 0 {
		store.dirty = true
		store.revision++
	}
	if refresh {
		if !store.dirty {
			store.revision++
		}
		store.dirty = true
	}
	return store
}

func (m *Manager) clock() time.Time {
	if m == nil || m.now == nil {
		return time.Now()
	}
	return m.now()
}

func (m *Manager) encode(store *Store) (string, error) {
	envelope := sessionEnvelope{
		Values:   store.values,
		Flashes:  store.outgoingFlashes,
		IssuedAt: m.clock().UnixMilli(),
	}
	var payload bytes.Buffer
	encoder := json.NewEncoder(&payload)
	// Session payloads are signed (and optionally encrypted), not embedded in
	// HTML. Avoid JSON's default \u00xx expansion for <, >, and &: it consumes
	// scarce cookie bytes without adding protection at this transport layer.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(envelope); err != nil {
		return "", err
	}
	encodedPayload := bytes.TrimSuffix(payload.Bytes(), []byte{'\n'})
	if m.opts.Encrypt {
		encrypted, err := encryptSessionPayload(m.secret, encodedPayload)
		if err != nil {
			return "", err
		}
		body := base64.RawURLEncoding.EncodeToString(encrypted)
		signature := sessionSignature(m.secret, []byte("v2."+body))
		return "v2." + body + "." + base64.RawURLEncoding.EncodeToString(signature), nil
	}
	body := base64.RawURLEncoding.EncodeToString(encodedPayload)
	signature := sessionSignature(m.secret, encodedPayload)
	return body + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (m *Manager) decode(value string) (sessionEnvelope, error) {
	envelope, _, err := m.decodeCookie(value)
	return envelope, err
}

func (m *Manager) decodeCookie(value string) (sessionEnvelope, bool, error) {
	parts := strings.Split(value, ".")
	var (
		envelope sessionEnvelope
		refresh  bool
		err      error
	)
	switch {
	case len(parts) == 2:
		envelope, refresh, err = m.decodeLegacyCookie(parts[0], parts[1])
	case len(parts) == 3 && parts[0] == "v2":
		envelope, refresh, err = m.decodeEncryptedCookie(parts[1], parts[2])
	default:
		return sessionEnvelope{}, false, fmt.Errorf("invalid session cookie format")
	}
	if err != nil {
		return sessionEnvelope{}, false, err
	}
	// Both cookie formats carry the issuance timestamp in the signed payload,
	// so one age check covers the legacy 2-part cookie and the v2 cookie.
	ageRefresh, err := m.checkEnvelopeAge(envelope)
	if err != nil {
		return sessionEnvelope{}, false, err
	}
	return envelope, refresh || ageRefresh, nil
}

// checkEnvelopeAge enforces the server-side session lifetime. It reports
// whether the caller must write a fresh cookie.
//
// A cookie without a timestamp comes from a GoSX release that did not stamp
// the envelope. The manager accepts such a cookie inside
// Options.LegacyCookieGrace and re-issues it with a timestamp, so an upgrade
// does not sign every user out. Trade-off: a captured timestamp-free cookie
// still replays inside that window, and the window restarts with the process.
// Set Options.LegacyCookieGrace to a negative value after the rollout to
// reject every timestamp-free cookie.
func (m *Manager) checkEnvelopeAge(envelope sessionEnvelope) (bool, error) {
	now := m.clock()
	maxAge := m.opts.MaxAge
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}
	if envelope.IssuedAt == 0 {
		grace := m.opts.LegacyCookieGrace
		if grace < 0 {
			return false, ErrSessionExpired
		}
		if now.After(m.startedAt.Add(grace)) {
			return false, ErrSessionExpired
		}
		return true, nil
	}
	issuedAt := time.UnixMilli(envelope.IssuedAt)
	if issuedAt.After(now.Add(clockSkewTolerance)) {
		return false, ErrSessionTimestampInFuture
	}
	age := now.Sub(issuedAt)
	if age > maxAge {
		return false, ErrSessionExpired
	}
	// Renew an active session once it passes half of its lifetime. An idle
	// session still expires at max age.
	if age > maxAge/2 {
		return true, nil
	}
	return false, nil
}

func (m *Manager) decodeLegacyCookie(payloadPart, signaturePart string) (sessionEnvelope, bool, error) {
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return sessionEnvelope{}, false, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil {
		return sessionEnvelope{}, false, err
	}
	keyIndex, ok := m.matchingSecret(payload, signature)
	if !ok {
		return sessionEnvelope{}, false, fmt.Errorf("invalid session signature")
	}
	envelope, err := decodeSessionEnvelope(payload)
	return envelope, keyIndex != 0 || m.opts.Encrypt, err
}

func (m *Manager) decodeEncryptedCookie(bodyPart, signaturePart string) (sessionEnvelope, bool, error) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(bodyPart)
	if err != nil {
		return sessionEnvelope{}, false, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(signaturePart)
	if err != nil {
		return sessionEnvelope{}, false, err
	}
	signed := []byte("v2." + bodyPart)
	keyIndex, ok := m.matchingSecret(signed, signature)
	if !ok {
		return sessionEnvelope{}, false, fmt.Errorf("invalid session signature")
	}
	payload, err := decryptSessionPayload(m.secretAt(keyIndex), ciphertext)
	if err != nil {
		return sessionEnvelope{}, false, err
	}
	envelope, err := decodeSessionEnvelope(payload)
	return envelope, keyIndex != 0, err
}

func decodeSessionEnvelope(payload []byte) (sessionEnvelope, error) {
	var envelope sessionEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return sessionEnvelope{}, err
	}
	return envelope, nil
}

func (m *Manager) matchingSecret(message []byte, signature []byte) (int, bool) {
	for i := 0; i < 1+len(m.previousSecrets); i++ {
		if subtle.ConstantTimeCompare(sessionSignature(m.secretAt(i), message), signature) == 1 {
			return i, true
		}
	}
	return 0, false
}

func (m *Manager) secretAt(index int) []byte {
	if index == 0 {
		return m.secret
	}
	return m.previousSecrets[index-1]
}

func sessionSignature(secret []byte, message []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(message)
	return mac.Sum(nil)
}

func constantTimeSessionStringEqual(a, b string) bool {
	aHash := sha256.Sum256([]byte(a))
	bHash := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aHash[:], bHash[:]) == 1 && len(a) == len(b)
}

func encryptSessionPayload(secret []byte, payload []byte) ([]byte, error) {
	block, err := aes.NewCipher(sessionEncryptionKey(secret))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(nonce)+len(payload)+aead.Overhead())
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, payload, nil)
	return out, nil
}

func decryptSessionPayload(secret []byte, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(sessionEncryptionKey(secret))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, fmt.Errorf("encrypted session payload is too short")
	}
	nonce := ciphertext[:aead.NonceSize()]
	body := ciphertext[aead.NonceSize():]
	return aead.Open(nil, nonce, body, nil)
}

func sessionEncryptionKey(secret []byte) []byte {
	sum := sha256.Sum256(secret)
	return sum[:]
}

func normalizePreviousSecrets(values []string) [][]byte {
	if len(values) == 0 {
		return nil
	}
	out := make([][]byte, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if len(value) < 16 {
			continue
		}
		secret := []byte(value)
		if len(out) == 0 || !bytes.Equal(out[len(out)-1], secret) {
			out = append(out, secret)
		}
	}
	return out
}

func (m *Manager) writeCookie(w http.ResponseWriter, store *Store) error {
	if w == nil || store == nil {
		return nil
	}
	header, err := m.cookieHeader(store)
	if err != nil {
		return err
	}
	w.Header().Add("Set-Cookie", header)
	return nil
}

// cookieHeader builds and validates the complete Set-Cookie value without
// touching the response. This is the transaction's preflight boundary: every
// failure remains recoverable until the caller adds the returned header.
func (m *Manager) cookieHeader(store *Store) (string, error) {
	if m == nil || store == nil {
		return "", ErrSessionMiddlewareRequired
	}
	var cookie *http.Cookie
	if store.destroyed || sessionEmpty(store) {
		cookie = &http.Cookie{
			Name:     m.opts.CookieName,
			Value:    "",
			Path:     m.opts.Path,
			Domain:   m.opts.Domain,
			MaxAge:   -1,
			Expires:  time.Unix(0, 0),
			Secure:   m.opts.Secure,
			HttpOnly: m.opts.HTTPOnly,
			SameSite: m.opts.SameSite,
		}
	} else {
		encoded, err := m.encode(store)
		if err != nil {
			return "", err
		}
		cookie = &http.Cookie{
			Name:     m.opts.CookieName,
			Value:    encoded,
			Path:     m.opts.Path,
			Domain:   m.opts.Domain,
			MaxAge:   int(m.opts.MaxAge / time.Second),
			Expires:  m.clock().Add(m.opts.MaxAge),
			Secure:   m.opts.Secure,
			HttpOnly: m.opts.HTTPOnly,
			SameSite: m.opts.SameSite,
		}
	}
	if err := cookie.Valid(); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidCookie, err)
	}
	header := cookie.String()
	if header == "" {
		return "", fmt.Errorf("%w: cookie serialized to an empty header", ErrInvalidCookie)
	}
	// Report an oversized cookie. A browser drops a cookie above the RFC 6265
	// budget without any signal, so the session would vanish at random.
	if size := len(header); size > MaxCookieSize {
		return "", fmt.Errorf("%w: %d of %d bytes", ErrSessionTooLarge, size, MaxCookieSize)
	}
	return header, nil
}

func (m *Manager) reportError(err error) {
	if m == nil || err == nil {
		return
	}
	if m.opts.OnError != nil {
		m.opts.OnError(err)
		return
	}
	log.Printf("[gosx] session: %v", err)
}

// Err returns the last cookie write failure for this request, such as an
// oversized session. It returns nil when the write succeeded.
func (s *Store) Err() error {
	if s == nil {
		return nil
	}
	return s.writeErr
}

func sessionEmpty(store *Store) bool {
	if store == nil {
		return true
	}
	return len(store.values) == 0 && len(store.outgoingFlashes) == 0
}

type responseWriter struct {
	http.ResponseWriter
	store       *Store
	wroteHeader bool
	failed      bool
}

func (w *responseWriter) WriteHeader(status int) {
	if status < 100 || status > 999 {
		panic(fmt.Sprintf("invalid WriteHeader code %d", status))
	}
	if w.wroteHeader || w.failed {
		return
	}
	// Informational responses do not end the handler's opportunity to mutate
	// or commit its session. Status 101 switches protocols and is final.
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		// An earlier explicit Commit staged the session cookie. Go's server
		// serializes current headers with each informational response, so hide
		// every cookie for the 1xx write and restore it for the final response.
		header := w.ResponseWriter.Header()
		cookies := append([]string(nil), header.Values("Set-Cookie")...)
		header.Del("Set-Cookie")
		w.ResponseWriter.WriteHeader(status)
		for _, cookie := range cookies {
			header.Add("Set-Cookie", cookie)
		}
		return
	}
	if err := w.store.attemptCommit(w.ResponseWriter); err != nil {
		w.failSessionResponse()
		return
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if w.failed {
		return len(data), nil
	}
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.failed {
		return len(data), nil
	}
	return w.ResponseWriter.Write(data)
}

func (w *responseWriter) flushError() error {
	if !w.wroteHeader && !w.failed {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(interface{ FlushError() error }); ok {
		return flusher.FlushError()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
		return nil
	}
	return http.ErrNotSupported
}

func (w *responseWriter) hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	if w.store != nil && !w.store.sealed {
		if w.store.dirty {
			return nil, nil, ErrSessionCommitRequired
		}
	}
	conn, readWriter, err := hijacker.Hijack()
	if err == nil && w.store != nil {
		// A clean Store has no cookie work to lose. Seal only after the
		// underlying hijack succeeds; a failed hijack leaves normal HTTP open.
		w.store.sealed = true
	}
	return conn, readWriter, err
}

func (w *responseWriter) push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		// A pushed request has its own response. Pushing must not prematurely
		// commit or seal the parent request's session.
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseWriter) sessionCore() *responseWriter { return w }

// wrapResponseWriter exposes only response capabilities the underlying writer
// can actually perform. Flush capability includes the newer FlushError method;
// the wrapper exposes both forms so handlers and http.ResponseController share
// the same session preflight.
func wrapResponseWriter(core *responseWriter) http.ResponseWriter {
	_, flush := core.ResponseWriter.(http.Flusher)
	if !flush {
		_, flush = core.ResponseWriter.(interface{ FlushError() error })
	}
	_, hijack := core.ResponseWriter.(http.Hijacker)
	_, push := core.ResponseWriter.(http.Pusher)

	switch {
	case flush && hijack && push:
		return &flushHijackPushResponseWriter{responseWriter: core}
	case flush && hijack:
		return &flushHijackResponseWriter{responseWriter: core}
	case flush && push:
		return &flushPushResponseWriter{responseWriter: core}
	case hijack && push:
		return &hijackPushResponseWriter{responseWriter: core}
	case flush:
		return &flushResponseWriter{responseWriter: core}
	case hijack:
		return &hijackResponseWriter{responseWriter: core}
	case push:
		return &pushResponseWriter{responseWriter: core}
	default:
		return core
	}
}

type flushResponseWriter struct{ *responseWriter }

func (w *flushResponseWriter) Flush()            { _ = w.flushError() }
func (w *flushResponseWriter) FlushError() error { return w.flushError() }

type hijackResponseWriter struct{ *responseWriter }

func (w *hijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}

type pushResponseWriter struct{ *responseWriter }

func (w *pushResponseWriter) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

type flushHijackResponseWriter struct{ *responseWriter }

func (w *flushHijackResponseWriter) Flush()            { _ = w.flushError() }
func (w *flushHijackResponseWriter) FlushError() error { return w.flushError() }
func (w *flushHijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}

type flushPushResponseWriter struct{ *responseWriter }

func (w *flushPushResponseWriter) Flush()            { _ = w.flushError() }
func (w *flushPushResponseWriter) FlushError() error { return w.flushError() }
func (w *flushPushResponseWriter) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

type hijackPushResponseWriter struct{ *responseWriter }

func (w *hijackPushResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}
func (w *hijackPushResponseWriter) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

type flushHijackPushResponseWriter struct{ *responseWriter }

func (w *flushHijackPushResponseWriter) Flush()            { _ = w.flushError() }
func (w *flushHijackPushResponseWriter) FlushError() error { return w.flushError() }
func (w *flushHijackPushResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.hijack()
}
func (w *flushHijackPushResponseWriter) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

func (w *responseWriter) finish() {
	if w == nil || w.wroteHeader || w.failed {
		return
	}
	if err := w.store.attemptCommit(w.ResponseWriter); err != nil {
		w.failSessionResponse()
	}
}

const sessionFailureBody = "Internal Server Error\n"

func (w *responseWriter) failSessionResponse() {
	if w == nil || w.wroteHeader || w.failed {
		return
	}
	w.failed = true
	if w.store != nil {
		w.store.terminal = true
	}
	header := w.ResponseWriter.Header()
	header.Del("Location")
	header.Del("Content-Length")
	header.Del("Content-Encoding")
	header.Del("Content-Range")
	header.Del("Accept-Ranges")
	header.Del("ETag")
	header.Del("Last-Modified")
	header.Del("Trailer")
	header.Del("Set-Cookie")
	header.Set("Content-Type", "text/plain; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	w.ResponseWriter.WriteHeader(http.StatusInternalServerError)
	_, _ = io.WriteString(w.ResponseWriter, sessionFailureBody)
}

func (s *Store) attemptCommit(destination http.ResponseWriter) error {
	if s == nil || s.manager == nil || s.response == nil {
		return ErrSessionMiddlewareRequired
	}
	if s.sealed {
		return nil
	}
	if s.terminal {
		if s.attemptErr != nil {
			return s.attemptErr
		}
		return ErrSessionCommitted
	}
	if s.attempted && s.attemptRevision == s.revision {
		return s.attemptErr
	}
	s.attempted = true
	s.attemptRevision = s.revision

	if !s.dirty {
		s.attemptErr = nil
		s.writeErr = nil
		s.sealed = true
		return nil
	}
	if err := s.manager.writeCookie(destination, s); err != nil {
		s.attemptErr = err
		s.writeErr = err
		s.manager.reportError(err)
		return err
	}

	s.dirty = false
	s.attemptErr = nil
	s.writeErr = nil
	s.sealed = true
	return nil
}

func writerContains(w http.ResponseWriter, target *responseWriter) bool {
	if w == nil || target == nil {
		return false
	}
	current := w
	for range 64 {
		if carrier, ok := current.(interface{ sessionCore() *responseWriter }); ok && carrier.sessionCore() == target {
			return true
		}
		unwrapper, ok := current.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return false
		}
		current = unwrapper.Unwrap()
		if current == nil {
			return false
		}
	}
	return false
}

func csrfProtectedMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func requestWantsJSON(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := r.Header.Get("Accept")
	contentType := r.Header.Get("Content-Type")
	return strings.Contains(accept, "application/json") || strings.HasPrefix(contentType, "application/json")
}

func writeCSRFFailure(w http.ResponseWriter, r *http.Request) {
	if requestWantsJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "invalid csrf token",
		})
		return
	}
	http.Error(w, "invalid csrf token", http.StatusForbidden)
}

func randomToken(size int) string {
	buf := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		panic(fmt.Sprintf("session: crypto/rand failed while generating token: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

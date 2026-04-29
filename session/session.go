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
	"fmt"
	"io"
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
)

// Options configures a cookie-backed session manager.
type Options struct {
	CookieName      string
	Path            string
	Domain          string
	MaxAge          time.Duration
	Secure          bool
	HTTPOnly        bool
	SameSite        http.SameSite
	Encrypt         bool
	PreviousSecrets []string
}

// Manager loads and persists signed cookie sessions.
type Manager struct {
	secret          []byte
	previousSecrets [][]byte
	opts            Options
}

type sessionEnvelope struct {
	Values  map[string]any   `json:"values,omitempty"`
	Flashes map[string][]any `json:"flashes,omitempty"`
}

// Store holds request-scoped session state.
type Store struct {
	manager         *Manager
	values          map[string]any
	incomingFlashes map[string][]any
	outgoingFlashes map[string][]any
	dirty           bool
	destroyed       bool
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
		opts.MaxAge = 30 * 24 * time.Hour
	}
	if opts.HTTPOnly == false {
		opts.HTTPOnly = true
	}
	if opts.SameSite == 0 {
		opts.SameSite = http.SameSiteLaxMode
	}
	return &Manager{
		secret:          []byte(secret),
		previousSecrets: normalizePreviousSecrets(opts.PreviousSecrets),
		opts:            opts,
	}, nil
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
		writer := &responseWriter{
			ResponseWriter: w,
			store:          store,
		}
		next.ServeHTTP(writer, r.WithContext(ctx))
		writer.commitCookie()
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
		store := m.Get(r)
		if store == nil {
			http.Error(w, "session middleware required before csrf protection", http.StatusInternalServerError)
			return
		}
		expected := store.ensureCSRFToken()
		actual := r.Header.Get("X-CSRF-Token")
		if actual == "" && !requestWantsJSON(r) {
			_ = r.ParseForm()
			actual = r.Form.Get(defaultCSRFField)
		}
		if !constantTimeSessionStringEqual(expected, actual) {
			writeCSRFFailure(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
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
func AddFlash(r *http.Request, key string, value any) bool {
	store := Current(r)
	if store == nil {
		return false
	}
	store.AddFlash(key, value)
	return true
}

// Destroy marks the current request session for deletion.
func Destroy(r *http.Request) {
	store := Current(r)
	if store == nil {
		return
	}
	store.Destroy()
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

// Set stores a session value.
func (s *Store) Set(key string, value any) {
	if s == nil {
		return
	}
	if s.values == nil {
		s.values = make(map[string]any)
	}
	s.values[key] = value
	s.dirty = true
}

// Delete removes a session value.
func (s *Store) Delete(key string) {
	if s == nil || s.values == nil {
		return
	}
	delete(s.values, key)
	s.dirty = true
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
func (s *Store) AddFlash(key string, value any) {
	if s == nil {
		return
	}
	if key == "" {
		key = defaultFlashKey
	}
	if s.outgoingFlashes == nil {
		s.outgoingFlashes = make(map[string][]any)
	}
	s.outgoingFlashes[key] = append(s.outgoingFlashes[key], value)
	s.dirty = true
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
func (s *Store) Destroy() {
	if s == nil {
		return
	}
	s.values = map[string]any{}
	s.incomingFlashes = map[string][]any{}
	s.outgoingFlashes = map[string][]any{}
	s.dirty = true
	s.destroyed = true
}

func (s *Store) ensureCSRFToken() string {
	if s == nil {
		return ""
	}
	if token, ok := s.values[defaultCSRFKey].(string); ok && token != "" {
		return token
	}
	token := randomToken(32)
	s.Set(defaultCSRFKey, token)
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
	}
	if refresh {
		store.dirty = true
	}
	return store
}

func (m *Manager) encode(store *Store) (string, error) {
	envelope := sessionEnvelope{
		Values:  store.values,
		Flashes: store.outgoingFlashes,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	if m.opts.Encrypt {
		encrypted, err := encryptSessionPayload(m.secret, payload)
		if err != nil {
			return "", err
		}
		body := base64.RawURLEncoding.EncodeToString(encrypted)
		signature := sessionSignature(m.secret, []byte("v2."+body))
		return "v2." + body + "." + base64.RawURLEncoding.EncodeToString(signature), nil
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	signature := sessionSignature(m.secret, payload)
	return body + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (m *Manager) decode(value string) (sessionEnvelope, error) {
	envelope, _, err := m.decodeCookie(value)
	return envelope, err
}

func (m *Manager) decodeCookie(value string) (sessionEnvelope, bool, error) {
	parts := strings.Split(value, ".")
	switch {
	case len(parts) == 2:
		return m.decodeLegacyCookie(parts[0], parts[1])
	case len(parts) == 3 && parts[0] == "v2":
		return m.decodeEncryptedCookie(parts[1], parts[2])
	default:
		return sessionEnvelope{}, false, fmt.Errorf("invalid session cookie format")
	}
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

func (m *Manager) writeCookie(w http.ResponseWriter, store *Store) {
	if w == nil || store == nil {
		return
	}
	if store.destroyed || sessionEmpty(store) {
		http.SetCookie(w, &http.Cookie{
			Name:     m.opts.CookieName,
			Value:    "",
			Path:     m.opts.Path,
			Domain:   m.opts.Domain,
			MaxAge:   -1,
			Expires:  time.Unix(0, 0),
			Secure:   m.opts.Secure,
			HttpOnly: m.opts.HTTPOnly,
			SameSite: m.opts.SameSite,
		})
		return
	}
	encoded, err := m.encode(store)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     m.opts.CookieName,
		Value:    encoded,
		Path:     m.opts.Path,
		Domain:   m.opts.Domain,
		MaxAge:   int(m.opts.MaxAge / time.Second),
		Expires:  time.Now().Add(m.opts.MaxAge),
		Secure:   m.opts.Secure,
		HttpOnly: m.opts.HTTPOnly,
		SameSite: m.opts.SameSite,
	})
}

func sessionEmpty(store *Store) bool {
	if store == nil {
		return true
	}
	return len(store.values) == 0 && len(store.outgoingFlashes) == 0
}

type responseWriter struct {
	http.ResponseWriter
	store     *Store
	committed bool
}

func (w *responseWriter) WriteHeader(status int) {
	w.commitCookie()
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *responseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		if !w.committed {
			w.WriteHeader(http.StatusOK)
		}
		flusher.Flush()
	}
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *responseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		w.commitCookie()
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseWriter) commitCookie() {
	if w.committed {
		return
	}
	w.committed = true
	if w.store == nil || !w.store.dirty {
		return
	}
	w.store.manager.writeCookie(w.ResponseWriter, w.store)
	w.store.dirty = false
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

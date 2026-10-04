package session

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type csrfCountingBody struct{ remaining, read int }

func (b *csrfCountingBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), b.remaining)
	for i := range p[:n] {
		p[i] = 'x'
	}
	b.remaining -= n
	b.read += n
	return n, nil
}
func (*csrfCountingBody) Close() error { return nil }

func TestProtectBoundsTokenReadsAndLeavesMultipartToHandler(t *testing.T) {
	m := MustNew("csrf-regression-secret", Options{})
	var cookie *http.Cookie
	var token string
	init := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { Current(r).Set("active", true); token = Token(r) }))
	w := httptest.NewRecorder()
	init.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	cookie = w.Result().Cookies()[0]
	for _, tc := range []struct {
		name, contentType, header string
		status, maxRead           int
		cookie                    bool
	}{
		{"multipart field", "multipart/form-data; boundary=test", "", 403, 0, true},
		{"anonymous", "application/x-www-form-urlencoded", "", 403, 0, false},
		{"bounded form", "application/x-www-form-urlencoded", "", 413, (1 << 20) + 1, true},
		{"multipart header", "multipart/form-data; boundary=test", token, 413, 129, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &csrfCountingBody{remaining: 5 << 20}
			r := httptest.NewRequest("POST", "/", b)
			r.Header.Set("Content-Type", tc.contentType)
			r.Header.Set("X-CSRF-Token", tc.header)
			if tc.cookie {
				r.AddCookie(cookie)
			}
			called := false
			h := m.Middleware(m.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.MultipartForm != nil {
					t.Fatal("middleware parsed multipart body")
				}
				_, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128))
				if err == nil {
					t.Fatal("downstream limit bypassed")
				}
				w.WriteHeader(413)
			})))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || b.read > tc.maxRead {
				t.Fatalf("status=%d bytes=%d", w.Code, b.read)
			}
			if called != (tc.header != "") {
				t.Fatalf("handler called=%v", called)
			}
		})
	}
}

func TestMaskedCSRFTokensVaryByResponseAndAcceptLegacy(t *testing.T) {
	m := MustNew("csrf-regression-secret", Options{})
	var raw, first, second string
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Current(r).Set("active", true)
		token := Token(r)
		if token != Token(r) {
			t.Fatal("token changed within response")
		}
		raw = Current(r).String(defaultCSRFKey)
		_, _ = io.WriteString(w, token)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	first = w.Body.String()
	cookie := w.Result().Cookies()[0]
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	second = w.Body.String()
	if first == second || first == raw || unmaskCSRFToken(first) != raw || unmaskCSRFToken(second) != raw {
		t.Fatal("incorrect response masking")
	}
	protected := m.Middleware(m.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })))
	for _, token := range []string{raw, first, second, "masked:invalid", maskCSRFToken(strings.Repeat("z", 43))} {
		r := httptest.NewRequest("POST", "/", nil)
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", token)
		w := httptest.NewRecorder()
		protected.ServeHTTP(w, r)
		want := 204
		if token != raw && token != first && token != second {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("token status=%d want=%d", w.Code, want)
		}
	}
}

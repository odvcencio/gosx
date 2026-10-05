package session

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProtectNativeMultipartPreservesUploadAndDownstreamLimit(t *testing.T) {
	m := MustNew("multipart-regression-secret", Options{MaxCSRFBodyBytes: 2 << 20})
	var token string
	w := httptest.NewRecorder()
	m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { Current(r).Set("active", true); token = Token(r) })).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	cookie := w.Result().Cookies()[0]
	for _, tc := range []struct {
		name             string
		size, downstream int
		parsed           bool
		token            string
		want             int
	}{
		{"native file", 1200000, 2 << 20, false, token, 204},
		{"already parsed file", 1024, 8192, true, token, 204},
		{"form parsed without multipart", 1024, 8192, false, token, 204},
		{"invalid field", 1024, 8192, false, "invalid", 403},
		{"missing field", 1024, 8192, false, "", 403},
		{"middleware cap", 3 << 20, 4 << 20, false, token, 413},
		{"downstream smaller cap", 1024, 512, false, token, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			parts := multipart.NewWriter(&body)
			// Put the file before the token, matching native multipart field order.
			file, err := parts.CreateFormFile("upload", "sample.bin")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.Write(bytes.Repeat([]byte("x"), tc.size)); err != nil {
				t.Fatal(err)
			}
			if err := parts.WriteField(defaultCSRFField, tc.token); err != nil {
				t.Fatal(err)
			}
			if err := parts.Close(); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/", &body)
			r.Header.Set("Content-Type", parts.FormDataContentType())
			r.AddCookie(cookie)
			counter := &csrfCountingBody{remaining: 3 << 20}
			if tc.name == "form parsed without multipart" {
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
			}
			if tc.parsed {
				r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, int64(tc.downstream))
				if err := r.ParseMultipartForm(128); err != nil {
					t.Fatal(err)
				}
				defer r.MultipartForm.RemoveAll()
				r.Body = counter
			}
			w := httptest.NewRecorder()
			called := false
			m.Middleware(m.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if !tc.parsed && r.MultipartForm != nil {
					t.Fatal("Protect cached multipart fields")
				}
				r.Body = http.MaxBytesReader(w, r.Body, int64(tc.downstream))
				if err := r.ParseMultipartForm(128); err != nil {
					w.WriteHeader(413)
					return
				}
				defer r.MultipartForm.RemoveAll()
				file, _, err := r.FormFile("upload")
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				data, err := io.ReadAll(file)
				if err != nil {
					t.Fatal(err)
				}
				if len(data) != tc.size {
					t.Fatalf("file bytes = %d, want %d", len(data), tc.size)
				}
				w.WriteHeader(204)
			}))).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
			if tc.parsed && counter.read != 0 {
				t.Fatal("already parsed body was read again")
			}
			if called != (tc.want == 204 || tc.name == "downstream smaller cap") {
				t.Fatalf("handler called = %v", called)
			}
		})
	}
}

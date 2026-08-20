package session

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExplicitCommitIsTerminalAndIdempotent(t *testing.T) {
	t.Parallel()

	manager := MustNew("explicit-commit-secret-value", Options{})
	underlying := newRecordingWriter()
	var (
		firstErr, secondErr error
		setAfterCommit      error
	)
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := Current(r)
		if err := store.Set("team", "platform"); err != nil {
			t.Fatal(err)
		}
		firstErr = store.Commit(w)
		secondErr = store.Commit(w)
		setAfterCommit = store.Set("late", true)
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))

	if firstErr != nil || secondErr != nil {
		t.Fatalf("commit errors = (%v, %v), want nil", firstErr, secondErr)
	}
	if !errors.Is(setAfterCommit, ErrSessionCommitted) {
		t.Fatalf("post-commit Set error = %v, want %v", setAfterCommit, ErrSessionCommitted)
	}
	if got := len(underlying.header.Values("Set-Cookie")); got != 1 {
		t.Fatalf("Set-Cookie count = %d, want 1", got)
	}
	if got, want := underlying.statuses, []int{http.StatusNoContent}; !equalInts(got, want) {
		t.Fatalf("statuses = %v, want %v (Commit itself must not write one)", got, want)
	}
}

func TestExplicitCommitFailureCanRetryOnlyAfterMutation(t *testing.T) {
	t.Parallel()

	var reports []error
	manager := MustNew("explicit-retry-secret-value", Options{
		OnError: func(err error) { reports = append(reports, err) },
	})
	var firstErr, sameErr, secondRevisionErr, secondSameErr, retryErr, storeErr error
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := Current(r)
		_ = store.Set("blob", strings.Repeat("a", 8192))
		firstErr = store.Commit(w)
		sameErr = store.Commit(w)
		_ = store.Set("blob", strings.Repeat("b", 8192))
		secondRevisionErr = store.Commit(w)
		secondSameErr = store.Commit(w)
		if got := w.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("failed explicit commit wrote cookies: %v", got)
		}
		_ = store.Delete("blob")
		_ = store.Set("ok", "small")
		retryErr = store.Commit(w)
		storeErr = store.Err()
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	for index, err := range []error{firstErr, sameErr, secondRevisionErr, secondSameErr} {
		if !errors.Is(err, ErrSessionTooLarge) {
			t.Fatalf("failed attempt %d = %v, want ErrSessionTooLarge", index, err)
		}
	}
	if retryErr != nil || storeErr != nil {
		t.Fatalf("retry/store errors = (%v, %v), want nil", retryErr, storeErr)
	}
	if len(reports) != 2 {
		t.Fatalf("OnError calls = %d, want one for each of two failed revisions", len(reports))
	}
	if got := len(response.Header().Values("Set-Cookie")); got != 1 {
		t.Fatalf("Set-Cookie count after successful retry = %d, want 1", got)
	}
}

func TestAutomaticCommitFailureReplacesEverySuccessShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{
			name: "redirect",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = Current(r).Set("blob", strings.Repeat("a", 8192))
				http.Redirect(w, r, "/after", http.StatusTemporaryRedirect)
			},
		},
		{
			name: "header then body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = Current(r).Set("blob", strings.Repeat("a", 8192))
				w.Header().Set("Content-Length", "7")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("success"))
			},
		},
		{
			name: "implicit write",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = Current(r).Set("blob", strings.Repeat("a", 8192))
				_, _ = w.Write([]byte("success"))
			},
		},
		{
			name: "end only",
			handler: func(_ http.ResponseWriter, r *http.Request) {
				_ = Current(r).Set("blob", strings.Repeat("a", 8192))
			},
		},
		{
			name: "encode error",
			handler: func(_ http.ResponseWriter, r *http.Request) {
				_ = Current(r).Set("cannot-json-encode", make(chan int))
			},
		},
		{
			name: "staged content encoding",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = Current(r).Set("blob", strings.Repeat("a", 8192))
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Cache-Control", "public, max-age=86400")
				w.Header().Set("ETag", `"successful-representation"`)
				w.Header().Set("Content-Range", "bytes 0-6/7")
				w.Header().Set("Accept-Ranges", "bytes")
				w.Header().Set("Last-Modified", "Wed, 19 Aug 2026 12:00:00 GMT")
				w.Header().Set("Trailer", "Digest")
				w.WriteHeader(http.StatusCreated)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			manager := MustNew("automatic-failure-secret", Options{})
			response := httptest.NewRecorder()
			manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Add("Set-Cookie", "unrelated=must-not-survive; Path=/")
				testCase.handler(w, r)
			})).ServeHTTP(
				response, httptest.NewRequest(http.MethodGet, "/", nil),
			)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", response.Code)
			}
			if got := response.Header().Get("Location"); got != "" {
				t.Fatalf("Location = %q, want empty", got)
			}
			if got := response.Header().Get("Content-Length"); got != "" {
				t.Fatalf("Content-Length = %q, want empty", got)
			}
			if got := response.Header().Get("Content-Encoding"); got != "" {
				t.Fatalf("Content-Encoding = %q, want empty", got)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
			for _, field := range []string{"Content-Range", "Accept-Ranges", "ETag", "Last-Modified", "Trailer"} {
				if got := response.Header().Get(field); got != "" {
					t.Fatalf("%s = %q, want empty", field, got)
				}
			}
			if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
				t.Fatalf("Set-Cookie = %v, want none", got)
			}
			if got := response.Body.String(); got != sessionFailureBody {
				t.Fatalf("body = %q, want %q", got, sessionFailureBody)
			}
		})
	}
}

func TestExplicitFailureThenRedirectReportsOnceAndFailsClosed(t *testing.T) {
	t.Parallel()

	reports := 0
	manager := MustNew("explicit-auto-failure-secret", Options{
		OnError: func(error) { reports++ },
	})
	response := httptest.NewRecorder()
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Current(r).Set("blob", strings.Repeat("a", 8192))
		if err := Commit(w, r); !errors.Is(err, ErrSessionTooLarge) {
			t.Fatalf("Commit error = %v, want ErrSessionTooLarge", err)
		}
		http.Redirect(w, r, "/must-not-escape", http.StatusSeeOther)
	})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if reports != 1 {
		t.Fatalf("OnError calls = %d, want 1", reports)
	}
	if response.Code != http.StatusInternalServerError || response.Header().Get("Location") != "" {
		t.Fatalf("response = %d Location %q, want 500 with no Location", response.Code, response.Header().Get("Location"))
	}
}

func TestFlushCommitsBeforeStreaming(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		manager := MustNew("flush-success-secret-value", Options{})
		response := httptest.NewRecorder()
		manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = Current(r).Set("stream", "ready")
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte("event"))
		})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != http.StatusOK || response.Body.String() != "event" {
			t.Fatalf("response = %d %q, want 200 event", response.Code, response.Body.String())
		}
		if got := len(response.Header().Values("Set-Cookie")); got != 1 {
			t.Fatalf("Set-Cookie count = %d, want 1", got)
		}
	})

	t.Run("failure", func(t *testing.T) {
		manager := MustNew("flush-failure-secret-value", Options{})
		response := httptest.NewRecorder()
		manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = Current(r).Set("blob", strings.Repeat("a", 8192))
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte("event"))
		})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != http.StatusInternalServerError || response.Body.String() != sessionFailureBody {
			t.Fatalf("response = %d %q, want terminal 500", response.Code, response.Body.String())
		}
	})
}

func TestInformationalResponseDoesNotSealSession(t *testing.T) {
	t.Parallel()

	manager := MustNew("informational-secret-value", Options{})
	underlying := newRecordingWriter()
	var mutationErr error
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		mutationErr = Current(r).Set("after-hints", "allowed")
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))

	if mutationErr != nil {
		t.Fatalf("mutation after 103 = %v, want nil", mutationErr)
	}
	if got, want := underlying.statuses, []int{http.StatusEarlyHints, http.StatusNoContent}; !equalInts(got, want) {
		t.Fatalf("statuses = %v, want %v", got, want)
	}
	if got := len(underlying.header.Values("Set-Cookie")); got != 1 {
		t.Fatalf("Set-Cookie count = %d, want 1", got)
	}
}

func TestExplicitCommitCookieIsSuppressedOnInformationalResponse(t *testing.T) {
	t.Parallel()

	manager := MustNew("explicit-hints-secret-value", Options{})
	underlying := newRecordingWriter()
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Current(r).Set("ready", true)
		if err := Commit(w, r); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))

	if got, want := underlying.statuses, []int{http.StatusEarlyHints, http.StatusNoContent}; !equalInts(got, want) {
		t.Fatalf("statuses = %v, want %v", got, want)
	}
	if len(underlying.cookiesAtStatus) != 2 {
		t.Fatalf("cookie snapshots = %d, want 2", len(underlying.cookiesAtStatus))
	}
	if got := underlying.cookiesAtStatus[0]; len(got) != 0 {
		t.Fatalf("103 carried Set-Cookie %v, want none", got)
	}
	if got := underlying.cookiesAtStatus[1]; len(got) != 1 {
		t.Fatalf("final response cookies = %v, want one", got)
	}
}

func TestSwitchingProtocolsIsFinal(t *testing.T) {
	t.Parallel()

	manager := MustNew("switching-protocol-secret", Options{})
	underlying := newRecordingWriter()
	var mutationErr error
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Current(r).Set("upgrade", "ready")
		w.WriteHeader(http.StatusSwitchingProtocols)
		mutationErr = Current(r).Set("late", true)
	})).ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))

	if !errors.Is(mutationErr, ErrSessionCommitted) {
		t.Fatalf("mutation after 101 = %v, want ErrSessionCommitted", mutationErr)
	}
	if got, want := underlying.statuses, []int{http.StatusSwitchingProtocols}; !equalInts(got, want) {
		t.Fatalf("statuses = %v, want %v", got, want)
	}
	if got := len(underlying.cookiesAtStatus[0]); got != 1 {
		t.Fatalf("101 cookie count = %d, want 1", got)
	}
}

func TestPushDoesNotSealParentSession(t *testing.T) {
	t.Parallel()

	manager := MustNew("push-parent-secret-value", Options{})
	underlying := newPushWriter()
	var mutationErr error
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Current(r).Set("before-push", "dirty")
		if err := w.(http.Pusher).Push("/asset.css", nil); err != nil {
			t.Fatal(err)
		}
		if got := w.Header().Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("Push prematurely committed cookies: %v", got)
		}
		mutationErr = Current(r).Set("after-push", "allowed")
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))

	if mutationErr != nil {
		t.Fatalf("mutation after Push = %v, want nil", mutationErr)
	}
	if got, want := underlying.pushes, []string{"/asset.css"}; !equalStrings(got, want) {
		t.Fatalf("pushes = %v, want %v", got, want)
	}
	if got := len(underlying.header.Values("Set-Cookie")); got != 1 {
		t.Fatalf("final Set-Cookie count = %d, want 1", got)
	}
}

func TestHijackRequiresCommitOnlyForDirtyStore(t *testing.T) {
	t.Parallel()

	t.Run("dirty", func(t *testing.T) {
		manager := MustNew("hijack-dirty-secret-value", Options{})
		underlying := newHijackWriter()
		var hijackErr error
		manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = Current(r).Set("dirty", true)
			_, _, hijackErr = w.(http.Hijacker).Hijack()
		})).ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))
		if !errors.Is(hijackErr, ErrSessionCommitRequired) {
			t.Fatalf("Hijack error = %v, want %v", hijackErr, ErrSessionCommitRequired)
		}
		if underlying.hijacks != 0 || underlying.peer != nil {
			t.Fatalf("underlying Hijack was called %d times for a dirty uncommitted Store", underlying.hijacks)
		}
	})

	t.Run("explicitly committed", func(t *testing.T) {
		manager := MustNew("hijack-commit-secret-value", Options{})
		underlying := newHijackWriter()
		var hijackErr error
		manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = Current(r).Set("ready", true)
			if err := Commit(w, r); err != nil {
				t.Fatal(err)
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			hijackErr = err
			if conn != nil {
				_ = conn.Close()
			}
		})).ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))
		underlying.close()
		if hijackErr != nil {
			t.Fatalf("Hijack after Commit = %v, want nil", hijackErr)
		}
		if got := len(underlying.header.Values("Set-Cookie")); got != 1 {
			t.Fatalf("staged cookies after committed Hijack = %d, want 1", got)
		}
	})

	t.Run("clean", func(t *testing.T) {
		manager := MustNew("hijack-clean-secret-value", Options{})
		underlying := newHijackWriter()
		var hijackErr, mutationErr error
		manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			hijackErr = err
			if conn != nil {
				_ = conn.Close()
			}
			mutationErr = Current(r).Set("late", true)
		})).ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))
		underlying.close()
		if hijackErr != nil {
			t.Fatalf("clean Hijack = %v, want nil", hijackErr)
		}
		if !errors.Is(mutationErr, ErrSessionCommitted) {
			t.Fatalf("mutation after clean Hijack = %v, want ErrSessionCommitted", mutationErr)
		}
	})
}

func TestAutomaticFailureMakesStoreTerminal(t *testing.T) {
	t.Parallel()

	manager := MustNew("automatic-terminal-secret", Options{})
	var mutationErr error
	response := httptest.NewRecorder()
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := Current(r)
		_ = store.Set("blob", strings.Repeat("a", 8192))
		w.WriteHeader(http.StatusCreated)
		mutationErr = store.Delete("blob")
	})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if !errors.Is(mutationErr, ErrSessionCommitted) {
		t.Fatalf("mutation after terminal failure = %v, want ErrSessionCommitted", mutationErr)
	}
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
}

func TestResponseWriterAdvertisesOnlySupportedCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                                            string
		underlying                                      http.ResponseWriter
		wantFlush, wantFlushError, wantHijack, wantPush bool
	}{
		{name: "plain", underlying: newRecordingWriter()},
		{name: "flush", underlying: &flushOnlyWriter{recordingWriter: newRecordingWriter()}, wantFlush: true, wantFlushError: true},
		{name: "flush error", underlying: &flushErrorOnlyWriter{recordingWriter: newRecordingWriter()}, wantFlush: true, wantFlushError: true},
		{name: "hijack", underlying: newHijackWriter(), wantHijack: true},
		{name: "push", underlying: newPushWriter(), wantPush: true},
		{name: "all", underlying: newAllCapabilityWriter(), wantFlush: true, wantFlushError: true, wantHijack: true, wantPush: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			manager := MustNew("capability-method-set-secret", Options{})
			manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
				if !ok || unwrapper.Unwrap() != testCase.underlying {
					t.Fatalf("Unwrap() did not return the exact immediate writer")
				}
				_, hasFlush := w.(http.Flusher)
				_, hasFlushError := w.(interface{ FlushError() error })
				_, hasHijack := w.(http.Hijacker)
				_, hasPush := w.(http.Pusher)
				if hasFlush != testCase.wantFlush || hasFlushError != testCase.wantFlushError || hasHijack != testCase.wantHijack || hasPush != testCase.wantPush {
					t.Fatalf("capabilities = flush:%v flush-error:%v hijack:%v push:%v, want %v/%v/%v/%v",
						hasFlush, hasFlushError, hasHijack, hasPush,
						testCase.wantFlush, testCase.wantFlushError, testCase.wantHijack, testCase.wantPush)
				}
			})).ServeHTTP(testCase.underlying, httptest.NewRequest(http.MethodGet, "/", nil))
		})
	}
}

func TestResponseControllerFlushCannotBypassCommit(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		manager := MustNew("controller-flush-success", Options{})
		underlying := &flushErrorOnlyWriter{recordingWriter: newRecordingWriter()}
		manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = Current(r).Set("ready", true)
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Fatal(err)
			}
		})).ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))
		if underlying.flushes != 1 {
			t.Fatalf("underlying flushes = %d, want 1", underlying.flushes)
		}
		if got := len(underlying.header.Values("Set-Cookie")); got != 1 {
			t.Fatalf("Set-Cookie count = %d, want 1", got)
		}
	})

	t.Run("failure", func(t *testing.T) {
		manager := MustNew("controller-flush-failure", Options{})
		underlying := &flushErrorOnlyWriter{recordingWriter: newRecordingWriter()}
		manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = Current(r).Set("blob", strings.Repeat("a", 8192))
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Fatal(err)
			}
		})).ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))
		if got, want := underlying.statuses, []int{http.StatusInternalServerError}; !equalInts(got, want) {
			t.Fatalf("statuses = %v, want %v", got, want)
		}
		if underlying.body.String() != sessionFailureBody {
			t.Fatalf("body = %q, want terminal failure", underlying.body.String())
		}
	})
}

func TestCleanCommitSealsWithoutCookie(t *testing.T) {
	t.Parallel()

	manager := MustNew("clean-commit-secret-value", Options{})
	var mutationErr error
	response := httptest.NewRecorder()
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := Commit(w, r); err != nil {
			t.Fatal(err)
		}
		mutationErr = Current(r).Set("late", true)
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if !errors.Is(mutationErr, ErrSessionCommitted) {
		t.Fatalf("mutation after clean Commit = %v, want ErrSessionCommitted", mutationErr)
	}
	if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("clean Commit cookies = %v, want none", got)
	}
}

func TestInvalidCookieConfigurationFailsBeforeAndAtCommit(t *testing.T) {
	t.Parallel()

	if _, err := New("invalid-cookie-config-secret", Options{CookieName: "bad name"}); !errors.Is(err, ErrInvalidCookie) {
		t.Fatalf("New invalid cookie error = %v, want %v", err, ErrInvalidCookie)
	}

	manager := MustNew("invalid-cookie-commit-secret", Options{})
	manager.opts.CookieName = "bad name" // Exercise Commit's defense-in-depth check.
	var commitErr, mutationErr error
	response := httptest.NewRecorder()
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := Current(r)
		_ = store.Set("team", "platform")
		commitErr = store.Commit(w)
		mutationErr = store.Set("retry", true)
	})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if !errors.Is(commitErr, ErrInvalidCookie) {
		t.Fatalf("Commit invalid cookie error = %v, want %v", commitErr, ErrInvalidCookie)
	}
	if mutationErr != nil {
		t.Fatalf("mutation after explicit failure = %v, want retryable Store", mutationErr)
	}
	if got := response.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("invalid cookie commit wrote headers: %v", got)
	}
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("automatic completion status = %d, want 500", response.Code)
	}
}

func TestCookieLimitUsesCompleteSerializedCookie(t *testing.T) {
	t.Parallel()

	for _, encrypt := range []bool{false, true} {
		name := "signed"
		if encrypt {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			manager := MustNew("complete-cookie-limit-secret", Options{Encrypt: encrypt})
			firstRejected := 0
			for size := 1; size < MaxCookieSize*2; size++ {
				store := manager.load(nil)
				_ = store.Set("blob", strings.Repeat("a", size))
				_, err := manager.cookieHeader(store)
				if errors.Is(err, ErrSessionTooLarge) {
					firstRejected = size
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if firstRejected == 0 {
				t.Fatal("did not find the first rejected cookie size")
			}

			accepted := manager.load(nil)
			_ = accepted.Set("blob", strings.Repeat("a", firstRejected-1))
			header, err := manager.cookieHeader(accepted)
			if err != nil {
				t.Fatalf("last accepted cookie: %v", err)
			}
			if len(header) > MaxCookieSize {
				t.Fatalf("accepted cookie length = %d, exceeds %d", len(header), MaxCookieSize)
			}

			rejected := manager.load(nil)
			_ = rejected.Set("blob", strings.Repeat("a", firstRejected))
			if _, err := manager.cookieHeader(rejected); !errors.Is(err, ErrSessionTooLarge) {
				t.Fatalf("first rejected cookie error = %v, want ErrSessionTooLarge", err)
			}
		})
	}
}

func TestSessionJSONDoesNotEscapeHTMLCharacters(t *testing.T) {
	t.Parallel()

	manager := MustNew("escape-html-session-secret", Options{})
	store := manager.load(nil)
	_ = store.Set("characters", "<>&")
	encoded, err := manager.encode(store)
	if err != nil {
		t.Fatal(err)
	}
	payloadPart := strings.Split(encoded, ".")[0]
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"characters":"<>&"`)) {
		t.Fatalf("payload = %s, want literal HTML characters", payload)
	}
	if bytes.Contains(payload, []byte(`\u003`)) || bytes.Contains(payload, []byte(`\u0026`)) {
		t.Fatalf("payload still contains HTML escapes: %s", payload)
	}
}

func TestCommitRequiresMatchingMiddlewareWriter(t *testing.T) {
	t.Parallel()

	if err := Commit(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)); !errors.Is(err, ErrSessionMiddlewareRequired) {
		t.Fatalf("Commit without middleware = %v, want %v", err, ErrSessionMiddlewareRequired)
	}

	manager := MustNew("matching-writer-secret-value", Options{})
	var mismatchErr, wrappedErr error
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Current(r).Set("team", "platform")
		mismatchErr = Current(r).Commit(httptest.NewRecorder())
		wrappedErr = Current(r).Commit(&unwrapOnlyWriter{ResponseWriter: w})
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !errors.Is(mismatchErr, ErrSessionWriterMismatch) {
		t.Fatalf("mismatch error = %v, want %v", mismatchErr, ErrSessionWriterMismatch)
	}
	if wrappedErr != nil {
		t.Fatalf("commit through Unwrap chain = %v, want nil", wrappedErr)
	}
}

func TestExplicitCommitStagesCookieOnSuppliedResponseBoundary(t *testing.T) {
	t.Parallel()

	manager := MustNew("isolated-response-boundary-secret", Options{})
	underlying := httptest.NewRecorder()
	var isolatedHeader http.Header
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = Current(r).Set("candidate", "selected only if committed")
		isolated := &isolatedHeaderWriter{
			ResponseWriter: w,
			header:         make(http.Header),
		}
		if err := Commit(isolated, r); err != nil {
			t.Fatal(err)
		}
		isolatedHeader = isolated.Header().Clone()
		// Deliberately discard the speculative response boundary. The cookie
		// must not leak to the underlying response unless an interceptor elects
		// to copy this header into the winning response.
	})).ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := len(isolatedHeader.Values("Set-Cookie")); got != 1 {
		t.Fatalf("isolated Set-Cookie count = %d, want 1", got)
	}
	if got := underlying.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("discarded boundary leaked cookies to underlying response: %v", got)
	}
}

func TestEveryMutatorRejectsAfterCommit(t *testing.T) {
	t.Parallel()

	manager := MustNew("sealed-mutator-secret-value", Options{})
	var got []error
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		store := Current(r)
		_ = store.Set("initial", true)
		if err := store.Commit(w); err != nil {
			t.Fatal(err)
		}
		got = append(got,
			store.Set("late", true),
			store.Delete("initial"),
			store.AddFlash("notice", "late"),
			AddFlash(r, "notice", "late through request"),
			store.Destroy(),
			Destroy(r),
		)
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	for index, err := range got {
		if !errors.Is(err, ErrSessionCommitted) {
			t.Fatalf("mutator %d error = %v, want %v", index, err, ErrSessionCommitted)
		}
	}
}

func TestPackageMutatorsRequireMiddleware(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	if err := AddFlash(r, "notice", "missing"); !errors.Is(err, ErrSessionMiddlewareRequired) {
		t.Fatalf("AddFlash error = %v, want %v", err, ErrSessionMiddlewareRequired)
	}
	if err := Destroy(r); !errors.Is(err, ErrSessionMiddlewareRequired) {
		t.Fatalf("Destroy error = %v, want %v", err, ErrSessionMiddlewareRequired)
	}
}

func TestDestroyStillWritesDeletionCookieThroughCommit(t *testing.T) {
	t.Parallel()

	manager := MustNew("destroy-commit-secret-value", Options{})
	response := httptest.NewRecorder()
	manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := Current(r).Destroy(); err != nil {
			t.Fatal(err)
		}
		if err := Commit(w, r); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/sign-out", nil))

	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("deletion cookies = %#v, want one cookie with negative MaxAge", cookies)
	}
}

type recordingWriter struct {
	header          http.Header
	statuses        []int
	cookiesAtStatus [][]string
	body            bytes.Buffer
}

func newRecordingWriter() *recordingWriter {
	return &recordingWriter{header: make(http.Header)}
}

func (w *recordingWriter) Header() http.Header { return w.header }

func (w *recordingWriter) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
	w.cookiesAtStatus = append(w.cookiesAtStatus, append([]string(nil), w.header.Values("Set-Cookie")...))
}

func (w *recordingWriter) Write(data []byte) (int, error) {
	if len(w.statuses) == 0 {
		w.statuses = append(w.statuses, http.StatusOK)
	}
	return w.body.Write(data)
}

type pushWriter struct {
	*recordingWriter
	pushes []string
}

type flushOnlyWriter struct {
	*recordingWriter
	flushes int
}

func (w *flushOnlyWriter) Flush() { w.flushes++ }

type flushErrorOnlyWriter struct {
	*recordingWriter
	flushes int
}

func (w *flushErrorOnlyWriter) FlushError() error {
	w.flushes++
	return nil
}

func newPushWriter() *pushWriter { return &pushWriter{recordingWriter: newRecordingWriter()} }

func (w *pushWriter) Push(target string, _ *http.PushOptions) error {
	w.pushes = append(w.pushes, target)
	return nil
}

type hijackWriter struct {
	*recordingWriter
	peer    net.Conn
	hijacks int
}

func newHijackWriter() *hijackWriter {
	return &hijackWriter{recordingWriter: newRecordingWriter()}
}

func (w *hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacks++
	server, peer := net.Pipe()
	w.peer = peer
	return server, bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)), nil
}

func (w *hijackWriter) close() {
	if w.peer != nil {
		_ = w.peer.Close()
	}
}

type allCapabilityWriter struct {
	*hijackWriter
	pushes  []string
	flushes int
}

func newAllCapabilityWriter() *allCapabilityWriter {
	return &allCapabilityWriter{hijackWriter: newHijackWriter()}
}

func (w *allCapabilityWriter) Flush() { w.flushes++ }
func (w *allCapabilityWriter) Push(target string, _ *http.PushOptions) error {
	w.pushes = append(w.pushes, target)
	return nil
}

type unwrapOnlyWriter struct{ http.ResponseWriter }

func (w *unwrapOnlyWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type isolatedHeaderWriter struct {
	http.ResponseWriter
	header http.Header
}

func (w *isolatedHeaderWriter) Header() http.Header         { return w.header }
func (w *isolatedHeaderWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

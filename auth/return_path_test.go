package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSafeReturnPathAcceptsRootRelativeRequestURIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "root", raw: "/", want: "/"},
		{name: "path", raw: "/draft/room", want: "/draft/room"},
		{name: "query", raw: "/wire?category=injury&week=1", want: "/wire?category=injury&week=1"},
		{name: "escaped path", raw: "/team%20room", want: "/team%20room"},
		{name: "unicode path is request escaped", raw: "/café", want: "/caf%C3%A9"},
		{name: "unicode query is request escaped", raw: "/search?q=café", want: "/search?q=caf%C3%A9"},
		{name: "raw dot segments", raw: "/draft/../admin", want: "/admin"},
		{name: "encoded dot segments", raw: "/draft/%2e%2e/admin", want: "/admin"},
		{name: "redundant slash", raw: "/draft//room", want: "/draft/room"},
		{name: "trailing slash survives cleaning", raw: "/draft//room/", want: "/draft/room/"},
		{name: "encoded unreserved byte", raw: "/%64raft", want: "/draft"},
		{name: "internal encoded slash", raw: "/draft%2Froom", want: "/draft/room"},
		{name: "literal encoded percent remains one layer", raw: "/%252f%252fevil", want: "/%252f%252fevil"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, ok := SafeReturnPath(testCase.raw)
			if !ok || got != testCase.want {
				t.Fatalf("SafeReturnPath(%q) = (%q, %v), want (%q, true)", testCase.raw, got, ok, testCase.want)
			}
		})
	}
}

func TestSafeReturnPathRejectsUnsafeDestinations(t *testing.T) {
	t.Parallel()

	invalidUTF8 := string([]byte{'/', 0xff})
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty"},
		{name: "relative", raw: "draft"},
		{name: "absolute URL", raw: "https://evil.example/path"},
		{name: "protocol relative", raw: "//evil.example/path"},
		{name: "three leading slashes", raw: "///evil.example/path"},
		{name: "encoded protocol relative", raw: "/%2Fevil.example/path"},
		{name: "two encoded leading slashes", raw: "/%2f%2fevil.example"},
		{name: "fragment", raw: "/draft#clock"},
		{name: "malformed escape", raw: "/draft%2"},
		{name: "invalid escaped utf8 path", raw: "/%FF"},
		{name: "invalid escaped utf8 query", raw: "/?q=%FF"},
		{name: "raw backslash", raw: `/\evil.example`},
		{name: "escaped path backslash", raw: "/%5cevil.example"},
		{name: "escaped query backslash", raw: "/draft?next=%5Cevil"},
		{name: "raw newline", raw: "/draft\nLocation:/admin"},
		{name: "escaped path newline", raw: "/draft%0aadmin"},
		{name: "escaped query newline", raw: "/draft?next=%0Aadmin"},
		{name: "escaped delete", raw: "/draft?next=%7fadmin"},
		{name: "raw C1 path control", raw: "/\u0080admin"},
		{name: "escaped C1 path control", raw: "/%C2%80admin"},
		{name: "raw C1 query control", raw: "/draft?next=\u009fadmin"},
		{name: "escaped C1 query control", raw: "/draft?next=%C2%9Fadmin"},
		{name: "leading whitespace", raw: " /draft"},
		{name: "trailing whitespace", raw: "/draft "},
		{name: "invalid utf8", raw: invalidUTF8},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got, ok := SafeReturnPath(testCase.raw); ok || got != "" {
				t.Fatalf("SafeReturnPath(%q) = (%q, %v), want (\"\", false)", testCase.raw, got, ok)
			}
		})
	}
}

func TestSafeReturnPathEnforcesEncodedByteBudget(t *testing.T) {
	t.Parallel()

	exact := "/" + strings.Repeat("a", maxReturnPathBytes-1)
	if got, ok := SafeReturnPath(exact); !ok || got != exact {
		t.Fatalf("exact boundary = (%d bytes, %v), want (%d bytes, true)", len(got), ok, maxReturnPathBytes)
	}

	firstOver := "/" + strings.Repeat("a", maxReturnPathBytes)
	if got, ok := SafeReturnPath(firstOver); ok || got != "" {
		t.Fatalf("first-over boundary = (%q, %v), want (\"\", false)", got, ok)
	}

	// The supplied UTF-8 representation is below the budget, but URL path
	// escaping expands it beyond the final RequestURI budget.
	expandsPastLimit := "/" + strings.Repeat("é", 341)
	if len(expandsPastLimit) >= maxReturnPathBytes {
		t.Fatalf("test fixture raw length = %d, want below %d", len(expandsPastLimit), maxReturnPathBytes)
	}
	if got, ok := SafeReturnPath(expandsPastLimit); ok || got != "" {
		t.Fatalf("expanded boundary = (%q, %v), want (\"\", false)", got, ok)
	}

	queryExact := "/?q=" + strings.Repeat("é", 170)
	queryWant := "/?q=" + strings.Repeat("%C3%A9", 170)
	if len(queryWant) != maxReturnPathBytes {
		t.Fatalf("canonical query fixture length = %d, want %d", len(queryWant), maxReturnPathBytes)
	}
	if got, ok := SafeReturnPath(queryExact); !ok || got != queryWant {
		t.Fatalf("exact escaped query = (%d bytes, %v), want (%d bytes, true)", len(got), ok, maxReturnPathBytes)
	}
	queryOver := queryExact + "a"
	queryOverWant := queryWant + "a"
	if len(queryOverWant) != maxReturnPathBytes+1 {
		t.Fatalf("first-over canonical query fixture length = %d, want %d", len(queryOverWant), maxReturnPathBytes+1)
	}
	if got, ok := SafeReturnPath(queryOver); ok || got != "" {
		t.Fatalf("first-over escaped query = (%q, %v), want (\"\", false)", got, ok)
	}
}

func TestSafeReturnPathMatchesRedirectAndIsIdempotent(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"/draft/../admin?from=queue",
		"/draft//room/",
		"/%64raft?name=café",
		"/draft%2Froom?tab=board",
		"/%252f%252fevil",
	} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			target, ok := SafeReturnPath(raw)
			if !ok {
				t.Fatalf("SafeReturnPath(%q) rejected a canonicalizable local path", raw)
			}
			again, ok := SafeReturnPath(target)
			if !ok || again != target {
				t.Fatalf("second validation = (%q, %v), want (%q, true)", again, ok, target)
			}

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "https://app.example.test/login", nil)
			http.Redirect(response, request, target, http.StatusSeeOther)
			if got := response.Header().Get("Location"); got != target {
				t.Fatalf("redirect Location = %q, want canonical target %q", got, target)
			}
		})
	}
}

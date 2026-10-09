package wire

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"testing"
)

type headerCorpusTransport func(*http.Request) (*http.Response, error)

func (f headerCorpusTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// RFC 9111 §5.2: only directive names, not words inside extension values,
// control caching. RFC 9110 §§5.6.1.2 and 8.4: ignore empty list elements;
// coding names are case-insensitive, but two codings remain two operations.
func TestFetchGeneratedHeaderCorpus(t *testing.T) {
	rng := rand.New(rand.NewSource(53403))
	recase := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' && rng.Intn(2) == 0 {
				return r - 32
			}
			return r
		}, s)
	}
	count := 0
	for i, seed := range []struct {
		cache                string
		shareable, immutable bool
	}{
		{"public, max-age=60, immutable", true, true},
		// RFC 8246 §2: ignore immutable arguments and repetitions.
		{`public, immutable="ignored"`, true, true},
		{"public, immutable, immutable", true, true},
		{`public, private="Set-Cookie"`, false, false},
		{"public, private, public", false, false},
		{"public, no-store", false, false},
		{`extension="private, no-store, immutable", max-age=60`, true, false},
		{`extension="a\",private", immutable`, true, true},
		{"x-private, x-no-store, x-immutable", true, false},
	} {
		for variant := 0; variant < 16; variant++ {
			t.Run(fmt.Sprintf("cache-%d/%02d", i, variant), func(t *testing.T) {
				h := http.Header{"Cache-Control": {" , \t" + recase(seed.cache) + ", , ", " , "}}
				res, _, _, err := corpusFetch(h, 200, []byte("fixture"))
				values := h.Values("Cache-Control")
				for i := range values {
					values[i] = strings.Trim(values[i], " \t")
				}
				if err != nil || res.Immutable != seed.immutable || (Route{Document: res}).EvaluatePolicies()[PolicyHTMLShareable].Pass != seed.shareable || res.CacheControl != strings.Join(values, ", ") {
					t.Fatalf("cache verdict or retained field changed: %v", err)
				}
			})
			count++
		}
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, _ = gz.Write([]byte("fixture"))
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	for variant := 0; variant < 24; variant++ {
		t.Run(fmt.Sprintf("encoding/%02d", variant), func(t *testing.T) {
			h := http.Header{"Content-Encoding": {" , \t" + recase("gzip") + "\t, ,", " , "}}
			res, body, _, err := corpusFetch(h, 200, compressed.Bytes())
			if err != nil || res.ContentEncoding != "gzip" || string(body) != "fixture" {
				t.Fatalf("coding transformation changed decoded bytes: %v", err)
			}
			h.Add("Content-Encoding", "gzip")
			if _, _, _, err := corpusFetch(h, 200, compressed.Bytes()); err == nil {
				t.Fatal("stacked encoding accepted")
			}
		})
		count += 2
		// RFC 9110 §§5.5 and 10.2.2: Location has surrounding OWS,
		// but its URI is case-sensitive and a comma is not a separator.
		t.Run(fmt.Sprintf("location/%02d", variant), func(t *testing.T) {
			h := http.Header{"Location": {" \t/Case-Sensitive,a\t "}}
			_, _, location, err := corpusFetch(h, 302, nil)
			if err != nil || location != "/Case-Sensitive,a" {
				t.Fatalf("singleton URI changed: %v", err)
			}
			h.Add("Location", "/other")
			if _, _, _, err := corpusFetch(h, 302, nil); err == nil {
				t.Fatal("repeated singleton accepted")
			}
		})
		count += 2
	}
	t.Logf("fixed-seed wire header corpus: %d cases", count)
}

func corpusFetch(h http.Header, status int, wire []byte) (Resource, []byte, string, error) {
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: headerCorpusTransport(func(req *http.Request) (*http.Response, error) {
		// Use the actual HTTP parser: singleton-field OWS is removed before
		// http.Client examines Location, even when redirects are disabled.
		var response bytes.Buffer
		fmt.Fprintf(&response, "HTTP/1.1 %d fixture\r\nContent-Length: %d\r\n", status, len(wire))
		if err := h.Write(&response); err != nil {
			return nil, err
		}
		response.WriteString("\r\n")
		response.Write(wire)
		return http.ReadResponse(bufio.NewReader(&response), req)
	})}
	res, body, _, location, err := fetchOnce(context.Background(), client, MobileUserAgent, "https://example.invalid/", "navigation")
	if location == nil {
		return res, body, "", err
	}
	return res, body, location.String(), err
}

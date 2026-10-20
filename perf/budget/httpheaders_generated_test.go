package budget

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/assetmeasure"
)

// CSP3 §§2.2.1, 6.7.3.2 and 6.7.3.3: directive names and keywords
// are ASCII case-insensitive; nonce/hash payloads are case-sensitive. The
// first directive wins. Each enforced policy must independently allow inline.
func TestInlineCSP3GeneratedCorpus(t *testing.T) {
	const nonce = "AbCdEf"
	script := `<script nonce="` + nonce + `">app()</script>`
	style := `<style nonce="` + nonce + `">p{color:red}</style>`
	allow := "default-src 'nonce-" + nonce + "'"
	type seed struct {
		name, body, policy string
		allowed            bool
	}
	seeds := []seed{
		{"nonce", script + style, allow, true},
		{"mixed-script-override", script, allow + "; SCRIPT-SRC 'NONE'", false},
		{"mixed-style-override", style, allow + "; STYLE-SRC 'NONE'", false},
		{"mixed-duplicate", script, allow + "; DEFAULT-SRC 'NONE'", true},
		{"mixed-nonce-prefix", script, "script-src 'NONCE-AbCdEf'", true},
		{"override", script, allow + "; script-src 'none'", false},
		{"element-override", script, allow + "; script-src-elem 'none'", false},
		{"style-override", style, allow + "; style-src 'none'", false},
		{"first-allow", script, allow + "; default-src 'none'", true},
		{"first-deny", script, "default-src 'none'; " + allow, false},
		{"first-script", script, "script-src 'nonce-AbCdEf'; script-src 'none'", true},
		{"first-element", script, "script-src-elem 'none'; script-src-elem 'nonce-AbCdEf'", false},
		{"two-compatible", script + style, allow + ", script-src 'nonce-AbCdEf'; style-src 'nonce-AbCdEf'", true},
		{"two-conflicting", script, allow + ", script-src 'none'", false},
		{"two-conflicting-reversed", script, "script-src 'none', " + allow, false},
		{"unrelated", script, "img-src 'none', " + allow, true},
		{"self", script, "script-src 'self'", false},
		{"none", script, "script-src 'none'", false},
		{"none-with-nonce", script, "script-src 'none' 'nonce-AbCdEf'", true},
		{"unsafe-inline", script, "script-src 'unsafe-inline'", true},
		{"unsafe-inline-style", style, "style-src 'unsafe-inline'", true},
		{"nonce-disables-unsafe", script, "script-src 'unsafe-inline' 'nonce-Other'", false},
		{"strict-disables-unsafe", script, "script-src 'unsafe-inline' 'strict-dynamic'", false},
		{"strict-style", style, "style-src 'unsafe-inline' 'strict-dynamic'", true},
		{"nonce-with-strict", script, "script-src 'strict-dynamic' 'nonce-AbCdEf'", true},
		{"nonce-case-sensitive", script, "script-src 'nonce-aBcDeF'", false},
		{"external-unsafe", `<script nonce="AbCdEf" src="/app.js"></script>`, "script-src 'unsafe-inline'", false},
		{"external-nonce", `<script nonce="AbCdEf" src="/app.js"></script>`, "script-src 'nonce-AbCdEf'", true},
		{"opaque-padded-nonce", `<script nonce="YWJjZA==">app()</script>`, "script-src 'nonce-YWJjZA=='", true},
		{"opaque-unpadded-nonce", `<script nonce="YWJjZA==">app()</script>`, "script-src 'nonce-YWJjZA'", false},
		{"opaque-url-nonce", `<script nonce="AA+/">app()</script>`, "script-src 'nonce-AA-_'", false},
		{"opaque-standard-nonce", `<script nonce="AA+/">app()</script>`, "script-src 'nonce-AA+/'", true},
	}
	for _, algorithm := range []string{"sha256", "sha384", "sha512"} {
		var digest []byte
		switch algorithm {
		case "sha256":
			value := sha256.Sum256([]byte("app()"))
			digest = value[:]
		case "sha384":
			value := sha512.Sum384([]byte("app()"))
			digest = value[:]
		case "sha512":
			value := sha512.Sum512([]byte("app()"))
			digest = value[:]
		}
		payload := base64.StdEncoding.EncodeToString(digest)
		// CSP3 §2.3.1 permits either alphabet and optional padding. Exercise
		// equivalent digests while nonce sources remain opaque strings.
		encodings := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}
		for variant, encoding := range encodings {
			encoded := encoding.EncodeToString(digest)
			seeds = append(seeds, seed{fmt.Sprintf("%s-hash-%d", algorithm, variant), script, "script-src '" + algorithm + "-" + encoded + "'", true})
		}
		wrong := swapFirstLetter(payload)
		seeds = append(seeds,
			seed{algorithm + "-case-sensitive", script, "script-src '" + algorithm + "-" + wrong + "'", false},
			seed{algorithm + "-disables-unsafe", script, "script-src 'unsafe-inline' '" + algorithm + "-" + wrong + "'", false})
		// Style hashes and parsed line endings use the same CSP3 algorithm.
		text := "p{\ncolor:red}\n"
		switch algorithm {
		case "sha256":
			value := sha256.Sum256([]byte(text))
			digest = value[:]
		case "sha384":
			value := sha512.Sum384([]byte(text))
			digest = value[:]
		case "sha512":
			value := sha512.Sum512([]byte(text))
			digest = value[:]
		}
		for variant, encoding := range encodings {
			payload = encoding.EncodeToString(digest)
			seeds = append(seeds,
				seed{fmt.Sprintf("%s-style-hash-%d", algorithm, variant), `<style nonce="AbCdEf">p{` + "\r\ncolor:red}\r" + `</style>`, "style-src '" + algorithm + "-" + payload + "'", true},
				seed{fmt.Sprintf("%s-style-case-sensitive-%d", algorithm, variant), `<style nonce="AbCdEf">p{` + "\r\ncolor:red}\r" + `</style>`, "style-src '" + algorithm + "-" + swapFirstLetter(payload) + "'", false})
		}
	}
	rng := rand.New(rand.NewSource(53403))
	insensitive := regexp.MustCompile(`(?i)(?:\b(?:default|script|style|img)-src(?:-elem)?\b|'(?:nonce-|sha256-|sha384-|sha512-|self'|none'|unsafe-inline'|strict-dynamic'))`)
	count := 0
	for i, s := range seeds {
		for variant := 0; variant < 24; variant++ {
			policy := s.policy
			if variant > 0 {
				policy = insensitive.ReplaceAllStringFunc(policy, func(token string) string { return headerCase(token, rng) })
				policy = strings.ReplaceAll(policy, ";", " \t; ;\t")
				policy = strings.ReplaceAll(policy, ",", "\t, , ")
				policy = " ; \t" + policy + " ; "
			}
			count++
			t.Run(fmt.Sprintf("%02d-%s/%02d", i, s.name, variant), func(t *testing.T) {
				if err := VerifyHTMLNonces([]byte(s.body), policy); (err == nil) != s.allowed {
					t.Fatalf("CSP verdict changed: allowed=%v, error=%v", s.allowed, err)
				}
			})
		}
	}
	t.Logf("fixed-seed CSP corpus: %d cases", count)
}

func headerCase(s string, rng *rand.Rand) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if r >= 'a' && r <= 'z' && rng.Intn(2) == 0 {
			r -= 'a' - 'A'
		}
		return r
	}, s)
}

func swapFirstLetter(s string) string {
	for i := range s {
		if s[i] >= 'a' && s[i] <= 'z' {
			return s[:i] + string(s[i]-32) + s[i+1:]
		}
		if s[i] >= 'A' && s[i] <= 'Z' {
			return s[:i] + string(s[i]+32) + s[i+1:]
		}
	}
	panic("test payload has no letters")
}

// RFC 9111 §§4.2.1, 5.2: names are case-insensitive; quoted arguments
// are equivalent to tokens. Duplicate max-age is treated as stale. Private
// and no-store remain restrictive on every field line, including qualified
// private (the measurement does not strip response fields before storage).
func TestHTTPMeasureRFC9111GeneratedCacheCorpus(t *testing.T) {
	seeds := []struct {
		value                string
		shareable, immutable bool
	}{
		{"public, max-age=31536000, immutable", true, true},
		{`public, max-age="31536000", immutable`, true, true},
		{`public, max-age="031536000", immutable`, true, true},
		// RFC 8246 §2: immutable arguments are ignored, and repeats
		// have the same meaning as one occurrence.
		{`max-age=31536000, immutable="ignored"`, true, true},
		{"max-age=31536000, immutable, immutable", true, true},
		{"public, private, max-age=31536000, immutable", false, true},
		{`public, private="Set-Cookie", max-age=31536000, immutable`, false, true},
		{"public, no-store, max-age=31536000, immutable", false, true},
		{"max-age=60, max-age=31536000, immutable", true, false},
		{"max-age=31536000, max-age=60, immutable", true, false},
		{"max-age=31536000, max-age=31536000, immutable", true, false},
		{`extension="private, no-store, immutable", max-age=31536000`, true, false},
		{`extension="a\",private", max-age=31536000, immutable`, true, true},
		{"x-private, x-no-store, x-immutable, max-age=31536000", true, false},
		{"public, private, public", false, false},
		{"public, no-store, public", false, false},
	}
	rng := rand.New(rand.NewSource(53403))
	for i, s := range seeds {
		for variant := 0; variant < 16; variant++ {
			value := headerCase(s.value, rng)
			// RFC 9110 §§5.6.1.2, 5.6.3: empty list members and OWS do
			// not change meaning. Split at a known directive boundary only.
			values := []string{" \t, " + value + ", , \t"}
			if variant%2 == 1 {
				values = append(values, " , \t")
			}
			t.Run(fmt.Sprintf("%02d/%02d", i, variant), func(t *testing.T) {
				h := http.Header{"Cache-Control": values, "Content-Type": {"text/html"}}
				result, err := generatedMeasure(t, []byte("<p>fixture</p>"), "html", "identity", h)
				if err != nil || testHTTPPolicy(result, "html-shareable") != s.shareable {
					t.Fatalf("shareability changed: %v", err)
				}
				h.Set("Content-Type", "text/javascript")
				result, err = generatedMeasure(t, []byte("fixture()"), "js", "identity", h)
				if err != nil || testHTTPPolicy(result, "immutable-hashed") != s.immutable {
					t.Fatalf("immutability changed: %v", err)
				}
			})
		}
	}
	t.Logf("fixed-seed Cache-Control corpus: %d cases (two verdicts each)", len(seeds)*16)
}

// RFC 9110 §§5.6.1.2, 8.3.1, 8.4: list empties, OWS, coding names,
// media types and parameter names. Actual stacked encodings remain unsupported.
func TestHTTPMeasureRFC9110GeneratedHeaderCorpus(t *testing.T) {
	rng := rand.New(rand.NewSource(53403))
	count := 0
	for _, encoding := range []string{"identity", "gzip", "br"} {
		for variant := 0; variant < 24; variant++ {
			t.Run(fmt.Sprintf("encoding-%s/%02d", encoding, variant), func(t *testing.T) {
				// RFC 9110 §5.6.6 allows empty parameters, quoted values
				// and semicolons inside quoted parameter values.
				parameter := headerCase("charset", rng) + "=" + headerCase("utf-8", rng)
				if variant%2 == 1 {
					parameter = headerCase("charset", rng) + `="` + headerCase("utf-8", rng) + `"`
				}
				h := http.Header{
					"Content-Type":     {" \t" + headerCase("text/javascript", rng) + "; ;\t" + parameter + `; extension="a;B\"c";` + "\t"},
					"Content-Encoding": {" , \t" + headerCase(encoding, rng) + "\t, ,", " , "},
				}
				result, err := generatedMeasure(t, []byte("fixture()"), "js", encoding, h)
				if err != nil || !testHTTPPolicy(result, "served-matches-build") {
					t.Fatalf("valid field transformation rejected: %v", err)
				}
				// RFC 9110 §8.4: duplicate codings describe two decoding
				// operations, unlike duplicate directives in a CSP policy.
				h.Add("Content-Encoding", encoding)
				if _, err := generatedMeasure(t, []byte("fixture()"), "js", encoding, h); err == nil {
					t.Fatal("stacked coding accepted")
				}
			})
			count += 2
		}
	}
	for variant := 0; variant < 24; variant++ {
		t.Run(fmt.Sprintf("retained-fields/%02d", variant), func(t *testing.T) {
			// Vary (§12.5.5) and Link (§5.3, RFC 8288 §3) are retained,
			// not graded here. Set-Cookie (§5.3) is never comma-joined.
			h := http.Header{
				"Content-Type":                        {headerCase("text/html", rng)},
				"Vary":                                {" ,\t" + headerCase("Accept-Encoding", rng) + ", ", headerCase("Accept-Language", rng), headerCase("Accept-Encoding", rng)},
				"Link":                                {`</style.css>; rel="preload"; as="style"`, `</module.js>; rel="modulepreload"`},
				"Content-Security-Policy":             {"DEFAULT-SRC 'NONCE-AbCdEf'", "SCRIPT-SRC 'nonce-AbCdEf'"},
				"Content-Security-Policy-Report-Only": {"SCRIPT-SRC 'none'"},
			}
			result, err := generatedMeasure(t, []byte(`<script nonce="AbCdEf">app()</script>`), "html", "identity", h)
			if err != nil || !testHTTPPolicy(result, "html-shareable") || !equalHeaderValues(result.header, h) {
				t.Fatalf("retained fields or enforced policies changed: %v", err)
			}
			h.Add("Content-Security-Policy", "ScRiPt-SrC 'none'")
			if _, err := generatedMeasure(t, []byte(`<script nonce="AbCdEf">app()</script>`), "html", "identity", h); err == nil {
				t.Fatal("mixed-case enforced policy ignored")
			}
			h.Del("Content-Security-Policy")
			h.Add("Set-Cookie", "id=AbCd; Expires=Wed, 21 Oct 2037 07:28:00 GMT")
			h.Add("Set-Cookie", "theme=Dark; Path=/")
			result, err = generatedMeasure(t, []byte("<p>fixture</p>"), "html", "identity", h)
			if err != nil || testHTTPPolicy(result, "no-cookie") || testHTTPPolicy(result, "html-shareable") || !equalHeaderValues(result.header, h) {
				t.Fatalf("repeated cookies were lost or combined: %v", err)
			}
		})
		count += 3
	}
	t.Logf("fixed-seed other-header corpus: %d cases", count)
}

// RFC 9110 §§5.5, 8.6, 10.2.2: the transport parses Content-Length and
// removes field OWS. Identical repeated lengths may be normalized or rejected;
// conflicting lengths must fail. Location remains one case-sensitive URI.
func TestHTTPMeasureRFC9110TransportCorpus(t *testing.T) {
	body := []byte("fixture()")
	for i, tc := range []struct {
		values  []string
		allowed bool
	}{
		{[]string{"9"}, true}, {[]string{" \t9\t "}, true},
		{[]string{"009"}, true}, {[]string{"9", "9"}, true},
		{[]string{"9", "10"}, false}, {[]string{"10", "9"}, false},
		{[]string{"9, 9"}, false}, {[]string{"9, 10"}, false},
		{[]string{"-9"}, false}, {[]string{"9.0"}, false},
	} {
		t.Run(fmt.Sprintf("length/%02d", i), func(t *testing.T) {
			h := http.Header{"Content-Length": tc.values, "Content-Type": {"text/javascript"}}
			client := &http.Client{Transport: testRoundTrip(func(req *http.Request) (*http.Response, error) {
				return parsedCorpusResponse(req, 200, h, body)
			})}
			opts := HTTPMeasureOptions{Client: client, BaseURL: "https://example.invalid", URL: "/asset.js", Kind: "js", ExpectedBody: body, ExpectedSHA256: testMeasureHash(body)}
			_, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
			if (err == nil) != tc.allowed {
				t.Fatalf("framing verdict changed: %v", err)
			}
		})
	}
	for i, locations := range [][]string{{"/Case-Sensitive,a"}, {" \t/Case-Sensitive,a\t "}, {"/Case-Sensitive,a", "/other"}} {
		t.Run(fmt.Sprintf("location/%02d", i), func(t *testing.T) {
			client := &http.Client{Transport: testRoundTrip(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/start" {
					return parsedCorpusResponse(req, 302, http.Header{"Location": locations, "Content-Length": {"0"}}, nil)
				}
				if req.URL.Path != "/Case-Sensitive,a" {
					t.Errorf("Location URI changed: %s", req.URL.Path)
				}
				return parsedCorpusResponse(req, 200, http.Header{"Content-Type": {"text/javascript"}, "Content-Length": {"9"}}, body)
			})}
			opts := HTTPMeasureOptions{Client: client, BaseURL: "https://example.invalid", URL: "/start", Kind: "js", ExpectedBody: body, ExpectedSHA256: testMeasureHash(body)}
			_, err := measureHTTP(context.Background(), opts, testBodyNormalizer)
			if (err == nil) != (i != 2) {
				t.Fatalf("redirect verdict changed: %v", err)
			}
		})
	}
	t.Log("transport corpus: 13 cases")
}

func parsedCorpusResponse(req *http.Request, status int, headers http.Header, body []byte) (*http.Response, error) {
	var response bytes.Buffer
	fmt.Fprintf(&response, "HTTP/1.1 %d fixture\r\n", status)
	if err := headers.Write(&response); err != nil {
		return nil, err
	}
	response.WriteString("\r\n")
	response.Write(body)
	return http.ReadResponse(bufio.NewReader(&response), req)
}

func equalHeaderValues(got, want http.Header) bool {
	for key, values := range want {
		if fmt.Sprint(got.Values(key)) != fmt.Sprint(values) {
			return false
		}
	}
	return true
}

func generatedMeasure(t *testing.T, body []byte, kind, encoding string, headers http.Header) (HTTPMeasurement, error) {
	t.Helper()
	gz, br := testMeasureEncodings(body)
	wire := body
	if encoding == "gzip" {
		wire = gz
	} else if encoding == "br" {
		wire = br
	}
	client := &http.Client{Transport: testRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: headers.Clone(), Body: io.NopCloser(bytes.NewReader(wire)), ContentLength: int64(len(wire))}, nil
	})}
	opts := HTTPMeasureOptions{Client: client, BaseURL: "https://example.invalid", URL: "/asset." + testMeasureHash(body)[:16] + ".js", Kind: kind,
		ExpectedBody: body, ExpectedSHA256: testMeasureHash(body), Representations: map[string][]byte{"gzip": gz, "br": br}}
	// Header semantics do not depend on the canonical compressor. Compute
	// the independent sizes once per measurement, rather than per redirect.
	sizes := assetmeasure.Sizes{Raw: int64(len(body)), SHA256: testMeasureHash(body)}
	return measureHTTP(context.Background(), opts, func([]byte) (assetmeasure.Sizes, error) { return sizes, nil })
}

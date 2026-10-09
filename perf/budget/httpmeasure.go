package budget

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/internal/assetmeasure"
	"m31labs.dev/gosx/internal/httpcache"
	"m31labs.dev/gosx/internal/httpcompress"
)

const maxMeasureBody = 64 << 20

// HTTPMeasureOptions are private fixture inputs. Representations contain exact
// release sidecars. ServingCompressors declares live HTML profiles only for
// encodings without a sidecar; it does not prescribe the compressed bytes.
type HTTPMeasureOptions struct {
	Client                             *http.Client
	BaseURL, URL, Kind, ExpectedSHA256 string
	ExpectedBody                       []byte
	Representations                    map[string][]byte
	HTMLFields                         []HTMLField
	ServingCompressors                 map[string]string
	Pin                                assetmeasure.CompressorPin
}

// HTTPMeasurement keeps served bytes separate from canonical normalization.
// Body, response headers and resolved URLs remain private intermediate data.
type HTTPMeasurement struct {
	Sizes          assetmeasure.Sizes
	RedirectSizes  []assetmeasure.Sizes
	WireBytes      int64
	Requests       int64
	Policies       []PolicyResult
	body           []byte
	header         http.Header
	finalURL       string
	finalWireBytes int64
	redirects      []httpRedirectResponse
}

type httpRedirectResponse struct {
	url       string
	sizes     assetmeasure.Sizes
	wireBytes int64
}

type bodyNormalizer func([]byte) (assetmeasure.Sizes, error)

func measureFailure(code, pointer string) error {
	return &InputError{Code: code, Reference: "measure", Pointer: pointer}
}

// MeasureHTTP verifies a body against its build identity and serving encoding.
// Redirect bodies and requests are included without automatic decompression.
func MeasureHTTP(ctx context.Context, opts HTTPMeasureOptions) (HTTPMeasurement, error) {
	if _, err := assetmeasure.Measure(nil, opts.Pin); err != nil {
		return HTTPMeasurement{}, measureFailure("noncanonical", "/pin")
	}
	return measureHTTP(ctx, opts, func(body []byte) (assetmeasure.Sizes, error) { return assetmeasure.Measure(body, opts.Pin) })
}

func measureHTTP(ctx context.Context, opts HTTPMeasureOptions, normalize bodyNormalizer) (HTTPMeasurement, error) {
	var out HTTPMeasurement
	base, err := url.Parse(opts.BaseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return out, measureFailure("invalid-input", "/base")
	}
	relative, err := url.Parse(opts.URL)
	if err != nil || relative.IsAbs() || relative.Host != "" || relative.RawQuery != "" || relative.Fragment != "" || !strings.HasPrefix(opts.URL, "/") || relative.Path != "/" && !safePath(strings.Trim(relative.Path, "/")) || relative.RawPath != "" {
		return out, measureFailure("invalid-input", "/url")
	}
	digest := sha256.Sum256(opts.ExpectedBody)
	if !shaPattern.MatchString(opts.ExpectedSHA256) || hex.EncodeToString(digest[:]) != opts.ExpectedSHA256 || len(opts.ExpectedBody) > maxMeasureBody {
		return out, measureFailure("wrong-fixture", "/body")
	}
	source := opts.Client
	if source == nil {
		source = http.DefaultClient
	}
	client := *source
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout == 0 {
		client.Timeout = 30 * time.Second
	}
	current := base.ResolveReference(relative)
	noCookie := true
	for hop := 0; hop <= 10; hop++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
		if err != nil {
			return out, measureFailure("invalid-input", "/request")
		}
		req.Header.Set("Accept-Encoding", "br, gzip")
		req.Header.Set("Cache-Control", "no-cache")
		response, err := client.Do(req)
		if err != nil {
			return out, measureFailure("environment", "/request")
		}
		wire, readErr := io.ReadAll(io.LimitReader(response.Body, maxMeasureBody+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(wire) > maxMeasureBody || response.Uncompressed || response.ContentLength >= 0 && response.ContentLength != int64(len(wire)) {
			return out, measureFailure("wrong-fixture", "/response")
		}
		noCookie = noCookie && len(response.Header.Values("Set-Cookie")) == 0
		// Repeated lines form an ordered coding list. The decoder accepts one
		// supported coding and rejects stacked encodings rather than ignoring them.
		encoding := httpcompress.ResponseEncoding(response.Header.Values("Content-Encoding"))
		raw, err := decodeServedBody(wire, encoding)
		if err != nil {
			return out, err
		}
		sizes, err := normalize(raw)
		if err != nil {
			return out, measureFailure("noncanonical", "/pin")
		}
		out.WireBytes += int64(len(wire))
		out.Requests++
		if response.StatusCode >= 300 && response.StatusCode <= 399 && response.StatusCode != http.StatusNotModified {
			out.RedirectSizes = append(out.RedirectSizes, sizes)
			// Location is a singleton; a comma can be part of its URI.
			if len(response.Header.Values("Location")) != 1 {
				return out, measureFailure("wrong-fixture", "/redirect")
			}
			out.redirects = append(out.redirects, httpRedirectResponse{url: current.String(), sizes: sizes, wireBytes: int64(len(wire))})
			location, err := response.Location()
			if err != nil || hop == 10 || location.Scheme != base.Scheme || location.Host != base.Host || location.User != nil || location.RawQuery != "" || location.Fragment != "" {
				return out, measureFailure("wrong-fixture", "/redirect")
			}
			current = location
			continue
		}
		matched := bytes.Equal(raw, opts.ExpectedBody) && sizes.SHA256 == opts.ExpectedSHA256
		if opts.Kind == "html" && len(opts.HTMLFields) != 0 {
			expected, expectedErr := measureHTML(opts.ExpectedBody, HTMLMeasureOptions{Fields: opts.HTMLFields}, normalize)
			served, servedErr := measureHTML(raw, HTMLMeasureOptions{Fields: opts.HTMLFields}, normalize)
			matched = expectedErr == nil && servedErr == nil && VerifyHTMLRenders(expected, served) == nil
			sizes = served.Sizes
		}
		if response.StatusCode != http.StatusOK || !matched {
			return out, measureFailure("wrong-fixture", "/body")
		}
		if encoding != "" && encoding != "identity" {
			representation, declared := opts.Representations[encoding]
			if declared {
				// A release sidecar must match exactly, even when a live profile
				// is also declared. A reconstruction cannot replace that artifact.
				if !bytes.Equal(wire, representation) || assetmeasure.VerifySidecar(raw, wire, encoding) != nil {
					return out, measureFailure("stale-sidecar", "/encoding")
				}
			} else if opts.Kind != "html" || !knownServingHTMLCompressor(encoding, opts.ServingCompressors[encoding]) {
				return out, measureFailure("stale-sidecar", "/encoding")
			}
			// Live streams have already passed bounded decoding and content
			// checks. Flush boundaries can change their encoded bytes and size.
		}
		// Content-Type is a singleton, not a list of alternative media types.
		contentTypes := response.Header.Values("Content-Type")
		if len(contentTypes) != 1 {
			return out, measureFailure("policy", "/contentType")
		}
		contentType, mimeErr := measureContentType(contentTypes[0])
		if mimeErr != nil || !measureMIME(opts.Kind, contentType) {
			return out, measureFailure("policy", "/contentType")
		}
		cache, validCache := httpcache.ParseDirectives(strings.Join(response.Header.Values("Cache-Control"), ","))
		if !validCache {
			return out, measureFailure("policy", "/cacheControl")
		}
		maxAge, uniqueMaxAge := cache.UniqueValue("max-age")
		// RFC 9111 §1.2.2: delta-seconds are decimal digits; leading zeros
		// do not change the declared lifetime. Repeated max-age stays stale.
		immutable := cache.Has("immutable") && uniqueMaxAge && strings.TrimLeft(maxAge, "0") == "31536000"
		out.Sizes = sizes
		out.body = raw
		out.header = response.Header.Clone()
		out.finalURL = current.String()
		out.finalWireBytes = int64(len(wire))
		out.Policies = []PolicyResult{{Name: "served-matches-build", Passed: true}, {Name: "no-cookie", Passed: noCookie},
			{Name: "assets-compressed", Passed: len(raw) == 0 || encoding == "gzip" || encoding == "br"}, {Name: "immutable-hashed", Passed: immutable && measureHashedPath(current.Path, opts.ExpectedSHA256)}}
		if opts.Kind == "wasm" {
			out.Policies = append(out.Policies, PolicyResult{Name: "wasm-streaming", Passed: contentType == "application/wasm"})
		}
		if opts.Kind == "html" {
			if err := VerifyHTMLNonces(raw, strings.Join(response.Header.Values("Content-Security-Policy"), ", ")); err != nil {
				return out, err
			}
			out.Policies[2].Name = "html-compressed"
			out.Policies[3] = PolicyResult{Name: "html-shareable", Passed: !cache.Has("private") && !cache.Has("no-store") && noCookie}
		}
		return out, nil
	}
	return out, measureFailure("wrong-fixture", "/redirect")
}

func decodeServedBody(wire []byte, encoding string) ([]byte, error) {
	input := bytes.NewReader(wire)
	var reader io.Reader = input
	switch encoding {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(input)
		if err != nil {
			return nil, measureFailure("stale-sidecar", "/encoding")
		}
		defer gz.Close()
		gz.Multistream(false)
		reader = gz
	case "br":
		reader = brotli.NewReader(input)
	default:
		return nil, measureFailure("policy", "/encoding")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxMeasureBody+1))
	if err != nil || len(raw) > maxMeasureBody || input.Len() != 0 {
		return nil, measureFailure("stale-sidecar", "/encoding")
	}
	return raw, nil
}

// RFC 9110 §5.6.6 permits empty semicolon-delimited parameters. Drop only
// those empty members before using the MIME parser; quoted semicolons and
// escaped quotes remain part of their original parameter values.
func measureContentType(value string) (string, error) {
	var parts []string
	start, quoted, escaped := 0, false, false
	for i := 0; i <= len(value); i++ {
		if i < len(value) {
			c := value[i]
			if escaped {
				escaped = false
				continue
			}
			if quoted && c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				quoted = !quoted
				continue
			}
			if c != ';' || quoted {
				continue
			}
		}
		part := strings.Trim(value[start:i], " \t")
		if part != "" || start == 0 {
			parts = append(parts, part)
		}
		start = i + 1
	}
	mediaType, _, err := mime.ParseMediaType(strings.Join(parts, ";"))
	return mediaType, err
}

func measureMIME(kind, mediaType string) bool {
	switch kind {
	case "html":
		return mediaType == "text/html"
	case "js":
		return mediaType == "text/javascript" || mediaType == "application/javascript"
	case "wasm":
		return mediaType == "application/wasm"
	case "css":
		return mediaType == "text/css"
	case "model":
		return mediaType == "model/gltf-binary" || mediaType == "model/gltf+json" || mediaType == "application/octet-stream" || mediaType == "application/json"
	case "program", "other":
		return mediaType == "application/octet-stream" || mediaType == "application/json"
	case "font":
		return mediaType == "font/woff2" || mediaType == "font/woff" || mediaType == "application/font-woff"
	case "image":
		return strings.HasPrefix(mediaType, "image/")
	case "video":
		return strings.HasPrefix(mediaType, "video/") || mediaType == "application/vnd.apple.mpegurl"
	}
	return false
}

func measureHashedPath(assetPath, sha string) bool {
	parts := strings.Split(path.Base(assetPath), ".")
	if len(parts) < 2 {
		return false
	}
	hash := parts[len(parts)-2]
	return (len(hash) == 16 || len(hash) == 64) && strings.HasPrefix(sha, hash)
}

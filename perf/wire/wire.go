// Package wire measures what a page costs over the network before any script
// runs: the document, every resource the HTML asks the browser to load, and
// every runtime asset the GoSX manifest tells the bootstrap to fetch.
//
// The measurement is deterministic. It needs no browser, so CI can gate bytes,
// request counts, compression and cache headers on every pull request, and a
// regression is a real regression rather than scheduler noise.
package wire

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/andybalholm/brotli"
	"golang.org/x/net/html"
)

// MobileUserAgent is sent with every request. It is the Lighthouse moto g
// power profile's user agent, so servers that vary on the user agent answer
// the way they answer a mid-range phone.
const MobileUserAgent = "Mozilla/5.0 (Linux; Android 11; moto g power (2022)) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36"

// AcceptEncoding matches what current browsers advertise.
const AcceptEncoding = "br, gzip"

// Resource kinds.
const (
	KindDocument = "html"
	KindScript   = "js"
	KindStyle    = "css"
	KindWASM     = "wasm"
	KindData     = "data"
	KindProgram  = "program"
	KindImage    = "image"
	KindFont     = "font"
	// KindLazyScript is a runtime chunk the page advertises for on-demand
	// loading (a data-gosx-*-url attribute naming a /gosx/ asset). Which
	// ones load depends on the device and the scene, so they are counted in
	// their own limit and in the policies, not in totals or requests.
	KindLazyScript = "lazy-js"
	// KindRedirect is a 3xx hop on the way to a document or resource.
	KindRedirect = "redirect"
	KindOther    = "other"
)

// Resource is one network response.
type Resource struct {
	URL             string `json:"url"`
	Kind            string `json:"kind"`
	Initiator       string `json:"initiator"`
	Status          int    `json:"status"`
	WireBytes       int64  `json:"wireBytes"`
	DecodedBytes    int64  `json:"decodedBytes"`
	ContentEncoding string `json:"contentEncoding,omitempty"`
	CacheControl    string `json:"cacheControl,omitempty"`
	Immutable       bool   `json:"immutable"`
	Hashed          bool   `json:"hashed"`
	SetCookie       bool   `json:"setCookie,omitempty"`
}

// Route is the measurement of one page.
type Route struct {
	App   string `json:"app"`
	Route string `json:"route"`
	URL   string `json:"url"`

	Document  Resource   `json:"document"`
	Resources []Resource `json:"resources"`

	// Requests counts the document and every distinct subresource.
	Requests int `json:"requests"`
	// WireBytes sums bytes on the wire by resource kind.
	WireBytes map[string]int64 `json:"wireBytes"`
	// TotalWireBytes sums every response body as transferred.
	TotalWireBytes int64 `json:"totalWireBytes"`
	// FrameworkJSWireBytes counts wire bytes of JavaScript and WASM served
	// from the framework's /gosx/ prefix plus inline executable script
	// (inline bytes travel inside the document, so they are counted at their
	// share of the document's compression ratio).
	FrameworkJSWireBytes int64 `json:"frameworkJsWireBytes"`

	// LazyWireBytes sums the on-demand runtime chunks the page advertises
	// (KindLazyScript). They are not in TotalWireBytes or Requests.
	LazyWireBytes int64 `json:"lazyWireBytes"`

	InlineScriptBytes int64 `json:"inlineScriptBytes"`
	InlineScriptMax   int64 `json:"inlineScriptMax"`
	InlineDataBytes   int64 `json:"inlineDataBytes"`
}

// Options control a crawl.
type Options struct {
	Client *http.Client
	// UserAgent overrides MobileUserAgent.
	UserAgent string
}

var hashedSegment = regexp.MustCompile(`[.-][0-9a-fA-F]{8,}[.-]|[.-][0-9a-fA-F]{8,}$|/[0-9a-fA-F]{16,}/`)

// IsHashedURL reports whether the URL path carries a content hash: a run of at
// least 8 hex digits delimited by '.' or '-', or a directory named by a hash.
// A query string such as ?v=123 does not count: caches and servers are free to
// ignore it, and the GoSX runtime server serves the same bytes with or without
// it.
func IsHashedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	p := u.Path
	base := path.Base(p)
	if ext := path.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
		// allow name.<hash>.min.js style: strip one more extension
		if ext2 := path.Ext(base); ext2 != "" && !hashedSegment.MatchString(ext2+".") {
			base = strings.TrimSuffix(base, ext2)
		}
	}
	return hashedSegment.MatchString(base) || hashedSegment.MatchString(path.Dir(p)+"/")
}

// Crawl fetches the page at base+route and every resource on its load path.
func Crawl(ctx context.Context, opts Options, app, base, route string) (Route, error) {
	client := opts.Client
	if client == nil {
		client = &http.Client{}
	}
	// Measure the wire: never let the transport decode for us, and follow
	// redirects by hand so each hop is counted.
	c := *client
	if t, ok := c.Transport.(*http.Transport); ok {
		t = t.Clone()
		t.DisableCompression = true
		c.Transport = t
	} else if c.Transport == nil {
		c.Transport = &http.Transport{DisableCompression: true}
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &c
	ua := opts.UserAgent
	if ua == "" {
		ua = MobileUserAgent
	}

	pageURL, err := url.Parse(strings.TrimRight(base, "/") + route)
	if err != nil {
		return Route{}, err
	}
	out := Route{App: app, Route: route, URL: pageURL.String(), WireBytes: map[string]int64{}}

	doc, docHops, body, finalURL, err := fetch(ctx, client, ua, pageURL.String(), "navigation")
	if err != nil {
		return Route{}, err
	}
	doc.Kind = KindDocument
	out.Document = doc
	out.Requests = 1
	out.WireBytes[KindDocument] += doc.WireBytes
	out.TotalWireBytes += doc.WireBytes
	add := func(hops []Resource) {
		for _, h := range hops {
			out.Resources = append(out.Resources, h)
			out.Requests++
			out.WireBytes[h.Kind] += h.WireBytes
			out.TotalWireBytes += h.WireBytes
		}
	}
	add(docHops)

	refs, inline, err := scanHTML(body)
	if err != nil {
		return Route{}, fmt.Errorf("%s: parse html: %w", pageURL, err)
	}
	out.InlineScriptBytes = inline.scriptBytes
	out.InlineScriptMax = inline.scriptMax
	out.InlineDataBytes = inline.dataBytes

	seen := map[string]bool{}
	var pending []ref
	pending = append(pending, refs...)
	for len(pending) > 0 {
		r := pending[0]
		pending = pending[1:]
		abs, err := finalURL.Parse(r.href)
		if err != nil || (abs.Scheme != "http" && abs.Scheme != "https") {
			continue
		}
		abs.Fragment = ""
		key := abs.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		res, hops, _, _, err := fetch(ctx, client, ua, key, r.initiator)
		add(hops)
		if err != nil {
			return Route{}, err
		}
		res.Kind = classify(abs.Path, res, r.kind)
		if r.kind == KindLazyScript {
			res.Kind = KindLazyScript
			out.Resources = append(out.Resources, res)
			out.LazyWireBytes += res.WireBytes
			continue
		}
		out.Resources = append(out.Resources, res)
		out.Requests++
		out.WireBytes[res.Kind] += res.WireBytes
		out.TotalWireBytes += res.WireBytes
		if isFramework(abs.Path) && (res.Kind == KindScript || res.Kind == KindWASM || res.Kind == KindProgram) {
			out.FrameworkJSWireBytes += res.WireBytes
		}
	}
	if out.InlineScriptBytes > 0 && doc.DecodedBytes > 0 {
		out.FrameworkJSWireBytes += out.InlineScriptBytes * doc.WireBytes / doc.DecodedBytes
	}
	sort.SliceStable(out.Resources, func(i, j int) bool { return out.Resources[i].URL < out.Resources[j].URL })
	return out, nil
}

func isFramework(p string) bool {
	return strings.HasPrefix(p, "/gosx/") || strings.Contains(p, "/_gosx/")
}

func classify(p string, res Resource, hint string) string {
	ext := strings.ToLower(path.Ext(p))
	switch {
	case ext == ".wasm":
		return KindWASM
	case ext == ".js" || ext == ".mjs":
		return KindScript
	case ext == ".css":
		return KindStyle
	case ext == ".gxi" || ext == ".gxb":
		return KindProgram
	case ext == ".woff2" || ext == ".woff" || ext == ".ttf" || ext == ".otf":
		return KindFont
	case ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".avif" || ext == ".gif" || ext == ".svg" || ext == ".ktx2":
		return KindImage
	case ext == ".json" || ext == ".bin":
		return KindData
	}
	if hint != "" {
		return hint
	}
	return KindOther
}

type ref struct {
	href      string
	kind      string
	initiator string
}

type inlineStats struct {
	scriptBytes int64
	scriptMax   int64
	dataBytes   int64
}

func scanHTML(body []byte) ([]ref, inlineStats, error) {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, inlineStats{}, err
	}
	var refs []ref
	var stats inlineStats
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if strings.HasPrefix(a.Key, "data-gosx-") && strings.HasSuffix(a.Key, "-url") && strings.HasPrefix(a.Val, "/gosx/") {
					refs = append(refs, ref{href: a.Val, kind: KindLazyScript, initiator: "on-demand"})
				}
			}
			switch n.Data {
			case "script":
				src := attr(n, "src")
				typ := strings.ToLower(strings.TrimSpace(attr(n, "type")))
				text := textOf(n)
				switch {
				case src != "" && (typ == "" || typ == "module" || strings.Contains(typ, "javascript")):
					refs = append(refs, ref{href: src, kind: KindScript, initiator: "script"})
				case src == "" && (typ == "" || typ == "module" || strings.Contains(typ, "javascript")):
					size := int64(len(text))
					stats.scriptBytes += size
					if size > stats.scriptMax {
						stats.scriptMax = size
					}
				case src == "" && strings.Contains(typ, "json"):
					stats.dataBytes += int64(len(text))
					if attr(n, "id") == "gosx-manifest" {
						refs = append(refs, manifestRefs(text)...)
					}
				}
			case "img":
				// Eager images load with the page. A lazy image loads only
				// near the viewport, so it is not on the initial load path.
				// Only src is followed (not srcset) so the measurement does
				// not depend on a viewport width.
				if src := attr(n, "src"); src != "" && !strings.EqualFold(strings.TrimSpace(attr(n, "loading")), "lazy") && !strings.HasPrefix(src, "data:") {
					refs = append(refs, ref{href: src, kind: KindImage, initiator: "img"})
				}
			case "link":
				rels := strings.Fields(strings.ToLower(attr(n, "rel")))
				href := attr(n, "href")
				if href == "" {
					break
				}
				for _, rel := range rels {
					switch rel {
					case "stylesheet":
						refs = append(refs, ref{href: href, kind: KindStyle, initiator: "stylesheet"})
					case "modulepreload":
						refs = append(refs, ref{href: href, kind: KindScript, initiator: "modulepreload"})
					case "preload":
						kind := KindOther
						switch strings.ToLower(attr(n, "as")) {
						case "script":
							kind = KindScript
						case "style":
							kind = KindStyle
						case "fetch":
							kind = KindData
						case "image":
							kind = KindImage
						case "font":
							kind = KindFont
						}
						// Fonts and images are content, not framework cost;
						// they still count as requests and bytes.
						refs = append(refs, ref{href: href, kind: kind, initiator: "preload"})
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return refs, stats, nil
}

// manifestRefs returns the framework assets named by a GoSX page manifest:
// the runtime WASM, bundles, island programs and engine chunks. The bootstrap
// fetches these after it starts, so they are on the page's load path even
// though the HTML does not reference them directly. Every string under a key
// named path, or ending in Ref or Path, whose value is a /gosx/ URL counts.
func manifestRefs(text string) []ref {
	var doc any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		return nil
	}
	var out []ref
	var walk func(key string, v any)
	walk = func(key string, v any) {
		switch t := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(k, t[k])
			}
		case []any:
			for _, item := range t {
				walk(key, item)
			}
		case string:
			if (key == "path" || strings.HasSuffix(key, "Ref") || strings.HasSuffix(key, "Path")) && strings.HasPrefix(t, "/gosx/") {
				out = append(out, ref{href: t, initiator: "manifest"})
			}
		}
	}
	walk("", doc)
	return out
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

// fetch follows up to 10 redirects by hand, so every hop is measured: each
// redirect response is returned in hops with its bytes, cookies and cache
// headers, and counts as a request.
func fetch(ctx context.Context, client *http.Client, ua, raw, initiator string) (Resource, []Resource, []byte, *url.URL, error) {
	var hops []Resource
	current := raw
	for hop := 0; ; hop++ {
		res, body, reqURL, location, err := fetchOnce(ctx, client, ua, current, initiator)
		if err != nil {
			return Resource{}, hops, nil, nil, err
		}
		if location == nil {
			if res.Status != http.StatusOK {
				return res, hops, body, reqURL, fmt.Errorf("GET %s: status %d", current, res.Status)
			}
			return res, hops, body, reqURL, nil
		}
		if hop >= 10 {
			return Resource{}, hops, nil, nil, fmt.Errorf("GET %s: more than 10 redirects", raw)
		}
		res.Kind = KindRedirect
		hops = append(hops, res)
		current = reqURL.ResolveReference(location).String()
	}
}

func fetchOnce(ctx context.Context, client *http.Client, ua, raw, initiator string) (Resource, []byte, *url.URL, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return Resource{}, nil, nil, nil, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept-Encoding", AcceptEncoding)
	if initiator == "navigation" {
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Site", "none")
	}
	resp, err := client.Do(req)
	if err != nil {
		return Resource{}, nil, nil, nil, fmt.Errorf("GET %s: %w", raw, err)
	}
	defer resp.Body.Close()
	wire, err := io.ReadAll(resp.Body)
	if err != nil {
		return Resource{}, nil, nil, nil, fmt.Errorf("GET %s: read: %w", raw, err)
	}
	enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	decoded, err := decode(enc, wire)
	if err != nil {
		return Resource{}, nil, nil, nil, fmt.Errorf("GET %s: decode %s: %w", raw, enc, err)
	}
	cc := resp.Header.Get("Cache-Control")
	res := Resource{
		URL:             req.URL.RequestURI(),
		Initiator:       initiator,
		Status:          resp.StatusCode,
		WireBytes:       int64(len(wire)),
		DecodedBytes:    int64(len(decoded)),
		ContentEncoding: enc,
		CacheControl:    cc,
		Immutable:       strings.Contains(strings.ToLower(cc), "immutable"),
		Hashed:          IsHashedURL(req.URL.String()),
		SetCookie:       len(resp.Header.Values("Set-Cookie")) > 0,
	}
	var location *url.URL
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		loc := resp.Header.Get("Location")
		if loc == "" {
			return Resource{}, nil, nil, nil, fmt.Errorf("GET %s: status %d without Location", raw, resp.StatusCode)
		}
		location, err = url.Parse(loc)
		if err != nil {
			return Resource{}, nil, nil, nil, fmt.Errorf("GET %s: bad Location %q: %w", raw, loc, err)
		}
	}
	return res, decoded, req.URL, location, nil
}

func decode(enc string, body []byte) ([]byte, error) {
	switch enc {
	case "", "identity":
		return body, nil
	case "br":
		return io.ReadAll(brotli.NewReader(bytes.NewReader(body)))
	case "gzip":
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return io.ReadAll(zr)
	default:
		return nil, fmt.Errorf("unsupported content encoding %q", enc)
	}
}

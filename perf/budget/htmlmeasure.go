package budget

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"io"
	"mime"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"m31labs.dev/gosx/internal/assetmeasure"
)

// HTMLField declares a transient attribute whose value is normalized. Every
// other byte, including asset hashes and executable content, is retained.
type HTMLField struct{ Element, Attribute string }
type HTMLMeasureOptions struct {
	Fields                []HTMLField
	FrameworkScriptSHA256 []string
	Pin                   assetmeasure.CompressorPin
}
type HTMLMeasurement struct {
	Sizes              assetmeasure.Sizes
	Framework          SizeTriple
	App                SizeTriple
	InlineAppScriptMax int64
	ExecutableScripts  int64
	full               []byte
	withoutFramework   []byte
}

// MeasureHTML uses complete recompressed documents for inline ownership; it
// never assigns a script a proportional share of document compression.
func MeasureHTML(body []byte, opts HTMLMeasureOptions) (HTMLMeasurement, error) {
	return measureHTML(body, opts, func(data []byte) (assetmeasure.Sizes, error) { return assetmeasure.Measure(data, opts.Pin) })
}
func measureHTML(body []byte, opts HTMLMeasureOptions, normalize bodyNormalizer) (HTMLMeasurement, error) {
	var result HTMLMeasurement
	if len(body) > 16<<20 || !utf8.Valid(body) {
		return result, measureFailure("wrong-fixture", "/html")
	}
	fields := map[string]bool{}
	for _, field := range opts.Fields {
		valid := field.Attribute == "nonce" && (field.Element == "script" || field.Element == "style" || field.Element == "link") ||
			(field.Attribute == "data-gosx-session" || field.Attribute == "data-gosx-build-timestamp") && (field.Element == "body" || field.Element == "html" || field.Element == "meta")
		key := field.Element + "|" + field.Attribute
		if !valid || fields[key] {
			return result, measureFailure("invalid-input", "/fields")
		}
		fields[key] = true
	}
	owned := map[string]bool{}
	for _, hash := range opts.FrameworkScriptSHA256 {
		if !shaPattern.MatchString(hash) || owned[hash] {
			return result, measureFailure("invalid-input", "/frameworkScripts")
		}
		owned[hash] = true
	}
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	var full, remaining, script bytes.Buffer
	active, executable := false, false
	templates := 0
	for {
		kind := tokenizer.Next()
		raw := append([]byte(nil), tokenizer.Raw()...)
		if kind == html.ErrorToken {
			if tokenizer.Err() != io.EOF || active {
				return result, measureFailure("wrong-fixture", "/html")
			}
			break
		}
		if active {
			if kind != html.EndTagToken {
				script.Write(raw)
				continue
			}
			token := tokenizer.Token()
			if token.Data != "script" {
				return result, measureFailure("wrong-fixture", "/html")
			}
			full.Write(script.Bytes())
			hash := sha256.Sum256(script.Bytes())
			framework := executable && owned[hex.EncodeToString(hash[:])]
			if !framework {
				remaining.Write(script.Bytes())
			}
			if executable {
				result.ExecutableScripts++
				if !framework && int64(script.Len()) > result.InlineAppScriptMax {
					result.InlineAppScriptMax = int64(script.Len())
				}
			}
			full.Write(raw)
			remaining.Write(raw)
			active = false
			script.Reset()
			continue
		}
		if kind == html.StartTagToken || kind == html.SelfClosingTagToken {
			token := tokenizer.Token()
			seen := map[string]bool{}
			attributes := map[string]string{}
			for _, attribute := range token.Attr {
				if seen[attribute.Key] {
					return result, measureFailure("wrong-fixture", "/html/attributes")
				}
				seen[attribute.Key] = true
				attributes[attribute.Key] = attribute.Val
				if fields[token.Data+"|"+attribute.Key] {
					raw = rewriteHTMLAttribute(raw, attribute.Key)
				}
			}
			if token.Data == "template" && kind == html.StartTagToken {
				templates++
			}
			if token.Data == "script" {
				active = true
				executable = templates == 0 && attributes["src"] == "" && executableScriptType(attributes["type"])
				if templates == 0 && attributes["src"] != "" && executableScriptType(attributes["type"]) {
					result.ExecutableScripts++
				}
			}
		} else if kind == html.EndTagToken {
			token := tokenizer.Token()
			if token.Data == "template" && templates > 0 {
				templates--
			}
		}
		full.Write(raw)
		remaining.Write(raw)
	}
	result.full = full.Bytes()
	result.withoutFramework = remaining.Bytes()
	sizes, err := normalize(result.full)
	if err != nil {
		return result, measureFailure("noncanonical", "/pin")
	}
	app, err := normalize(result.withoutFramework)
	if err != nil {
		return result, measureFailure("noncanonical", "/pin")
	}
	marginal := func(total, remainder int64) int64 {
		if total > remainder {
			return total - remainder
		}
		return 0
	}
	result.Sizes = sizes
	result.Framework = SizeTriple{Raw: marginal(sizes.Raw, app.Raw), Gzip: marginal(sizes.Gzip, app.Gzip), Brotli: marginal(sizes.Brotli, app.Brotli)}
	result.App = SizeTriple{Raw: sizes.Raw - result.Framework.Raw, Gzip: sizes.Gzip - result.Framework.Gzip, Brotli: sizes.Brotli - result.Framework.Brotli}
	return result, nil
}

// Locate raw attribute value spans after structural token decoding, preserving
// quoting, whitespace and every undeclared byte rather than re-rendering HTML.
func rewriteHTMLAttribute(raw []byte, name string) []byte {
	space := func(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' }
	i := 1
	for i < len(raw) && !space(raw[i]) && raw[i] != '>' {
		i++
	}
	for i < len(raw) {
		for i < len(raw) && (space(raw[i]) || raw[i] == '/') {
			i++
		}
		start := i
		for i < len(raw) && !space(raw[i]) && raw[i] != '=' && raw[i] != '>' {
			i++
		}
		attribute := string(raw[start:i])
		for i < len(raw) && space(raw[i]) {
			i++
		}
		if i >= len(raw) || raw[i] != '=' {
			if i < len(raw) && raw[i] == '>' {
				break
			}
			continue
		}
		i++
		for i < len(raw) && space(raw[i]) {
			i++
		}
		if i >= len(raw) {
			break
		}
		quote := byte(0)
		if raw[i] == '\'' || raw[i] == '"' {
			quote = raw[i]
			i++
		}
		valueStart := i
		for i < len(raw) && (quote != 0 && raw[i] != quote || quote == 0 && !space(raw[i]) && raw[i] != '>') {
			i++
		}
		if strings.EqualFold(attribute, name) {
			replacement := []byte("gosx-normalized")
			out := append([]byte(nil), raw[:valueStart]...)
			out = append(out, replacement...)
			return append(out, raw[i:]...)
		}
		if quote != 0 && i < len(raw) {
			i++
		}
	}
	return raw
}

// VerifyHTMLRenders rejects nondeterministic content after declared structural
// normalization. This checks the full document, not only a script inventory.
func VerifyHTMLRenders(first, second HTMLMeasurement) error {
	if first.Sizes.SHA256 != second.Sizes.SHA256 || !bytes.Equal(first.full, second.full) {
		return measureFailure("wrong-fixture", "/html/renders")
	}
	return nil
}

func executableScriptType(typ string) bool {
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ != "" && typ != "module" {
		mediaType, _, err := mime.ParseMediaType(typ)
		if err != nil {
			return false
		}
		typ = mediaType
	}
	switch typ {
	case "", "module", "application/javascript", "application/ecmascript", "application/x-javascript", "application/x-ecmascript", "text/javascript", "text/ecmascript", "text/jscript", "text/livescript", "text/x-javascript", "text/x-ecmascript", "text/javascript1.0", "text/javascript1.1", "text/javascript1.2", "text/javascript1.3", "text/javascript1.4", "text/javascript1.5":
		return true
	}
	return false
}

// VerifyHTMLNonces verifies that nonce-bearing elements are allowed by each
// enforced CSP policy. Inline elements can also be authorized by a matching
// content hash or an effective unsafe-inline source. Nonce and hash values
// remain private and are never included in an error or report.
func VerifyHTMLNonces(body []byte, csp string) error {
	var policies []map[string][]string
	// CSP field values may themselves contain comma-separated policies.
	for _, policy := range strings.Split(csp, ",") {
		directives := map[string][]string{}
		for _, part := range strings.Split(policy, ";") {
			fields := strings.FieldsFunc(part, cspSpace)
			if len(fields) > 0 {
				// CSP3 §2.2.1: ASCII-fold names before checking duplicates;
				// the first occurrence of a directive wins.
				name := cspLower(fields[0])
				if _, exists := directives[name]; exists {
					continue
				}
				directives[name] = fields[1:]
			}
		}
		policies = append(policies, directives)
	}
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	templates := 0
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			if tokenizer.Err() != io.EOF {
				return measureFailure("wrong-fixture", "/html")
			}
			return nil
		}
		token := tokenizer.Token()
		if token.Data == "template" {
			if kind == html.StartTagToken {
				templates++
			}
			if kind == html.EndTagToken && templates > 0 {
				templates--
			}
		}
		if templates > 0 || kind != html.StartTagToken && kind != html.SelfClosingTagToken || token.Data != "script" && token.Data != "style" {
			continue
		}
		var nonce string
		hasNonce, external := false, false
		for _, attribute := range token.Attr {
			if attribute.Key == "nonce" && !hasNonce {
				nonce, hasNonce = attribute.Val, true
			}
			external = external || token.Data == "script" && attribute.Key == "src"
		}
		if !hasNonce {
			continue
		}
		if nonce == "" {
			return measureFailure("policy", "/html/nonce")
		}
		var source strings.Builder
		if kind == html.StartTagToken {
			// Hash the parsed element text, not HTML source bytes. HTML
			// parsing normalizes CR/CRLF to LF before CSP3 §6.7.3.3.
			for next := tokenizer.Next(); next != html.ErrorToken; next = tokenizer.Next() {
				child := tokenizer.Token()
				if next == html.EndTagToken && child.Data == token.Data {
					break
				}
				if next == html.TextToken {
					source.WriteString(child.Data)
				}
			}
		}
		key, fallback := "script-src-elem", "script-src"
		if token.Data == "style" {
			key, fallback = "style-src-elem", "style-src"
		}
		bound := false
		for _, directives := range policies {
			allowed, found := directives[key]
			if !found {
				allowed, found = directives[fallback]
			}
			if !found {
				allowed, found = directives["default-src"]
			}
			// An unrelated policy does not restrict this element; another
			// enforced policy must still provide an applicable directive.
			if !found {
				continue
			}
			bound = true
			if !cspAllowsElement(allowed, token.Data, nonce, source.String(), external) {
				return measureFailure("policy", "/html/nonce")
			}
		}
		if !bound {
			return measureFailure("policy", "/html/nonce")
		}
	}
}

var cspSourcePattern = regexp.MustCompile(`^'([A-Za-z0-9]+)-([A-Za-z0-9+/_-]+={0,2})'$`)

// CSP3 §§6.7.3.2–6.7.3.3: nonce/hash sources suppress unsafe-inline;
// strict-dynamic suppresses it for scripts only. Payloads are never folded.
func cspAllowsElement(sources []string, element, nonce, text string, external bool) bool {
	unsafe, restricted, matches := false, false, false
	for _, source := range sources {
		switch cspLower(source) {
		case "'unsafe-inline'":
			unsafe = true
		case "'strict-dynamic'":
			restricted = restricted || element == "script"
		}
		parts := cspSourcePattern.FindStringSubmatch(source)
		if parts == nil {
			continue
		}
		algorithm, payload := cspLower(parts[1]), parts[2]
		var digest []byte
		switch algorithm {
		case "nonce":
			restricted = true
			matches = matches || payload == nonce
			continue
		case "sha256":
			d := sha256.Sum256([]byte(text))
			digest = d[:]
		case "sha384":
			d := sha512.Sum384([]byte(text))
			digest = d[:]
		case "sha512":
			d := sha512.Sum512([]byte(text))
			digest = d[:]
		default:
			continue
		}
		restricted = true
		// External scripts need their fetched content and integrity metadata;
		// an inline text hash or unsafe-inline cannot authorize that fetch.
		payload = strings.NewReplacer("-", "+", "_", "/").Replace(payload)
		matches = matches || !external && base64.StdEncoding.EncodeToString(digest) == payload
	}
	return matches || !external && unsafe && !restricted
}

func cspSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' }
func cspLower(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, s)
}

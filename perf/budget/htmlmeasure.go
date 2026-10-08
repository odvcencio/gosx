package budget

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
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
	Sizes                 assetmeasure.Sizes
	Framework             SizeTriple
	App                   SizeTriple
	InlineAppScriptMax    int64
	ExecutableScripts     int64
	SyncExecutableScripts int64
	full                  []byte
	withoutFramework      []byte
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
				if templates == 0 && executableScriptType(attributes["type"]) && strings.TrimSpace(strings.ToLower(attributes["type"])) != "module" && (attributes["src"] == "" || !seen["defer"] && !seen["async"]) {
					result.SyncExecutableScripts++
				}
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

// VerifyHTMLNonces binds declared nonce attributes to their served CSP directive.
// Nonce values remain private and are never included in an error or report.
func VerifyHTMLNonces(body []byte, csp string) error {
	directives := map[string][]string{}
	for _, part := range strings.Split(csp, ";") {
		fields := strings.Fields(part)
		if len(fields) > 0 {
			if _, exists := directives[fields[0]]; exists {
				return measureFailure("policy", "/html/nonce")
			}
			directives[fields[0]] = fields[1:]
		}
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
		for _, attribute := range token.Attr {
			if attribute.Key != "nonce" {
				continue
			}
			key := "script-src-elem"
			fallback := "script-src"
			if token.Data == "style" {
				key, fallback = "style-src-elem", "style-src"
			}
			allowed, found := directives[key]
			if !found {
				allowed, found = directives[fallback]
			}
			if !found {
				allowed = directives["default-src"]
			}
			matches := false
			for _, source := range allowed {
				matches = matches || source == "'nonce-"+attribute.Val+"'"
			}
			if attribute.Val == "" || !matches {
				return measureFailure("policy", "/html/nonce")
			}
		}
	}
}

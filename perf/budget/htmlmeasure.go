package budget

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"golang.org/x/net/html"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"m31labs.dev/gosx/internal/assetmeasure"
	"m31labs.dev/gosx/internal/pagecaps"
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
	full                  []byte
	withoutFramework      []byte
	SyncExecutableScripts int64
	ExecutableSources     int64
	InlineAppScriptBytes  int64
	capabilities          pagecaps.Capabilities
	inlineFramework       bool
}

func (m HTMLMeasurement) executionCounts() HTMLExecution {
	return HTMLExecution{ExecutableSources: m.ExecutableSources, ExecutableScripts: m.ExecutableScripts,
		SyncExecutableScripts: m.SyncExecutableScripts, InlineAppScriptMax: m.InlineAppScriptMax,
		InlineAppScriptBytes: m.InlineAppScriptBytes, inlineFramework: m.inlineFramework}
}

// HTMLExecution records active document execution, including srcdoc. Sources
// include script elements, event handlers and executable URLs. The legacy Script
// byte fields include all inline code except verified framework script bodies;
// Max is per executable source, Bytes is their sum.
type HTMLExecution struct {
	ExecutableSources, ExecutableScripts, SyncExecutableScripts int64
	InlineAppScriptMax, InlineAppScriptBytes                    int64
	// Verified framework presence is independent of compressed marginal bytes.
	inlineFramework bool
}

func (total *HTMLExecution) include(other HTMLExecution) {
	total.ExecutableSources += other.ExecutableSources
	total.ExecutableScripts += other.ExecutableScripts
	total.SyncExecutableScripts += other.SyncExecutableScripts
	total.InlineAppScriptBytes += other.InlineAppScriptBytes
	total.InlineAppScriptMax = max(total.InlineAppScriptMax, other.InlineAppScriptMax)
	total.inlineFramework = total.inlineFramework || other.inlineFramework
}

// union retains the permitted observation when verified aliases share one
// physical document. The same body is counted once even if both uses permit it.
func (total *HTMLExecution) union(other HTMLExecution) {
	total.ExecutableSources = max(total.ExecutableSources, other.ExecutableSources)
	total.ExecutableScripts = max(total.ExecutableScripts, other.ExecutableScripts)
	total.SyncExecutableScripts = max(total.SyncExecutableScripts, other.SyncExecutableScripts)
	total.InlineAppScriptBytes = max(total.InlineAppScriptBytes, other.InlineAppScriptBytes)
	total.InlineAppScriptMax = max(total.InlineAppScriptMax, other.InlineAppScriptMax)
	total.inlineFramework = total.inlineFramework || other.inlineFramework
}

func (total *HTMLExecution) observe(source pagecaps.ExecutableSource, owned map[string]bool) {
	total.ExecutableSources++
	if source.Script {
		total.ExecutableScripts++
		if source.Synchronous {
			total.SyncExecutableScripts++
		}
		if !source.Inline {
			return
		}
		hash := sha256.Sum256(source.Body)
		if source.ExactBody && owned[hex.EncodeToString(hash[:])] {
			total.inlineFramework = true
			return
		}
	}
	size := int64(len(source.Body))
	total.InlineAppScriptMax = max(total.InlineAppScriptMax, size)
	total.InlineAppScriptBytes += size
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
	classified, err := classifyHTML(body)
	if err != nil {
		return result, err
	}
	tree, err := pagecaps.ParseDocumentTree(body, "", nil)
	if err != nil {
		return result, measureFailure("capability", "/html")
	}
	var execution HTMLExecution
	caps, err := inspectExecutionTree(tree, func(_ *pagecaps.Document, source pagecaps.ExecutableSource) { execution.observe(source, owned) })
	if err != nil {
		return result, err
	}
	result.capabilities = caps
	result.ExecutableSources = execution.ExecutableSources
	result.ExecutableScripts = execution.ExecutableScripts
	result.SyncExecutableScripts = execution.SyncExecutableScripts
	result.InlineAppScriptMax = execution.InlineAppScriptMax
	result.InlineAppScriptBytes = execution.InlineAppScriptBytes
	result.inlineFramework = execution.inlineFramework
	var edits []htmlSourceEdit
	for _, item := range classified.starts {
		raw := body[item.start:item.end]
		for _, attr := range item.token.Attr {
			if fields[item.token.Data+"|"+attr.Key] {
				raw = rewriteHTMLAttribute(raw, attr.Key)
			}
		}
		if !bytes.Equal(raw, body[item.start:item.end]) {
			edits = append(edits, htmlSourceEdit{item.start, item.end, raw})
		}
	}
	// Remove only verified executable bodies from the complete raw document.
	// Source order is independent of tree order (e.g. table foster parenting).
	var framework []htmlSourceEdit
	for _, element := range classified.elements {
		if !element.executable {
			continue
		}
		if element.external {
			continue
		}
		source := body[element.bodyStart:element.bodyEnd]
		hash := sha256.Sum256(source)
		if owned[hex.EncodeToString(hash[:])] {
			framework = append(framework, htmlSourceEdit{element.bodyStart, element.bodyEnd, nil})

		}
	}
	// Both documents apply tree-associated start-tag edits to original source
	// offsets. Removing a verified body also removes any edits inside that body.
	result.full = applyHTMLSourceEdits(body, edits)
	result.withoutFramework = applyHTMLSourceEdits(body, append(framework, edits...))
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

type htmlSourceEdit struct {
	start, end int
	value      []byte
}

func applyHTMLSourceEdits(body []byte, edits []htmlSourceEdit) []byte {
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start < edits[j].start
		}
		return edits[i].end > edits[j].end
	})
	var result bytes.Buffer
	offset := 0
	for _, edit := range edits {
		if edit.start < offset {
			// Tree-associated tags and script bodies are nested or disjoint.
			// The containing removed body takes precedence over nested edits.
			continue
		}
		result.Write(body[offset:edit.start])
		result.Write(edit.value)
		offset = edit.end
	}
	result.Write(body[offset:])
	return result.Bytes()
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
	classified, err := classifyHTML(body)
	if err != nil {
		return err
	}
	for _, element := range classified.elements {
		if !element.hasNonce {
			continue
		}
		if element.nonce == "" {
			return measureFailure("policy", "/html/nonce")
		}
		bound := false
		for _, directives := range policies {
			allowed, found := directives[element.directive]
			if !found {
				allowed, found = directives[element.fallback]
			}
			if !found {
				allowed, found = directives["default-src"]
			}
			if !found {
				continue
			}
			bound = true
			kind := "script"
			if element.directive == "style-src-elem" {
				kind = "style"
			}
			if !cspAllowsElement(allowed, kind, element.nonce, element.text, element.external) {
				return measureFailure("policy", "/html/nonce")
			}
		}
		if !bound {
			return measureFailure("policy", "/html/nonce")
		}
	}
	return nil
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
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(payload)
		}
		matches = matches || !external && err == nil && bytes.Equal(digest, decoded)
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

// Execution selection belongs to pagecaps; raw spans belong to the structural
// classifier so byte counts and signature checks retain exact served source.
func inspectExecutionTree(tree *pagecaps.DocumentTree, observe func(*pagecaps.Document, pagecaps.ExecutableSource)) (pagecaps.Capabilities, error) {
	scripts := map[*pagecaps.Document][]htmlElement{}
	for _, doc := range tree.Documents {
		if !doc.ScriptsAllowed {
			continue
		}
		hasScripts := false
		_ = pagecaps.WalkHTML(doc.Root, func(_ *html.Node, state pagecaps.HTMLState) error {
			hasScripts = hasScripts || state.Script
			return nil
		})
		if !hasScripts {
			continue
		}
		classified, err := classifyHTML(doc.Body)
		if err != nil {
			return pagecaps.Capabilities{}, err
		}
		for _, element := range classified.elements {
			if element.executable {
				scripts[doc] = append(scripts[doc], element)
			}
		}
	}
	offsets := map[*pagecaps.Document]int{}
	invalid := false
	caps, err := pagecaps.InspectDocumentTree(tree, func(doc *pagecaps.Document, source pagecaps.ExecutableSource) {
		if source.Script {
			offset := offsets[doc]
			if offset >= len(scripts[doc]) {
				invalid = true
				return
			}
			element := scripts[doc][offset]
			offsets[doc]++
			source.Inline = !element.external
			source.Body = doc.Body[element.bodyStart:element.bodyEnd]
			source.ExactBody = true
		}
		if observe != nil {
			observe(doc, source)
		}
	})
	if err != nil || invalid {
		return pagecaps.Capabilities{}, measureFailure("capability", "/html")
	}
	return caps, nil
}

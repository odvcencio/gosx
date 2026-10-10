package wire

import (
	"bytes"
	"encoding/json"
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/evanw/esbuild/pkg/api"
	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"golang.org/x/net/html"
	"m31labs.dev/gosx/hydrate"
)

// Reference is a private dependency hint. Potential references advertise a
// conditional resource; a typed producer graph must determine their load phase.
type Reference struct {
	URL, Kind string
	Potential bool
}

// ReferenceSet never certifies runtime behavior. Complete means the supported
// syntax had no unresolved reference or loader escape; declarations and browser
// reconciliation still determine whether the dependency graph covers a page.
type ReferenceSet struct {
	Resources []Reference
	Complete  bool
}

// ReferenceError identifies a failed scan without copying source text or URLs.
type ReferenceError struct {
	Code, Reference, Pointer string
}

func (e *ReferenceError) Error() string { return e.Code + " at " + e.Reference + e.Pointer }

func referenceFailure() error {
	return &ReferenceError{Code: "invalid-input", Reference: "references", Pointer: "/body"}
}

// ScanReferences extracts active HTML, CSS and module references without
// changing the compatibility crawler or its accounting rules. It does not
// resolve URLs, fetch resources or copy native values into a public report.
func ScanReferences(body []byte, kind string) (out ReferenceSet, resultErr error) {
	return scanReferences(body, kind, nil)
}

// The optional observer audits omissions without retaining source or allocating
// a discard log in production. The observer stays in the private scanner state.
func scanReferences(body []byte, kind string, onDrop func(referenceDropReason)) (out ReferenceSet, resultErr error) {
	state := referenceScanner{ReferenceSet: ReferenceSet{Resources: []Reference{}, Complete: true}, onDrop: onDrop}
	defer func() {
		if resultErr != nil {
			state.drop(dropUnresolved)
		}
		out = state.ReferenceSet
	}()
	if len(body) > 16<<20 || !utf8.Valid(body) {
		return state.ReferenceSet, referenceFailure()
	}
	switch kind {
	case KindDocument:
		if err := scanDocumentReferences(body, &state); err != nil {
			return state.ReferenceSet, err
		}
	case KindStyle, KindScript:
		if err := scanSyntaxReferences(body, kind, &state); err != nil {
			return state.ReferenceSet, err
		}
	default:
		return state.ReferenceSet, referenceFailure()
	}
	sort.Slice(state.Resources, func(i, j int) bool {
		a, b := state.Resources[i], state.Resources[j]
		if a.URL != b.URL {
			return a.URL < b.URL
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return !a.Potential && b.Potential
	})
	dedup := state.Resources[:0]
	for _, ref := range state.Resources {
		if len(dedup) > 0 && dedup[len(dedup)-1].URL == ref.URL && dedup[len(dedup)-1].Kind == ref.Kind {
			dedup[len(dedup)-1].Potential = dedup[len(dedup)-1].Potential && ref.Potential
			state.drop(dropDuplicateReference)
			continue
		}
		dedup = append(dedup, ref)
	}
	state.Resources = dedup
	return state.ReferenceSet, nil
}

func addFetchReference(out *referenceScanner, raw, kind string, potential bool) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(value, "#") {
		// Empty/fragment fetches can target the current document. Without a
		// loading base they are unresolved, regardless of the resource kind.
		out.drop(dropUnresolved)
		return
	}
	if !understoodReferenceURL(value, kind) {
		out.drop(dropUnresolved)
		return
	}
	if strings.HasPrefix(strings.ToLower(value), "data:") {
		// Image/font data is opaque only in an understood non-executable
		// context. Never infer that context from a data payload's suffix.
		out.drop(dropOpaqueData)
		return
	}
	if kind == "" {
		kind = referenceKind(value)
	}
	out.Resources = append(out.Resources, Reference{URL: value, Kind: kind, Potential: potential})
}

func understoodReferenceURL(value, kind string) bool {
	if strings.ContainsAny(value, "\\ \t\r\n") {
		return false
	}
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "", "http", "https":
		return true
	case "data":
		return kind != "" && kind == opaqueDataReferenceKind(value)
	default:
		// Blob bodies, executable URLs and unknown scheme handlers have not
		// been scanned. Fetch Standard 4.3 does not make them inert resources.
		return false
	}
}

func opaqueDataReferenceKind(value string) string {
	value = strings.ToLower(value)
	switch {
	case strings.HasPrefix(value, "data:image/"):
		return KindImage
	case strings.HasPrefix(value, "data:font/"), strings.HasPrefix(value, "data:application/font-"), strings.HasPrefix(value, "data:application/x-font-"), strings.HasPrefix(value, "data:application/vnd.ms-fontobject"):
		return KindFont
	}
	return ""
}

func referenceKind(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	switch strings.ToLower(path.Ext(raw)) {
	case ".css":
		return KindStyle
	case ".js", ".mjs":
		return KindScript
	case ".wasm":
		return KindWASM
	case ".woff", ".woff2", ".ttf", ".otf":
		return KindFont
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".avif", ".ktx2":
		return KindImage
	default:
		return KindOther
	}
}

func executableType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "module", "text/javascript", "application/javascript", "text/ecmascript", "application/ecmascript", "application/x-javascript", "text/jscript", "text/livescript":
		return true
	default:
		return false
	}
}

func scanDocumentReferences(body []byte, out *referenceScanner) error {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return referenceFailure()
	}
	return scanDocumentTree(root, out)
}

func scanDocumentTree(root *html.Node, out *referenceScanner) error {
	type pending struct {
		node  *html.Node
		depth int
	}
	stack := []pending{{root, 0}}
	manifestSeen := false
	for len(stack) > 0 {
		entry := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := entry.node
		if entry.depth > 256 {
			return referenceFailure()
		}
		if n.Type == html.ElementNode {
			if n.Namespace != "" || !htmlReferenceElements[n.Data] {
				out.drop(dropUnresolved)
			}
			seen := map[string]bool{}
			for _, a := range n.Attr {
				if seen[a.Key] || strings.Contains(a.Val, "&#") {
					// Duplicate fields and retained entity text cannot prove coverage.
					out.drop(dropUnresolved)
				}
				seen[a.Key] = true
				if err := scanHTMLReferenceAttribute(n, a, out); err != nil {
					return err
				}
			}
			if n.Namespace == "" && n.Data == "template" {
				// Attributes (including unsupported shadow-root declarations) have
				// already been classified. Ordinary template contents are inert.
				out.drop(dropTemplateContent)
				continue
			}
			switch n.Data {
			case "script":
				if executableType(attr(n, "type")) {
					if hasHTMLReferenceAttribute(n, "src") {
						// The attribute dispatcher handles src even with a manifest ID.
						out.drop(dropExternalScriptBody)
					} else if err := scanSyntaxReferences([]byte(textOf(n)), KindScript, out); err != nil {
						return err
					}
				} else if knownHTMLDataScript(attr(n, "type")) {
					if attr(n, "id") == "gosx-manifest" && !hasHTMLReferenceAttribute(n, "src") {
						if manifestSeen {
							return referenceFailure()
						}
						manifestSeen = true
						if err := scanHydrationReferences(textOf(n), out); err != nil {
							return err
						}
					} else {
						out.drop(dropInertDataScript)
					}
				} else {
					out.drop(dropUnresolved)
				}
			case "style":
				if err := scanSyntaxReferences([]byte(textOf(n)), KindStyle, out); err != nil {
					return err
				}
			default:
				// All reference-bearing attributes were dispatched above; these
				// elements have no additional modelled loading body.
				out.drop(dropInertHTML)
			}
		} else {
			// Text in scripts/styles is scanned with its owning element. Other
			// text, comments and document nodes do not initiate loads.
			out.drop(dropInertHTML)
		}
		for c := n.LastChild; c != nil; c = c.PrevSibling {
			stack = append(stack, pending{c, entry.depth + 1})
		}
	}
	return nil
}

func scanHydrationReferences(raw string, out *referenceScanner) error {
	var manifest hydrate.Manifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return referenceFailure()
	}
	// Unknown producer fields cannot silently acquire an unmodelled loader.
	// Preserve known references from otherwise valid JSON while failing closed.
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		out.drop(dropUnresolved)
	}
	if manifest.Version != "0.1.0" {
		out.drop(dropUnresolved)
	}
	selectedBundles := map[string]bool{}
	for _, entry := range manifest.Islands {
		if entry.Static {
			out.drop(dropDormantManifest)
			continue
		}
		if entry.ProgramRef != "" {
			addFetchReference(out, entry.ProgramRef, KindProgram, false)
		} else {
			selectedBundles[entry.BundleID] = true
			bundle, ok := manifest.Bundles[entry.BundleID]
			if !ok {
				out.drop(dropUnresolved)
			} else {
				addFetchReference(out, bundle.Path, KindWASM, false)
			}
		}
	}
	for id := range manifest.Bundles {
		if !selectedBundles[id] {
			out.drop(dropDormantManifest)
		}
	}
	for _, entry := range manifest.ComputeIslands {
		addFetchReference(out, entry.ProgramRef, KindProgram, false)
	}
	for _, entry := range manifest.Engines {
		if entry.ProgramRef != "" || entry.Runtime == "go-wasm" {
			addFetchReference(out, entry.ProgramRef, "", false)
		} else {
			out.drop(dropDormantManifest)
		}
	}
	common, bridge := hydrationRuntimeConsumers(manifest)
	if common || bridge {
		addFetchReference(out, manifest.Runtime.Path, KindWASM, !common)
		if !common {
			out.drop(dropUnresolved)
		}
	} else {
		out.drop(dropDormantManifest)
	}
	// Classify every remaining field against the producer schema. Free-form
	// values and unsupported loader declarations cannot inherit metadata safety.
	scanManifestFields(raw, out)
	return nil
}

// hydrationRuntimeConsumers mirrors the cold-load predicates in
// client/js/bootstrap-src. Both loaders select nonempty islands and
// computeIslands, including static island entries (26-runtime-tail.ts:253-254,
// 30k-tail-init.ts:66-67), and engines with runtime exactly "shared"
// (26-runtime-tail.ts:257-262, 30b-tail-engine-mounting.ts:45-46).
// The monolith additionally selects hubs (30k-tail-init.ts:68), clientIdentity
// (:69), video engines (:98-104), and the exact keyboard/pointer/gamepad
// capabilities (:79-85; 10-runtime-scene-utils.ts:862-863).
// Both require runtime.path (26-runtime-tail.ts:485-496, 30k-tail-init.ts:75-76).
// A manifest does not identify which loader executes it. Bridge-only consumers
// therefore yield a potential reference with incomplete coverage, rather than
// certifying a WASM request that the selective loader does not make.
func hydrationRuntimeConsumers(manifest hydrate.Manifest) (common, bridge bool) {
	common = len(manifest.Islands) > 0 || len(manifest.ComputeIslands) > 0
	bridge = len(manifest.Hubs) > 0 || manifest.ClientIdentity != nil
	for _, entry := range manifest.Engines {
		common = common || entry.Runtime == "shared"
		bridge = bridge || entry.Kind == "video"
		for _, capability := range entry.Capabilities {
			switch capability {
			case "keyboard", "pointer", "gamepad":
				bridge = true
			}
		}
	}
	return common, bridge
}

func scanSyntaxReferences(body []byte, kind string, out *referenceScanner) error {
	if len(bytes.TrimSpace(body)) == 0 {
		out.drop(dropEmptySyntax)
		return nil
	}
	var lang *ts.Language
	if kind == KindStyle {
		lang = grammars.CssLanguage()
	} else {
		lang = grammars.JavascriptLanguage()
	}
	if lang == nil {
		return referenceFailure()
	}
	tree, err := ts.NewParser(lang).Parse(body)
	if err != nil || tree == nil {
		return referenceFailure()
	}
	if kind == KindScript && tree.RootNode().HasErrorOrMissing() {
		// Retry valid minified statement boundaries through the bundle producer's
		// pinned parser. Formatting changes neither emitted bodies nor byte counts.
		// Existing successfully parsed syntax retains its conservative treatment.
		tree.Release()
		formatted := api.Transform(string(body), api.TransformOptions{
			Loader: api.LoaderJS, Target: api.ESNext, Charset: api.CharsetUTF8,
			TreeShaking: api.TreeShakingFalse, LogLevel: api.LogLevelSilent,
		})
		if len(formatted.Errors) != 0 || len(formatted.Code) > 32<<20 {
			return referenceFailure()
		}
		body = formatted.Code
		tree, err = ts.NewParser(lang).Parse(body)
		if err != nil || tree == nil {
			return referenceFailure()
		}
	}
	defer tree.Release()
	if tree.RootNode().HasErrorOrMissing() {
		return referenceFailure()
	}
	type pending struct {
		node  *ts.Node
		depth int
	}
	stack := []pending{{tree.RootNode(), 0}}
	nodes := 0
	for len(stack) > 0 {
		entry := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := entry.node
		nodes++
		if entry.depth > 256 || nodes > 250000 {
			return referenceFailure()
		}
		if kind == KindStyle {
			cssReference(n, lang, body, out)
		} else {
			moduleReference(n, lang, body, out)
		}
		for i := n.NamedChildCount() - 1; i >= 0; i-- {
			stack = append(stack, pending{n.NamedChild(i), entry.depth + 1})
		}
	}
	return nil
}

func javascriptReferenceLiteral(n *ts.Node, lang *ts.Language, body []byte) (string, bool) {
	if n == nil {
		return "", false
	}
	if n.Type(lang) != "string" {
		return "", false
	}
	raw := n.Text(body)
	if len(raw) < 2 {
		return "", false
	}
	if raw[0] == '"' {
		var decoded string
		if json.Unmarshal([]byte(raw), &decoded) == nil {
			return decoded, true
		}
	}
	// Other JavaScript string forms need a full language decoder. An unresolved
	// literal never establishes coverage.
	if raw[0] == '\'' && raw[len(raw)-1] == '\'' && !strings.Contains(raw, "\\") {
		return raw[1 : len(raw)-1], true
	}
	return "", false
}

func cssReferenceLiteral(n *ts.Node, lang *ts.Language, body []byte) (string, bool) {
	if n == nil {
		return "", false
	}
	raw := n.Text(body)
	// CSS Syntax Level 3 escapes are different from JavaScript/JSON escapes.
	// Until decoded with CSS rules, every escaped value remains unresolved.
	if strings.Contains(raw, "\\") {
		return "", false
	}
	switch n.Type(lang) {
	case "plain_value":
		return raw, !strings.ContainsAny(raw, "()")
	case "string_value":
		if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') && raw[len(raw)-1] == raw[0] {
			return raw[1 : len(raw)-1], true
		}
	}
	return "", false
}

func cssReference(n *ts.Node, lang *ts.Language, body []byte, out *referenceScanner) {
	if (n.Type(lang) == "plain_value" || n.Type(lang) == "function_name") && strings.Contains(n.Text(body), "\\") {
		// Escaped names may conceal a loader that is not decoded here.
		out.drop(dropUnresolved)
		return
	}
	if n.Type(lang) == "import_statement" {
		sourceSeen := false
		for i := 0; i < n.NamedChildCount(); i++ {
			c := n.NamedChild(i)
			if c.Type(lang) == "string_value" {
				sourceSeen = true
				raw, ok := cssReferenceLiteral(c, lang, body)
				if !ok {
					out.drop(dropUnresolved)
				}
				if ok {
					addCSSReference(out, raw, KindStyle)
				}
			} else if c.Type(lang) == "call_expression" && c.NamedChildCount() > 0 && strings.EqualFold(c.NamedChild(0).Text(body), "url") {
				// The nested URL call checks its own argument.
				sourceSeen = true
				out.drop(dropNestedScan)
			} else {
				out.drop(dropNonLoadingSyntax)
			}
		}
		if !sourceSeen {
			out.drop(dropUnresolved)
		}
		return
	}
	if n.Type(lang) != "call_expression" {
		out.drop(dropNonLoadingSyntax)
		return
	}
	if n.NamedChildCount() < 2 {
		out.drop(dropUnresolved)
		return
	}
	name := strings.ToLower(n.NamedChild(0).Text(body))
	args := n.NamedChild(1)
	if name == "var" {
		// A substituted value can contain a URL; bindings are not resolved here.
		out.drop(dropUnresolved)
	} else if name == "url" {
		kind := ""
		if parent := n.Parent(); parent != nil && parent.Type(lang) == "import_statement" {
			kind = KindStyle
		}
		contents := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(args.Text(body), "("), ")"))
		if strings.HasPrefix(strings.ToLower(contents), "data:") && !strings.ContainsAny(contents, "()") {
			// The parser can split an unquoted data body into multiple nodes.
			// It still needs the same context policy as a quoted URL, especially
			// inside @import where a stylesheet can load more resources.
			addCSSReference(out, contents, kind)
			return
		}
		if args.NamedChildCount() != 1 {
			out.drop(dropUnresolved)
			return
		}
		raw, ok := cssReferenceLiteral(args.NamedChild(0), lang, body)
		if !ok {
			out.drop(dropUnresolved)
		}
		if ok {
			addCSSReference(out, raw, kind)
		}
	} else if name == "image-set" || name == "-webkit-image-set" {
		if args.NamedChildCount() == 0 {
			out.drop(dropUnresolved)
		}
		for i := 0; i < args.NamedChildCount(); i++ {
			c := args.NamedChild(i)
			switch c.Type(lang) {
			case "string_value":
				raw, ok := cssReferenceLiteral(c, lang, body)
				if !ok {
					out.drop(dropUnresolved)
				}
				if ok {
					addCSSReference(out, raw, KindImage)
				}
			case "integer_value", "float_value":
				// Resolution descriptors do not load a resource.
				out.drop(dropNonLoadingSyntax)
			case "call_expression":
				// URL calls are scanned separately; type() is a MIME descriptor.
				if c.NamedChildCount() == 0 || (strings.ToLower(c.NamedChild(0).Text(body)) != "url" && strings.ToLower(c.NamedChild(0).Text(body)) != "type") {
					out.drop(dropUnresolved)
				} else {
					out.drop(dropNestedScan)
				}
			default:
				out.drop(dropUnresolved)
			}
		}
	} else {
		// Only understood functions can establish CSS coverage. Other functions
		// may interpret strings as resources or synthesize substituted URLs.
		switch name {
		case "local", "format", "tech", "type", "rgb", "rgba", "hsl", "hsla", "hwb", "lab", "lch", "oklab", "oklch", "color", "color-mix", "calc", "min", "max", "clamp", "linear-gradient", "radial-gradient", "conic-gradient", "repeating-linear-gradient", "repeating-radial-gradient", "repeating-conic-gradient", "cubic-bezier", "steps", "counter", "counters":
			out.drop(dropNonLoadingSyntax)
		default:
			out.drop(dropUnresolved)
		}
	}
}

func addCSSReference(out *referenceScanner, raw, kind string) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(value, "#") {
		out.drop(dropCSSFragment)
		return
	}
	if kind == "" {
		// CSS url() image/font data cannot create a nested document or script.
		// An @import retains KindStyle and must never take this opaque path.
		kind = opaqueDataReferenceKind(strings.TrimSpace(raw))
	}
	addFetchReference(out, raw, kind, false)
}

func moduleReference(n *ts.Node, lang *ts.Language, body []byte, out *referenceScanner) {
	switch n.Type(lang) {
	case "import_statement", "export_statement":
		source := n.ChildByFieldName("source", lang)
		if source == nil {
			if n.Type(lang) == "export_statement" {
				out.drop(dropNonLoadingSyntax)
			} else {
				out.drop(dropUnresolved)
			}
			return
		}
		raw, ok := javascriptReferenceLiteral(source, lang, body)
		if !ok {
			out.drop(dropUnresolved)
		}
		if ok {
			addFetchReference(out, raw, KindScript, false)
		}
	case "call_expression", "new_expression":
		name := n.ChildByFieldName("function", lang)
		if name == nil {
			name = n.ChildByFieldName("constructor", lang)
		}
		if name == nil {
			out.drop(dropUnresolved)
			return
		}
		value := moduleLoaderName(name, lang, body)
		if value == "" {
			// The callee and arguments still traverse the loader-capability
			// policy, including unsupported tokens and computed accesses.
			out.drop(dropNestedScan)
			return
		}
		args := n.ChildByFieldName("arguments", lang)
		if args == nil || args.NamedChildCount() == 0 {
			out.drop(dropUnresolved)
			return
		}
		if value == "URL" && (args.NamedChildCount() != 2 || args.NamedChild(1).Text(body) != "import.meta.url") {
			out.drop(dropUnresolved)
			return
		}
		raw, ok := javascriptReferenceLiteral(args.NamedChild(0), lang, body)
		// Worker(new URL(...)) is covered by the nested URL expression.
		if !ok && (value == "Worker" || value == "SharedWorker") && args.NamedChild(0).Type(lang) == "new_expression" {
			constructor := args.NamedChild(0).ChildByFieldName("constructor", lang)
			if moduleLoaderName(constructor, lang, body) == "URL" {
				out.drop(dropNestedScan)
				return
			}
		}
		if !ok {
			out.drop(dropUnresolved)
		}
		if ok {
			kind := ""
			if value == "import" || value == "Worker" || value == "SharedWorker" || value == "URL" && workerURLArgument(n, lang, body) {
				kind = KindScript
			}
			addFetchReference(out, raw, kind, false)
		}
	case "identifier", "property_identifier", "shorthand_property_identifier", "shorthand_property_identifier_pattern", "string":
		moduleLoaderUse(n, lang, body, out)
	case "subscript_expression", "computed_property_name":
		// Computed access can conceal any loader, including on an aliased global.
		// No property evaluation or alias analysis establishes coverage here.
		out.drop(dropUnresolved)
	default:
		out.drop(dropNonLoadingSyntax)
	}
}

func moduleLoaderName(n *ts.Node, lang *ts.Language, body []byte) string {
	if n == nil {
		return ""
	}
	if n.Type(lang) == "import" {
		return "import"
	}
	if n.Type(lang) == "member_expression" {
		n = n.ChildByFieldName("property", lang)
		if n == nil || n.Type(lang) != "property_identifier" {
			return ""
		}
	} else if n.Type(lang) != "identifier" {
		return ""
	}
	switch name := n.Text(body); name {
	case "fetch", "Worker", "SharedWorker", "URL":
		return name
	}
	return ""
}

func moduleLoaderUse(n *ts.Node, lang *ts.Language, body []byte, out *referenceScanner) {
	callee := n
	name := n.Text(body)
	if n.Type(lang) == "string" {
		parent := n.Parent()
		if parent == nil || (parent.Type(lang) != "pair_pattern" && parent.Type(lang) != "pair") || parent.ChildByFieldName("key", lang) != n {
			out.drop(dropNonLoadingSyntax)
			return
		}
		var ok bool
		name, ok = javascriptReferenceLiteral(n, lang, body)
		if !ok {
			out.drop(dropUnresolved)
			return
		}
	}
	if n.Type(lang) == "property_identifier" {
		parent := n.Parent()
		if parent == nil {
			out.drop(dropUnresolved)
			return
		}
		if parent.Type(lang) == "member_expression" {
			if parent.ChildByFieldName("property", lang) != n {
				out.drop(dropNonLoadingSyntax)
				return
			}
			callee = parent
		}
	}
	if strings.Contains(name, "\\") {
		out.drop(dropUnresolved)
		return
	}
	if unmodeledJavaScriptLoader(name) {
		out.drop(dropUnresolved)
		return
	}
	switch name {
	case "fetch", "Worker", "SharedWorker", "URL":
		parent := callee.Parent()
		if parent == nil || (parent.Type(lang) != "call_expression" && parent.Type(lang) != "new_expression") ||
			(parent.ChildByFieldName("function", lang) != callee && parent.ChildByFieldName("constructor", lang) != callee) {
			out.drop(dropUnresolved)
		}
	case "setTimeout", "setInterval":
		// Timers coerce non-function handlers to source text. An inline function
		// avoids that construction; its body is scanned for loaders too.
		// https://html.spec.whatwg.org/multipage/timers-and-user-prompts.html#timers
		parent := callee.Parent()
		if parent == nil || parent.Type(lang) != "call_expression" || parent.ChildByFieldName("function", lang) != callee {
			out.drop(dropUnresolved)
			return
		}
		args := parent.ChildByFieldName("arguments", lang)
		if args == nil || args.NamedChildCount() == 0 {
			out.drop(dropUnresolved)
			return
		}
		switch args.NamedChild(0).Type(lang) {
		case "arrow_function", "function_expression":
			out.drop(dropNestedScan)
		default:
			out.drop(dropUnresolved)
		}
	default:
		out.drop(dropNonLoadingSyntax)
	}
}

func workerURLArgument(n *ts.Node, lang *ts.Language, body []byte) bool {
	args := n.Parent()
	if args == nil || args.Type(lang) != "arguments" || args.NamedChildCount() == 0 || args.NamedChild(0) != n {
		return false
	}
	call := args.Parent()
	if call == nil {
		return false
	}
	name := call.ChildByFieldName("constructor", lang)
	if name == nil {
		name = call.ChildByFieldName("function", lang)
	}
	loader := moduleLoaderName(name, lang, body)
	return loader == "Worker" || loader == "SharedWorker"
}

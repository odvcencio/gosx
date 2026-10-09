package wire

import (
	"bytes"
	"encoding/json"
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
// syntax had no unresolved reference; declarations and browser reconciliation
// still determine whether the dependency graph covers a page.
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
func ScanReferences(body []byte, kind string) (ReferenceSet, error) {
	out := ReferenceSet{Resources: []Reference{}, Complete: true}
	if len(body) > 16<<20 || !utf8.Valid(body) {
		return out, referenceFailure()
	}
	switch kind {
	case KindDocument:
		if err := scanDocumentReferences(body, &out); err != nil {
			return out, err
		}
	case KindStyle, KindScript:
		if err := scanSyntaxReferences(body, kind, &out); err != nil {
			return out, err
		}
	default:
		return out, referenceFailure()
	}
	sort.Slice(out.Resources, func(i, j int) bool {
		a, b := out.Resources[i], out.Resources[j]
		if a.URL != b.URL {
			return a.URL < b.URL
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return !a.Potential && b.Potential
	})
	dedup := out.Resources[:0]
	for _, ref := range out.Resources {
		if len(dedup) > 0 && dedup[len(dedup)-1].URL == ref.URL && dedup[len(dedup)-1].Kind == ref.Kind {
			dedup[len(dedup)-1].Potential = dedup[len(dedup)-1].Potential && ref.Potential
			continue
		}
		dedup = append(dedup, ref)
	}
	out.Resources = dedup
	return out, nil
}

func addReference(out *ReferenceSet, raw, kind string, potential bool) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(value, "#") || strings.HasPrefix(strings.ToLower(value), "data:") {
		return
	}
	if strings.ContainsAny(value, "\\ \t\r\n") {
		out.Complete = false
		return
	}
	if kind == "" {
		kind = referenceKind(value)
	}
	out.Resources = append(out.Resources, Reference{URL: value, Kind: kind, Potential: potential})
}

func referenceKind(raw string) string {
	switch strings.ToLower(path.Ext(strings.SplitN(raw, "?", 2)[0])) {
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

func scanDocumentReferences(body []byte, out *ReferenceSet) error {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return referenceFailure()
	}
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
			if n.Data == "template" {
				continue
			}
			seen := map[string]bool{}
			for _, a := range n.Attr {
				if seen[a.Key] {
					out.Complete = false
				}
				seen[a.Key] = true
				if strings.HasPrefix(a.Key, "data-gosx-") && strings.HasSuffix(a.Key, "-url") {
					addReference(out, a.Val, "", true)
				}
				if a.Key == "style" {
					if err := scanSyntaxReferences([]byte(".inline{"+a.Val+"}"), KindStyle, out); err != nil {
						return err
					}
				}
			}
			switch n.Data {
			case "base":
				if attr(n, "href") != "" {
					out.Complete = false
				}
			case "script":
				if attr(n, "id") == "gosx-manifest" {
					if manifestSeen {
						return referenceFailure()
					}
					manifestSeen = true
					if err := scanHydrationReferences(textOf(n), out); err != nil {
						return err
					}
				} else if executableType(attr(n, "type")) {
					if src := attr(n, "src"); src != "" {
						addReference(out, src, KindScript, false)
					} else if err := scanSyntaxReferences([]byte(textOf(n)), KindScript, out); err != nil {
						return err
					}
				} else if strings.EqualFold(attr(n, "type"), "importmap") {
					out.Complete = false
				}
			case "style":
				if err := scanSyntaxReferences([]byte(textOf(n)), KindStyle, out); err != nil {
					return err
				}
			case "link":
				for _, rel := range strings.Fields(strings.ToLower(attr(n, "rel"))) {
					switch rel {
					case "stylesheet":
						addReference(out, attr(n, "href"), KindStyle, false)
					case "modulepreload":
						addReference(out, attr(n, "href"), KindScript, false)
					case "preload", "prefetch":
						kind := ""
						switch strings.ToLower(attr(n, "as")) {
						case "script":
							kind = KindScript
						case "style":
							kind = KindStyle
						case "font":
							kind = KindFont
						case "image":
							kind = KindImage
						}
						addReference(out, attr(n, "href"), kind, false)
					}
				}
			case "img", "source", "video", "audio", "track", "iframe", "embed":
				addReference(out, attr(n, "src"), "", false)
				addReference(out, attr(n, "poster"), KindImage, false)
				// Candidate selection depends on viewport and MIME support.
				// Keep coverage unknown until the producer/browser resolves it.
				if attr(n, "srcset") != "" {
					out.Complete = false
				}
			case "object":
				addReference(out, attr(n, "data"), "", false)
			}
		}
		for c := n.LastChild; c != nil; c = c.PrevSibling {
			stack = append(stack, pending{c, entry.depth + 1})
		}
	}
	return nil
}

func scanHydrationReferences(raw string, out *ReferenceSet) error {
	var manifest hydrate.Manifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return referenceFailure()
	}
	if manifest.Version != "0.1.0" {
		out.Complete = false
	}
	shared := false
	for _, entry := range manifest.Islands {
		if entry.Static {
			continue
		}
		shared = true
		if entry.ProgramRef != "" {
			addReference(out, entry.ProgramRef, KindProgram, false)
		} else {
			bundle, ok := manifest.Bundles[entry.BundleID]
			if !ok || bundle.Path == "" {
				out.Complete = false
			} else {
				addReference(out, bundle.Path, KindWASM, false)
			}
		}
	}
	for _, entry := range manifest.ComputeIslands {
		shared = true
		if entry.ProgramRef == "" {
			out.Complete = false
		}
		addReference(out, entry.ProgramRef, KindProgram, false)
	}
	for _, entry := range manifest.Engines {
		shared = shared || entry.Runtime == "shared"
		addReference(out, entry.ProgramRef, "", false)
		if entry.Runtime == "go-wasm" && entry.ProgramRef == "" {
			out.Complete = false
		}
	}
	if manifest.Runtime.Path != "" {
		addReference(out, manifest.Runtime.Path, KindWASM, false)
	} else if shared {
		out.Complete = false
	}
	return nil
}

func scanSyntaxReferences(body []byte, kind string, out *ReferenceSet) error {
	if len(bytes.TrimSpace(body)) == 0 {
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

func referenceLiteral(n *ts.Node, lang *ts.Language, body []byte) (string, bool) {
	if n == nil {
		return "", false
	}
	kind := n.Type(lang)
	if kind != "string" && kind != "string_value" && kind != "plain_value" {
		return "", false
	}
	raw := n.Text(body)
	if kind == "plain_value" {
		return raw, !strings.ContainsAny(raw, "\\()")
	}
	if len(raw) < 2 {
		return "", false
	}
	if raw[0] == '"' {
		var decoded string
		if json.Unmarshal([]byte(raw), &decoded) == nil {
			return decoded, true
		}
	}
	// Escaped single-quoted JS/CSS and CSS escape syntax need a full
	// language decoder. An unresolved literal never establishes coverage.
	if raw[0] == '\'' && raw[len(raw)-1] == '\'' && !strings.Contains(raw, "\\") {
		return raw[1 : len(raw)-1], true
	}
	return "", false
}

func cssReference(n *ts.Node, lang *ts.Language, body []byte, out *ReferenceSet) {
	if n.Type(lang) == "import_statement" {
		for i := 0; i < n.NamedChildCount(); i++ {
			c := n.NamedChild(i)
			if c.Type(lang) == "string_value" {
				raw, ok := referenceLiteral(c, lang, body)
				out.Complete = out.Complete && ok
				if ok {
					addReference(out, raw, KindStyle, false)
				}
			}
		}
		return
	}
	if n.Type(lang) != "call_expression" || n.NamedChildCount() < 2 {
		return
	}
	name := strings.ToLower(n.NamedChild(0).Text(body))
	args := n.NamedChild(1)
	if name == "url" {
		contents := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(args.Text(body), "("), ")"))
		if strings.HasPrefix(strings.ToLower(contents), "data:") && !strings.ContainsAny(contents, "()") {
			return
		}
		if args.NamedChildCount() != 1 {
			out.Complete = false
			return
		}
		raw, ok := referenceLiteral(args.NamedChild(0), lang, body)
		out.Complete = out.Complete && ok
		if ok {
			kind := ""
			if parent := n.Parent(); parent != nil && parent.Type(lang) == "import_statement" {
				kind = KindStyle
			}
			addReference(out, raw, kind, false)
		}
	} else if name == "image-set" || name == "-webkit-image-set" {
		for i := 0; i < args.NamedChildCount(); i++ {
			c := args.NamedChild(i)
			if c.Type(lang) == "string_value" {
				raw, ok := referenceLiteral(c, lang, body)
				out.Complete = out.Complete && ok
				if ok {
					addReference(out, raw, KindImage, false)
				}
			}
		}
	}
}

func moduleReference(n *ts.Node, lang *ts.Language, body []byte, out *ReferenceSet) {
	switch n.Type(lang) {
	case "import_statement", "export_statement":
		source := n.ChildByFieldName("source", lang)
		if source == nil {
			return
		}
		raw, ok := referenceLiteral(source, lang, body)
		out.Complete = out.Complete && ok
		if ok {
			addReference(out, raw, KindScript, false)
		}
	case "call_expression", "new_expression":
		name := n.ChildByFieldName("function", lang)
		if name == nil {
			name = n.ChildByFieldName("constructor", lang)
		}
		if name == nil {
			return
		}
		value := name.Text(body)
		if name.Type(lang) == "member_expression" {
			property := name.ChildByFieldName("property", lang)
			if property != nil && property.Type(lang) == "property_identifier" && property.Text(body) == "fetch" {
				value = "fetch"
			}
		}
		if value != "import" && value != "fetch" && value != "Worker" && value != "SharedWorker" && value != "URL" {
			return
		}
		args := n.ChildByFieldName("arguments", lang)
		if args == nil || args.NamedChildCount() == 0 {
			out.Complete = false
			return
		}
		if value == "URL" && (args.NamedChildCount() != 2 || args.NamedChild(1).Text(body) != "import.meta.url") {
			out.Complete = false
			return
		}
		raw, ok := referenceLiteral(args.NamedChild(0), lang, body)
		// Worker(new URL(...)) is covered by the nested URL expression.
		if !ok && (value == "Worker" || value == "SharedWorker") && args.NamedChild(0).Type(lang) == "new_expression" {
			constructor := args.NamedChild(0).ChildByFieldName("constructor", lang)
			if constructor != nil && constructor.Text(body) == "URL" {
				return
			}
		}
		out.Complete = out.Complete && ok
		if ok {
			kind := ""
			if value == "import" || value == "Worker" || value == "SharedWorker" || value == "URL" && workerURLArgument(n, lang, body) {
				kind = KindScript
			}
			addReference(out, raw, kind, false)
		}
	case "identifier":
		switch n.Text(body) {
		case "eval", "Function", "XMLHttpRequest", "WebSocket", "EventSource":
			out.Complete = false
		case "fetch":
			parent := n.Parent()
			if parent == nil || parent.Type(lang) != "call_expression" || parent.ChildByFieldName("function", lang) != n {
				out.Complete = false
			}
		}
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
	return name != nil && (name.Text(body) == "Worker" || name.Text(body) == "SharedWorker")
}

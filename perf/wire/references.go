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
	Base      string
	Worker    bool // The target starts a worker environment, rather than inheriting one.
}

const (
	ReferenceBaseDocument    = "document"
	ReferenceBaseSource      = "source"
	ReferenceBaseEnvironment = "environment"
	ReferenceBaseWorker      = "worker"
)

// ReferenceSet never certifies runtime behavior. Complete means the supported
// syntax had no unresolved reference; declarations and browser reconciliation
// still determine whether the dependency graph covers a page.
type ReferenceSet struct {
	Resources   []Reference
	Complete    bool
	BaseHref    string
	HasBaseHref bool
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
		if a.Base != b.Base {
			return a.Base < b.Base
		}
		if a.Worker != b.Worker {
			return !a.Worker
		}
		return !a.Potential && b.Potential
	})
	dedup := out.Resources[:0]
	for _, ref := range out.Resources {
		if len(dedup) > 0 && dedup[len(dedup)-1].URL == ref.URL && dedup[len(dedup)-1].Kind == ref.Kind && dedup[len(dedup)-1].Base == ref.Base && dedup[len(dedup)-1].Worker == ref.Worker {
			dedup[len(dedup)-1].Potential = dedup[len(dedup)-1].Potential && ref.Potential
			continue
		}
		dedup = append(dedup, ref)
	}
	out.Resources = dedup
	return out, nil
}

func addReference(out *ReferenceSet, raw, kind string, potential bool) {
	addContextReference(out, raw, kind, potential, ReferenceBaseDocument, false)
}

func addContextReference(out *ReferenceSet, raw, kind string, potential bool, base string, worker bool) {
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
	out.Resources = append(out.Resources, Reference{URL: value, Kind: kind, Potential: potential, Base: base, Worker: worker})
}

func scanInlineReferences(body []byte, kind string, out *ReferenceSet) error {
	start := len(out.Resources)
	if err := scanSyntaxReferences(body, kind, out); err != nil {
		return err
	}
	for i := start; i < len(out.Resources); i++ {
		if out.Resources[i].Base == ReferenceBaseSource {
			out.Resources[i].Base = ReferenceBaseDocument
		}
	}
	return nil
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
					if err := scanInlineReferences([]byte(".inline{"+a.Val+"}"), KindStyle, out); err != nil {
						return err
					}
				}
			}
			switch n.Data {
			case "base":
				if n.Namespace == "" && !out.HasBaseHref {
					for _, a := range n.Attr {
						if a.Key == "href" {
							out.BaseHref, out.HasBaseHref = a.Val, true
							break
						}
					}
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
					} else if err := scanInlineReferences([]byte(textOf(n)), KindScript, out); err != nil {
						return err
					}
				} else if strings.EqualFold(attr(n, "type"), "importmap") {
					out.Complete = false
				}
			case "style":
				if err := scanInlineReferences([]byte(textOf(n)), KindStyle, out); err != nil {
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
					addContextReference(out, raw, KindStyle, false, ReferenceBaseSource, false)
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
			addContextReference(out, raw, "", false, ReferenceBaseSource, false)
		}
	} else if name == "image-set" || name == "-webkit-image-set" {
		for i := 0; i < args.NamedChildCount(); i++ {
			c := args.NamedChild(i)
			if c.Type(lang) == "string_value" {
				raw, ok := referenceLiteral(c, lang, body)
				out.Complete = out.Complete && ok
				if ok {
					addContextReference(out, raw, KindImage, false, ReferenceBaseSource, false)
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
			addContextReference(out, raw, KindScript, false, ReferenceBaseSource, false)
		}
	case "call_expression", "new_expression":
		value := loaderName(n, lang, body)
		if value == "XMLHttpRequest" {
			out.Complete = out.Complete && directXHROpen(n, lang, body)
			return
		}
		switch value {
		case "import", "fetch", "Worker", "SharedWorker", "URL", "EventSource", "WebSocket", "xhr-open", "importScripts":
		default:
			return
		}
		args := n.ChildByFieldName("arguments", lang)
		if args == nil || args.NamedChildCount() == 0 {
			out.Complete = false
			return
		}
		if value == "importScripts" {
			for i := 0; i < args.NamedChildCount(); i++ {
				raw, ok := referenceLiteral(args.NamedChild(i), lang, body)
				out.Complete = out.Complete && ok
				if ok {
					addContextReference(out, raw, KindScript, false, ReferenceBaseWorker, false)
				}
			}
			return
		}
		if value == "URL" && (args.NamedChildCount() != 2 || args.NamedChild(1).Text(body) != "import.meta.url") {
			out.Complete = false
			return
		}
		first := 0
		if value == "xhr-open" {
			first = 1
		}
		if args.NamedChildCount() <= first {
			out.Complete = false
			return
		}
		raw, ok := referenceLiteral(args.NamedChild(first), lang, body)
		if !ok && value != "URL" && loaderName(args.NamedChild(first), lang, body) == "URL" {
			// The nested URL expression records its explicit module base.
			return
		}
		out.Complete = out.Complete && ok
		if !ok {
			return
		}
		base, kind := ReferenceBaseEnvironment, ""
		worker := value == "Worker" || value == "SharedWorker"
		if value == "import" || value == "URL" {
			base = ReferenceBaseSource
		}
		if value == "import" || worker {
			kind = KindScript
		}
		if value == "URL" {
			parent := n.Parent()
			if parent != nil && parent.Type(lang) == "arguments" {
				loader := loaderName(parent.Parent(), lang, body)
				worker = loader == "Worker" || loader == "SharedWorker"
				if worker {
					kind = KindScript
				}
			}
		}
		addContextReference(out, raw, kind, false, base, worker)
	case "identifier":
		value := n.Text(body)
		switch value {
		case "eval", "Function":
			out.Complete = false
		case "XMLHttpRequest":
			out.Complete = out.Complete && directXHROpen(n.Parent(), lang, body)
		case "fetch", "Worker", "SharedWorker", "URL", "EventSource", "WebSocket", "importScripts":
			parent := n.Parent()
			if parent == nil || loaderName(parent, lang, body) != value {
				out.Complete = false
			}
		}
	case "member_expression":
		property := n.ChildByFieldName("property", lang)
		if property == nil {
			return
		}
		switch property.Text(body) {
		case "fetch", "Worker", "SharedWorker", "URL", "EventSource", "WebSocket", "importScripts", "XMLHttpRequest":
			if loaderName(n.Parent(), lang, body) != property.Text(body) {
				out.Complete = false
			}
		}
	case "subscript_expression":
		object := n.ChildByFieldName("object", lang)
		if object != nil && (object.Text(body) == "globalThis" || object.Text(body) == "window" || object.Text(body) == "self") {
			out.Complete = false
		}
	}
}

func loaderName(n *ts.Node, lang *ts.Language, body []byte) string {
	if n == nil || n.Type(lang) != "call_expression" && n.Type(lang) != "new_expression" {
		return ""
	}
	name := n.ChildByFieldName("function", lang)
	if name == nil {
		name = n.ChildByFieldName("constructor", lang)
	}
	if name == nil {
		return ""
	}
	if name.Type(lang) != "member_expression" {
		return name.Text(body)
	}
	object, property := name.ChildByFieldName("object", lang), name.ChildByFieldName("property", lang)
	if object == nil || property == nil {
		return ""
	}
	if property.Text(body) == "open" && loaderName(object, lang, body) == "XMLHttpRequest" {
		return "xhr-open"
	}
	switch object.Text(body) {
	case "globalThis", "window", "self":
		return property.Text(body)
	default:
		return ""
	}
}

func directXHROpen(n *ts.Node, lang *ts.Language, body []byte) bool {
	if n == nil || n.Type(lang) != "new_expression" || loaderName(n, lang, body) != "XMLHttpRequest" {
		return false
	}
	parent := n.Parent()
	return parent != nil && parent.Type(lang) == "member_expression" && loaderName(parent.Parent(), lang, body) == "xhr-open"
}

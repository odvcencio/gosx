//go:build !js || !wasm

package highlight

import (
	"html"
	"sort"
	"strings"

	gosxlang "github.com/odvcencio/gosx"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type parseResult struct {
	source   []byte
	treeSrc  []byte
	root     *gotreesitter.Node
	lang     *gotreesitter.Language
	offset   uint32
	classify func(*gotreesitter.Node, *gotreesitter.Language, []byte) string
	release  func()
}

type span struct {
	start uint32
	end   uint32
	class string
}

type parseAttempt struct {
	source string
	offset uint32
}

func renderHTML(lang, source string) string {
	if source == "" {
		return ""
	}
	spec, err := parseForHighlight(lang, source)
	if err != nil || spec == nil || spec.root == nil || spec.lang == nil {
		return html.EscapeString(source)
	}
	defer spec.release()
	return renderSegment(spec)
}

func parseForHighlight(lang, source string) (*parseResult, error) {
	switch NormalizeLanguage(lang) {
	case LangGo:
		return bestGoParse(source)
	case LangGoSX:
		return bestGoSXParse(source)
	case LangJavaScript:
		return parseGenericSnippet("snippet.js", source)
	case LangJSON:
		return parseGenericSnippet("snippet.json", source)
	case LangBash:
		return parseGenericSnippet("snippet.sh", source)
	default:
		return nil, nil
	}
}

func bestGoParse(source string) (*parseResult, error) {
	return bestParseAttempt(source, classifyGoNode, func(src string) (*gotreesitter.Tree, *gotreesitter.Language, error) {
		lang := grammars.GoLanguage()
		parser := gotreesitter.NewParser(lang)
		tree, err := parseGoSource(parser, lang, []byte(src))
		if err != nil {
			return nil, nil, err
		}
		return tree, lang, nil
	}, []parseAttempt{
		{source: source},
		{
			source: "package demo\n\nfunc _snippet() {\n" + source + "\n}\n",
			offset: uint32(len("package demo\n\nfunc _snippet() {\n")),
		},
	})
}

func parseGoSource(parser *gotreesitter.Parser, lang *gotreesitter.Language, source []byte) (*gotreesitter.Tree, error) {
	if _, ok := lang.SymbolByName("source_file_token1"); !ok {
		return parser.Parse(source)
	}
	tokenSource, err := grammars.NewGoTokenSource(source, lang)
	if err != nil {
		return nil, err
	}
	return parser.ParseWithTokenSource(source, tokenSource)
}

func bestGoSXParse(source string) (*parseResult, error) {
	return bestParseAttempt(source, classifyGoSXNode, func(src string) (*gotreesitter.Tree, *gotreesitter.Language, error) {
		return gosxlang.Parse([]byte(src))
	}, []parseAttempt{
		{source: source},
		{
			source: "package demo\n\nfunc _snippet() {\n" + source + "\n}\n",
			offset: uint32(len("package demo\n\nfunc _snippet() {\n")),
		},
	})
}

func bestParseAttempt(
	source string,
	classify func(*gotreesitter.Node, *gotreesitter.Language, []byte) string,
	parse func(string) (*gotreesitter.Tree, *gotreesitter.Language, error),
	attempts []parseAttempt,
) (*parseResult, error) {
	var best *parseResult
	bestScore := -1
	original := []byte(source)

	for _, attempt := range attempts {
		tree, lang, err := parse(attempt.source)
		if err != nil || tree == nil || lang == nil {
			continue
		}
		score := parseProblemCount(tree.RootNode())
		spec := &parseResult{
			source:   original,
			treeSrc:  []byte(attempt.source),
			root:     tree.RootNode(),
			lang:     lang,
			offset:   attempt.offset,
			classify: classify,
			release:  tree.Release,
		}
		if best == nil || score < bestScore {
			if best != nil {
				best.release()
			}
			best = spec
			bestScore = score
			continue
		}
		tree.Release()
	}

	return best, nil
}

func parseGenericSnippet(filename, source string) (*parseResult, error) {
	boundTree, err := grammars.ParseFile(filename, []byte(source))
	if err != nil || boundTree == nil {
		return nil, err
	}
	return &parseResult{
		source:   []byte(source),
		treeSrc:  boundTree.Source(),
		root:     boundTree.RootNode(),
		lang:     boundTree.Language(),
		classify: classifyGenericNode,
		release:  boundTree.Release,
	}, nil
}

func renderSegment(spec *parseResult) string {
	var spans []span
	collectSpans(spec.root, spec.lang, spec.treeSrc, spec.classify, &spans)
	if len(spans) == 0 {
		return html.EscapeString(string(spec.source))
	}

	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end < spans[j].end
		}
		return spans[i].start < spans[j].start
	})

	start := spec.offset
	end := spec.offset + uint32(len(spec.source))
	pos := start

	var b strings.Builder
	for _, span := range spans {
		if span.start < start || span.end > end || span.end <= pos {
			continue
		}
		if span.start > pos {
			b.WriteString(html.EscapeString(string(spec.treeSrc[pos:span.start])))
		}
		b.WriteString(`<span class="`)
		b.WriteString(span.class)
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(string(spec.treeSrc[span.start:span.end])))
		b.WriteString(`</span>`)
		pos = span.end
	}
	if pos < end {
		b.WriteString(html.EscapeString(string(spec.treeSrc[pos:end])))
	}
	return b.String()
}

func collectSpans(
	node *gotreesitter.Node,
	lang *gotreesitter.Language,
	source []byte,
	classify func(*gotreesitter.Node, *gotreesitter.Language, []byte) string,
	spans *[]span,
) {
	if node == nil || classify == nil {
		return
	}
	if class := classify(node, lang, source); class != "" && node.EndByte() > node.StartByte() {
		*spans = append(*spans, span{
			start: node.StartByte(),
			end:   node.EndByte(),
			class: class,
		})
		return
	}
	for i := 0; i < node.ChildCount(); i++ {
		collectSpans(node.Child(i), lang, source, classify, spans)
	}
}

func classifyGoNode(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	return classifyCommonNode(node, lang, source, true)
}

func classifyGoSXNode(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	nodeType := node.Type(lang)
	parentType := ""
	if parent := node.Parent(); parent != nil {
		parentType = parent.Type(lang)
	}

	switch nodeType {
	case "jsx_tag_name", "jsx_identifier":
		if parentType == "jsx_tag_name" {
			return "ts-tag"
		}
	case "jsx_attr_name":
		return "ts-attr"
	case "jsx_string_literal":
		return "ts-string"
	case "jsx_text":
		if strings.TrimSpace(node.Text(source)) != "" {
			return "ts-text"
		}
	}

	return classifyCommonNode(node, lang, source, true)
}

func classifyGenericNode(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte) string {
	return classifyCommonNode(node, lang, source, false)
}

func classifyCommonNode(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, goTypes bool) string {
	nodeType := node.Type(lang)
	text := node.Text(source)

	switch {
	case strings.Contains(nodeType, "comment"):
		return "ts-comment"
	case strings.Contains(nodeType, "string"):
		return "ts-string"
	case isNumberNodeType(nodeType):
		return "ts-number"
	}

	switch text {
	case "true", "false":
		return "ts-bool"
	case "nil", "null", "undefined":
		return "ts-builtin"
	}

	switch nodeType {
	case "type_identifier":
		return "ts-type"
	case "field_identifier", "property_identifier":
		return "ts-property"
	case "interpreted_string_literal", "raw_string_literal", "string_literal", "template_string":
		return "ts-string"
	}

	if keywords[nodeType] || keywords[text] {
		return "ts-keyword"
	}
	if builtins[text] {
		return "ts-builtin"
	}
	if goTypes {
		switch nodeType {
		case "package_identifier":
			return "ts-namespace"
		case "identifier":
			if len(text) > 0 && text[0] >= 'A' && text[0] <= 'Z' {
				return "ts-type"
			}
		}
	}
	if isOperatorToken(nodeType) || isOperatorToken(text) {
		return "ts-operator"
	}
	if isPunctuationToken(nodeType) {
		return "ts-punctuation"
	}
	return ""
}

func parseProblemCount(node *gotreesitter.Node) int {
	if node == nil {
		return 0
	}
	count := 0
	if node.IsError() || node.IsMissing() {
		count++
	}
	for i := 0; i < node.ChildCount(); i++ {
		count += parseProblemCount(node.Child(i))
	}
	return count
}

func isNumberNodeType(nodeType string) bool {
	switch nodeType {
	case "int_literal", "float_literal", "imaginary_literal", "rune_literal", "number":
		return true
	default:
		return false
	}
}

func isOperatorToken(token string) bool {
	switch token {
	case "+", "-", "*", "/", "%", "=", "==", "!=", "<", ">", "<=", ">=", ":=", "&&", "||", "!", "...":
		return true
	default:
		return false
	}
}

func isPunctuationToken(token string) bool {
	switch token {
	case "(", ")", "{", "}", "[", "]", "<", ">", "</", "/>", ",", ".", ":", ";", "\"", "'", "`", "/":
		return true
	default:
		return false
	}
}

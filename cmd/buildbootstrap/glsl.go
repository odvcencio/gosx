package main

// Shorten built-in shader locals while retaining readable authored GLSL.
// Interface names and shader hook replacement text stay unchanged. Edits keep
// every newline, so the compacted source maps remain composable.
import (
	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"sort"
	"strings"
)

type shaderEdit struct {
	start, end int
	text       string
}
type shaderPiece struct{ start, end, offset int }
type shaderToken struct {
	text       string
	start, end int
	identifier bool
}

func shaderIdentifierStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
func shaderIdentifierPart(c byte) bool { return shaderIdentifierStart(c) || c >= '0' && c <= '9' }
func shaderTokens(code string) []shaderToken {
	var tokens []shaderToken
	for i := 0; i < len(code); {
		start := i
		switch {
		case i+1 < len(code) && code[i:i+2] == "//":
			for i < len(code) && code[i] != '\n' {
				i++
			}
		case i+1 < len(code) && code[i:i+2] == "/*":
			i += 2
			for i+1 < len(code) && code[i:i+2] != "*/" {
				i++
			}
			i = min(len(code), i+2)
		case shaderIdentifierStart(code[i]):
			i++
			for i < len(code) && shaderIdentifierPart(code[i]) {
				i++
			}
			tokens = append(tokens, shaderToken{code[start:i], start, i, true})
		case code[i] >= '0' && code[i] <= '9' || code[i] == '.' && i+1 < len(code) && code[i+1] >= '0' && code[i+1] <= '9':
			i++
			for i < len(code) && (shaderIdentifierPart(code[i]) || code[i] == '.' || (code[i] == '+' || code[i] == '-') && (code[i-1] == 'e' || code[i-1] == 'E')) {
				i++
			}
		case code[i] <= ' ':
			i++
		default:
			i++
			tokens = append(tokens, shaderToken{code[start:i], start, i, false})
		}
	}
	return tokens
}
func shaderType(name string) bool {
	switch name {
	case "void", "float", "int", "uint", "bool", "vec2", "vec3", "vec4", "ivec2", "ivec3", "ivec4", "uvec2", "uvec3", "uvec4", "mat2", "mat3", "mat4":
		return true
	}
	return false
}
func shaderPair(tokens []shaderToken, open int, left, right string) int {
	depth := 0
	for i := open; i < len(tokens); i++ {
		if tokens[i].text == left {
			depth++
		}
		if tokens[i].text == right {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
func shaderAlias(i int) string {
	if i < 26 {
		return string(rune('a' + i))
	}
	i -= 26
	return string([]rune{rune('a' + i/26), rune('a' + i%26)})
}
func shaderLocalEdits(code string, pinned map[string]bool) []shaderEdit {
	tokens := shaderTokens(code)
	type function struct{ start, end, name int }
	var functions []function
	covered := make([]bool, len(tokens))
	for i := 0; i+3 < len(tokens); i++ {
		if !shaderType(tokens[i].text) || !tokens[i+1].identifier || tokens[i+2].text != "(" {
			continue
		}
		close := shaderPair(tokens, i+2, "(", ")")
		if close < 0 || close+1 >= len(tokens) || tokens[close+1].text != "{" {
			continue
		}
		end := shaderPair(tokens, close+1, "{", "}")
		if end < 0 {
			continue
		}
		functions = append(functions, function{i, end, i + 1})
		for j := i; j <= end; j++ {
			covered[j] = true
		}
		i = end
	}
	globals := map[string]bool{}
	for i, token := range tokens {
		if !covered[i] && token.identifier {
			globals[token.text] = true
		}
	}
	for _, fn := range functions {
		globals[tokens[fn.name].text] = true
	}
	var edits []shaderEdit
	for _, fn := range functions {
		locals := map[string]bool{}
		var names []string
		add := func(name string) {
			if len(name) <= 2 || locals[name] || globals[name] || pinned[name] || shaderType(name) || strings.HasPrefix(name, "gl_") {
				return
			}
			locals[name] = true
			names = append(names, name)
		}
		for i := fn.start; i+1 <= fn.end; i++ {
			if !shaderType(tokens[i].text) || !tokens[i+1].identifier || i+1 == fn.name {
				continue
			}
			add(tokens[i+1].text)
			// Commas in calls or initializers are not additional declarators.
			depth := 0
			for j := i + 2; j <= fn.end; j++ {
				text := tokens[j].text
				if depth == 0 && (text == ";" || text == ")" || text == "{") {
					break
				}
				if text == "(" || text == "[" {
					depth++
				}
				if text == ")" || text == "]" {
					depth--
				}
				if depth == 0 && text == "," && j+1 <= fn.end && tokens[j+1].identifier {
					if shaderType(tokens[j+1].text) {
						break
					}
					add(tokens[j+1].text)
				}
			}
		}
		blocked := map[string]bool{"do": true, "if": true, "in": true}
		for name := range pinned {
			blocked[name] = true
		}
		for name := range globals {
			blocked[name] = true
		}
		for i := fn.start; i <= fn.end; i++ {
			token := tokens[i]
			if token.identifier && (!locals[token.text] || i > 0 && tokens[i-1].text == ".") {
				blocked[token.text] = true
			}
		}
		frequency := map[string]int{}
		for i := fn.start; i <= fn.end; i++ {
			if i == 0 || tokens[i-1].text != "." {
				frequency[tokens[i].text]++
			}
		}
		sort.SliceStable(names, func(i, j int) bool { return frequency[names[i]] > frequency[names[j]] })
		aliases := map[string]string{}
		index := 0
		for _, name := range names {
			alias := shaderAlias(index)
			index++
			for blocked[alias] {
				alias = shaderAlias(index)
				index++
			}
			aliases[name] = alias
			blocked[alias] = true
		}
		for i := fn.start; i <= fn.end; i++ {
			token := tokens[i]
			alias, ok := aliases[token.text]
			if ok && token.identifier && (i == 0 || tokens[i-1].text != ".") && alias != token.text {
				edits = append(edits, shaderEdit{token.start, token.end, alias})
			}
		}
	}
	return edits
}

// Mask JS escapes/interpolations with equal-length spaces. GLSL identifiers
// contain no escapes, and token offsets still address the authored literals.
func shaderLiteral(code []byte, node *gotreesitter.Node, grammar *gotreesitter.Language) (int, int, string) {
	start, end := int(node.StartByte())+1, int(node.EndByte())-1
	masked := append([]byte(nil), code[start:end]...)
	for i := 0; i < len(masked); i++ {
		if masked[i] == '\\' {
			masked[i] = ' '
			i++
			if i < len(masked) {
				masked[i] = ' '
			}
		}
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type(grammar) != "template_substitution" {
			continue
		}
		for j := max(start, int(child.StartByte())); j < min(end, int(child.EndByte())); j++ {
			if masked[j-start] != '\n' {
				masked[j-start] = ' '
			}
		}
	}
	return start, end, string(masked)
}
func packBuiltinGLSL(code string) string {
	grammar := grammars.TypescriptLanguage()
	tree, err := gotreesitter.NewParser(grammar).Parse([]byte(code))
	if err != nil || tree.RootNode() == nil {
		return code
	}
	defer tree.Release()
	bytes := []byte(code)
	pinned := map[string]bool{}
	var constants []*gotreesitter.Node
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		kind := node.Type(grammar)
		if kind == "call_expression" {
			callee := node.ChildByFieldName("function", grammar)
			arguments := node.ChildByFieldName("arguments", grammar)
			if callee != nil && arguments != nil && strings.HasSuffix(callee.Text(bytes), ".replace") {
				for i := 0; i < int(arguments.ChildCount()); i++ {
					child := arguments.Child(i)
					if child.Type(grammar) != "string" {
						continue
					}
					_, _, value := shaderLiteral(bytes, child, grammar)
					for _, token := range shaderTokens(value) {
						if token.identifier {
							pinned[token.text] = true
						}
					}
					break
				}
			}
		}
		if kind == "variable_declarator" && !node.HasError() {
			name := node.ChildByFieldName("name", grammar)
			value := node.ChildByFieldName("value", grammar)
			if name != nil && value != nil && strings.HasPrefix(name.Text(bytes), "SCENE_") {
				text := name.Text(bytes)
				if strings.Contains(text, "SOURCE") || strings.Contains(text, "GLSL") || text == "SCENE_SKY_FRAGMENT" {
					if !strings.Contains(value.Text(bytes), ".replace(") {
						constants = append(constants, value)
					}
				}
			}
		}
		for i := 0; i < int(node.ChildCount()); i++ {
			walk(node.Child(i))
		}
	}
	walk(tree.RootNode())
	var edits []shaderEdit
	for _, constant := range constants {
		var pieces []shaderPiece
		var joined strings.Builder
		var gather func(*gotreesitter.Node)
		gather = func(node *gotreesitter.Node) {
			kind := node.Type(grammar)
			if kind == "string" || kind == "template_string" {
				start, end, value := shaderLiteral(bytes, node, grammar)
				pieces = append(pieces, shaderPiece{start, end, joined.Len()})
				joined.WriteString(value)
				joined.WriteByte('\n')
				return
			}
			for i := 0; i < int(node.ChildCount()); i++ {
				gather(node.Child(i))
			}
		}
		gather(constant)
		for _, piece := range pieces {
			for row := piece.start; row < piece.end; {
				end := row
				for end < piece.end && (bytes[end] == ' ' || bytes[end] == '\t') {
					end++
				}
				if end > row {
					edits = append(edits, shaderEdit{row, end, ""})
				}
				next := row
				for next < piece.end && bytes[next] != '\n' {
					next++
				}
				row = next + 1
			}
		}
		for _, edit := range shaderLocalEdits(joined.String(), pinned) {
			for _, piece := range pieces {
				if edit.start >= piece.offset && edit.end <= piece.offset+piece.end-piece.start {
					edits = append(edits, shaderEdit{piece.start + edit.start - piece.offset, piece.start + edit.end - piece.offset, edit.text})
					break
				}
			}
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		code = code[:edit.start] + edit.text + code[edit.end:]
	}
	return code
}

// Compaction may remove JavaScript comments, never text inside a literal.
// Template substitutions also stay intact until esbuild parses the full chunk.
func literalSourceLines(code string) map[int]bool {
	grammar := grammars.TypescriptLanguage()
	tree, err := gotreesitter.NewParser(grammar).Parse([]byte(code))
	if err != nil || tree.RootNode() == nil {
		return nil
	}
	defer tree.Release()
	lines := map[int]bool{}
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		kind := node.Type(grammar)
		if kind == "template_string" || kind == "string" {
			for row := int(node.StartPoint().Row); row <= int(node.EndPoint().Row); row++ {
				lines[row] = true
			}
			return
		}
		for i := 0; i < int(node.ChildCount()); i++ {
			walk(node.Child(i))
		}
	}
	walk(tree.RootNode())
	return lines
}

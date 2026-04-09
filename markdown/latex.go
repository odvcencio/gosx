package markdown

import (
	"fmt"
	"html"
	"strings"
	"sync"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammargen"
)

// LaTeX math → HTML renderer. Uses gotreesitter's grammargen to define
// a LaTeX math grammar at init time, generates a parser blob, and renders
// parsed math expressions to semantic HTML with CSS classes.
//
// No external dependencies. No KaTeX. No MathJax. Just Go.

var (
	latexLang     *gotreesitter.Language
	latexLangOnce sync.Once
	latexLangErr  error
)

func latexLanguage() (*gotreesitter.Language, error) {
	latexLangOnce.Do(func() {
		g := latexMathGrammar()
		blob, err := grammargen.Generate(g)
		if err != nil {
			latexLangErr = fmt.Errorf("generate latex grammar: %w", err)
			return
		}
		lang, err := gotreesitter.LoadLanguage(blob)
		if err != nil {
			latexLangErr = fmt.Errorf("load latex language: %w", err)
			return
		}
		latexLang = lang
	})
	return latexLang, latexLangErr
}

func latexMathGrammar() *grammargen.Grammar {
	g := grammargen.NewGrammar("latex_math")

	// Top-level: sequence of math content
	g.Define("math", grammargen.Repeat1(grammargen.Sym("_element")))

	// Element: any math construct (underscore prefix = hidden/anonymous)
	g.Define("_element", grammargen.Choice(
		grammargen.Sym("command"),
		grammargen.Sym("group"),
		grammargen.Sym("superscript"),
		grammargen.Sym("subscript"),
		grammargen.Sym("number"),
		grammargen.Sym("operator"),
		grammargen.Sym("punctuation"),
		grammargen.Sym("text"),
	))

	// Braced group: { content }
	g.Define("group", grammargen.Seq(
		grammargen.Str("{"),
		grammargen.Optional(grammargen.Sym("math")),
		grammargen.Str("}"),
	))

	// Superscript: ^{group} or ^char
	g.Define("superscript", grammargen.Seq(
		grammargen.Str("^"),
		grammargen.Choice(
			grammargen.Sym("group"),
			grammargen.Sym("_atom"),
		),
	))

	// Subscript: _{group} or _char
	g.Define("subscript", grammargen.Seq(
		grammargen.Str("_"),
		grammargen.Choice(
			grammargen.Sym("group"),
			grammargen.Sym("_atom"),
		),
	))

	// Command: \name, \name{arg}, \name{arg1}{arg2}
	g.Define("command", grammargen.Choice(
		grammargen.Seq(
			grammargen.Field("name", grammargen.Sym("command_name")),
			grammargen.Field("arg1", grammargen.Sym("group")),
			grammargen.Field("arg2", grammargen.Sym("group")),
		),
		grammargen.Seq(
			grammargen.Field("name", grammargen.Sym("command_name")),
			grammargen.Field("arg1", grammargen.Sym("group")),
		),
		grammargen.Field("name", grammargen.Sym("command_name")),
	))

	// Command name token: \letters
	g.Define("command_name", grammargen.Token(grammargen.Seq(
		grammargen.Str("\\"),
		grammargen.Repeat1(grammargen.Pat(`[a-zA-Z]`)),
	)))

	// Single char atom (for bare ^x or _x)
	g.Define("_atom", grammargen.Token(grammargen.Pat(`[a-zA-Z0-9]`)))

	// Number
	g.Define("number", grammargen.Token(grammargen.Seq(
		grammargen.Optional(grammargen.Str("-")),
		grammargen.Repeat1(grammargen.Pat(`[0-9]`)),
		grammargen.Optional(grammargen.Seq(
			grammargen.Str("."),
			grammargen.Repeat1(grammargen.Pat(`[0-9]`)),
		)),
	)))

	// Text: plain letters
	g.Define("text", grammargen.Token(grammargen.Repeat1(grammargen.Pat(`[a-zA-Z]`))))

	// Operators
	g.Define("operator", grammargen.Token(grammargen.Pat(`[=+\-*/<>!|&~]`)))

	// Punctuation
	g.Define("punctuation", grammargen.Token(grammargen.Pat(`[,;:()\[\].]`)))

	// Whitespace
	g.SetExtras(grammargen.Pat(`[ \t\n\r]`))

	// Resolve ambiguity: two-arg commands before one-arg before bare
	grammargen.AddConflict(g, "command")

	return g
}

// Command → HTML mapping for zero-argument commands.
var latexSymbols = map[string]string{
	// Greek lowercase
	`\alpha`: "α", `\beta`: "β", `\gamma`: "γ", `\delta`: "δ",
	`\epsilon`: "ε", `\zeta`: "ζ", `\eta`: "η", `\theta`: "θ",
	`\iota`: "ι", `\kappa`: "κ", `\lambda`: "λ", `\mu`: "μ",
	`\nu`: "ν", `\xi`: "ξ", `\pi`: "π", `\rho`: "ρ",
	`\sigma`: "σ", `\tau`: "τ", `\upsilon`: "υ", `\phi`: "φ",
	`\chi`: "χ", `\psi`: "ψ", `\omega`: "ω",
	// Greek uppercase
	`\Gamma`: "Γ", `\Delta`: "Δ", `\Theta`: "Θ", `\Lambda`: "Λ",
	`\Xi`: "Ξ", `\Pi`: "Π", `\Sigma`: "Σ", `\Phi`: "Φ",
	`\Psi`: "Ψ", `\Omega`: "Ω",
	// Arrows
	`\rightarrow`: "→", `\leftarrow`: "←", `\leftrightarrow`: "↔",
	`\Rightarrow`: "⇒", `\Leftarrow`: "⇐", `\Leftrightarrow`: "⇔",
	`\to`: "→", `\gets`: "←", `\mapsto`: "↦",
	`\uparrow`: "↑", `\downarrow`: "↓",
	// Big operators
	`\sum`: "∑", `\prod`: "∏", `\int`: "∫", `\oint`: "∮",
	`\bigcup`: "⋃", `\bigcap`: "⋂", `\coprod`: "∐",
	// Relations
	`\leq`: "≤", `\geq`: "≥", `\neq`: "≠", `\approx`: "≈",
	`\equiv`: "≡", `\sim`: "∼", `\simeq`: "≃", `\propto`: "∝",
	`\subset`: "⊂", `\supset`: "⊃", `\subseteq`: "⊆", `\supseteq`: "⊇",
	`\in`: "∈", `\notin`: "∉", `\ni`: "∋",
	// Miscellaneous
	`\infty`: "∞", `\partial`: "∂", `\nabla`: "∇",
	`\forall`: "∀", `\exists`: "∃", `\neg`: "¬",
	`\cdot`: "·", `\cdots`: "⋯", `\ldots`: "…", `\dots`: "…",
	`\times`: "×", `\div`: "÷", `\pm`: "±", `\mp`: "∓",
	`\circ`: "∘", `\bullet`: "•", `\star`: "⋆",
	`\emptyset`: "∅", `\varnothing`: "∅",
	`\ell`: "ℓ", `\hbar`: "ℏ", `\Re`: "ℜ", `\Im`: "ℑ",
	// Spacing
	`\quad`: "\u2003", `\qquad`: "\u2003\u2003",
	`\,`: "\u2009", `\;`: "\u2005", `\!`: "",
}

// renderLatexMath parses a LaTeX math string and returns HTML.
// Falls back to escaped literal if parsing fails.
func renderLatexMath(src string) string {
	lang, err := latexLanguage()
	if err != nil || lang == nil {
		return html.EscapeString(src)
	}

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse([]byte(src))
	if err != nil || tree == nil {
		return html.EscapeString(src)
	}

	root := tree.RootNode()
	return renderLatexNode(root, lang, src)
}

func renderLatexNode(node *gotreesitter.Node, lang *gotreesitter.Language, src string) string {
	nodeType := node.Type(lang)

	switch nodeType {
	case "math":
		return renderLatexChildren(node, lang, src)

	case "group":
		return renderLatexChildren(node, lang, src)

	case "superscript":
		inner := ""
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child.Type(lang) != "^" {
				inner = renderLatexNode(child, lang, src)
			}
		}
		return "<sup>" + inner + "</sup>"

	case "subscript":
		inner := ""
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child.Type(lang) != "_" {
				inner = renderLatexNode(child, lang, src)
			}
		}
		return "<sub>" + inner + "</sub>"

	case "command":
		return renderLatexCommand(node, lang, src)

	case "number", "text", "operator", "punctuation":
		return html.EscapeString(latexNodeText(node, src))

	default:
		if node.ChildCount() > 0 {
			return renderLatexChildren(node, lang, src)
		}
		return html.EscapeString(latexNodeText(node, src))
	}
}

func renderLatexCommand(node *gotreesitter.Node, lang *gotreesitter.Language, src string) string {
	nameNode := node.ChildByFieldName("name", lang)
	if nameNode == nil {
		return html.EscapeString(latexNodeText(node, src))
	}
	name := latexNodeText(nameNode, src)

	arg1Node := node.ChildByFieldName("arg1", lang)
	arg2Node := node.ChildByFieldName("arg2", lang)

	// Zero-arg: symbol lookup
	if arg1Node == nil {
		if sym, ok := latexSymbols[name]; ok {
			return sym
		}
		// Unknown command: render as-is
		return html.EscapeString(name)
	}

	arg1 := renderLatexNode(arg1Node, lang, src)

	switch name {
	case `\frac`:
		arg2 := ""
		if arg2Node != nil {
			arg2 = renderLatexNode(arg2Node, lang, src)
		}
		return `<span class="math-frac"><span class="math-frac-num">` + arg1 + `</span><span class="math-frac-den">` + arg2 + `</span></span>`

	case `\sqrt`:
		return `<span class="math-sqrt">√<span class="math-sqrt-inner">` + arg1 + `</span></span>`

	case `\text`, `\mathrm`, `\textrm`:
		return `<span class="math-text">` + arg1 + `</span>`

	case `\textbf`, `\mathbf`:
		return `<strong>` + arg1 + `</strong>`

	case `\textit`, `\mathit`, `\emph`:
		return `<em>` + arg1 + `</em>`

	case `\hat`:
		return arg1 + "̂"
	case `\bar`:
		return arg1 + "̄"
	case `\tilde`:
		return arg1 + "̃"
	case `\vec`:
		return arg1 + "⃗"
	case `\dot`:
		return arg1 + "̇"
	case `\ddot`:
		return arg1 + "̈"

	case `\overline`:
		return `<span class="math-overline">` + arg1 + `</span>`
	case `\underline`:
		return `<span class="math-underline">` + arg1 + `</span>`

	case `\xrightarrow`:
		return `<span class="math-xarrow">→<sup class="math-xarrow-label">` + arg1 + `</sup></span>`
	case `\xleftarrow`:
		return `<span class="math-xarrow">←<sup class="math-xarrow-label">` + arg1 + `</sup></span>`

	case `\underbrace`:
		return `<span class="math-underbrace">` + arg1 + `</span>`
	case `\overbrace`:
		return `<span class="math-overbrace">` + arg1 + `</span>`

	case `\mathbb`:
		// Blackboard bold: map to Unicode double-struck
		return renderBlackboardBold(arg1)
	case `\mathcal`:
		return `<span class="math-cal">` + arg1 + `</span>`

	default:
		// Unknown one-arg command — render as function application
		if sym, ok := latexSymbols[name]; ok {
			return sym + "(" + arg1 + ")"
		}
		return html.EscapeString(name) + "(" + arg1 + ")"
	}
}

func renderLatexChildren(node *gotreesitter.Node, lang *gotreesitter.Language, src string) string {
	var b strings.Builder
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		t := child.Type(lang)
		if t == "{" || t == "}" || t == "^" || t == "_" {
			continue
		}
		b.WriteString(renderLatexNode(child, lang, src))
	}
	return b.String()
}

func latexNodeText(node *gotreesitter.Node, src string) string {
	start := node.StartByte()
	end := node.EndByte()
	if int(end) > len(src) {
		end = uint32(len(src))
	}
	return src[start:end]
}

var blackboardBold = map[rune]string{
	'A': "𝔸", 'B': "𝔹", 'C': "ℂ", 'D': "𝔻", 'E': "𝔼", 'F': "𝔽", 'G': "𝔾",
	'H': "ℍ", 'I': "𝕀", 'J': "𝕁", 'K': "𝕂", 'L': "𝕃", 'M': "𝕄", 'N': "ℕ",
	'O': "𝕆", 'P': "ℙ", 'Q': "ℚ", 'R': "ℝ", 'S': "𝕊", 'T': "𝕋", 'U': "𝕌",
	'V': "𝕍", 'W': "𝕎", 'X': "𝕏", 'Y': "𝕐", 'Z': "ℤ",
}

func renderBlackboardBold(s string) string {
	var b strings.Builder
	for _, r := range s {
		if bb, ok := blackboardBold[r]; ok {
			b.WriteString(bb)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

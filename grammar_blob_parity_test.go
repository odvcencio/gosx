//go:build !tinygo

package gosx

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Blob and in-code grammar parity.
//
// Language() loads gosx-grammar.blob and never calls GosxGrammar() when the
// blob is present. A stale blob therefore parses differently from the grammar
// in this repository, and no other test sees the difference. These tests parse
// one corpus through both paths and compare the concrete syntax trees.
//
// Regenerate the blob after any grammar change:
//
//	go run ./cmd/gosx-grammar-blob

var (
	parityBlobLangOnce sync.Once
	parityBlobLang     *gotreesitter.Language
	parityBlobLangErr  error

	parityCodeLangOnce sync.Once
	parityCodeLang     *gotreesitter.Language
	parityCodeLangErr  error
)

// blobLanguage returns the language decoded from the embedded blob.
func blobLanguage(t testing.TB) *gotreesitter.Language {
	t.Helper()
	parityBlobLangOnce.Do(func() {
		if len(embeddedGrammarBlob) == 0 {
			parityBlobLangErr = fmt.Errorf("embedded grammar blob is empty")
			return
		}
		parityBlobLang, parityBlobLangErr = LoadLanguageBlob(embeddedGrammarBlob)
		if parityBlobLangErr == nil && parityBlobLang != nil {
			parityBlobLang.ExternalScanner = newGSXScanner(parityBlobLang)
		}
	})
	if parityBlobLangErr != nil {
		t.Fatalf("load embedded grammar blob: %v", parityBlobLangErr)
	}
	return parityBlobLang
}

// codeLanguage returns the language generated from GosxGrammar().
func codeLanguage(t testing.TB) *gotreesitter.Language {
	t.Helper()
	parityCodeLangOnce.Do(func() {
		parityCodeLang, _, parityCodeLangErr = GenerateLanguageAndBlob(GosxGrammar())
		if parityCodeLangErr == nil && parityCodeLang != nil {
			parityCodeLang.ExternalScanner = newGSXScanner(parityCodeLang)
		}
	})
	if parityCodeLangErr != nil {
		t.Fatalf("generate language from GosxGrammar(): %v", parityCodeLangErr)
	}
	return parityCodeLang
}

// dumpCST writes a full tree description: node type, byte range, field name,
// and the named, missing, and error flags. Node.SExpr drops anonymous nodes,
// field names, and ranges, so it is too weak for a parity check.
func dumpCST(root *gotreesitter.Node, lang *gotreesitter.Language) string {
	var b strings.Builder
	var walk func(n *gotreesitter.Node, field string, depth int)
	walk = func(n *gotreesitter.Node, field string, depth int) {
		if n == nil {
			return
		}
		b.WriteString(strings.Repeat("  ", depth))
		if field != "" {
			b.WriteString(field)
			b.WriteString(": ")
		}
		fmt.Fprintf(&b, "%s [%d,%d)", n.Type(lang), n.StartByte(), n.EndByte())
		if !n.IsNamed() {
			b.WriteString(" anon")
		}
		if n.IsMissing() {
			b.WriteString(" missing")
		}
		if n.IsError() {
			b.WriteString(" error")
		}
		b.WriteByte('\n')
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i), n.FieldNameForChild(i, lang), depth+1)
		}
	}
	walk(root, "", 0)
	return b.String()
}

// parseWith parses source with one explicit language.
func parseWith(t testing.TB, lang *gotreesitter.Language, source []byte) *gotreesitter.Node {
	t.Helper()
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree.RootNode()
}

// firstDiffLine reports the first differing line of two dumps.
func firstDiffLine(a, b string) string {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	n := len(al)
	if len(bl) < n {
		n = len(bl)
	}
	for i := 0; i < n; i++ {
		if al[i] != bl[i] {
			return fmt.Sprintf("line %d:\n  blob: %q\n  code: %q", i+1, al[i], bl[i])
		}
	}
	if len(al) != len(bl) {
		return fmt.Sprintf("dump length differs: blob %d lines, code %d lines", len(al), len(bl))
	}
	return "no line difference found"
}

// parityCorpus holds the GSX shapes whose parses the grammar comments call out
// as fragile. Each entry guards one documented LR-generator failure.
var parityCorpus = []struct {
	name   string
	source string
}{
	{"plain_element", `package main

func App() Node {
	return <div class="counter">hello</div>
}
`},
	{"nested_elements", `package main

func App() Node {
	return <div><span>inner</span></div>
}
`},
	{"nested_inline_anchor_with_expression_child", `package main

func App(data Data) Node {
	return <span class="music-title"><a href={data.post.Music} target="_blank" rel="noopener">{data.musicTitle}</a></span>
}
`},
	{"nested_child_self_closing_then_element", `package main

func App(data Data) Node {
	return <span class="post-mood"><img class={"mood-icon " + data.moodAnim} src={data.moodIcon} alt={data.post.Mood} width="18" height="18" /><span class="mood-label">{data.post.Mood}</span></span>
}
`},
	{"component_child_wraps_self_closing_then_element", `package main

func App(data Data) Node {
	return <If when={data.post.Mood != ""}><span class="post-mood"><img class={"mood-icon " + data.moodAnim} src={data.moodIcon} alt={data.post.Mood} width="18" height="18" /><span class="mood-label">{data.post.Mood}</span></span></If>
}
`},
	{"nested_component_wraps_self_closing_then_element", `package main

func App(data Data) Node {
	return <If when={data.post.Mood != "" || data.post.Music != ""}><div class="post-mood-music"><If when={data.post.Mood != ""}><span class="post-mood"><img class={"mood-icon " + data.moodAnim} src={data.moodIcon} alt={data.post.Mood} width="18" height="18" /><span class="mood-label">{data.post.Mood}</span></span></If></div></If>
}
`},
	{"multiline_nested_component_wraps_self_closing_then_element", `package main

func App(data Data) Node {
	return <If when={data.post.Mood != "" || data.post.Music != ""}>
		<div class="post-mood-music">
			<If when={data.post.Mood != ""}>
				<span class="post-mood">
					<img class={"mood-icon " + data.moodAnim} src={data.moodIcon} alt={data.post.Mood} width="18" height="18" />
					<span class="mood-label">{data.post.Mood}</span>
				</span>
			</If>
		</div>
	</If>
}
`},
	{"element_parent_wraps_nested_component_with_self_closing_child", `package main

func App(data Data) Node {
	return <header class="post-header">
		<If when={data.post.Mood != "" || data.post.Music != ""}>
			<div class="post-mood-music">
				<If when={data.post.Mood != ""}>
					<span class="post-mood">
						<img class={"mood-icon " + data.moodAnim} src={data.moodIcon} alt={data.post.Mood} width="18" height="18" />
						<span class="mood-label">{data.post.Mood}</span>
					</span>
				</If>
			</div>
		</If>
	</header>
}
`},
	{"sibling_button_before_nested_inline_anchor", `package main

func App(data Data) Node {
	return <span class="post-music"><button class="music-play" data-youtube-url={data.post.Music} aria-label="Play music">&#9654;</button><span class="music-title"><a href={data.post.Music} target="_blank" rel="noopener">{data.musicTitle}</a></span></span>
}
`},
	{"post_header_mood_music_conditionals", `package main

func App(data Data) Node {
	return <header class="post-header">
		<If when={data.post.Mood != "" || data.post.Music != ""}>
			<div class="post-mood-music">
				<If when={data.post.Mood != ""}>
					<span class="post-mood">
						<img class={"mood-icon " + data.moodAnim} src={data.moodIcon} alt={data.post.Mood} width="18" height="18" />
						<span class="mood-label">{data.post.Mood}</span>
					</span>
				</If>
				<If when={data.post.Music != ""}>
					<span class="post-music">
						<button class="music-play" data-youtube-url={data.post.Music} aria-label="Play music">&#9654;</button>
						<span class="music-title"><a href={data.post.Music} target="_blank" rel="noopener">{data.musicTitle}</a></span>
					</span>
				</If>
			</div>
		</If>
	</header>
}
`},
	{"inline_if_with_element_child", `package main

func App(data Data) Node {
	return <If when={len(data.attribution.TopSources) == 0}><p class="admin-empty">No source data yet.</p></If>
}
`},
	{"multiline_text_with_inline_code_and_trailing_dot", `package main

func App(data Data) Node {
	return <article class="prose">
		<p>
			The auth middleware resolves the current user once, stores it on the request context, and exposes it to file-routed
			<span class="inline-code">.gsx</span>
			pages as
			<span class="inline-code">user</span>
			.
		</p>
	</article>
}
`},
	{"docs_auth_template_form_and_callout", `package main

func App(data Data) Node {
	return <article class="prose">
		<div class="page-topper">
			<span class="eyebrow">Auth</span>
			<p class="lede">
				Session-backed auth state now rides on the same request context as file pages, actions, and route middleware.
			</p>
		</div>
		<h1>
			Auth in GoSX is a session concern, not a separate framework bolted on later.
		</h1>
		<p>
			The auth middleware resolves the current user once, stores it on the request context, and exposes it to file-routed
			<span class="inline-code">.gsx</span>
			pages as
			<span class="inline-code">user</span>
			.
		</p>
		<div class="note-grid">
			<div class="note">
				<strong>Current user</strong>
				<p>{user.name}</p>
			</div>
			<div class="note">
				<strong>Session flash</strong>
				<p>{flash.notice}</p>
			</div>
		</div>
		<form class="docs-form" method="post" action="/gosx/action/signIn">
			<input type="hidden" name="csrf_token" value={csrf.token}></input>
			<label class="field">
				<span>Name</span>
				<input name="name" required></input>
			</label>
			<div class="hero-actions">
				<button class="cta-link primary" type="submit">Sign in to the docs demo</button>
				<button class="cta-link" type="submit" formaction="/gosx/action/signOut">Sign out</button>
			</div>
		</form>
		<section class="callout">
			<strong>Protected route</strong>
			<p>
				Try the guarded lab route:
				<a href="/labs/secret" data-gosx-link class="cta-link">Open the secret page</a>
			</p>
		</section>
	</article>
}
`},
	{"text_adjacent_to_tags", `package main

func App() Node {
	return <p>a<span>b</span>c</p>
}
`},
	{"component_and_expression", `package main

func App(n int) Node {
	return <Counter count={n} label="hits" />
}
`},
	{"fragment", `package main

func App() Node {
	return <><span>a</span><span>b</span></>
}
`},
	{"spread_attribute", `package main

func App(p Props) Node {
	return <div {...p}>text</div>
}
`},
	{"ternary_in_text_child", `package main

func App(c Signal) Node {
	return <span>{c.Get() ? "yes" : "no"}</span>
}
`},
	{"conditional_and", `package main

func App(ok bool) Node {
	return <div>{ok && <span>yes</span>}</div>
}
`},
	// Raw-text elements are children, never top-level expressions. The
	// grammar adds jsx_element, jsx_self_closing_element, and jsx_fragment to
	// _expression, but keeps jsx_raw_text_element inside _jsx_child. Each raw
	// case therefore sits inside an ordinary element.
	{"raw_text_script", `package main

func App() Node {
	return <div><script>if (a < b) { go(); }</script></div>
}
`},
	{"raw_text_style", `package main

func App() Node {
	return <div><style>.a { color: red; }</style></div>
}
`},
	{"script_expression_hole", `package main

func App() Node {
	return <div><script>{ClientScript()}</script></div>
}
`},
	{"raw_text_element_with_attrs", `package main

func App() Node {
	return <div><script type="module">export const a = 1 < 2;</script></div>
}
`},
	{"raw_text_then_sibling_element", `package main

func App() Node {
	return <div><script>var s = "</div>";</script><span>after</span></div>
}
`},
	{"island_directive", `package main

//gosx:island
func Counter() Node {
	count := Signal(0)
	return <button onClick={count.Set}>{count.Get()}</button>
}
`},
	{"dotted_component", `package main

func App() Node {
	return <ui.Card title="hi"><p>body</p></ui.Card>
}
`},
	{"plain_go_only", `package main

import "fmt"

func main() {
	for i := 0; i < 3; i++ {
		fmt.Println(i)
	}
}
`},
}

// TestGrammarBlobMatchesInCodeGrammar proves the two parse paths agree.
//
// Language() prefers gosx-grammar.blob over GosxGrammar(). A stale blob would
// otherwise parse differently from the checked-in grammar without any failure.
func TestGrammarBlobMatchesInCodeGrammar(t *testing.T) {
	if testing.Short() {
		t.Skip("grammar generation is slow; skipped in short mode")
	}
	blobLang := blobLanguage(t)
	codeLang := codeLanguage(t)

	for _, tc := range parityCorpus {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.source)
			blobDump := dumpCST(parseWith(t, blobLang, src), blobLang)
			codeDump := dumpCST(parseWith(t, codeLang, src), codeLang)
			if blobDump != codeDump {
				t.Fatalf("blob and in-code grammar disagree.\n%s\n\nblob tree:\n%s\ncode tree:\n%s\nRegenerate with: go run ./cmd/gosx-grammar-blob",
					firstDiffLine(blobDump, codeDump), blobDump, codeDump)
			}
		})
	}
}

// TestGrammarBlobParsesCorpusWithoutErrors checks the corpus parses cleanly.
//
// Parity alone passes when both paths break the same way. This test also
// requires each corpus entry to produce a tree with no ERROR node.
func TestGrammarBlobParsesCorpusWithoutErrors(t *testing.T) {
	lang, err := Language()
	if err != nil {
		t.Fatalf("Language(): %v", err)
	}
	for _, tc := range parityCorpus {
		t.Run(tc.name, func(t *testing.T) {
			root := parseWith(t, lang, []byte(tc.source))
			if root.HasError() {
				t.Fatalf("parse error:\n%s", dumpCST(root, lang))
			}
		})
	}
}

func TestGrammarBlobParsesCRLFTemplateWithoutErrors(t *testing.T) {
	lang, err := Language()
	if err != nil {
		t.Fatalf("Language(): %v", err)
	}
	for _, tc := range parityCorpus {
		if tc.name != "docs_auth_template_form_and_callout" {
			continue
		}
		src := []byte(strings.ReplaceAll(tc.source, "\n", "\r\n"))
		root := parseWith(t, lang, src)
		if root.HasError() {
			t.Fatalf("parse error:\n%s", dumpCST(root, lang))
		}
		return
	}
	t.Fatal("docs_auth_template_form_and_callout fixture missing")
}

// gsxCorpusFiles collects the .gsx files that ship in this repository.
func gsxCorpusFiles(t testing.TB) []string {
	t.Helper()
	roots := []string{"examples", "cmd/gosx/templates", "route"}
	var files []string
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "dist" {
					return fs.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".gsx") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return files
}

// TestGrammarBlobMatchesInCodeGrammarOnRepoCorpus repeats the parity check on
// every .gsx file in the repository. The inline corpus covers known shapes;
// this covers the code that actually ships.
func TestGrammarBlobMatchesInCodeGrammarOnRepoCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("grammar generation is slow; skipped in short mode")
	}
	files := gsxCorpusFiles(t)
	if len(files) == 0 {
		// Fail, never skip. The repository always holds .gsx files, so an empty
		// corpus means the walk roots went stale. A skip would hide that and
		// report green with the shipped-code parity check switched off.
		t.Fatal("no .gsx files found in the repository corpus; the walk roots in gsxCorpusFiles went stale")
	}
	blobLang := blobLanguage(t)
	codeLang := codeLanguage(t)

	for _, path := range files {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			blobDump := dumpCST(parseWith(t, blobLang, src), blobLang)
			codeDump := dumpCST(parseWith(t, codeLang, src), codeLang)
			if blobDump != codeDump {
				t.Fatalf("blob and in-code grammar disagree on %s.\n%s\nRegenerate with: go run ./cmd/gosx-grammar-blob",
					path, firstDiffLine(blobDump, codeDump))
			}
		})
	}
}

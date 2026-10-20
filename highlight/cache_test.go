//go:build !js || !wasm

package highlight

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"sync"
	"testing"
)

func unpooledGoParse(source string) (*parseResult, error) {
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

func TestHighlightCachedAndPooledOutput(t *testing.T) {
	for _, sample := range []struct{ lang, source string }{
		{LangGo, benchGo}, {LangGo, "var x MyType"}, {LangGo, "func {"},
		{LangGoSX, benchGSX}, {LangGoSX, "component Card(props: Props) { return <div>{props.Name}</div> }"},
		{LangBash, benchBash}, {LangJavaScript, benchJS}, {LangJSON, benchJSON},
		{LangText, "<script>&"}, {LangGo, ""},
	} {
		t.Run(sample.lang+sample.source, func(t *testing.T) {
			var want string
			if sample.lang == LangGo && sample.source != "" {
				spec, err := unpooledGoParse(sample.source)
				if err != nil {
					t.Fatal(err)
				}
				defer spec.release()
				want = renderSegment(spec)
			} else if filename := map[string]string{LangBash: "snippet.sh", LangJavaScript: "snippet.js", LangJSON: "snippet.json"}[sample.lang]; filename != "" {
				tree, err := grammars.ParseFile(filename, []byte(sample.source))
				if err != nil {
					t.Fatal(err)
				}
				defer tree.Release()
				want = renderSegment(&parseResult{source: []byte(sample.source), treeSrc: tree.Source(), root: tree.RootNode(), lang: tree.Language(), classify: classifyGenericNode})
			} else {
				want = renderHTML(sample.lang, sample.source)
			}
			for i := 0; i < 3; i++ {
				if got := HTML(sample.lang, sample.source); got != want {
					t.Fatalf("output changed: got %q; want %q", got, want)
				}
			}
		})
	}
}

func TestHighlightCacheBoundsAndLRU(t *testing.T) {
	c := newHTMLCache(2, 512)
	a, b, d := htmlCacheKey{language: LangGo}, htmlCacheKey{language: LangBash}, htmlCacheKey{language: LangJSON}
	c.put(a, "a")
	c.put(b, "b")
	c.get(a)
	c.put(d, "d")
	if _, ok := c.get(b); ok {
		t.Fatal("least recently used entry retained")
	}
	c.put(a, string(make([]byte, 513)))
	if got, _ := c.get(a); got != "a" {
		t.Fatal("oversized entry replaced cached value")
	}
	c.put(htmlCacheKey{language: LangText}, string(make([]byte, 300)))
	if _, ok := c.get(a); ok {
		t.Fatal("byte budget did not evict old entry")
	}
	if c.bytes > 512 || c.order.Len() > 2 {
		t.Fatal("cache exceeded bounds")
	}
}

func TestHighlightConcurrent(t *testing.T) {
	want := HTML(LangGo, benchGo)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if got := HTML(LangGo, benchGo); got != want {
					t.Error("concurrent output changed")
				}
				if got := renderHTML(LangGo, benchGo); got != want {
					t.Error("pooled output changed")
				}
			}
		}()
	}
	wg.Wait()
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

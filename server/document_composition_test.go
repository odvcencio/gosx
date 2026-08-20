package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestHTMLDocumentEscapesDocumentFieldsAndComposesBodyAttrs(t *testing.T) {
	doc := &DocumentContext{
		Title:    ` <title>&" </title> `,
		Language: "  en-US\"><script>alert(1)</script>  ",
		Body:     gosx.El("main", gosx.Text("content")),
		BodyAttrs: gosx.Attrs(
			gosx.Attr("data-hostile", `"><script>alert(1)</script>`),
		),
	}
	rendered := gosx.RenderHTML(HTMLDocument(doc))

	if strings.Contains(rendered, `<title> <title>`) || strings.Contains(rendered, `</title> <`) {
		t.Fatalf("title was not treated as text: %q", rendered)
	}
	for _, escaped := range []string{
		`&lt;title&gt;&amp;&#34;`,
		`lang="en-US&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`,
		`data-hostile="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`,
	} {
		if !strings.Contains(rendered, escaped) {
			t.Fatalf("expected escaped fragment %q in %q", escaped, rendered)
		}
	}
	if strings.Contains(rendered, `<script>alert(1)</script>`) {
		t.Fatalf("hostile document fields reached HTML unescaped: %q", rendered)
	}
	if !strings.Contains(rendered, `<body data-gosx-document-body="true" data-gosx-enhancement-layer="html" data-hostile=`) {
		t.Fatalf("framework and application body attrs did not compose: %q", rendered)
	}
}

func TestHTMLDocumentOwnsExactlyOneViewport(t *testing.T) {
	doc := &DocumentContext{
		Head: gosx.Fragment(
			gosx.RawHTML(`<meta name="description" content="document shell">`),
		),
	}
	rendered := gosx.RenderHTML(HTMLDocument(doc))
	if got := strings.Count(rendered, `<meta name="viewport"`); got != 1 {
		t.Fatalf("expected exactly one framework-owned viewport, got %d in %q", got, rendered)
	}
	if !strings.Contains(rendered, `<meta name="viewport" content="width=device-width, initial-scale=1">`) {
		t.Fatalf("missing framework viewport in %q", rendered)
	}
}

func TestHTMLDocumentDirectContextAddsOneNonceBearingContractWithoutMutation(t *testing.T) {
	doc := &DocumentContext{
		Request: httptest.NewRequest(http.MethodGet, "/direct", nil),
		Title:   "Direct",
		Nonce:   "framework-nonce",
		Head:    gosx.RawHTML(`<script data-gosx-document-contract nonce="raw-nonce">{"raw":true}</script>`),
		Body:    gosx.El("main", gosx.Text("direct")),
	}
	before := *doc
	beforeHead := gosx.RenderHTML(doc.Head)
	rendered := gosx.RenderHTML(HTMLDocument(doc))

	if !reflect.DeepEqual(before, *doc) {
		t.Fatalf("HTMLDocument mutated its input context: before=%#v after=%#v", before, *doc)
	}
	if got := gosx.RenderHTML(doc.Head); got != beforeHead {
		t.Fatalf("HTMLDocument mutated the input head: before=%q after=%q", beforeHead, got)
	}
	if got := strings.Count(rendered, "data-gosx-document-contract"); got != 2 {
		// The application deliberately supplied an arbitrary RawHTML contract;
		// the framework must not inspect or rewrite it. The second marker is the
		// one framework-owned contract added for the direct context.
		t.Fatalf("expected the supplied raw contract plus one framework contract, got %d in %q", got, rendered)
	}
	if !strings.Contains(rendered, `data-gosx-document-contract nonce="raw-nonce"`) {
		t.Fatalf("arbitrary RawHTML contract was rewritten: %q", rendered)
	}
	if !strings.Contains(rendered, `data-gosx-document-contract nonce="framework-nonce"`) {
		t.Fatalf("direct context did not receive its nonce-bearing framework contract: %q", rendered)
	}
}

func TestHTMLDocumentPreparedHeadIsAuthoritativeAndCustomWrapperDoesNotDuplicate(t *testing.T) {
	ctx := newContext(httptest.NewRequest(http.MethodGet, "/prepared", nil))
	ctx.SetNonce("prepared-nonce")
	prepared := ctx.documentContext("GET /prepared", "Prepared", gosx.Text("body"), true)
	if !prepared.documentContractPrepared {
		t.Fatal("request pipeline did not mark its prepared document contract")
	}
	before := gosx.RenderHTML(prepared.Head)

	rendered := gosx.RenderHTML(HTMLDocument(prepared))
	if got := strings.Count(rendered, "data-gosx-document-contract"); got != 1 {
		t.Fatalf("prepared head was duplicated, got %d contract markers in %q", got, rendered)
	}
	if !strings.Contains(rendered, `data-gosx-document-contract nonce="prepared-nonce"`) {
		t.Fatalf("prepared contract lost its nonce: %q", rendered)
	}
	if after := gosx.RenderHTML(prepared.Head); after != before {
		t.Fatalf("rendering a prepared document mutated its head: before=%q after=%q", before, after)
	}

	app := New()
	app.Page("GET /", func(ctx *Context) gosx.Node { return gosx.Text("wrapped") })
	app.SetDocument(func(doc *DocumentContext) gosx.Node { return HTMLDocument(doc) })
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := strings.Count(w.Body.String(), "data-gosx-document-contract"); got != 1 {
		t.Fatalf("custom HTMLDocument wrapper duplicated contract, got %d in %q", got, w.Body.String())
	}
}

func TestHTMLDocumentNilAndZeroContextsAreValid(t *testing.T) {
	tests := []struct {
		name string
		doc  *DocumentContext
	}{
		{name: "nil"},
		{name: "zero", doc: &DocumentContext{}},
		{
			name: "explicit zero head and body",
			doc: &DocumentContext{
				Head: gosx.Node{},
				Body: gosx.Node{},
			},
		},
		{
			name: "prepared marker with zero head",
			doc: &DocumentContext{
				Head:                     gosx.Node{},
				Body:                     gosx.Node{},
				documentContractPrepared: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var before *DocumentContext
			if tt.doc != nil {
				copy := *tt.doc
				before = &copy
			}

			rendered := gosx.RenderHTML(HTMLDocument(tt.doc))
			for _, shell := range []string{
				"<!DOCTYPE html>",
				`<html data-gosx-document="true">`,
				"<head>",
				"<title></title>",
				`<body data-gosx-document-body="true" data-gosx-enhancement-layer="html">`,
				"</body>",
				"</html>",
			} {
				if !strings.Contains(rendered, shell) {
					t.Fatalf("empty document is missing shell fragment %q: %q", shell, rendered)
				}
			}
			if strings.Contains(rendered, "<></>") {
				t.Fatalf("zero node leaked a synthetic empty tag into the document: %q", rendered)
			}
			if got := strings.Count(rendered, "data-gosx-document-contract"); got != 1 {
				t.Fatalf("expected one framework contract, got %d in %q", got, rendered)
			}
			if before != nil && !reflect.DeepEqual(*before, *tt.doc) {
				t.Fatalf("HTMLDocument mutated its empty input: before=%#v after=%#v", *before, *tt.doc)
			}
		})
	}
}

func TestDocumentRendererCleanBreakRemovesRetiredSymbols(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(filepath.Dir(file))
	var checked int
	retired := []string{"HTMLDocumentWith" + "Language", "HTMLDocumentWith" + "Nonce", "HTMLDocumentWith" + "BodyAttrs", "renderDocumentWith" + "Context"}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".gotmpl" && ext != ".gsx" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		for _, symbol := range retired {
			if strings.Contains(string(data), symbol) {
				return &retiredRendererError{path: path, symbol: symbol}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("did not inspect any server implementation files")
	}
}

type retiredRendererError struct {
	path   string
	symbol string
}

func (e *retiredRendererError) Error() string {
	return "retired document renderer symbol " + e.symbol + " remains in " + e.path
}

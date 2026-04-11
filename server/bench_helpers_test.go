package server

import "github.com/odvcencio/gosx"

// benchSimplePageNode is a tiny body node for the lightweight bench cases.
func benchSimplePageNode() gosx.Node {
	return gosx.El("main",
		gosx.Attrs(gosx.Attr("class", "main")),
		gosx.El("h1", gosx.Text("Hello world")),
		gosx.El("p", gosx.Text("A small server-rendered page used by benchmarks.")),
	)
}

// benchComplexPageNode is a moderately deep body — nav, hero, list of cards,
// footer — that exercises attribute writing and nested children. Mirrors the
// shape of a typical landing page so renderDocumentWithContext bench numbers
// reflect realistic work.
func benchComplexPageNode() gosx.Node {
	cardsArgs := []any{gosx.Attrs(gosx.Attr("class", "cards"))}
	for i := 0; i < 6; i++ {
		cardsArgs = append(cardsArgs, gosx.El("article",
			gosx.Attrs(
				gosx.Attr("class", "card"),
				gosx.Attr("data-index", "card"),
			),
			gosx.El("h2", gosx.Text("Card title")),
			gosx.El("p", gosx.Text("A short description of the card body content.")),
			gosx.El("a",
				gosx.Attrs(gosx.Attr("href", "/cards/item"), gosx.Attr("class", "more")),
				gosx.Text("Read more"),
			),
		))
	}

	return gosx.El("body",
		gosx.El("header",
			gosx.Attrs(gosx.Attr("class", "site-header")),
			gosx.El("nav",
				gosx.El("a", gosx.Attrs(gosx.Attr("href", "/")), gosx.Text("Home")),
				gosx.El("a", gosx.Attrs(gosx.Attr("href", "/docs")), gosx.Text("Docs")),
				gosx.El("a", gosx.Attrs(gosx.Attr("href", "/blog")), gosx.Text("Blog")),
			),
		),
		gosx.El("main",
			gosx.Attrs(gosx.Attr("class", "main")),
			gosx.El("section",
				gosx.Attrs(gosx.Attr("class", "hero")),
				gosx.El("h1", gosx.Text("A bigger heading")),
				gosx.El("p", gosx.Text("Hero subtitle for the page rendering benchmark.")),
			),
			gosx.El("section", cardsArgs...),
		),
		gosx.El("footer",
			gosx.Attrs(gosx.Attr("class", "site-footer")),
			gosx.Text("© bench"),
		),
	)
}

// benchSimplePageContext returns a DocumentContext with no head additions
// for the simple renderDocument bench.
func benchSimplePageContext() *DocumentContext {
	return &DocumentContext{
		Title: "Bench Page",
		Head:  gosx.Text(""),
		Body:  benchSimplePageNode(),
	}
}

// benchComplexPageContext returns a DocumentContext with a few head fragments
// (meta tags) plus the complex body, mirroring a realistic page render.
func benchComplexPageContext() *DocumentContext {
	head := gosx.Fragment(
		gosx.El("meta", gosx.Attrs(gosx.Attr("name", "description"), gosx.Attr("content", "Bench page description"))),
		gosx.El("meta", gosx.Attrs(gosx.Attr("property", "og:title"), gosx.Attr("content", "Bench"))),
		gosx.El("link", gosx.Attrs(gosx.Attr("rel", "canonical"), gosx.Attr("href", "https://example.test/bench"))),
	)
	return &DocumentContext{
		Title: "Bench Page — Complex",
		Head:  head,
		Body:  benchComplexPageNode(),
	}
}

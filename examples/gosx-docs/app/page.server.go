package docs

import "github.com/odvcencio/gosx/route"

func init() {
	RegisterStaticDocsPage(
		"Overview",
		"A paper-and-ink docs surface that exists to prove GoSX can route files, swap pages, validate forms, and stay coherent.",
		route.FileModuleOptions{},
	)
}

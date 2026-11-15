// Package markup builds GoSX node trees and renders them to HTML, without the
// GoSX compiler.
//
// Use it in browser (js/wasm) code that builds HTML from Go, such as the game
// kit screens. Package gosx re-exports every name here (gosx.Node is an alias
// of markup.Node, gosx.El calls markup.El), but importing package gosx also
// links the .gsx parser and compiler, which adds megabytes to a WASM binary.
// Server code can use either package; the values are the same.
//
//	n := markup.El("p", markup.Attrs(markup.Attr("class", "note")), markup.Text("Saved"))
//	html := markup.RenderHTML(n) // <p class="note">Saved</p>
//
// The package also defines the managed-form and progressive-enhancement
// attributes, because RenderHTML expands the managed-form shorthand on a
// <form> element. The rest of the runtime attribute contract stays in package
// gosx.
//
// The js/wasm dependency guard in game/deps_test.go fails if this package
// imports the compiler, the hub or net/http.
package markup

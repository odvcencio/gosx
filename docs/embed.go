// Package docs embeds the Markdown guides in this folder so the docs site can
// render them from one source. The guides stay plain Markdown, readable on
// GitHub; this package only exposes them to Go.
package docs

import "embed"

// Guides holds every Markdown guide in docs/.
//
//go:embed *.md
var Guides embed.FS

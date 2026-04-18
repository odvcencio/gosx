// Package markdown preserves the historical GoSX markdown import path.
//
// New code should import github.com/odvcencio/mdpp directly. This package is
// intentionally a thin compatibility layer so downstream projects can migrate
// at their own pace while GoSX shares the same parser and renderer.
package markdown

import (
	"net/http"

	"github.com/odvcencio/mdpp"
)

type NodeType = mdpp.NodeType

const (
	NodeDocument        = mdpp.NodeDocument
	NodeHeading         = mdpp.NodeHeading
	NodeParagraph       = mdpp.NodeParagraph
	NodeCodeBlock       = mdpp.NodeCodeBlock
	NodeBlockquote      = mdpp.NodeBlockquote
	NodeList            = mdpp.NodeList
	NodeListItem        = mdpp.NodeListItem
	NodeTable           = mdpp.NodeTable
	NodeTableRow        = mdpp.NodeTableRow
	NodeTableCell       = mdpp.NodeTableCell
	NodeThematicBreak   = mdpp.NodeThematicBreak
	NodeLink            = mdpp.NodeLink
	NodeImage           = mdpp.NodeImage
	NodeEmphasis        = mdpp.NodeEmphasis
	NodeStrong          = mdpp.NodeStrong
	NodeStrikethrough   = mdpp.NodeStrikethrough
	NodeCodeSpan        = mdpp.NodeCodeSpan
	NodeText            = mdpp.NodeText
	NodeSoftBreak       = mdpp.NodeSoftBreak
	NodeHardBreak       = mdpp.NodeHardBreak
	NodeHTMLBlock       = mdpp.NodeHTMLBlock
	NodeHTMLInline      = mdpp.NodeHTMLInline
	NodeFootnoteRef     = mdpp.NodeFootnoteRef
	NodeFootnoteDef     = mdpp.NodeFootnoteDef
	NodeMathInline      = mdpp.NodeMathInline
	NodeMathBlock       = mdpp.NodeMathBlock
	NodeAdmonition      = mdpp.NodeAdmonition
	NodeDefinitionList  = mdpp.NodeDefinitionList
	NodeDefinitionTerm  = mdpp.NodeDefinitionTerm
	NodeDefinitionDesc  = mdpp.NodeDefinitionDesc
	NodeSuperscript     = mdpp.NodeSuperscript
	NodeSubscript       = mdpp.NodeSubscript
	NodeTaskListItem    = mdpp.NodeTaskListItem
	NodeFrontmatter     = mdpp.NodeFrontmatter
	NodeTableOfContents = mdpp.NodeTableOfContents
	NodeAutoEmbed       = mdpp.NodeAutoEmbed
	NodeEmoji           = mdpp.NodeEmoji
	NodeDiagram         = mdpp.NodeDiagram
)

type Node = mdpp.Node
type Document = mdpp.Document
type TOCEntry = mdpp.TOCEntry
type Heading = mdpp.Heading
type Renderer = mdpp.Renderer
type Option = mdpp.Option

func Parse(source []byte) *Document {
	return mdpp.Parse(source)
}

func NewRenderer(opts ...Option) *Renderer {
	return mdpp.NewRenderer(opts...)
}

func WithHighlightCode(enabled bool) Option {
	return mdpp.WithHighlightCode(enabled)
}

func WithHeadingIDs(enabled bool) Option {
	return mdpp.WithHeadingIDs(enabled)
}

func WithUnsafeHTML(enabled bool) Option {
	return mdpp.WithUnsafeHTML(enabled)
}

func WithHardWraps(enabled bool) Option {
	return mdpp.WithHardWraps(enabled)
}

func WithWrapEmoji(enabled bool) Option {
	return mdpp.WithWrapEmoji(enabled)
}

func WithImageResolver(fn func(string) string) Option {
	return mdpp.WithImageResolver(fn)
}

func RenderString(source string) string {
	return mdpp.RenderString(source)
}

func GrammarBlobHandler() http.Handler {
	return mdpp.GrammarBlobHandler()
}

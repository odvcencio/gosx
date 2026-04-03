package markdown

// Renderer converts Markdown source into HTML.
type Renderer struct {
	highlightCode bool
	headingIDs    bool
	unsafeHTML    bool
	hardWraps     bool
	imageResolver func(string) string
}

// Option configures a Renderer.
type Option func(*Renderer)

// NewRenderer creates a Renderer with the given options.
func NewRenderer(opts ...Option) *Renderer {
	r := &Renderer{}
	for _, o := range opts {
		o(r)
	}
	return r
}

// WithHighlightCode enables or disables code highlighting placeholders.
func WithHighlightCode(enabled bool) Option {
	return func(r *Renderer) { r.highlightCode = enabled }
}

// WithHeadingIDs enables or disables automatic id attributes on headings.
func WithHeadingIDs(enabled bool) Option {
	return func(r *Renderer) { r.headingIDs = enabled }
}

// WithUnsafeHTML enables or disables raw HTML passthrough.
func WithUnsafeHTML(enabled bool) Option {
	return func(r *Renderer) { r.unsafeHTML = enabled }
}

// WithHardWraps makes single newlines render as <br> instead of whitespace.
func WithHardWraps(enabled bool) Option {
	return func(r *Renderer) { r.hardWraps = enabled }
}

// WithImageResolver sets a function to resolve image URLs.
func WithImageResolver(fn func(string) string) Option {
	return func(r *Renderer) { r.imageResolver = fn }
}

// Parse parses Markdown source into a Document using the package-level parser.
func (r *Renderer) Parse(source []byte) *Document {
	return Parse(source)
}

// RenderString parses and renders a Markdown string to HTML.
func (r *Renderer) RenderString(source string) string {
	doc := Parse([]byte(source))
	return r.Render(doc)
}

// Render converts a parsed Document AST into an HTML string.
func (r *Renderer) Render(doc *Document) string {
	return renderNode(r, doc.Root)
}

// RenderString is a package-level convenience that parses and renders
// Markdown to HTML with default settings.
func RenderString(source string) string {
	return NewRenderer().RenderString(source)
}

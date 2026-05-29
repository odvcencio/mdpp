package mdpp

import "strings"

// NodeRenderer can override rendering for a node type. It should write any
// desired HTML into b and return true when it handled n.
type NodeRenderer func(r *Renderer, b *strings.Builder, n *Node) bool

// RenderFragment is a top-level rendered block with its source range.
type RenderFragment struct {
	Index int
	Type  NodeType
	Range Range
	HTML  string
}

// Renderer converts Markdown source into HTML.
type Renderer struct {
	highlightCode   bool
	headingIDs      bool
	unsafeHTML      bool
	hardWraps       bool
	wrapEmoji       bool
	imageResolver   func(string) string
	math            MathOption
	containerHTML   func(c *Node, body string) string
	sourcePositions bool
	nodeRenderers   map[NodeType]NodeRenderer
	sirenaRenderer  SirenaRenderer
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

// WithWrapEmoji wraps emoji output in an accessible <span> with role="img" and aria-label.
func WithWrapEmoji(enabled bool) Option {
	return func(r *Renderer) { r.wrapEmoji = enabled }
}

// WithImageResolver sets a function to resolve image URLs.
func WithImageResolver(fn func(string) string) Option {
	return func(r *Renderer) { r.imageResolver = fn }
}

// WithContainerRenderer sets a custom renderer for container directives.
func WithContainerRenderer(fn func(c *Node, body string) string) Option {
	return func(r *Renderer) { r.containerHTML = fn }
}

// WithSourcePositions emits data-mdpp-source-* attributes on rendered elements.
func WithSourcePositions(enabled bool) Option {
	return func(r *Renderer) { r.sourcePositions = enabled }
}

// WithNodeRenderer installs a custom renderer for one node type.
func WithNodeRenderer(typ NodeType, fn NodeRenderer) Option {
	return func(r *Renderer) {
		if r.nodeRenderers == nil {
			r.nodeRenderers = make(map[NodeType]NodeRenderer)
		}
		if fn == nil {
			delete(r.nodeRenderers, typ)
			return
		}
		r.nodeRenderers[typ] = fn
	}
}

// Parse parses Markdown source into a Document using the package-level parser.
func (r *Renderer) Parse(source []byte) *Document {
	return MustParse(source)
}

// RenderString parses and renders a Markdown string to HTML.
func (r *Renderer) RenderString(source string) string {
	doc := MustParse([]byte(source))
	return r.Render(doc)
}

// Render converts a parsed Document AST into an HTML string.
func (r *Renderer) Render(doc *Document) string {
	if doc == nil || doc.Root == nil {
		return ""
	}
	var b strings.Builder
	if len(doc.Source) > 0 {
		b.Grow(len(doc.Source) + len(doc.Source)/4)
	}
	renderNodeInto(r, &b, doc.Root)
	return b.String()
}

// RenderNodeInto writes one AST node into b using this renderer's options.
func (r *Renderer) RenderNodeInto(b *strings.Builder, n *Node) {
	renderNodeInto(r, b, n)
}

// RenderChildrenInto writes a node's children into b using this renderer's options.
func (r *Renderer) RenderChildrenInto(b *strings.Builder, n *Node) {
	renderChildrenInto(r, b, n)
}

// RenderWithFragments renders a document and returns top-level HTML fragments
// suitable for preview clients that want block-level incremental updates.
func (r *Renderer) RenderWithFragments(doc *Document) (string, []RenderFragment) {
	if doc == nil || doc.Root == nil {
		return "", nil
	}
	if doc.Root.Type != NodeDocument {
		var b strings.Builder
		renderNodeInto(r, &b, doc.Root)
		html := b.String()
		return html, []RenderFragment{{Index: 0, Type: doc.Root.Type, Range: doc.Root.Range, HTML: html}}
	}
	fragments := make([]RenderFragment, 0, len(doc.Root.Children))
	var out strings.Builder
	if len(doc.Source) > 0 {
		out.Grow(len(doc.Source) + len(doc.Source)/4)
	}
	for i, child := range doc.Root.Children {
		var part strings.Builder
		renderNodeInto(r, &part, child)
		html := part.String()
		out.WriteString(html)
		fragments = append(fragments, RenderFragment{
			Index: i,
			Type:  child.Type,
			Range: child.Range,
			HTML:  html,
		})
	}
	return out.String(), fragments
}

// RenderString is a package-level convenience that parses and renders
// Markdown to HTML with default settings.
func RenderString(source string) string {
	return NewRenderer().RenderString(source)
}

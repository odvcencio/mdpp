package mdpp

// RenderOptions configures one of three HTML trust modes. With UnsafeHTML and
// Sanitize both false (the default), raw HTML is escaped and blocked URLs are
// emitted with empty URL attributes. With Sanitize true, blocked URLs are
// filtered; raw HTML remains escaped unless UnsafeHTML is also true, in which
// case it passes through an allow-list sanitizer. With UnsafeHTML true and
// Sanitize false, raw HTML and URLs pass through unchanged for trusted-author
// content.
type RenderOptions struct {
	HighlightCode       bool
	HeadingIDs          bool
	UnsafeHTML          bool
	HardWraps           bool
	WrapEmoji           bool
	ImageResolver       func(src string) string
	ImageTitleAttribute bool
	ContainerRenderer   func(c *Node, body string) string
	SourcePositions     bool
	NodeRenderers       map[NodeType]NodeRenderer
	Math                MathOption
	Sanitize            bool
	// URLPolicy can reject or rewrite URLs after the built-in safe policy.
	// Returning false emits an empty URL attribute. It cannot allow a URL the
	// built-in policy rejects.
	URLPolicy func(kind URLKind, raw string) (string, bool)
	// SirenaRenderer, when set, renders ```sirena / ```sir fences to inline
	// SVG. Wired by consumers (e.g. cmd/mdpp) so the library stays sirena-free.
	SirenaRenderer SirenaRenderer
}

// MathOption selects how math nodes render.
type MathOption int

const (
	MathServer MathOption = iota
	MathRaw
	MathOmit
)

// Render produces HTML for a parsed document.
func Render(doc *Document, opts RenderOptions) ([]byte, error) {
	r := rendererFromOptions(opts)
	html := r.Render(doc)
	return []byte(html), nil
}

// RenderWithFragments produces HTML plus top-level source-mapped fragments.
func RenderWithFragments(doc *Document, opts RenderOptions) ([]byte, []RenderFragment, error) {
	r := rendererFromOptions(opts)
	html, fragments := r.RenderWithFragments(doc)
	return []byte(html), fragments, nil
}

func rendererFromOptions(opts RenderOptions) *Renderer {
	rendererOptions := []Option{
		WithHighlightCode(opts.HighlightCode),
		WithHeadingIDs(opts.HeadingIDs),
		WithUnsafeHTML(opts.UnsafeHTML),
		WithHardWraps(opts.HardWraps),
		WithWrapEmoji(opts.WrapEmoji),
		WithImageResolver(opts.ImageResolver),
		WithImageTitleAttribute(opts.ImageTitleAttribute),
		WithContainerRenderer(opts.ContainerRenderer),
		WithSourcePositions(opts.SourcePositions),
		WithURLPolicy(opts.URLPolicy),
	}
	for typ, fn := range opts.NodeRenderers {
		rendererOptions = append(rendererOptions, WithNodeRenderer(typ, fn))
	}
	if opts.SirenaRenderer != nil {
		rendererOptions = append(rendererOptions, WithSirenaRenderer(opts.SirenaRenderer))
	}
	r := NewRenderer(rendererOptions...)
	r.sanitize = opts.Sanitize
	r.math = opts.Math
	return r
}

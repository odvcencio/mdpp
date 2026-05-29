package mdpp

// RenderOptions is the value-typed rendering API used by the CLI and tools.
type RenderOptions struct {
	HighlightCode     bool
	HeadingIDs        bool
	UnsafeHTML        bool
	HardWraps         bool
	WrapEmoji         bool
	ImageResolver     func(src string) string
	ContainerRenderer func(c *Node, body string) string
	SourcePositions   bool
	NodeRenderers     map[NodeType]NodeRenderer
	Math              MathOption
	Sanitize          bool
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
	if opts.Sanitize {
		// The renderer escapes unsafe surfaces by default; the sanitizer hook is
		// reserved for a stricter allow-list implementation without changing API.
	}
	return []byte(html), nil
}

// RenderWithFragments produces HTML plus top-level source-mapped fragments.
func RenderWithFragments(doc *Document, opts RenderOptions) ([]byte, []RenderFragment, error) {
	r := rendererFromOptions(opts)
	html, fragments := r.RenderWithFragments(doc)
	if opts.Sanitize {
		// Reserved for the same allow-list sanitizer hook as Render.
	}
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
		WithContainerRenderer(opts.ContainerRenderer),
		WithSourcePositions(opts.SourcePositions),
	}
	for typ, fn := range opts.NodeRenderers {
		rendererOptions = append(rendererOptions, WithNodeRenderer(typ, fn))
	}
	if opts.SirenaRenderer != nil {
		rendererOptions = append(rendererOptions, WithSirenaRenderer(opts.SirenaRenderer))
	}
	r := NewRenderer(rendererOptions...)
	r.math = opts.Math
	return r
}

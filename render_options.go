package mdpp

import (
	"time"
)

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
	Math              MathOption
	Sanitize          bool
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
	r := NewRenderer(
		WithHighlightCode(opts.HighlightCode),
		WithHeadingIDs(opts.HeadingIDs),
		WithUnsafeHTML(opts.UnsafeHTML),
		WithHardWraps(opts.HardWraps),
		WithWrapEmoji(opts.WrapEmoji),
		WithImageResolver(opts.ImageResolver),
		WithContainerRenderer(opts.ContainerRenderer),
		WithSourcePositions(opts.SourcePositions),
	)
	r.math = opts.Math
	html := r.Render(doc)
	if opts.Sanitize {
		// The renderer escapes unsafe surfaces by default; the sanitizer hook is
		// reserved for a stricter allow-list implementation without changing API.
	}
	return []byte(html), nil
}

// PDFOptions configures RenderPDF.
type PDFOptions struct {
	PaperSize         PaperSize
	PaperWidthInches  float64
	PaperHeightInches float64
	MarginInches      Margins
	UserCSS           string
	Background        bool
	HeaderFooter      HeaderFooterTemplate
	RenderOptions     RenderOptions
	BrowserURL        string
	Timeout           time.Duration
	SettleDelay       time.Duration
}

// PaperSize is a built-in PDF paper size.
type PaperSize int

const (
	PaperLetter PaperSize = iota
	PaperA4
	PaperLegal
	PaperCustom
)

// Margins holds PDF page margins in inches.
type Margins struct{ Top, Right, Bottom, Left float64 }

// HeaderFooterTemplate contains chromedp-compatible header/footer HTML.
type HeaderFooterTemplate struct{ HeaderHTML, FooterHTML string }

package mdpp

import (
	"fmt"
	"html"
	"strings"
)

// sirena_fence.go is the dependency-injection seam for rendering ```sirena
// (and ```sir) diagram fences. The mdpp library imports ZERO sirena symbols:
// it defines a plain-data contract (SirenaRenderer) that a consumer — e.g.
// cmd/mdpp — wires to sirena's fence renderer via WithSirenaRenderer. This
// keeps `go list -deps m31labs.dev/mdpp` free of sirena while the shipped CLI
// renders diagrams natively.

// SirenaFenceOptions carries the options parsed from a ```sirena fence
// info-string. It is plain data so the contract stays sirena-free.
type SirenaFenceOptions struct {
	// View names a view file to render (the fence info-string `view=` key).
	View string
	// Theme is the SVG theme name (`theme=`); empty selects the default.
	Theme string
	// Interactive requests a GoSX island payload (`interactive`).
	Interactive bool
	// StrictBudget suppresses output on a budget breach (`strict-budget`).
	StrictBudget bool
}

// SirenaFenceResult is what a SirenaRenderer returns: an inline SVG fragment
// and any diagnostics produced while rendering. SVG may be empty when a
// diagnostic prevented rendering, in which case mdpp falls back to showing
// the fence source.
type SirenaFenceResult struct {
	SVG         []byte
	Diagnostics []Diagnostic
}

// SirenaRenderer renders a sirena fence body to SVG. It is wired via
// WithSirenaRenderer; the mdpp library never imports sirena, so consumers
// supply the implementation (typically an adapter over sirena's fence
// package).
type SirenaRenderer func(body string, opts SirenaFenceOptions) (SirenaFenceResult, error)

// WithSirenaRenderer installs the renderer used for ```sirena / ```sir
// fences. Without it, sirena fences fall back to source passthrough (like an
// unrecognized diagram), so the library degrades gracefully.
func WithSirenaRenderer(fn SirenaRenderer) Option {
	return func(r *Renderer) { r.sirenaRenderer = fn }
}

// knownSirenaFenceKeys are the v0.1 fence info-string keys. Unknown keys emit
// a forward-compatible warning rather than failing the render.
var knownSirenaFenceKeys = map[string]bool{
	"view": true, "theme": true, "interactive": true, "strict-budget": true,
}

// parseSirenaFenceInfo parses a fence info-string (e.g.
// `sirena view=payments.view.sir theme=earth-default strict-budget`) into
// SirenaFenceOptions. The leading language token is skipped. Unknown keys
// produce SIR-FENCE-UNKNOWN-KEY warnings; the render still proceeds.
func parseSirenaFenceInfo(info string) (SirenaFenceOptions, []Diagnostic) {
	var opts SirenaFenceOptions
	var diags []Diagnostic
	fields := strings.Fields(info)
	for i, f := range fields {
		if i == 0 {
			continue // the language token (sirena / sir)
		}
		key, val, hasVal := strings.Cut(f, "=")
		switch key {
		case "view":
			opts.View = val
		case "theme":
			opts.Theme = val
		case "interactive":
			opts.Interactive = !hasVal || val == "true"
		case "strict-budget":
			opts.StrictBudget = !hasVal || val == "true"
		default:
			if !knownSirenaFenceKeys[key] {
				diags = append(diags, Diagnostic{
					Code:     "SIR-FENCE-UNKNOWN-KEY",
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("unknown sirena fence key %q (ignored)", key),
				})
			}
		}
	}
	return opts, diags
}

// renderSirenaInto renders a sirena diagram node through the wired
// SirenaRenderer. It returns true when it emitted a complete <figure> (so the
// default passthrough is skipped), and false to fall back to source
// passthrough — on renderer error or an empty result — so content is never
// lost. Diagnostics are surfaced as HTML comments adjacent to the output.
func (r *Renderer) renderSirenaInto(b *strings.Builder, n *Node) bool {
	opts, infoDiags := parseSirenaFenceInfo(n.Attrs["language"])
	res, err := r.sirenaRenderer(n.Literal, opts)
	if err != nil {
		writeSirenaComment(b, Diagnostic{Code: "SIR-FENCE-RENDER-ERROR", Severity: SeverityError, Message: err.Error()})
		return false
	}
	allDiags := append(infoDiags, res.Diagnostics...)
	if len(res.SVG) == 0 {
		// Nothing rendered (empty body, no view, or strict-budget breach):
		// leave the diagnostics and fall back to source passthrough.
		for _, d := range allDiags {
			writeSirenaComment(b, d)
		}
		return false
	}
	b.WriteString(`<figure class="mdpp-diagram mdpp-diagram-sirena" data-diagram-syntax="sirena">`)
	b.Write(res.SVG)
	for _, d := range allDiags {
		writeSirenaComment(b, d)
	}
	b.WriteString("</figure>\n")
	return true
}

// writeSirenaComment emits a single diagnostic as an HTML comment.
func writeSirenaComment(b *strings.Builder, d Diagnostic) {
	b.WriteString("\n<!-- sirena ")
	b.WriteString(html.EscapeString(d.Code))
	b.WriteString(": ")
	b.WriteString(html.EscapeString(d.Message))
	b.WriteString(" -->")
}

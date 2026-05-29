package mdpp

import (
	"strings"
	"testing"
)

// TestParseSirenaFenceInfo covers info-string option parsing and the
// forward-compatible unknown-key warning.
func TestParseSirenaFenceInfo(t *testing.T) {
	opts, diags := parseSirenaFenceInfo("sirena view=payments.view.sir theme=earth-default strict-budget interactive lod=2")
	if opts.View != "payments.view.sir" {
		t.Errorf("View = %q, want payments.view.sir", opts.View)
	}
	if opts.Theme != "earth-default" {
		t.Errorf("Theme = %q, want earth-default", opts.Theme)
	}
	if !opts.StrictBudget {
		t.Error("StrictBudget = false, want true (bare key)")
	}
	if !opts.Interactive {
		t.Error("Interactive = false, want true (bare key)")
	}
	found := false
	for _, d := range diags {
		if d.Code == "SIR-FENCE-UNKNOWN-KEY" && strings.Contains(d.Message, "lod") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected SIR-FENCE-UNKNOWN-KEY for lod; got %+v", diags)
	}
}

// TestDiagramFenceInfo_Sirena confirms the sirena/sir languages are
// recognized as diagram fences with syntax "sirena".
func TestDiagramFenceInfo_Sirena(t *testing.T) {
	for _, lang := range []string{"sirena", "sir", "SIRENA"} {
		syntax, _, ok := diagramFenceInfo(lang, "service api")
		if !ok || syntax != "sirena" {
			t.Errorf("diagramFenceInfo(%q) = (%q, _, %v), want (sirena, _, true)", lang, syntax, ok)
		}
	}
}

// TestRenderSirena_WiredRendererInvoked confirms a wired SirenaRenderer
// produces an inline-SVG <figure> for a ```sirena fence.
func TestRenderSirena_WiredRendererInvoked(t *testing.T) {
	stub := func(body string, opts SirenaFenceOptions) (SirenaFenceResult, error) {
		return SirenaFenceResult{SVG: []byte(`<svg data-stub="1">` + body + `</svg>`)}, nil
	}
	doc := MustParse([]byte("```sirena\nservice api\n```\n"))
	out, err := Render(doc, RenderOptions{SirenaRenderer: stub})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `mdpp-diagram-sirena`) {
		t.Errorf("missing sirena figure class:\n%s", s)
	}
	if !strings.Contains(s, `<svg data-stub="1">service api`) {
		t.Errorf("wired renderer SVG not inlined:\n%s", s)
	}
}

// TestRenderSirena_FallbackWithoutRenderer confirms that without a wired
// renderer, a sirena fence degrades to source passthrough (content preserved).
func TestRenderSirena_FallbackWithoutRenderer(t *testing.T) {
	doc := MustParse([]byte("```sirena\nservice api\n```\n"))
	out, err := Render(doc, RenderOptions{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "service api") {
		t.Errorf("fence source not preserved on fallback:\n%s", s)
	}
	if strings.Contains(s, "<svg") {
		t.Errorf("unexpected SVG without a wired renderer:\n%s", s)
	}
}

// TestRenderSirena_ErrorFallsBack confirms a renderer error falls back to
// source passthrough rather than dropping content.
func TestRenderSirena_ErrorFallsBack(t *testing.T) {
	boom := func(body string, opts SirenaFenceOptions) (SirenaFenceResult, error) {
		return SirenaFenceResult{}, errStub
	}
	doc := MustParse([]byte("```sirena\nservice api\n```\n"))
	out, err := Render(doc, RenderOptions{SirenaRenderer: boom})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "service api") {
		t.Errorf("content dropped on renderer error:\n%s", s)
	}
	if !strings.Contains(s, "SIR-FENCE-RENDER-ERROR") {
		t.Errorf("missing error breadcrumb comment:\n%s", s)
	}
}

// TestRenderMermaid_Unchanged is the regression guard: a mermaid fence still
// passes through to <pre><code> exactly as before, regardless of sirena wiring.
func TestRenderMermaid_Unchanged(t *testing.T) {
	doc := MustParse([]byte("```mermaid\ngraph TD; A-->B;\n```\n"))
	out, err := Render(doc, RenderOptions{SirenaRenderer: func(string, SirenaFenceOptions) (SirenaFenceResult, error) {
		t.Fatal("sirena renderer must not be called for a mermaid fence")
		return SirenaFenceResult{}, nil
	}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "mdpp-diagram-mermaid") || !strings.Contains(s, "<pre><code") {
		t.Errorf("mermaid passthrough changed:\n%s", s)
	}
}

var errStub = stubError("sirena renderer failed")

type stubError string

func (e stubError) Error() string { return string(e) }

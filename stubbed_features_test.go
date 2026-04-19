package mdpp

import (
	"strings"
	"testing"
)

// These tests document features that have reserved AST node types but no
// parser/postprocessor wiring yet. Each test is t.Skip()'d with a clear
// message so a future contributor who wires the feature can drop the Skip
// and the assertion becomes active coverage.

func TestStubbed_DefinitionList(t *testing.T) {
	t.Skip("Definition lists not wired: NodeDefinitionList/Term/Desc reserved in ast.go but no parser or postProcess step emits them.")
	src := "Term\n: Definition\n\nAnother\n: Another definition\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<dl>")
	assertContains(t, html, "<dt>Term</dt>")
	assertContains(t, html, "<dd>Definition</dd>")
}

func TestStubbed_AutoEmbed(t *testing.T) {
	t.Skip("AutoEmbed not wired: NodeAutoEmbed reserved in ast.go but no authoring syntax triggers it.")
	src := "{{autoembed https://youtube.com/watch?v=x}}\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `class="mdpp-embed"`)
}

func TestStubbed_Autolink(t *testing.T) {
	t.Skip("Autolinks <https://example.com> render as escaped text; tree-sitter-markdown emits autolink nodes but convertInline does not map them to NodeLink.")
	html := NewRenderer().RenderString("<https://example.com>")
	assertContains(t, html, `<a href="https://example.com">https://example.com</a>`)
}

func TestStubbed_ReferenceStyleLink(t *testing.T) {
	t.Skip("Reference-style links [text][ref] + [ref]: url render as literal text. Parser collects the link-reference-definition but does not resolve downstream [text][ref] references to them.")
	src := "See [the site][main].\n\n[main]: https://example.com\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `<a href="https://example.com">the site</a>`)
}

func TestStubbed_RequirementDiagramAlias(t *testing.T) {
	t.Skip("requirement diagram alias falls through to a plain code block. diagram.go lists 'requirementdiagram' but requirement by itself does not hit the diagram fence path.")
	src := "```requirement\nrequirementDiagram\n  req1\n```\n"
	html := NewRenderer().RenderString(src)
	if !strings.Contains(html, "mdpp-diagram-requirement") {
		t.Fatalf("expected requirement diagram wrapper, got:\n%s", html)
	}
}

func TestStubbed_NestedBlockquote(t *testing.T) {
	t.Skip("Nested blockquotes (> > inner) render as a single blockquote containing literal `> inner` text. parseSimpleBlockquoteDocument or convertBlock does not recurse for additional quote markers.")
	src := "> outer\n>\n> > inner\n"
	html := NewRenderer().RenderString(src)
	if strings.Count(html, "<blockquote>") < 2 {
		t.Fatalf("expected 2 blockquote elements, got:\n%s", html)
	}
}

func TestStubbed_BlockquoteContainingBlockStructures(t *testing.T) {
	t.Skip("Blockquotes do not re-parse their quoted content as markdown blocks: `> - a` renders as a paragraph with literal `- a`, not a nested <ul>.")
	src := "> - a\n> - b\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<blockquote>")
	assertContains(t, html, "<ul>")
	assertContains(t, html, "a")
}

func TestStubbed_IndentedCodeBlock(t *testing.T) {
	t.Skip("4-space-indented code blocks render as paragraphs. tree-sitter-markdown supports indented_code_block; the converter is not mapping it to NodeCodeBlock.")
	src := "    fmt.Println(\"hi\")\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<pre><code>")
	assertContains(t, html, "fmt.Println")
}

func TestStubbed_InlineMarkdownInLinkText(t *testing.T) {
	t.Skip("Inline emphasis/code/emoji inside link text are rendered literally: [*em*](url) becomes <a href=\"url\">*em*</a>. Link-text children are not re-run through the inline processors.")
	html := NewRenderer().RenderString("[*em* here](https://example.com)")
	assertContains(t, html, "<em>em</em>")
}

func TestStubbed_CRLFNormalization(t *testing.T) {
	t.Skip("mdpp does not normalize CRLF — `\\r` leaks into rendered output as `<h1>T\\r</h1>`. Fix: strip `\\r` in Parse or in tree-sitter source preprocessing.")
	html := NewRenderer().RenderString("# T\r\n\r\nBody\r\n")
	if strings.Contains(html, "\r") {
		t.Fatalf("unexpected CR in output: %q", html)
	}
}

func TestStubbed_DeepListNestingBeyond4Levels(t *testing.T) {
	t.Skip("tree-sitter-markdown folds list items beyond 4 levels of nesting into parent text. A deeper parser config or a post-process tree-walk would be needed to support 5+ level nesting.")
	var buf strings.Builder
	for i := 0; i < 6; i++ {
		buf.WriteString(strings.Repeat("  ", i) + "- level " + string(rune('0'+i)) + "\n")
	}
	html := NewRenderer().RenderString(buf.String())
	if strings.Count(html, "<ul>") < 6 {
		t.Fatalf("expected 6 <ul> levels, got %d", strings.Count(html, "<ul>"))
	}
}

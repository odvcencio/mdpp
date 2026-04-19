package mdpp

import (
	"strings"
	"testing"
)

// These tests document features that have reserved AST node types but no
// parser/postprocessor wiring yet. Each test is t.Skip()'d with a clear
// message so a future contributor who wires the feature can drop the Skip
// and the assertion becomes active coverage.

func TestDefinitionList(t *testing.T) {
	// Wired: processDefinitionLists transforms `Term\n: Def` paragraphs into NodeDefinitionList.
	src := "Term\n: Definition\n\nAnother\n: Another definition\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<dl>")
	assertContains(t, html, "<dt>Term</dt>")
	assertContains(t, html, "<dd>Definition</dd>")
	assertContains(t, html, "<dt>Another</dt>")
	assertContains(t, html, "<dd>Another definition</dd>")
}

func TestDefinitionListAdjacentTermsMerge(t *testing.T) {
	// Consecutive term/desc paragraphs collapse into one <dl>.
	src := "A\n: 1\n\nB\n: 2\n"
	html := NewRenderer().RenderString(src)
	if strings.Count(html, "<dl>") != 1 {
		t.Fatalf("expected one <dl>, got:\n%s", html)
	}
}

func TestDefinitionListMultipleDescriptions(t *testing.T) {
	src := "Term\n: First def\n: Second def\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<dd>First def</dd>")
	assertContains(t, html, "<dd>Second def</dd>")
}

func TestDefinitionListWithInlineMarkdown(t *testing.T) {
	src := "**Bold Term**\n: *italic def* and `code`\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<dt><strong>Bold Term</strong></dt>")
	assertContains(t, html, "<em>italic def</em>")
	assertContains(t, html, "<code>code</code>")
}

func TestAutoEmbed(t *testing.T) {
	// Wired: [[embed:url]] directive matches the [[toc]] family and emits
	// a NodeAutoEmbed with provider detection for common hosts.
	src := "[[embed:https://youtube.com/watch?v=xyz]]\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `class="mdpp-embed mdpp-embed-youtube"`)
	assertContains(t, html, `data-src="https://youtube.com/watch?v=xyz"`)
	assertContains(t, html, `data-provider="youtube"`)
	assertContains(t, html, `<a href="https://youtube.com/watch?v=xyz">https://youtube.com/watch?v=xyz</a>`)
}

func TestAutoEmbedUnknownProvider(t *testing.T) {
	src := "[[embed:https://my.private.host/video.mp4]]\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `class="mdpp-embed"`)
	assertContains(t, html, `data-src="https://my.private.host/video.mp4"`)
}

func TestAutoEmbedInlineIsLiteral(t *testing.T) {
	// Inline uses remain literal, same contract as [[toc]].
	src := "See [[embed:https://x.com]] inline.\n"
	html := NewRenderer().RenderString(src)
	assertNotContains(t, html, `class="mdpp-embed"`)
}

func TestAutolink(t *testing.T) {
	// Wired: uri_autolink / email_autolink handled in convertInline.
	html := NewRenderer().RenderString("<https://example.com>")
	assertContains(t, html, `<a href="https://example.com">https://example.com</a>`)

	emailHTML := NewRenderer().RenderString("<foo@example.com>")
	assertContains(t, emailHTML, `<a href="mailto:foo@example.com">foo@example.com</a>`)
}

func TestReferenceStyleLink(t *testing.T) {
	// Wired: collectLinkRefDefs captures [label]: url defs; processReferenceLinks resolves.
	src := "See [the site][main].\n\n[main]: https://example.com\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `<a href="https://example.com">the site</a>`)
}

func TestCollapsedReferenceLink(t *testing.T) {
	// [foo][] — collapsed: use link text as label.
	src := "See [Foo][].\n\n[foo]: https://example.com/foo\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `href="https://example.com/foo"`)
}

func TestShortcutReferenceLink(t *testing.T) {
	// [foo] — shortcut: use link text as label, no brackets after.
	src := "See [Foo].\n\n[foo]: https://example.com/foo\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `href="https://example.com/foo"`)
}

func TestReferenceLinkWithTitle(t *testing.T) {
	src := "Go to [the docs][d].\n\n[d]: https://example.com \"Official docs\"\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `href="https://example.com"`)
	assertContains(t, html, `title="Official docs"`)
}

func TestReferenceLinkCaseInsensitive(t *testing.T) {
	src := "[Foo][BAR]\n\n[bar]: https://example.com\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `href="https://example.com"`)
}

func TestRequirementDiagramAlias(t *testing.T) {
	// Wired: requirement/c4/quadrant/xychart/block/sankey/packet/architecture added to diagramFenceInfo.
	for _, lang := range []string{"requirement", "c4", "quadrant", "xychart", "block", "sankey", "packet", "architecture"} {
		src := "```" + lang + "\nsome body\n```\n"
		html := NewRenderer().RenderString(src)
		if !strings.Contains(html, "mdpp-diagram-"+lang) {
			t.Fatalf("expected mdpp-diagram-%s wrapper, got:\n%s", lang, html)
		}
	}
}

func TestNestedBlockquote(t *testing.T) {
	// Wired: parseSimpleBlockquoteDocument recursively re-parses stripped content.
	src := "> outer\n>\n> > inner\n"
	html := NewRenderer().RenderString(src)
	if strings.Count(html, "<blockquote>") < 2 {
		t.Fatalf("expected 2 blockquote elements, got:\n%s", html)
	}
	assertContains(t, html, "outer")
	assertContains(t, html, "inner")
}

func TestBlockquoteContainingBlockStructures(t *testing.T) {
	// Wired: blockquotes now preserve nested markdown block structure.
	// (tree-sitter marks this as a loose list, so items render <li><p>x</p></li>.)
	src := "> - a\n> - b\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<blockquote>")
	assertContains(t, html, "<ul>")
	assertContains(t, html, "a")
	assertContains(t, html, "b")
}

func TestBlockquoteContainingCodeAndHeading(t *testing.T) {
	src := "> ## Heading in quote\n>\n> ```\n> code\n> ```\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<blockquote>")
	assertContains(t, html, "<h2>Heading in quote</h2>")
	assertContains(t, html, "<pre><code>")
	assertContains(t, html, "code")
}

func TestIndentedCodeBlock(t *testing.T) {
	// Wired: tree-sitter emits indented_code_block when surrounded by blank
	// lines (CommonMark requires a blank line before). convertCodeBlock now
	// strips the 4-space indent and trailing blank line.
	src := "Para.\n\n    fmt.Println(\"hi\")\n    x := 1\n\nAfter.\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<pre><code>")
	assertContains(t, html, "fmt.Println")
	assertContains(t, html, "x := 1")
	// Indent must be stripped.
	assertNotContains(t, html, "    fmt.Println")
}

func TestStubbed_IndentedCodeBlockStandalone(t *testing.T) {
	t.Skip("tree-sitter-markdown requires a blank line before an indented code block to recognize it. A document consisting solely of `    code` is parsed as a paragraph per CommonMark spec.")
	html := NewRenderer().RenderString("    fmt.Println(\"hi\")\n")
	assertContains(t, html, "<pre><code>")
}

func TestInlineMarkdownInLinkText(t *testing.T) {
	// Wired: convertInlineChildren now re-parses link_text through parseInline.
	html := NewRenderer().RenderString("[*em* here](https://example.com)")
	assertContains(t, html, `<a href="https://example.com">`)
	assertContains(t, html, "<em>em</em>")

	// Strong, code, and emoji inside link text too.
	html2 := NewRenderer(WithWrapEmoji(true)).RenderString("[**bold** `code` :rocket:](https://x.com)")
	assertContains(t, html2, "<strong>bold</strong>")
	assertContains(t, html2, "<code>code</code>")
	assertContains(t, html2, "🚀")
}

func TestCRLFNormalization(t *testing.T) {
	// Wired: normalizeLineEndings in parse.go converts CRLF/CR → LF before tree-sitter.
	html := NewRenderer().RenderString("# T\r\n\r\nBody\r\n")
	if strings.Contains(html, "\r") {
		t.Fatalf("unexpected CR in output: %q", html)
	}
	assertContains(t, html, "<h1>T</h1>")
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

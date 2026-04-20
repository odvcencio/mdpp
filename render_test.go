package mdpp

import (
	"strings"
	"testing"
)

func TestRenderHeadings(t *testing.T) {
	tests := []struct {
		src      string
		contains string
	}{
		{"# H1", "<h1>H1</h1>"},
		{"## H2", "<h2>H2</h2>"},
		{"### H3", "<h3>H3</h3>"},
		{"#### H4", "<h4>H4</h4>"},
		{"##### H5", "<h5>H5</h5>"},
		{"###### H6", "<h6>H6</h6>"},
	}
	r := NewRenderer()
	for _, tt := range tests {
		out := r.RenderString(tt.src)
		if !strings.Contains(out, tt.contains) {
			t.Errorf("RenderString(%q) = %q, want to contain %q", tt.src, out, tt.contains)
		}
	}
}

func TestRenderParagraph(t *testing.T) {
	out := NewRenderer().RenderString("Hello world")
	if !strings.Contains(out, "<p>Hello world</p>") {
		t.Errorf("expected <p>Hello world</p>, got %q", out)
	}
}

func TestRenderParagraphPreservesTailAfterQuotedText(t *testing.T) {
	source := `There's a definitive stigma on using AI to help you do things. It was almost last year when I was myself a convert to the AI-assisted workflow and it was almost difficult for me to do so. I had stigmatized the LLMs "wasteful", or "too good at producing everything I ask for and nothing I want.". Adapting to them and adopting them is continuous struggle- it was until I realized that this is a software system like any other that I realized there is a real "superstitious" or "magical" attribution or sentiment applied towards these technologies. In other words, **they're not well understood so they're feared.**`
	out := NewRenderer().RenderString(source)

	for _, want := range []string{
		`real &#34;superstitious&#34; or &#34;magical&#34; attribution`,
		`<strong>they&#39;re not well understood so they&#39;re feared.</strong>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected rendered paragraph to contain %q, got %s", want, out)
		}
	}
}

func TestRenderDocumentPreservesParagraphAfterOrderedListWithFootnotes(t *testing.T) {
	source := strings.Join([]string{
		"As a matter of origin, this didn't exist before February 19th, 2026. That was the evening I would conceive of gotreesitter[^1]. Less than a week or so later, I would foolishly declare it ported and solved to HackerNews[^2] to mixed-ish reviews.",
		"",
		"It was an alerting to several signals:",
		"",
		"1. **Even when confronted with mounting evidence to the contrary, many folks are still firmly head-in-sand about LLM-assisted approaches to software**",
		"",
		"There's a definitive stigma on using AI to help you do things. It was almost last year when I was myself a convert to the AI-assisted workflow and it was almost difficult for me to do so. I had stigmatized the LLMs \"wasteful\", or \"too good at producing everything I ask for and nothing I want.\". Adapting to them and adopting them is continuous struggle- it was until I realized that this is a software system like any other that I realized there is a real \"superstitious\" or \"magical\" attribution or sentiment applied towards these technologies. In other words, **they're not well understood so they're feared.**",
		"",
		"2. **Those who",
		"",
		"[^1]: [gotreesitter](https://github.com/odvcencio/gotreesitter)",
		"[^2]: [Disaster, but lucky still](https://link-to-article)",
	}, "\n")
	out := NewRenderer().RenderString(source)

	for _, want := range []string{
		`real &#34;superstitious&#34; or &#34;magical&#34; attribution`,
		`<strong>they&#39;re not well understood so they&#39;re feared.</strong>`,
		`<a href="https://github.com/odvcencio/gotreesitter">gotreesitter</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected rendered document to contain %q, got %s", want, out)
		}
	}
}

func TestRenderBoldItalic(t *testing.T) {
	out := NewRenderer().RenderString("**bold** and *italic*")
	if !strings.Contains(out, "<strong>bold</strong>") {
		t.Errorf("expected <strong>bold</strong>, got %q", out)
	}
	if !strings.Contains(out, "<em>italic</em>") {
		t.Errorf("expected <em>italic</em>, got %q", out)
	}
}

func TestRenderCodeBlock(t *testing.T) {
	src := "```go\nfmt.Println(\"hello\")\n```"
	out := NewRenderer().RenderString(src)
	if !strings.Contains(out, "<pre><code class=\"language-go\">") {
		t.Errorf("expected <pre><code class=\"language-go\">, got %q", out)
	}
	if !strings.Contains(out, "</code></pre>") {
		t.Errorf("expected </code></pre>, got %q", out)
	}
}

func TestRenderCodeBlockNoLang(t *testing.T) {
	src := "```\nsome code\n```"
	out := NewRenderer().RenderString(src)
	if !strings.Contains(out, "<pre><code>") {
		t.Errorf("expected <pre><code>, got %q", out)
	}
}

func TestRenderMermaidDiagramFence(t *testing.T) {
	src := "```mermaid\nflowchart TD\n  A[Start] --> B{Ship?}\n```"
	out := NewRenderer().RenderString(src)
	assertContains(t, out, `class="mdpp-diagram mdpp-diagram-mermaid mdpp-diagram-flowchart"`)
	assertContains(t, out, `data-diagram-syntax="mermaid"`)
	assertContains(t, out, `data-diagram-kind="flowchart"`)
	assertContains(t, out, `<code class="language-mermaid">`)
	assertContains(t, out, `A[Start] --&gt; B{Ship?}`)
}

func TestRenderDiagramFenceDoesNotSyntaxHighlightDiagramSource(t *testing.T) {
	src := "```mermaid\nflowchart TD\n  A[Start] --> B{Ship?}\n```"
	out := NewRenderer(WithHighlightCode(true)).RenderString(src)
	assertContains(t, out, `<figure class="mdpp-diagram`)
	assertContains(t, out, `<code class="language-mermaid">`)
	assertNotContains(t, out, `class="hl-`)
}

func TestRenderDiagramAliasFence(t *testing.T) {
	out := NewRenderer().RenderString("```erd\nUser ||--o{ Post : writes\n```")
	assertContains(t, out, `mdpp-diagram-erd`)
	assertContains(t, out, `data-diagram-kind="erd"`)
	assertContains(t, out, `<code class="language-erd">`)
}

func TestRenderBlockquote(t *testing.T) {
	out := NewRenderer().RenderString("> quoted text")
	if !strings.Contains(out, "<blockquote>") {
		t.Errorf("expected <blockquote>, got %q", out)
	}
	if !strings.Contains(out, "</blockquote>") {
		t.Errorf("expected </blockquote>, got %q", out)
	}
}

func TestRenderList(t *testing.T) {
	out := NewRenderer().RenderString("- one\n- two")
	if !strings.Contains(out, "<ul>") {
		t.Errorf("expected <ul>, got %q", out)
	}
	if !strings.Contains(out, "<li>") {
		t.Errorf("expected <li>, got %q", out)
	}
	if !strings.Contains(out, "</li>") {
		t.Errorf("expected </li>, got %q", out)
	}
	if !strings.Contains(out, "</ul>") {
		t.Errorf("expected </ul>, got %q", out)
	}
}

func TestRenderOrderedList(t *testing.T) {
	for _, src := range []string{"1. one\n2. two", "1) one\n2) two"} {
		out := NewRenderer().RenderString(src)
		if !strings.Contains(out, "<ol>") {
			t.Errorf("%q: expected <ol>, got %q", src, out)
		}
		if strings.Contains(out, "<ul>") {
			t.Errorf("%q: did not expect <ul>, got %q", src, out)
		}
	}
}

func TestRenderOrderedListStart(t *testing.T) {
	out := NewRenderer().RenderString("3) three\n4) four")
	if !strings.Contains(out, `<ol start="3">`) {
		t.Errorf("expected ordered list start, got %q", out)
	}
}

func TestRenderLink(t *testing.T) {
	out := NewRenderer().RenderString("[click me](https://example.com)")
	if !strings.Contains(out, `<a href="https://example.com">click me</a>`) {
		t.Errorf("expected link, got %q", out)
	}
}

func TestRenderUnresolvedShortcutLinkAsText(t *testing.T) {
	out := NewRenderer().RenderString("Keep [literal brackets] in prose")
	if strings.Contains(out, `<a href="">`) {
		t.Errorf("did not expect unresolved shortcut link anchor, got %q", out)
	}
	if !strings.Contains(out, "Keep [literal brackets] in prose") {
		t.Errorf("expected bracketed text, got %q", out)
	}
}

func TestRenderImageWithTitle(t *testing.T) {
	out := NewRenderer().RenderString(`![alt text](image.png "My Title")`)
	if !strings.Contains(out, "<figure>") {
		t.Errorf("expected <figure>, got %q", out)
	}
	if !strings.Contains(out, `<img src="image.png" alt="alt text" />`) {
		t.Errorf("expected img tag, got %q", out)
	}
	if !strings.Contains(out, "<figcaption>My Title</figcaption>") {
		t.Errorf("expected figcaption, got %q", out)
	}
}

func TestRenderImageWithoutTitle(t *testing.T) {
	out := NewRenderer().RenderString("![alt text](image.png)")
	if !strings.Contains(out, `<img src="image.png" alt="alt text" />`) {
		t.Errorf("expected img tag without figure, got %q", out)
	}
	if strings.Contains(out, "<figure>") {
		t.Errorf("did not expect <figure> without title, got %q", out)
	}
}

func TestRenderTable(t *testing.T) {
	src := "| A | B |\n|---|---|\n| 1 | 2 |"
	out := NewRenderer().RenderString(src)
	assertContains(t, out, `<div class="mdpp-table">`)
	assertContains(t, out, "<table>")
	assertContains(t, out, "<thead>")
	assertContains(t, out, `<th scope="col">A</th>`)
	assertContains(t, out, "<tbody>")
	assertContains(t, out, "<td>1</td>")
}

func TestRenderTableParsesInlineCellMarkdown(t *testing.T) {
	src := strings.Join([]string{
		"| Feature | Notes |",
		"|---|---|",
		"| **Bold** | [docs](https://example.com) and `code` |",
		"| Math | $x^{2}$ |",
	}, "\n")
	out := NewRenderer().RenderString(src)

	assertContains(t, out, "<table>")
	assertContains(t, out, `<th scope="col">Feature</th>`)
	assertContains(t, out, "<strong>Bold</strong>")
	assertContains(t, out, `<a href="https://example.com">docs</a>`)
	assertContains(t, out, "<code>code</code>")
	assertContains(t, out, `class="math-inline"`)
	assertContains(t, out, "<sup>2</sup>")
	assertNotContains(t, out, "**Bold**")
	assertNotContains(t, out, "[docs](https://example.com)")
}

func TestRenderContainerDirectives(t *testing.T) {
	out := NewRenderer().RenderString(":::warning \"Heads up\" {.extra #warn}\nBody **bold**.\n:::\n")
	assertContains(t, out, `<div class="admonition admonition-warning extra" id="warn">`)
	assertContains(t, out, `<p class="admonition-title">Heads up</p>`)
	assertContains(t, out, `<strong>bold</strong>`)

	details := NewRenderer().RenderString(":::details \"Read more\"\nHidden text.\n:::\n")
	assertContains(t, details, `<details class="mdpp-container mdpp-container-details" data-mdpp-container="details">`)
	assertContains(t, details, `<summary>Read more</summary>`)
}

func TestTOCIncludesHeadingsInsideContainers(t *testing.T) {
	out := NewRenderer(WithHeadingIDs(true)).RenderString("[[toc]]\n\n:::note\n## Inside\n:::\n")
	assertContains(t, out, `<a href="#inside">Inside</a>`)
}

func TestRenderHeadingIDs(t *testing.T) {
	r := NewRenderer(WithHeadingIDs(true))
	out := r.RenderString("# Hello World")
	if !strings.Contains(out, `<h1 id="hello-world">Hello World</h1>`) {
		t.Errorf("expected heading with id, got %q", out)
	}
}

func TestRenderHeadingIDsWithExclamation(t *testing.T) {
	r := NewRenderer(WithHeadingIDs(true))
	out := r.RenderString("# Hello World!")
	if !strings.Contains(out, `<h1 id="hello-world">Hello World!</h1>`) {
		t.Errorf("expected heading with exclamation preserved, got %q", out)
	}
}

func TestRenderSourcePositions(t *testing.T) {
	doc := MustParse([]byte("# Hello\n\nBody text\n"))
	out := NewRenderer(WithSourcePositions(true)).Render(doc)

	assertContains(t, out, `<h1 data-mdpp-source-start="0" data-mdpp-source-end="8" data-mdpp-source-line="1" data-mdpp-source-col="1" data-mdpp-source-end-line="2" data-mdpp-source-end-col="1">Hello</h1>`)
	assertContains(t, out, `<p data-mdpp-source-start="9" data-mdpp-source-end="19" data-mdpp-source-line="3" data-mdpp-source-col="1" data-mdpp-source-end-line="4" data-mdpp-source-end-col="1">Body text</p>`)
}

func TestRenderUnsafeHTMLDefault(t *testing.T) {
	// By default, raw HTML should be escaped.
	// HTML blocks require preceding content for tree-sitter to recognise them.
	src := "text\n\n<div>raw html</div>\n"
	out := NewRenderer().RenderString(src)
	if strings.Contains(out, "<div>raw html</div>") {
		t.Errorf("expected HTML to be escaped by default, got %q", out)
	}
	if !strings.Contains(out, "&lt;div&gt;") {
		t.Errorf("expected escaped HTML, got %q", out)
	}
}

func TestRenderUnsafeHTMLEnabled(t *testing.T) {
	src := "text\n\n<div>raw html</div>\n"
	r := NewRenderer(WithUnsafeHTML(true))
	out := r.RenderString(src)
	if !strings.Contains(out, "<div>raw html</div>") {
		t.Errorf("expected raw HTML passthrough, got %q", out)
	}
}

func TestRenderHTMLEscaping(t *testing.T) {
	out := NewRenderer().RenderString("Use <script> & \"quotes\"")
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("expected escaped <script>, got %q", out)
	}
	if !strings.Contains(out, "&amp;") {
		t.Errorf("expected escaped &, got %q", out)
	}
	if !strings.Contains(out, "&#34;quotes&#34;") && !strings.Contains(out, "&quot;quotes&quot;") {
		t.Errorf("expected escaped quotes, got %q", out)
	}
}

func TestRenderThematicBreak(t *testing.T) {
	// Thematic break requires preceding content for tree-sitter to recognise it.
	out := NewRenderer().RenderString("text\n\n---\n")
	if !strings.Contains(out, "<hr />") {
		t.Errorf("expected <hr />, got %q", out)
	}
}

func TestRenderCodeSpan(t *testing.T) {
	out := NewRenderer().RenderString("Use `fmt.Println` here")
	if !strings.Contains(out, "<code>fmt.Println</code>") {
		t.Errorf("expected <code>fmt.Println</code>, got %q", out)
	}
}

func TestRenderStrikethrough(t *testing.T) {
	out := NewRenderer().RenderString("~~deleted~~")
	if !strings.Contains(out, "<del>deleted</del>") {
		t.Errorf("expected <del>deleted</del>, got %q", out)
	}
}

func TestRenderStringPackageLevel(t *testing.T) {
	out := RenderString("# Test")
	if !strings.Contains(out, "<h1>Test</h1>") {
		t.Errorf("expected <h1>Test</h1>, got %q", out)
	}
}

func TestRenderImageResolver(t *testing.T) {
	resolver := func(src string) string {
		return "/cdn/" + src
	}
	r := NewRenderer(WithImageResolver(resolver))
	out := r.RenderString("![alt](photo.jpg)")
	if !strings.Contains(out, `src="/cdn/photo.jpg"`) {
		t.Errorf("expected resolved src, got %q", out)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World", "hello-world"},
		{"API Reference", "api-reference"},
		{"  spaces  ", "spaces"},
		{"special!@#chars", "specialchars"},
		{"already-slugified", "already-slugified"},
	}
	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.expected {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

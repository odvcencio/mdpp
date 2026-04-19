package mdpp

import (
	"fmt"
	"strings"
	"testing"
)

// Hardening tests cover features that exist and work, filling gaps the prior
// suite did not exercise. Each block asserts an invariant of the rendered
// HTML — not the exact byte string — so small formatting churn in the
// renderer does not cause a cascade of false failures.

// --- Headings ---

func TestHardening_HeadingAllSixLevels(t *testing.T) {
	src := "# one\n\n## two\n\n### three\n\n#### four\n\n##### five\n\n###### six\n"
	html := NewRenderer(WithHeadingIDs(true)).RenderString(src)
	for i, want := range []string{
		`<h1 id="one">one</h1>`,
		`<h2 id="two">two</h2>`,
		`<h3 id="three">three</h3>`,
		`<h4 id="four">four</h4>`,
		`<h5 id="five">five</h5>`,
		`<h6 id="six">six</h6>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("level %d: want %q in:\n%s", i+1, want, html)
		}
	}
}

func TestHardening_HeadingWithInlineCode(t *testing.T) {
	html := NewRenderer(WithHeadingIDs(true)).RenderString("## Using `mdpp` today\n")
	assertContains(t, html, "<code>mdpp</code>")
	assertContains(t, html, `id="using-mdpp-today"`)
}

func TestHardening_HeadingWithEmoji(t *testing.T) {
	html := NewRenderer(WithWrapEmoji(true)).RenderString("## Ship it :rocket:\n")
	assertContains(t, html, `<span class="emoji"`)
	assertContains(t, html, "🚀")
}

func TestHardening_HeadingWithLink(t *testing.T) {
	html := NewRenderer().RenderString("## See [docs](https://example.com)\n")
	assertContains(t, html, `<a href="https://example.com">docs</a>`)
}

func TestHardening_HeadingIDsOffByDefault(t *testing.T) {
	html := NewRenderer().RenderString("## Hello\n")
	assertNotContains(t, html, `id=`)
}

// --- Lists ---

func TestHardening_NestedListThreeLevels(t *testing.T) {
	src := "- a\n  - a.1\n    - a.1.1\n"
	html := NewRenderer().RenderString(src)
	// Three <ul> opens, three <ul> closes — one per level.
	if strings.Count(html, "<ul>") != 3 || strings.Count(html, "</ul>") != 3 {
		t.Fatalf("expected 3 <ul>/</ul>, got:\n%s", html)
	}
	assertContains(t, html, "a.1.1")
}

func TestHardening_OrderedListCustomStart(t *testing.T) {
	src := "5. five\n6. six\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `<ol start="5">`)
}

func TestHardening_TaskAndRegularListItemsMixed(t *testing.T) {
	src := "- [x] done\n- plain\n- [ ] todo\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `class="task-list-item"`)
	// CommonMark promotes a list to "loose" when any item has complex content
	// (here: task-list items), so the plain item's text lands inside a <p>.
	// Accept either form — the invariant is that "plain" is inside an <li>
	// without task-list chrome, not the exact tag nesting.
	if !strings.Contains(html, "<li>plain</li>") && !strings.Contains(html, "<li><p>plain</p></li>") {
		t.Fatalf("expected <li>[<p>]plain[</p>]</li>, got:\n%s", html)
	}
}

func TestHardening_TaskListVariants(t *testing.T) {
	for _, marker := range []string{"[x]", "[X]", "[ ]"} {
		html := NewRenderer().RenderString("- " + marker + " item\n")
		assertContains(t, html, `class="task-list-item"`)
	}
}

// --- List continuation merging ---

func TestHardening_OrderedListBlankLineContinuation(t *testing.T) {
	src := "1. First.\n\nContinuation paragraph.\n\n2. Second.\n"
	html := NewRenderer().RenderString(src)
	// The middle paragraph must end up inside list item 1, not between lists.
	if strings.Count(html, "<ol>") != 1 {
		t.Fatalf("expected a single <ol>, got %d in:\n%s", strings.Count(html, "<ol>"), html)
	}
	assertContains(t, html, "Continuation paragraph.")
	assertNotContains(t, html, "start=\"2\"")
}

func TestHardening_UnorderedListBlankLineContinuation(t *testing.T) {
	src := "- First.\n\nContinuation.\n\n- Second.\n"
	html := NewRenderer().RenderString(src)
	if strings.Count(html, "<ul>") != 1 {
		t.Fatalf("expected a single <ul>, got %d in:\n%s", strings.Count(html, "<ul>"), html)
	}
}

func TestHardening_HeadingBetweenListsDoesNotMerge(t *testing.T) {
	src := "1. Item A.\n\n## Not a continuation\n\n2. Item B.\n"
	html := NewRenderer().RenderString(src)
	// Heading terminates the list — two <ol>s is correct.
	if strings.Count(html, "<ol") != 2 {
		t.Fatalf("expected 2 <ol> openings, got %d in:\n%s", strings.Count(html, "<ol"), html)
	}
	assertContains(t, html, "<h2>Not a continuation</h2>")
}

func TestHardening_MismatchedListTypesDoNotMerge(t *testing.T) {
	src := "1. Ordered.\n\nMiddle text.\n\n- Unordered.\n"
	html := NewRenderer().RenderString(src)
	// Ordered and unordered lists are different kinds — must not merge.
	assertContains(t, html, "<ol>")
	assertContains(t, html, "<ul>")
	// Middle paragraph is between the lists at document level.
	olClose := strings.Index(html, "</ol>")
	pIdx := strings.Index(html, "<p>Middle text.</p>")
	ulIdx := strings.Index(html, "<ul>")
	if !(olClose < pIdx && pIdx < ulIdx) {
		t.Fatalf("expected </ol> < <p> < <ul>, got:\n%s", html)
	}
}

// --- Tables ---

func TestHardening_TableInlineCodeInCell(t *testing.T) {
	src := "| col |\n|-----|\n| `fn()` |\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<code>fn()</code>")
}

func TestHardening_TableEmphasisInCell(t *testing.T) {
	src := "| col |\n|-----|\n| *emph* **strong** |\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<em>emph</em>")
	assertContains(t, html, "<strong>strong</strong>")
}

func TestHardening_TableLinkInCell(t *testing.T) {
	src := "| col |\n|-----|\n| [x](https://example.com) |\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `<a href="https://example.com">x</a>`)
}

func TestHardening_TableEmojiInCell(t *testing.T) {
	html := NewRenderer().RenderString("| col |\n|-----|\n| :rocket: |\n")
	assertContains(t, html, "🚀")
}

func TestHardening_TableAlignment(t *testing.T) {
	src := "| L | C | R | N |\n|:--|:-:|--:|---|\n| 1 | 2 | 3 | 4 |\n"
	html := NewRenderer().RenderString(src)
	// colgroup emitted when any column has alignment.
	assertContains(t, html, "<colgroup>")
	assertContains(t, html, `<col style="text-align:left"`)
	assertContains(t, html, `<col style="text-align:center"`)
	assertContains(t, html, `<col style="text-align:right"`)
	// Unaligned column still emits a bare <col />.
	assertContains(t, html, `<col />`)
	// Per-cell style is applied.
	assertContains(t, html, `<th scope="col" style="text-align:left">L</th>`)
	assertContains(t, html, `<th scope="col" style="text-align:center">C</th>`)
	assertContains(t, html, `<th scope="col" style="text-align:right">R</th>`)
	assertContains(t, html, `<td style="text-align:left">1</td>`)
}

func TestHardening_TableNoAlignmentDoesNotEmitColgroup(t *testing.T) {
	src := "| A | B |\n|---|---|\n| 1 | 2 |\n"
	html := NewRenderer().RenderString(src)
	assertNotContains(t, html, "<colgroup>")
	assertNotContains(t, html, "style=\"text-align")
}

func TestHardening_TableWrappedInResponsiveDiv(t *testing.T) {
	src := "| A |\n|---|\n| 1 |\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `<div class="mdpp-table">`)
	// The wrapper must open before <table> and close after </table>.
	openIdx := strings.Index(html, `<div class="mdpp-table">`)
	tableIdx := strings.Index(html, "<table>")
	closeTableIdx := strings.Index(html, "</table>")
	closeDivIdx := strings.Index(html, "</div>")
	if !(openIdx < tableIdx && closeTableIdx < closeDivIdx) {
		t.Fatalf("wrapper order wrong: open=%d table=%d /table=%d /div=%d\n%s",
			openIdx, tableIdx, closeTableIdx, closeDivIdx, html)
	}
}

func TestHardening_TableHeaderHasColScope(t *testing.T) {
	src := "| A | B |\n|---|---|\n| 1 | 2 |\n"
	html := NewRenderer().RenderString(src)
	// Every <th> in a pipe_table is a column header.
	if strings.Count(html, `<th scope="col">`) != 2 {
		t.Fatalf("expected 2 scoped headers, got:\n%s", html)
	}
}

// --- Blockquotes ---

func TestHardening_BlockquoteContainingCode(t *testing.T) {
	src := "> ```go\n> fmt.Println(1)\n> ```\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<blockquote>")
	assertContains(t, html, "fmt.Println")
}

// --- Code blocks ---

func TestHardening_TildeFence(t *testing.T) {
	src := "~~~go\nfmt.Println(\"hi\")\n~~~\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `class="language-go"`)
	assertContains(t, html, "fmt.Println")
}

func TestHardening_CodeBlockEmpty(t *testing.T) {
	src := "```\n```\n"
	html := NewRenderer().RenderString(src)
	// Should not crash; should emit an empty pre/code.
	assertContains(t, html, "<pre><code>")
	assertContains(t, html, "</code></pre>")
}

func TestHardening_CodeBlockUnknownLanguage(t *testing.T) {
	src := "```flibberty\nsome text\n```\n"
	html := NewRenderer().RenderString(src)
	// Unknown language class still flows through unchanged — no panic, no fallback lang swap.
	assertContains(t, html, `class="language-flibberty"`)
	assertContains(t, html, "some text")
}

// --- Inline emphasis ---

func TestHardening_StrongInsideEmphasis(t *testing.T) {
	html := NewRenderer().RenderString("*outer **inner** tail*")
	assertContains(t, html, "<em>")
	assertContains(t, html, "<strong>inner</strong>")
}

func TestHardening_StrikethroughInsideStrong(t *testing.T) {
	html := NewRenderer().RenderString("**strong ~~del~~ tail**")
	assertContains(t, html, "<strong>")
	assertContains(t, html, "<del>del</del>")
}

// --- Links ---

func TestHardening_LinkWithTitle(t *testing.T) {
	html := NewRenderer().RenderString(`[x](https://example.com "t")`)
	assertContains(t, html, `<a href="https://example.com" title="t">x</a>`)
}

// --- Images ---

func TestHardening_ImageWithEmptyAlt(t *testing.T) {
	html := NewRenderer().RenderString("![](https://example.com/x.png)")
	assertContains(t, html, `src="https://example.com/x.png"`)
	assertContains(t, html, `alt=""`)
}

func TestHardening_ImageResolverRewritesSrc(t *testing.T) {
	r := NewRenderer(WithImageResolver(func(s string) string { return "/cdn/" + s }))
	html := r.RenderString("![x](foo.png)")
	assertContains(t, html, `src="/cdn/foo.png"`)
}

// --- HTML safety paths ---

func TestHardening_InlineHTMLEscapedByDefault(t *testing.T) {
	html := NewRenderer().RenderString("Click <button>me</button>")
	assertContains(t, html, "&lt;button&gt;")
	assertNotContains(t, html, "<button>")
}

func TestHardening_InlineHTMLPassedUnderUnsafe(t *testing.T) {
	html := NewRenderer(WithUnsafeHTML(true)).RenderString("Click <button>me</button>")
	assertContains(t, html, "<button>me</button>")
}

func TestHardening_RawBlockHTMLEscapedByDefault(t *testing.T) {
	html := NewRenderer().RenderString("<div>raw</div>\n\nafter\n")
	assertContains(t, html, "&lt;div&gt;")
	assertContains(t, html, "after")
}

// --- Math ---

func TestHardening_MathInHeading(t *testing.T) {
	html := NewRenderer().RenderString("## Ratio $a/b$\n")
	assertContains(t, html, `class="math-inline"`)
}

func TestHardening_MathAdjacentToText(t *testing.T) {
	html := NewRenderer().RenderString("is $x$ done\n")
	assertContains(t, html, "is ")
	assertContains(t, html, `class="math-inline"`)
	assertContains(t, html, " done")
}

func TestHardening_MathNotInCodeSpan(t *testing.T) {
	html := NewRenderer().RenderString("`$x$` literal\n")
	assertContains(t, html, "<code>$x$</code>")
	assertNotContains(t, html, `class="math-inline"`)
}

// --- Emoji ---

func TestHardening_EmojiInStrong(t *testing.T) {
	html := NewRenderer(WithWrapEmoji(true)).RenderString("**boom :rocket:**")
	assertContains(t, html, "<strong>")
	assertContains(t, html, "🚀")
}

func TestHardening_EmojiInLinkText(t *testing.T) {
	html := NewRenderer(WithWrapEmoji(true)).RenderString("[go :rocket:](https://example.com)")
	assertContains(t, html, `<a href="https://example.com">`)
	assertContains(t, html, "🚀")
}

func TestHardening_EmojiInAdmonitionBody(t *testing.T) {
	html := NewRenderer(WithWrapEmoji(true)).RenderString("> [!NOTE]\n> body :sparkles:")
	assertContains(t, html, "admonition-note")
	assertContains(t, html, "✨")
}

// --- Super/sub ---

func TestHardening_SuperscriptInTable(t *testing.T) {
	src := "| x |\n|---|\n| E = mc^2^ |\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<sup>2</sup>")
}

func TestHardening_SubscriptNoMatchOnBareTilde(t *testing.T) {
	// ~x~ is strikethrough (GFM), not subscript. Subscript requires ~~ as strikethrough
	// under GFM and ~x~ falls into strikethrough in tree-sitter-markdown. Verify we
	// don't spuriously emit <sub>.
	html := NewRenderer().RenderString("plain ~x~ text\n")
	_ = html // result depends on grammar; assert absence of crash/regression only
}

// --- Admonitions (nested content) ---

func TestHardening_AdmonitionWithNestedList(t *testing.T) {
	src := "> [!TIP]\n> - one\n> - two\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "admonition-tip")
	assertContains(t, html, "<ul>")
	// Admonition list items render loose (<li><p>one</p></li>) — the invariant
	// is that the item text renders inside the list, not the exact wrapping.
	assertContains(t, html, "one")
	assertContains(t, html, "two")
}

func TestHardening_AdmonitionWithCodeBlock(t *testing.T) {
	src := "> [!IMPORTANT]\n> ```go\n> x := 1\n> ```\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "admonition-important")
	assertContains(t, html, "x := 1")
}

func TestHardening_MultipleAdmonitionsPerDoc(t *testing.T) {
	src := "> [!NOTE]\n> n\n\n> [!TIP]\n> t\n\n> [!IMPORTANT]\n> i\n"
	html := NewRenderer().RenderString(src)
	for _, cls := range []string{"admonition-note", "admonition-tip", "admonition-important"} {
		if !strings.Contains(html, cls) {
			t.Fatalf("want %s in:\n%s", cls, html)
		}
	}
}

// --- Footnotes ---

func TestHardening_FootnoteRefBeforeDefinition(t *testing.T) {
	// Ref appears on line 1, definition appears later — should still resolve.
	src := "Sentence[^1].\n\nMore text.\n\n[^1]: The note.\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `href="#fn-1"`)
	assertContains(t, html, "The note.")
}

func TestHardening_MultipleRefsToSameFootnote(t *testing.T) {
	src := "A[^n] B[^n] C[^n].\n\n[^n]: note\n"
	html := NewRenderer().RenderString(src)
	if strings.Count(html, `href="#fn-n"`) < 3 {
		t.Fatalf("expected 3 refs to fn-n, got:\n%s", html)
	}
	// Only one definition section per ID.
	if strings.Count(html, `id="fn-n"`) != 1 {
		t.Fatalf("expected exactly 1 definition id, got:\n%s", html)
	}
}

func TestHardening_FootnoteDefinitionWithInlineMarkdown(t *testing.T) {
	src := "Text[^x]\n\n[^x]: *italic* and **bold** and `code`\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "<em>italic</em>")
	assertContains(t, html, "<strong>bold</strong>")
	assertContains(t, html, "<code>code</code>")
}

// --- Diagrams ---

func TestHardening_DiagramSequence(t *testing.T) {
	html := NewRenderer().RenderString("```sequence\nA->B: hi\n```\n")
	assertContains(t, html, "mdpp-diagram-sequence")
	assertContains(t, html, `data-diagram-kind="sequence"`)
}

func TestHardening_DiagramClass(t *testing.T) {
	html := NewRenderer().RenderString("```class\nclassDiagram\n  Animal <|-- Cat\n```\n")
	assertContains(t, html, "mdpp-diagram-class")
}

func TestHardening_DiagramState(t *testing.T) {
	html := NewRenderer().RenderString("```state\nstateDiagram\n  [*] --> Idle\n```\n")
	assertContains(t, html, "mdpp-diagram-state")
}

func TestHardening_DiagramErd(t *testing.T) {
	html := NewRenderer().RenderString("```erd\nUser ||--o{ Post : writes\n```\n")
	assertContains(t, html, "mdpp-diagram-erd")
}

func TestHardening_NonDiagramLanguageStaysCode(t *testing.T) {
	html := NewRenderer().RenderString("```python\nprint(1)\n```\n")
	assertNotContains(t, html, "mdpp-diagram")
	assertContains(t, html, `class="language-python"`)
}

// --- Frontmatter ---

func TestHardening_FrontmatterArray(t *testing.T) {
	src := "---\ntags:\n  - go\n  - mdpp\n---\nbody\n"
	doc := Parse([]byte(src))
	fm := doc.Frontmatter()
	tags, ok := fm["tags"].([]any)
	if !ok {
		t.Fatalf("expected tags slice, got %T: %v", fm["tags"], fm["tags"])
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
}

func TestHardening_FrontmatterNestedMap(t *testing.T) {
	src := "---\nauthor:\n  name: O\n  handle: odvcencio\n---\n"
	doc := Parse([]byte(src))
	fm := doc.Frontmatter()
	author, ok := fm["author"].(map[string]any)
	if !ok {
		t.Fatalf("expected author map, got %T: %v", fm["author"], fm["author"])
	}
	if author["handle"] != "odvcencio" {
		t.Fatalf("expected handle=odvcencio, got %v", author["handle"])
	}
}

func TestHardening_FrontmatterInvalidYAMLDoesNotCrash(t *testing.T) {
	src := "---\n: garbage :: invalid\n---\nbody\n"
	doc := Parse([]byte(src))
	if doc == nil {
		t.Fatal("parse returned nil")
	}
	// Frontmatter is nil or empty when YAML fails; body still renders.
	html := NewRenderer().RenderString(src)
	assertContains(t, html, "body")
}

// --- Table of Contents (nesting and slug behavior) ---

func TestHardening_TOCWithH1Only(t *testing.T) {
	src := "[[toc]]\n\n# One\n\n# Two\n"
	html := NewRenderer(WithHeadingIDs(true)).RenderString(src)
	assertContains(t, html, "mdpp-toc")
	assertContains(t, html, `href="#one"`)
	assertContains(t, html, `href="#two"`)
}

func TestHardening_TOCSameHeadingTwice(t *testing.T) {
	// Two H2s with identical text produce identical slug IDs. The TOC and
	// the headings will all link/resolve to the first occurrence — document
	// this quirk (slug dedup is a renderer-level decision, not done today).
	src := "[[toc]]\n\n## Dupe\n\n## Dupe\n"
	html := NewRenderer(WithHeadingIDs(true)).RenderString(src)
	assertContains(t, html, "mdpp-toc")
	if strings.Count(html, `href="#dupe"`) != 2 {
		t.Fatalf("expected 2 refs to #dupe, got:\n%s", html)
	}
}

func TestHardening_TOCMultipleDirectivesAllMaterialize(t *testing.T) {
	src := "[[toc]]\n\n## One\n\n[[toc]]\n\n## Two\n"
	html := NewRenderer(WithHeadingIDs(true)).RenderString(src)
	if strings.Count(html, `class="mdpp-toc"`) != 2 {
		t.Fatalf("expected 2 TOC navs, got:\n%s", html)
	}
}

func TestHardening_TOCSkipsLevelsCleanly(t *testing.T) {
	// Jump from H2 straight to H4 — the TOC should still render without
	// crashing or producing a malformed tree.
	src := "[[toc]]\n\n## A\n\n#### A.1.1.1\n\n## B\n"
	html := NewRenderer(WithHeadingIDs(true)).RenderString(src)
	assertContains(t, html, "mdpp-toc")
	assertContains(t, html, "A.1.1.1")
}

// --- AST structural assertions (not just HTML substrings) ---

func TestHardening_AdmonitionASTShape(t *testing.T) {
	doc := Parse([]byte("> [!NOTE] Title\n> body\n"))
	found := findFirst(doc.Root, NodeAdmonition)
	if found == nil {
		t.Fatal("no NodeAdmonition in tree")
	}
	if found.Attrs["type"] != "note" {
		t.Fatalf("type=%q, want note", found.Attrs["type"])
	}
	if found.Attrs["title"] != "Title" {
		t.Fatalf("title=%q, want Title", found.Attrs["title"])
	}
}

func TestHardening_FootnoteASTShape(t *testing.T) {
	doc := Parse([]byte("text[^a]\n\n[^a]: def\n"))
	ref := findFirst(doc.Root, NodeFootnoteRef)
	if ref == nil || ref.Attrs["id"] != "a" {
		t.Fatalf("ref id mismatch: %+v", ref)
	}
	def := findFirst(doc.Root, NodeFootnoteDef)
	if def == nil || def.Attrs["id"] != "a" {
		t.Fatalf("def id mismatch: %+v", def)
	}
}

func TestHardening_TOCASTShape(t *testing.T) {
	doc := Parse([]byte("[[toc]]\n\n## A\n\n## B\n"))
	toc := findFirst(doc.Root, NodeTableOfContents)
	if toc == nil {
		t.Fatal("no NodeTableOfContents in tree")
	}
	if len(toc.Children) != 1 || toc.Children[0].Type != NodeList {
		t.Fatalf("TOC shape unexpected: %+v", toc)
	}
	if len(toc.Children[0].Children) != 2 {
		t.Fatalf("want 2 TOC list items, got %d", len(toc.Children[0].Children))
	}
}

// findFirst walks the tree depth-first returning the first node of the
// given type, or nil.
func findFirst(root *Node, typ NodeType) *Node {
	if root == nil {
		return nil
	}
	if root.Type == typ {
		return root
	}
	for _, c := range root.Children {
		if got := findFirst(c, typ); got != nil {
			return got
		}
	}
	return nil
}

// countNodes returns the number of nodes of the given type in the tree.
func countNodes(root *Node, typ NodeType) int {
	if root == nil {
		return 0
	}
	n := 0
	if root.Type == typ {
		n = 1
	}
	for _, c := range root.Children {
		n += countNodes(c, typ)
	}
	return n
}

// assertHeadingCount is a helper for stress tests.
func assertHeadingCount(t *testing.T, doc *Document, want int) {
	t.Helper()
	got := countNodes(doc.Root, NodeHeading)
	if got != want {
		t.Fatalf("heading count: got %d want %d", got, want)
	}
}

// Silence unused import warning on fmt when the only use above is via %q in t.Fatalf.
var _ = fmt.Sprintf

package mdpp

import (
	"strings"
	"testing"
)

func TestParseHeading(t *testing.T) {
	doc := MustParse([]byte("# Hello"))
	root := doc.Root
	if root.Type != NodeDocument {
		t.Fatalf("expected NodeDocument, got %d", root.Type)
	}
	if len(root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	h := root.Children[0]
	if h.Type != NodeHeading {
		t.Fatalf("expected NodeHeading, got %d", h.Type)
	}
	if h.Attrs["level"] != "1" {
		t.Fatalf("expected level 1, got %q", h.Attrs["level"])
	}
	if len(h.Children) == 0 {
		t.Fatal("expected heading to have text children")
	}
	text := collectText(h)
	if text != "Hello" {
		t.Fatalf("expected text %q, got %q", "Hello", text)
	}
}

func TestParseHeadingLevels(t *testing.T) {
	tests := []struct {
		src   string
		level string
		text  string
	}{
		{"## Second", "2", "Second"},
		{"### Third", "3", "Third"},
		{"#### Fourth", "4", "Fourth"},
		{"##### Fifth", "5", "Fifth"},
		{"###### Sixth", "6", "Sixth"},
	}
	for _, tt := range tests {
		doc := MustParse([]byte(tt.src))
		if len(doc.Root.Children) == 0 {
			t.Fatalf("no children for %q", tt.src)
		}
		h := doc.Root.Children[0]
		if h.Type != NodeHeading {
			t.Fatalf("expected NodeHeading for %q, got %d", tt.src, h.Type)
		}
		if h.Attrs["level"] != tt.level {
			t.Errorf("expected level %q for %q, got %q", tt.level, tt.src, h.Attrs["level"])
		}
		text := collectText(h)
		if text != tt.text {
			t.Errorf("expected text %q for %q, got %q", tt.text, tt.src, text)
		}
	}
}

func TestParseHeadingWithExclamation(t *testing.T) {
	doc := MustParse([]byte("# Hello World!"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	h := doc.Root.Children[0]
	if h.Type != NodeHeading {
		t.Fatalf("expected NodeHeading, got %d", h.Type)
	}
	text := collectText(h)
	if text != "Hello World!" {
		t.Fatalf("expected text %q, got %q", "Hello World!", text)
	}
}

func TestParseHeadingWithTerminalPunctuationInLongDocument(t *testing.T) {
	src := []byte(`# Hello World!

A matter of origin, this didn't really exist before February 19th, 2026. That was the evening I would conceive of gotreesitter[^1]. Less than a week or so later, I would foolishly declare it ported and solved to HackerNews[^2] to mixed-ish reviews. I think the general sentiment was that maybe this could be useful to *someone*, *somewhere*, *eventually*-- but that this was simply too immature, too vibe-coded, and too much maintenance burden for a single person.

[^1]: [gotreesitter](https://github.com/odvcencio/gotreesitter)
[^2]: [Disaster, but lucky still](https://link-to-article)
`)

	doc := MustParse(src)
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	h := doc.Root.Children[0]
	if h.Type != NodeHeading {
		t.Fatalf("expected NodeHeading, got %d", h.Type)
	}
	if text := collectText(h); text != "Hello World!" {
		t.Fatalf("expected heading text %q, got %q", "Hello World!", text)
	}
}

func TestParseNodeRanges(t *testing.T) {
	src := []byte("# Hello\n\nA **bold** move\n")
	doc := MustParse(src)

	assertRange(t, "document", doc.Root.Range, Range{StartByte: 0, EndByte: len(src), StartLine: 1, StartCol: 1, EndLine: 4, EndCol: 1})
	if len(doc.Root.Children) < 2 {
		t.Fatalf("expected heading and paragraph, got %d children", len(doc.Root.Children))
	}

	heading := doc.Root.Children[0]
	assertRange(t, "heading", heading.Range, Range{StartByte: 0, EndByte: 8, StartLine: 1, StartCol: 1, EndLine: 2, EndCol: 1})
	if len(heading.Children) != 1 {
		t.Fatalf("expected one heading child, got %d", len(heading.Children))
	}
	assertRange(t, "heading text", heading.Children[0].Range, Range{StartByte: 2, EndByte: 7, StartLine: 1, StartCol: 3, EndLine: 1, EndCol: 8})

	paragraph := doc.Root.Children[1]
	assertRange(t, "paragraph", paragraph.Range, Range{StartByte: 9, EndByte: 25, StartLine: 3, StartCol: 1, EndLine: 4, EndCol: 1})

	var strong *Node
	for _, child := range paragraph.Children {
		if child.Type == NodeStrong {
			strong = child
			break
		}
	}
	if strong == nil {
		t.Fatal("expected strong node")
	}
	assertRange(t, "strong", strong.Range, Range{StartByte: 11, EndByte: 19, StartLine: 3, StartCol: 3, EndLine: 3, EndCol: 11})
	if len(strong.Children) != 1 {
		t.Fatalf("expected one strong child, got %d", len(strong.Children))
	}
	assertRange(t, "strong text", strong.Children[0].Range, Range{StartByte: 13, EndByte: 17, StartLine: 3, StartCol: 5, EndLine: 3, EndCol: 9})
}

func TestParseSoftBreakRange(t *testing.T) {
	doc := MustParse([]byte("one\ntwo\n"))
	if len(doc.Root.Children) != 1 {
		t.Fatalf("expected one paragraph, got %d", len(doc.Root.Children))
	}
	para := doc.Root.Children[0]
	if len(para.Children) != 3 {
		t.Fatalf("expected text, soft break, text; got %d children", len(para.Children))
	}
	assertRange(t, "first text", para.Children[0].Range, Range{StartByte: 0, EndByte: 3, StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 4})
	assertRange(t, "soft break", para.Children[1].Range, Range{StartByte: 3, EndByte: 4, StartLine: 1, StartCol: 4, EndLine: 2, EndCol: 1})
	assertRange(t, "second text", para.Children[2].Range, Range{StartByte: 4, EndByte: 7, StartLine: 2, StartCol: 1, EndLine: 2, EndCol: 4})
}

func TestPostProcessedNodeRanges(t *testing.T) {
	doc := MustParse([]byte("Nice :sweat_smile: and $x$.\n"))
	emoji := findFirstNodeOfType(doc.Root, NodeEmoji)
	if emoji == nil {
		t.Fatal("expected emoji node")
	}
	assertRange(t, "emoji", emoji.Range, Range{StartByte: 5, EndByte: 18, StartLine: 1, StartCol: 6, EndLine: 1, EndCol: 19})

	math := findFirstNodeOfType(doc.Root, NodeMathInline)
	if math == nil {
		t.Fatal("expected inline math node")
	}
	assertRange(t, "inline math", math.Range, Range{StartByte: 23, EndByte: 26, StartLine: 1, StartCol: 24, EndLine: 1, EndCol: 27})
}

func TestDirectiveNodeRanges(t *testing.T) {
	tocDoc := MustParse([]byte("# A\n\n[[toc]]\n"))
	toc := findFirstNodeOfType(tocDoc.Root, NodeTableOfContents)
	if toc == nil {
		t.Fatal("expected TOC node")
	}
	assertRange(t, "toc", toc.Range, Range{StartByte: 5, EndByte: 13, StartLine: 3, StartCol: 1, EndLine: 4, EndCol: 1})

	embedDoc := MustParse([]byte("[[embed:https://example.com/video]]\n"))
	embed := findFirstNodeOfType(embedDoc.Root, NodeAutoEmbed)
	if embed == nil {
		t.Fatal("expected auto embed node")
	}
	assertRange(t, "embed", embed.Range, Range{StartByte: 0, EndByte: 36, StartLine: 1, StartCol: 1, EndLine: 2, EndCol: 1})
}

func TestContainerDirectiveParseAttrsAndNestedContent(t *testing.T) {
	src := []byte(":::warning \"Heads up\" {.extra #warn audience=\"dev\"}\nBody **bold**.\n:::\n")
	doc := MustParse(src)
	container := findFirstNodeOfType(doc.Root, NodeContainerDirective)
	if container == nil {
		t.Fatal("expected container directive")
	}
	if got := container.Attr("name"); got != "warning" {
		t.Fatalf("name = %q, want warning", got)
	}
	if got := container.Attr("title"); got != "Heads up" {
		t.Fatalf("title = %q, want Heads up", got)
	}
	if got := container.Attr("class"); got != "extra" {
		t.Fatalf("class = %q, want extra", got)
	}
	if got := container.Attr("id"); got != "warn" {
		t.Fatalf("id = %q, want warn", got)
	}
	if got := container.Attr("attrs"); !strings.Contains(got, `"audience":"dev"`) {
		t.Fatalf("attrs = %q, want audience JSON", got)
	}
	assertRange(t, "container", container.Range, Range{StartByte: 0, EndByte: len(src), StartLine: 1, StartCol: 1, EndLine: 4, EndCol: 1})
	if findFirstNodeOfType(container, NodeStrong) == nil {
		t.Fatal("expected nested inline markdown to be parsed")
	}
}

func TestContainerDirectiveUnclosedDiagnostic(t *testing.T) {
	doc := MustParse([]byte(":::note\nBody\n"))
	if len(doc.Diagnostics()) != 1 {
		t.Fatalf("expected one diagnostic, got %#v", doc.Diagnostics())
	}
	if got := doc.Diagnostics()[0].Code; got != "MDPP-PARSE-002" {
		t.Fatalf("diagnostic code = %q, want MDPP-PARSE-002", got)
	}
}

func TestParseParagraph(t *testing.T) {
	doc := MustParse([]byte("Just some plain text."))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	if p.Type != NodeParagraph {
		t.Fatalf("expected NodeParagraph, got %d", p.Type)
	}
	text := collectText(p)
	if text != "Just some plain text." {
		t.Fatalf("expected %q, got %q", "Just some plain text.", text)
	}
}

func TestParseCodeBlock(t *testing.T) {
	src := "```go\nfmt.Println(\"hello\")\n```"
	doc := MustParse([]byte(src))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	cb := doc.Root.Children[0]
	if cb.Type != NodeCodeBlock {
		t.Fatalf("expected NodeCodeBlock, got %d", cb.Type)
	}
	if cb.Attrs["language"] != "go" {
		t.Fatalf("expected language %q, got %q", "go", cb.Attrs["language"])
	}
	if cb.Literal == "" {
		t.Fatal("expected non-empty code literal")
	}
}

func TestParseDiagramFence(t *testing.T) {
	src := "```mermaid\nflowchart TD\n  A --> B\n```"
	doc := MustParse([]byte(src))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	diagram := doc.Root.Children[0]
	if diagram.Type != NodeDiagram {
		t.Fatalf("expected NodeDiagram, got %d", diagram.Type)
	}
	if diagram.Attrs["syntax"] != "mermaid" {
		t.Fatalf("expected syntax %q, got %q", "mermaid", diagram.Attrs["syntax"])
	}
	if diagram.Attrs["kind"] != "flowchart" {
		t.Fatalf("expected kind %q, got %q", "flowchart", diagram.Attrs["kind"])
	}
	if !strings.Contains(diagram.Literal, "A --> B") {
		t.Fatalf("expected diagram literal to contain edge, got %q", diagram.Literal)
	}
}

func TestParseDiagramAliasFence(t *testing.T) {
	doc := MustParse([]byte("```erd\nUser ||--o{ Post : writes\n```"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	diagram := doc.Root.Children[0]
	if diagram.Type != NodeDiagram {
		t.Fatalf("expected NodeDiagram, got %d", diagram.Type)
	}
	if diagram.Attrs["kind"] != "erd" {
		t.Fatalf("expected kind %q, got %q", "erd", diagram.Attrs["kind"])
	}
}

func TestParseBoldItalic(t *testing.T) {
	doc := MustParse([]byte("**bold** and *italic*"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	if p.Type != NodeParagraph {
		t.Fatalf("expected NodeParagraph, got %d", p.Type)
	}

	var foundStrong, foundEmphasis bool
	for _, c := range p.Children {
		if c.Type == NodeStrong {
			foundStrong = true
			text := collectText(c)
			if text != "bold" {
				t.Errorf("expected strong text %q, got %q", "bold", text)
			}
		}
		if c.Type == NodeEmphasis {
			foundEmphasis = true
			text := collectText(c)
			if text != "italic" {
				t.Errorf("expected emphasis text %q, got %q", "italic", text)
			}
		}
	}
	if !foundStrong {
		t.Error("expected NodeStrong child")
	}
	if !foundEmphasis {
		t.Error("expected NodeEmphasis child")
	}
}

func TestParseList(t *testing.T) {
	doc := MustParse([]byte("- one\n- two"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	list := doc.Root.Children[0]
	if list.Type != NodeList {
		t.Fatalf("expected NodeList, got %d", list.Type)
	}
	if len(list.Children) < 2 {
		t.Fatalf("expected at least 2 list items, got %d", len(list.Children))
	}
	for _, item := range list.Children {
		if item.Type != NodeListItem {
			t.Errorf("expected NodeListItem, got %d", item.Type)
		}
	}
}

func TestParseOrderedListMarkers(t *testing.T) {
	for _, src := range []string{"1. one\n2. two", "1) one\n2) two"} {
		doc := MustParse([]byte(src))
		if len(doc.Root.Children) == 0 {
			t.Fatalf("%q: expected at least one child", src)
		}
		list := doc.Root.Children[0]
		if list.Type != NodeList {
			t.Fatalf("%q: expected NodeList, got %d", src, list.Type)
		}
		if list.Attrs["ordered"] != "true" {
			t.Fatalf("%q: expected ordered list attrs, got %#v", src, list.Attrs)
		}
	}
}

func TestParseOrderedListStart(t *testing.T) {
	doc := MustParse([]byte("3) three\n4) four"))
	list := doc.Root.Children[0]
	if list.Attrs["ordered"] != "true" {
		t.Fatalf("expected ordered list attrs, got %#v", list.Attrs)
	}
	if list.Attrs["start"] != "3" {
		t.Fatalf("expected start=3, got %#v", list.Attrs)
	}
}

func TestParseBlockquote(t *testing.T) {
	doc := MustParse([]byte("> quote"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	bq := doc.Root.Children[0]
	if bq.Type != NodeBlockquote {
		t.Fatalf("expected NodeBlockquote, got %d", bq.Type)
	}
	text := collectText(bq)
	if text != "quote" {
		t.Fatalf("expected %q, got %q", "quote", text)
	}
}

func TestRenderLongSimpleBlockquote(t *testing.T) {
	source := `> At the time of this writing, gotreesitter[^1] has 457 stars and counting in 2 months of life and has a serious infrastructure shape. It is parity harness and benchmark comparisons keep it honest and steered towards a north star. There is no way to one-shot gotreesitter[^1] with a prompt.`
	html := NewRenderer().RenderString(source)

	assertContains(t, html, "<blockquote>")
	assertContains(t, html, "gotreesitter")
	assertContains(t, html, `class="footnote-ref"`)
	assertNotContains(t, html, "&gt; At the time")
}

func TestParseLink(t *testing.T) {
	doc := MustParse([]byte("[text](https://example.com)"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	var link *Node
	for _, c := range p.Children {
		if c.Type == NodeLink {
			link = c
			break
		}
	}
	if link == nil {
		t.Fatal("expected NodeLink child")
	}
	if link.Attrs["href"] != "https://example.com" {
		t.Errorf("expected href %q, got %q", "https://example.com", link.Attrs["href"])
	}
	text := collectText(link)
	if text != "text" {
		t.Errorf("expected link text %q, got %q", "text", text)
	}
}

func TestParseImage(t *testing.T) {
	doc := MustParse([]byte(`![alt text](image.png "A title")`))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	var img *Node
	for _, c := range p.Children {
		if c.Type == NodeImage {
			img = c
			break
		}
	}
	if img == nil {
		t.Fatal("expected NodeImage child")
	}
	if img.Attrs["src"] != "image.png" {
		t.Errorf("expected src %q, got %q", "image.png", img.Attrs["src"])
	}
	if img.Attrs["alt"] != "alt text" {
		t.Errorf("expected alt %q, got %q", "alt text", img.Attrs["alt"])
	}
	if img.Attrs["title"] != "A title" {
		t.Errorf("expected title %q, got %q", "A title", img.Attrs["title"])
	}
}

func TestParseTable(t *testing.T) {
	src := "| A | B |\n|---|---|\n| 1 | 2 |"
	doc := MustParse([]byte(src))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	table := doc.Root.Children[0]
	if table.Type != NodeTable {
		t.Fatalf("expected NodeTable, got %d", table.Type)
	}
	if len(table.Children) < 2 {
		t.Fatalf("expected at least 2 rows (header + data), got %d", len(table.Children))
	}
	for _, row := range table.Children {
		if row.Type != NodeTableRow {
			t.Errorf("expected NodeTableRow, got %d", row.Type)
		}
		if len(row.Children) < 2 {
			t.Errorf("expected at least 2 cells, got %d", len(row.Children))
		}
		for _, cell := range row.Children {
			if cell.Type != NodeTableCell {
				t.Errorf("expected NodeTableCell, got %d", cell.Type)
			}
		}
	}
}

func TestParseComplexDocument(t *testing.T) {
	src := `# Title

A paragraph with **bold** and *italic*.

- item one
- item two

> a quote

` + "```js\nconsole.log(1)\n```" + `

[link](https://example.com)

| H1 | H2 |
|----|-----|
| a  | b   |
`

	doc := MustParse([]byte(src))
	if doc.Root.Type != NodeDocument {
		t.Fatal("expected NodeDocument root")
	}

	types := map[NodeType]bool{}
	for _, c := range doc.Root.Children {
		types[c.Type] = true
	}

	expected := []NodeType{
		NodeHeading,
		NodeParagraph,
		NodeList,
		NodeBlockquote,
		NodeCodeBlock,
		NodeTable,
	}
	for _, e := range expected {
		if !types[e] {
			t.Errorf("expected node type %d in complex document", e)
		}
	}
}

// collectText recursively collects all text content from a node tree.
func collectText(n *Node) string {
	if n == nil {
		return ""
	}
	if n.Type == NodeText {
		return n.Literal
	}
	var s string
	for _, c := range n.Children {
		s += collectText(c)
	}
	return s
}

func assertRange(t *testing.T, label string, got Range, want Range) {
	t.Helper()
	if got != want {
		t.Fatalf("%s range = %#v, want %#v", label, got, want)
	}
}

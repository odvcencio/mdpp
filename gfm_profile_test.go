package mdpp

import (
	"strings"
	"testing"
)

func parseGFM(t testing.TB, src string) *Document {
	t.Helper()
	doc, err := ParseWithOptions([]byte(src), ParseOptions{Profile: ProfileGFM})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// blockSummary describes the top-level blocks of doc, one word each.
func blockSummary(doc *Document) []string {
	var out []string
	for _, c := range doc.Root.Children {
		s := c.Type.String()
		if c.Type == NodeHeading {
			s += c.Attr("level")
		}
		out = append(out, s+"("+strings.TrimSpace(collectNodeText(c))+")")
	}
	return out
}

// The expectations below come from the GFM specification (0.29-gfm, built on
// CommonMark 0.31.2), not from the output of the parser.
func TestGFMProfileReadsTheSpecification(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"a paragraph between two lists ends the first (5.3: no merge)",
			"- a\n\nText\n- b", "<ul>\n<li>a</li>\n</ul>\n<p>Text</p>\n<ul>\n<li>b</li>\n</ul>\n"},
		{"container fences are text (no directives in GFM)",
			":::note\n## Extra\n:::", "<p>:::note</p>\n<h2>Extra</h2>\n<p>:::</p>\n"},
		{"no front matter: a break, a setext heading, an ATX heading (4.1, 4.3)",
			"---\ntitle: x\n---\n\n## H", "<hr />\n<h2>title: x</h2>\n<h2>H</h2>\n"},
		{"an alert is a block quote with a lazy continuation, then a heading (5.1, 4.2)",
			"> [!NOTE]\nx\n## Evidence\nObserved.", "<blockquote>\n<p>[!NOTE]\nx</p>\n</blockquote>\n<h2>Evidence</h2>\n<p>Observed.</p>\n"},
		{"an alert with a blank line before the heading",
			"> [!NOTE]\nx\n\n## Evidence\nObserved.", "<blockquote>\n<p>[!NOTE]\nx</p>\n</blockquote>\n<h2>Evidence</h2>\n<p>Observed.</p>\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := parseGFM(t, tc.src)
			out, err := Render(doc, RenderOptions{UnsafeHTML: true})
			if err != nil {
				t.Fatal(err)
			}
			if normalizeSpecHTML(string(out)) != normalizeSpecHTML(tc.want) {
				t.Errorf("%q\ngot:  %q\nwant: %q\n%s", tc.src, out, tc.want, fullNodeDump(doc.Root))
			}
			for _, typ := range []NodeType{NodeComponent, NodeContainerDirective, NodeAdmonition, NodeFrontmatter} {
				if n := doc.Root.Find(typ); len(n) > 0 {
					t.Errorf("%q: found a %v node in the GFM profile", tc.src, typ)
				}
			}
			if doc.Profile() != ProfileGFM {
				t.Errorf("Profile() is %v", doc.Profile())
			}
		})
	}
	// "<H2 class=...>" after a paragraph is raw HTML, not a component. The
	// specification makes it an HTML block (4.6, type 6, which interrupts a
	// paragraph); the block grammar does not end the paragraph, so the tag is
	// inline raw HTML here (see the findings for the grammar).
	doc := parseGFM(t, "Before.\n<H2 class=\"x\">Extra</H2>")
	if len(doc.Root.Find(NodeComponent)) != 0 {
		t.Errorf("a component in the GFM profile:\n%s", fullNodeDump(doc.Root))
	}
	var tags []string
	doc.Root.Walk(func(n *Node) bool {
		if n.Type == NodeHTMLInline || n.Type == NodeHTMLBlock {
			tags = append(tags, strings.TrimSpace(n.Literal))
		}
		return true
	})
	if len(tags) == 0 || tags[0] != "<H2 class=\"x\">" && !strings.HasPrefix(tags[0], "<H2 class=\"x\">") {
		t.Errorf("want the H2 tag as raw HTML, got %q:\n%s", tags, fullNodeDump(doc.Root))
	}
}

// Every HTML block start condition (4.6), in upper, lower and mixed case, is an
// HTML block or is reported: no component, whatever the case of the tag. The
// grammar is the only reader of blocks in this profile; where it does not read
// an HTML block as one block, the document says so (DiagHTMLBlockUnread) over
// the lines of the block, and the tree there is not trusted.
func TestGFMProfileHTMLBlockStartsInAnyCase(t *testing.T) {
	forms := []string{
		"<%s>\ntext\n</%s>",                 // 1: pre, script, style, textarea
		"<%s class=\"a\">\ntext\n</%s>\n\n", // 6: block-level names
		"<%s />\ntext",                      // 7: a complete tag on its own line
	}
	names := map[int][]string{
		0: {"pre", "PRE", "Pre", "script", "SCRIPT", "Script", "style", "STYLE", "textarea", "TextArea"},
		1: {"div", "DIV", "Div", "h2", "H2", "section", "SECTION", "Table", "details", "Details", "p", "P"},
		2: {"span", "SPAN", "Card", "Callout", "MyComponent", "x-foo", "X-Foo"},
	}
	for i, form := range forms {
		for _, name := range names[i] {
			src := strings.ReplaceAll(form, "%s", name)
			doc := parseGFM(t, src)
			requireHTMLBlockOrReport(t, doc, src)
			if n := doc.Root.Find(NodeComponent); len(n) > 0 {
				t.Errorf("%q: found a component", src)
			}
		}
	}
	others := []string{"<!-- comment -->\ntext", "<?php echo 1; ?>\ntext", "<!DOCTYPE html>\ntext", "<![CDATA[ x ]]>\ntext", "<!doctype html>\ntext"}
	for _, src := range others {
		doc := parseGFM(t, src)
		requireHTMLBlockOrReport(t, doc, src)
	}
	// An inline tag with a capital is raw HTML (6.6), not a component.
	doc := parseGFM(t, "A <Card title=\"x\">inside</Card> b <Badge/> c\n")
	if len(doc.Root.Find(NodeComponent)) != 0 || len(doc.Root.Find(NodeHTMLInline)) != 3 {
		t.Errorf("inline tags:\n%s", fullNodeDump(doc.Root))
	}
}

// One document that uses each Markdown++ extension once. Read as GFM, none of
// them produces its node, and the GFM constructs in it stay.
func TestGFMProfileTurnsEveryExtensionOff(t *testing.T) {
	src := "---\ntitle: T\n---\n\n" +
		"# Title\n\n[[toc]]\n\n" +
		"> [!NOTE] Heads up\n> body\n\n" +
		":::warning\ninside\n:::\n\n" +
		"Term\n: Definition\n\n" +
		"Text[^1] with $a^2$, x^2^, :smile: and {expr} and <Card prop=\"1\" />.\n\n" +
		"H~2~O\n\n" +
		"[[embed:https://example.com/v]]\n\n" +
		"$$\nx\n$$\n\n" +
		"```mermaid\ngraph TD\nA-->B\n```\n\n" +
		"```go {1-3}\nx := 1\n```\n\n" +
		"- a\n\nbetween\n- b\n\n" +
		"- [x] task\n\n| h |\n|---|\n| c |\n\n~~gone~~ <https://auto.link> https://bare.example.com\n\n" +
		"[^1]: the note\n"
	doc := parseGFM(t, src)
	banned := []NodeType{NodeComponent, NodeExpression, NodeContainerDirective, NodeAdmonition, NodeFrontmatter,
		NodeTableOfContents, NodeAutoEmbed, NodeDiagram, NodeMathInline, NodeMathBlock, NodeFootnoteRef, NodeFootnoteDef,
		NodeDefinitionList, NodeDefinitionTerm, NodeDefinitionDesc, NodeSuperscript, NodeSubscript, NodeEmoji, NodeSlide}
	for _, typ := range banned {
		if n := doc.Root.Find(typ); len(n) > 0 {
			t.Errorf("a %v node in the GFM profile:\n%s", typ, fullNodeDump(doc.Root))
		}
	}
	if doc.Frontmatter() != nil {
		t.Errorf("front matter data %v", doc.Frontmatter())
	}
	for _, typ := range []NodeType{NodeTaskListItem, NodeTable, NodeStrikethrough, NodeLink} {
		if n := doc.Root.Find(typ); len(n) == 0 {
			t.Errorf("no %v node in the GFM profile", typ)
		}
	}
	// Two lists around the paragraph (no merge), the mermaid fence is a code
	// block, and the "{1-3}" fence attribute is not read.
	for _, cb := range doc.Root.Find(NodeCodeBlock) {
		if cb.HasAttr("highlights") {
			t.Errorf("code block has highlights %q", cb.Attr("highlights"))
		}
	}
	if blocks := doc.Root.Find(NodeCodeBlock); len(blocks) != 2 || blocks[0].Attr("language") != "mermaid" {
		t.Errorf("code blocks: %d", len(blocks))
	}
	// The same document in the default profile does produce them.
	def := MustParse([]byte(src))
	for _, typ := range []NodeType{NodeComponent, NodeContainerDirective, NodeAdmonition, NodeFrontmatter, NodeTableOfContents,
		NodeAutoEmbed, NodeDiagram, NodeMathInline, NodeFootnoteDef, NodeDefinitionList, NodeSuperscript, NodeSubscript, NodeEmoji, NodeExpression} {
		if len(def.Root.Find(typ)) == 0 {
			t.Errorf("the default profile reads no %v node", typ)
		}
	}
	if def.Profile() != ProfileMarkdownPlus {
		t.Errorf("default Profile() is %v", def.Profile())
	}
}

// GFM reads one tilde pair as strikethrough (6.5 of the GFM specification).
func TestGFMProfileSingleTildeIsStrikethrough(t *testing.T) {
	for _, src := range []string{"a ~b~ c\n", "a ~**b**~ c\n", "***\n\na ~b~ c\n"} {
		doc := parseGFM(t, src)
		if len(doc.Root.Find(NodeSubscript)) != 0 || len(doc.Root.Find(NodeStrikethrough)) != 1 {
			t.Errorf("%q:\n%s", src, fullNodeDump(doc.Root))
		}
	}
	if len(MustParse([]byte("a ~b~ c\n")).Root.Find(NodeSubscript)) != 1 {
		t.Error("the default profile lost the subscript")
	}
}

func TestParseProfileByName(t *testing.T) {
	for name, want := range map[string]ParseProfile{"gfm": ProfileGFM, "markdown++": ProfileMarkdownPlus, "mdpp": ProfileMarkdownPlus, "": ProfileMarkdownPlus} {
		if got, ok := ParseProfileByName(name); !ok || got != want {
			t.Errorf("%q -> %v %v", name, got, ok)
		}
	}
	if _, ok := ParseProfileByName("commonmark"); ok {
		t.Error("commonmark is not a profile yet")
	}
	if ProfileGFM.String() != "gfm" || ProfileMarkdownPlus.String() != "markdown++" {
		t.Error("String")
	}
}

// requireHTMLBlockOrReport fails unless the first block of doc is an HTML block
// or a DiagHTMLBlockUnread diagnostic covers the first line of src.
func requireHTMLBlockOrReport(t *testing.T, doc *Document, src string) {
	t.Helper()
	if len(doc.Root.Children) > 0 && doc.Root.Children[0].Type == NodeHTMLBlock {
		return
	}
	for _, d := range doc.Diagnostics() {
		if d.Code == DiagHTMLBlockUnread && d.Range.StartByte == 0 && d.Range.EndByte > 0 {
			return
		}
	}
	t.Errorf("%q: the first block is not an HTML block and nothing reports it:\n%s", src, fullNodeDump(doc.Root))
}

// A label that Markdown++ reserves is an ordinary link reference label in GFM:
// "[^a]" (a footnote in Markdown++) and "[!a]" or "[!NOTE]" (an alert marker)
// are references to the definition with that label. The test checks the target
// that each reference resolves to, and not only the absence of a footnote or
// admonition node.
func TestGFMProfileReservedLabelsAreOrdinaryReferences(t *testing.T) {
	for _, label := range []string{"^a", "^1", "^a-b", "!a", "!NOTE", "!WARNING", "!TIP", "!IMPORTANT", "!CAUTION", "!note"} {
		forms := map[string]string{
			"shortcut":  "[" + label + "]: /u\n\n[" + label + "]\n",
			"collapsed": "[" + label + "]: /u\n\n[" + label + "][]\n",
			"full":      "[" + label + "]: /u\n\n[text][" + label + "]\n",
			"image":     "[" + label + "]: /u\n\n![text][" + label + "]\n",
		}
		for name, src := range forms {
			doc := parseGFM(t, src)
			var hrefs []string
			walkNodes(doc.Root, func(n *Node, parent *Node, index int) bool {
				if n.Type == NodeLink && n.Attrs["href"] != "" {
					hrefs = append(hrefs, n.Attrs["href"])
				}
				if n.Type == NodeImage && n.Attrs["src"] != "" {
					hrefs = append(hrefs, n.Attrs["src"])
				}
				return true
			})
			if len(hrefs) != 1 || hrefs[0] != "/u" {
				t.Errorf("label %q, %s form %q: resolved targets %q, want [/u]:\n%s", label, name, src, hrefs, fullNodeDump(doc.Root))
			}
			if len(doc.Root.Find(NodeFootnoteRef))+len(doc.Root.Find(NodeFootnoteDef))+len(doc.Root.Find(NodeAdmonition)) != 0 {
				t.Errorf("label %q, %s form: a footnote or admonition node:\n%s", label, name, fullNodeDump(doc.Root))
			}
		}
	}
	// In the default profile the same labels stay reserved.
	for _, src := range []string{"[^a]: x\n\n[^a]\n"} {
		if len(MustParse([]byte(src)).Root.Find(NodeFootnoteRef)) != 1 {
			t.Errorf("default profile: %q is not a footnote reference", src)
		}
	}
}

// The grammar is the only reader of blocks in the GFM profile. Where it does
// not read an HTML block as one block, the document reports the lines of the
// block (DiagHTMLBlockUnread), so a heading that the tree shows inside them is
// not trusted. Blocks that the grammar reads as HTML blocks are not reported.
func TestGFMProfileReportsHTMLBlocksTheGrammarDoesNotRead(t *testing.T) {
	reported := []struct{ src, covered string }{
		{"Before.\n<h2>\n# h\n", "# h"},
		{"<pre>\n# h\n</pre>\n", "# h"},
		{"<script>\n# h\n- [ ] t\n</script>\n", "- [ ] t"},
		{"<span />\n# h\n", "# h"},
	}
	for _, tc := range reported {
		doc := parseGFM(t, tc.src)
		found := false
		for _, d := range doc.Diagnostics() {
			if d.Code == DiagHTMLBlockUnread && string(doc.Source[d.Range.StartByte:d.Range.EndByte]) != "" &&
				strings.Contains(string(doc.Source[d.Range.StartByte:d.Range.EndByte]), tc.covered) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: no %s diagnostic covers %q:\n%s", tc.src, DiagHTMLBlockUnread, tc.covered, fullNodeDump(doc.Root))
		}
	}
	for _, src := range []string{
		"<!-- c -->\n\n## S\n\n- [ ] t\n",
		"<!--\nmulti\nline\n-->\n\ntext\n",
		"<details>\n<summary>x</summary>\n\n- [ ] t\n\n</details>\n",
		"# h\n\ntext <!-- c --> more\n",
		"text\n\n<div>\n# h\n</div>\n",
	} {
		for _, d := range parseGFM(t, src).Diagnostics() {
			if d.Code == DiagHTMLBlockUnread {
				t.Errorf("%q: unexpected %s over %v", src, DiagHTMLBlockUnread, d.Range)
			}
		}
	}
}

// A document that starts with a "---" line, a block and another "---" line has
// no front matter in GFM: the lines are thematic breaks (or a setext
// underline), and a heading in the block is a heading. The grammar reads front
// matter there and hides the block, so mdpp puts blank lines before the
// document for the grammar; the ranges are mapped back and are exact.
func TestGFMProfileLeadingDashesAreNotFrontMatter(t *testing.T) {
	src := "---\nt: x\n# h\n---\n\ntext\n"
	doc := parseGFM(t, src)
	headings := doc.Root.Find(NodeHeading)
	if len(headings) != 1 || len(doc.Root.Find(NodeThematicBreak)) != 2 {
		t.Fatalf("%d headings, %d breaks, want the heading `# h` between two breaks:\n%s", len(headings), len(doc.Root.Find(NodeThematicBreak)), fullNodeDump(doc.Root))
	}
	h := headings[0]
	if got := string(doc.Source[h.Range.StartByte:h.Range.EndByte]); got != "# h\n" {
		t.Errorf("heading range covers %q, want %q", got, "# h\n")
	}
	if len(doc.Root.Find(NodeFrontmatter)) != 0 {
		t.Errorf("front matter read in GFM")
	}
	if first := doc.Root.Children[0]; first.Type != NodeThematicBreak || first.Range.StartByte != 0 || first.Range.EndByte != 4 || first.Range.StartLine != 1 {
		t.Errorf("first block %v %+v, want the thematic break [0,4)", first.Type, first.Range)
	}
	if string(doc.Source) != src {
		t.Errorf("document source changed: %q", doc.Source)
	}
	if problems := checkNodeRanges(doc); len(problems) != 0 {
		t.Errorf("range problems: %v", problems)
	}
}

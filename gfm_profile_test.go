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

// Every HTML block start condition (4.6), in upper, lower and mixed case, is
// an HTML block: no component, whatever the case of the tag.
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
			if len(doc.Root.Children) == 0 || doc.Root.Children[0].Type != NodeHTMLBlock {
				t.Errorf("%q: first block is not an HTML block:\n%s", src, fullNodeDump(doc.Root))
			}
			if n := doc.Root.Find(NodeComponent); len(n) > 0 {
				t.Errorf("%q: found a component", src)
			}
		}
	}
	others := []string{"<!-- comment -->\ntext", "<?php echo 1; ?>\ntext", "<!DOCTYPE html>\ntext", "<![CDATA[ x ]]>\ntext", "<!doctype html>\ntext"}
	for _, src := range others {
		doc := parseGFM(t, src)
		if len(doc.Root.Children) == 0 || doc.Root.Children[0].Type != NodeHTMLBlock {
			t.Errorf("%q: first block is not an HTML block:\n%s", src, fullNodeDump(doc.Root))
		}
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

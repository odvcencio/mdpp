package mdpp

import (
	"strings"
	"testing"
)

func TestReviewInlineCommentKeepsFormatting(t *testing.T) {
	cases := []struct {
		src  string
		typ  NodeType
		html string
	}{
		{"*a <!-- c --> b*\n", NodeEmphasis, "<em>a &lt;!-- c --&gt; b</em>"},
		{"**a <!-- c --> b**\n", NodeStrong, "<strong>a &lt;!-- c --&gt; b</strong>"},
		{"~~a <!-- c --> b~~\n", NodeStrikethrough, "<del>a &lt;!-- c --&gt; b</del>"},
	}
	for _, tc := range cases {
		t.Run(tc.src, func(t *testing.T) {
			d := MustParse([]byte(tc.src))
			if len(d.Root.Find(tc.typ)) != 1 {
				t.Errorf("missing wrapper %v: %s", tc.typ, DumpTreeForSnapshot(d.Root))
			}
			if comments := d.Root.Find(NodeHTMLInline); len(comments) != 1 || comments[0].Literal != "<!-- c -->" {
				t.Errorf("comments = %+v", comments)
			}
			if got := RenderString(tc.src); !strings.Contains(got, tc.html) {
				t.Errorf("render = %q, want substring %q", got, tc.html)
			}
		})
	}
}

func TestReviewInlineCommentFormattingContexts(t *testing.T) {
	cases := []struct {
		name, source string
		types        []NodeType
		comments     []string
	}{
		{"prefix and suffix", "before *a <!-- c --> b* after\n", []NodeType{NodeEmphasis}, []string{"<!-- c -->"}},
		{"nested", "**bold *a <!-- c --> b* end**\n", []NodeType{NodeStrong, NodeEmphasis}, []string{"<!-- c -->"}},
		{"nested link label", "[x *a <!-- c --> b*](/u)\n", []NodeType{NodeLink, NodeEmphasis}, []string{"<!-- c -->"}},
		{"link inside emphasis", "*[a <!-- c --> b](/u)*\n", []NodeType{NodeEmphasis}, []string{"<!-- c -->"}},
		{"two comments", "*a <!--x--> b <!--y--> c*\n", []NodeType{NodeEmphasis}, []string{"<!--x-->", "<!--y-->"}},
		{"nested delimiters in comment", "*a <!-- _**_ --> b*\n", []NodeType{NodeEmphasis}, []string{"<!-- _**_ -->"}},
		{"multiline", "*a <!-- first\n second --> b*\n", []NodeType{NodeEmphasis}, []string{"<!-- first\n second -->"}},
		{"placeholder collision", "<x      > *a <!-- c --> b*\n", []NodeType{NodeEmphasis}, []string{"<x      >", "<!-- c -->"}},
		{"list", "- *a <!-- c --> b*\n", []NodeType{NodeList, NodeEmphasis}, []string{"<!-- c -->"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := MustParse([]byte(tc.source))
			for _, typ := range tc.types {
				if len(d.Root.Find(typ)) == 0 {
					t.Errorf("missing %v: %s", typ, DumpTreeForSnapshot(d.Root))
				}
			}
			found := d.Root.Find(NodeHTMLInline)
			if len(found) != len(tc.comments) {
				t.Fatalf("HTML nodes=%d want=%d: %s", len(found), len(tc.comments), DumpTreeForSnapshot(d.Root))
			}
			for i, n := range found {
				if n.Literal != tc.comments[i] {
					t.Errorf("HTML %d=%q want=%q", i, n.Literal, tc.comments[i])
				}
			}
		})
	}
}

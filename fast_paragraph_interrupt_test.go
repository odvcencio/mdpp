package mdpp

import (
	"strings"
	"testing"
)

// CommonMark 0.31.2, sections 4.1 and 4.8: a thematic break can interrupt a
// paragraph, so the paragraph ends on the line before it.
// The leading HTML comment sends the rest of the document through the
// fast block path.
func TestThematicBreakInterruptsParagraphAfterLeadingBlock(t *testing.T) {
	breaks := []string{"***", "___", "* * *", "_ _ _", "- - -", "*****", "  ***", "-\t-\t-"}
	for _, br := range breaks {
		for _, lead := range []string{"", "<!-- c -->\n\n"} {
			for _, tc := range []struct {
				name, body string
				want       []NodeType
			}{
				{"after two lines", "T\nU\n" + br + "\n", []NodeType{NodeParagraph, NodeThematicBreak}},
				{"after one line", "T\n" + br + "\n", []NodeType{NodeParagraph, NodeThematicBreak}},
				{"between lines", "T\n" + br + "\nU\n", []NodeType{NodeParagraph, NodeThematicBreak, NodeParagraph}},
			} {
				src := lead + tc.body
				doc := MustParse([]byte(src))
				var got []NodeType
				for _, c := range doc.Root.Children {
					if c.Type != NodeHTMLBlock {
						got = append(got, c.Type)
					}
				}
				if len(got) != len(tc.want) {
					t.Errorf("%q: blocks %v, want %v\n%s", src, got, tc.want, DumpTreeForSnapshot(doc.Root))
					continue
				}
				for i := range got {
					if got[i] != tc.want[i] {
						t.Errorf("%q: blocks %v, want %v", src, got, tc.want)
						break
					}
				}
				doc.Root.Walk(func(n *Node) bool {
					if n.Type == NodeText && strings.Contains(n.Literal, br) && strings.TrimSpace(br) != "" && strings.ContainsAny(n.Literal, "*_-") {
						t.Errorf("%q: break text %q left in a paragraph", src, n.Literal)
					}
					return true
				})
			}
		}
	}
}

func TestThematicBreakAfterLeadingBlockParagraphRanges(t *testing.T) {
	doc := MustParse([]byte("<!-- c -->\n\nT\nU\n***\n"))
	kids := doc.Root.Children
	if len(kids) != 3 || kids[1].Type != NodeParagraph || kids[2].Type != NodeThematicBreak {
		t.Fatalf("unexpected blocks:\n%s", DumpTreeForSnapshot(doc.Root))
	}
	if r := kids[2].Range; r.StartLine != 5 || string(doc.Source[r.StartByte:r.EndByte]) != "***\n" {
		t.Fatalf("break range = %+v", r)
	}
}

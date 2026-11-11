package mdpp

import (
	"fmt"
	"strings"
	"testing"
)

// Expectations in this file come from CommonMark 0.31.2, section 4.3: a
// paragraph of one or more lines over a line of "=" (level 1) or "-"
// (level 2) is a setext heading, wherever it sits in the document.

// inlineLines returns the Text literals of a heading or paragraph and the
// number of soft breaks between them.
func inlineLines(n *Node) (lines []string, softBreaks int) {
	for _, c := range n.Children {
		switch c.Type {
		case NodeText:
			lines = append(lines, c.Literal)
		case NodeSoftBreak:
			softBreaks++
		}
	}
	return lines, softBreaks
}

func TestSetextHeadingAfterLeadingBlock(t *testing.T) {
	leads := []struct {
		name   string
		source string
		typ    NodeType
	}{
		{"html comment", "<!-- c -->\n", NodeHTMLBlock},
		{"html pre block", "<pre>\nx\n</pre>\n", NodeHTMLBlock},
		{"html div block", "<div>\nx\n</div>\n", NodeHTMLBlock},
		{"thematic break stars", "***\n", NodeThematicBreak},
		{"thematic break underscores", "___\n", NodeThematicBreak},
		{"thematic break spaced", "* * *\n", NodeThematicBreak},
	}
	underlines := []struct {
		marker string
		level  string
	}{{"=", "1"}, {"===", "1"}, {"-", "2"}, {"---", "2"}}

	for _, lead := range leads {
		for count := 1; count <= 3; count++ {
			for _, u := range underlines {
				name := fmt.Sprintf("%s/%d lines/%s", lead.name, count, u.marker)
				t.Run(name, func(t *testing.T) {
					want := make([]string, count)
					for i := range want {
						want[i] = fmt.Sprintf("T%d", i+1)
					}
					src := lead.source + "\n" + strings.Join(want, "\n") + "\n" + u.marker
					doc := MustParse([]byte(src))
					kids := doc.Root.Children
					if len(kids) != 2 || kids[0].Type != lead.typ || kids[1].Type != NodeHeading {
						t.Fatalf("source %q: want [%v, Heading], got:\n%s", src, lead.typ, DumpTreeForSnapshot(doc.Root))
					}
					h := kids[1]
					if h.Attr("level") != u.level {
						t.Fatalf("source %q: level = %q, want %q", src, h.Attr("level"), u.level)
					}
					got, soft := inlineLines(h)
					if strings.Join(got, "|") != strings.Join(want, "|") || soft != count-1 {
						t.Fatalf("source %q: heading text %v with %d soft breaks, want %v with %d", src, got, soft, want, count-1)
					}
				})
			}
		}
	}
}

func TestSetextHeadingAfterLeadingBlockKeepsFollowingBlocks(t *testing.T) {
	src := "<!-- c -->\n\nT\nU\n=\n\nafter\n\n- x\n"
	doc := MustParse([]byte(src))
	kids := doc.Root.Children
	if len(kids) != 4 || kids[0].Type != NodeHTMLBlock || kids[1].Type != NodeHeading ||
		kids[2].Type != NodeParagraph || kids[3].Type != NodeList {
		t.Fatalf("unexpected blocks:\n%s", DumpTreeForSnapshot(doc.Root))
	}
	r := kids[1].Range
	if r.StartLine != 3 || strings.TrimSpace(string(doc.Source[r.StartByte:r.EndByte])) != "T\nU\n=" {
		t.Fatalf("heading range = %+v (%q), want it to start on line 3 and cover T, U and the underline", r, doc.Source[r.StartByte:r.EndByte])
	}
}

// Text that is not a setext heading stays a paragraph after a leading block.
func TestNoSetextHeadingAfterLeadingBlockWithoutUnderline(t *testing.T) {
	cases := []string{
		"<!-- c -->\n\nT\nU\n",
		"<!-- c -->\n\nT\nU\n= x\n", // text after the markers: not an underline
		"<!-- c -->\n\nT\nU\n\n=\n", // a blank line ends the paragraph first
		"***\n\nT\nU\n==x\n",        // not an underline
		"***\n\nT\n\nU\n",           // two paragraphs
	}
	for _, src := range cases {
		doc := MustParse([]byte(src))
		doc.Root.Walk(func(n *Node) bool {
			if n.Type == NodeHeading {
				t.Errorf("source %q: unexpected heading:\n%s", src, DumpTreeForSnapshot(doc.Root))
			}
			return true
		})
	}
}

// Large documents are split at their headings and the pieces take the same
// block path, so a multi-line setext heading must survive there too.
func TestSetextHeadingInSegmentedDocument(t *testing.T) {
	var b strings.Builder
	b.WriteString("# One\n\n")
	b.WriteString(strings.Repeat("filler text for the first section\n", 40))
	b.WriteString("\n## Two\n\nFirst line\nSecond line\n===\n\n")
	b.WriteString(strings.Repeat("filler text for the second section\n", 40))
	b.WriteString("\n## Three\n\nlast\n")
	src := b.String()
	if len(src) < segmentedDocumentMinBytes || len(topLevelHeadingChunks([]byte(src))) < 2 {
		t.Fatalf("test input does not take the segmented path")
	}
	doc := MustParse([]byte(src))
	found := false
	for _, c := range doc.Root.Children {
		if c.Type == NodeHeading && c.Attr("level") == "1" {
			if got, soft := inlineLines(c); strings.Join(got, "|") == "First line|Second line" && soft == 1 {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("two-line setext heading not read in a segmented document")
	}
}

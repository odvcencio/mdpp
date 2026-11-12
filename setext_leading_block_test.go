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

// An underline may be indented by up to three spaces and may end in white
// space; four spaces of indentation make it paragraph text (CommonMark 4.3).
func TestSetextUnderlineIndentationAndTrailingWhitespace(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		heading bool
	}{
		{"no indent", "***\n\nT\nU\n===\n", true},
		{"one space", "***\n\nT\nU\n ===\n", true},
		{"three spaces", "***\n\nT\nU\n   ===\n", true},
		{"four spaces", "***\n\nT\nU\n    ===\n", false},
		{"trailing spaces", "***\n\nT\nU\n===  \n", true},
		{"trailing tab", "***\n\nT\nU\n---\t\n", true},
		{"inner space", "***\n\nT\nU\n= =\n", false},
		{"text after", "***\n\nT\nU\n=== x\n", false},
	}
	for _, tc := range cases {
		doc := MustParse([]byte(tc.src))
		got := len(doc.Root.Children) == 2 && doc.Root.Children[1].Type == NodeHeading
		if got != tc.heading {
			t.Errorf("%s %q: heading = %v, want %v\n%s", tc.name, tc.src, got, tc.heading, DumpTreeForSnapshot(doc.Root))
		}
	}
}

// Ranges refer to Document.Source, in which CRLF is normalized to LF.
func TestSetextHeadingAfterLeadingBlockExactRanges(t *testing.T) {
	for _, src := range []string{"<!-- c -->\n\nT\nU\n=\n", "<!-- c -->\r\n\r\nT\r\nU\r\n=\r\n"} {
		doc := MustParse([]byte(src))
		kids := doc.Root.Children
		if len(kids) != 2 || kids[1].Type != NodeHeading {
			t.Fatalf("%q: unexpected blocks:\n%s", src, DumpTreeForSnapshot(doc.Root))
		}
		h := kids[1]
		if got := string(doc.Source[h.Range.StartByte:h.Range.EndByte]); got != "T\nU\n=\n" {
			t.Errorf("%q: heading covers %q", src, got)
		}
		if h.Range.StartLine != 3 || h.Range.StartCol != 1 || h.Range.EndLine != 6 || h.Range.EndCol != 1 {
			t.Errorf("%q: heading points %+v", src, h.Range)
		}
		want := []struct {
			typ         NodeType
			raw         string
			line, col   int
			eLine, eCol int
		}{{NodeText, "T", 3, 1, 3, 2}, {NodeSoftBreak, "\n", 3, 2, 4, 1}, {NodeText, "U", 4, 1, 4, 2}}
		if len(h.Children) != len(want) {
			t.Fatalf("%q: heading children:\n%s", src, DumpTreeForSnapshot(doc.Root))
		}
		for i, w := range want {
			c := h.Children[i]
			r := c.Range
			if c.Type != w.typ || string(doc.Source[r.StartByte:r.EndByte]) != w.raw ||
				r.StartLine != w.line || r.StartCol != w.col || r.EndLine != w.eLine || r.EndCol != w.eCol {
				t.Errorf("%q: child %d %v has range %+v (%q)", src, i, c.Type, r, doc.Source[r.StartByte:r.EndByte])
			}
		}
	}
}

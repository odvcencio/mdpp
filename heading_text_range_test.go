package mdpp

import (
	"strings"
	"testing"
)

// Every inline node of an ATX heading keeps a source range. Headings whose
// text ends in ".", "?" or "!" after a space, and headings with a raw-text
// HTML element, are repaired after the block parse; the repair must carry the
// text's source offset.
func TestATXHeadingTextKeepsSourceRange(t *testing.T) {
	cases := []struct {
		name   string
		source string
		text   string // the plain text the heading's Text nodes must cover
	}{
		{"question mark", "## What does this PR do?\n", "What does this PR do?"},
		{"full stop", "## Install the tool.\n", "Install the tool."},
		{"exclamation mark", "# Read this first!\n", "Read this first!"},
		{"closing hashes", "## What now? ##\n", "What now?"},
		{"indented marker", "   ### Why not?\n", "Why not?"},
		{"after a paragraph", "intro\n\n## What is it?\n\ntail\n", "What is it?"},
		{"second heading", "# One\n\n## Which one?\n", "Which one?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := MustParse([]byte(tc.source))
			var heading *Node
			doc.Root.Walk(func(n *Node) bool {
				if n.Type == NodeHeading {
					heading = n // keep the last heading
				}
				return true
			})
			if heading == nil {
				t.Fatalf("no heading in:\n%s", DumpTreeForSnapshot(doc.Root))
			}
			var got strings.Builder
			for _, c := range heading.Children {
				if c.Type != NodeText {
					t.Fatalf("unexpected inline node %v in:\n%s", c.Type, DumpTreeForSnapshot(doc.Root))
				}
				r := c.Range
				if r.EndByte <= r.StartByte {
					t.Fatalf("text %q has the empty range %d-%d", c.Literal, r.StartByte, r.EndByte)
				}
				if slice := string(doc.Source[r.StartByte:r.EndByte]); slice != c.Literal {
					t.Fatalf("text %q has range %d-%d covering %q", c.Literal, r.StartByte, r.EndByte, slice)
				}
				if line := strings.Count(string(doc.Source[:r.StartByte]), "\n") + 1; r.StartLine != line {
					t.Fatalf("text %q starts on line %d, range says %d", c.Literal, line, r.StartLine)
				}
				got.WriteString(c.Literal)
			}
			if got.String() != tc.text {
				t.Fatalf("heading text = %q, want %q", got.String(), tc.text)
			}
		})
	}
}

// pointAt returns the line and byte column (both 1-based) of offset in source.
func pointAt(source []byte, offset int) (line, col int) {
	line = 1 + strings.Count(string(source[:offset]), "\n")
	col = 1 + offset - (strings.LastIndex(string(source[:offset]), "\n") + 1)
	return line, col
}

// wantRange is the exact range of the source text raw, found after offset from.
func wantRange(t *testing.T, source []byte, raw string, from int) (Range, int) {
	t.Helper()
	at := strings.Index(string(source[from:]), raw)
	if at < 0 {
		t.Fatalf("%q not found in source %q after %d", raw, source, from)
	}
	start := from + at
	end := start + len(raw)
	startLine, startCol := pointAt(source, start)
	endLine, endCol := pointAt(source, end)
	return Range{StartByte: start, EndByte: end, StartLine: startLine, StartCol: startCol, EndLine: endLine, EndCol: endCol}, end
}

// The children of a repaired heading have exact ranges: byte offsets, lines
// and byte columns, in Document.Source (line ends normalized to LF).
func TestRepairedHeadingChildrenHaveExactRanges(t *testing.T) {
	type child struct {
		typ NodeType
		raw string // the source text the node covers
	}
	cases := []struct {
		name   string
		source string
		want   []child
	}{
		{"ascii", "## What now?\n", []child{{NodeText, "What now?"}}},
		{"multibyte", "é\n\n## 猫 café?\n", []child{{NodeText, "猫 café?"}}},
		{"crlf", "é\r\n\r\n  ## 猫 café? ##\r\n", []child{{NodeText, "猫 café?"}}},
		{"nested markup", "## **bold** and `code`?\n", []child{{NodeStrong, "**bold**"}, {NodeText, " and "}, {NodeCodeSpan, "`code`"}, {NodeText, "?"}}},
		{"entity", "# Fish &amp; chips?\n", []child{{NodeText, "Fish &amp; chips?"}}},
		{"entity line feed", "# &#10; ?\n", []child{{NodeText, "&#10; ?"}}},
		{"entity line feed before merged punctuation", "# &#10; !\n", []child{{NodeText, "&#10; !"}}},
		{"raw text html", "# <script>x</script> y\n", []child{{NodeHTMLInline, "<script>"}, {NodeText, "x"}, {NodeHTMLInline, "</script>"}, {NodeText, " y"}}},
		{"after a paragraph", "intro\n\n## What is it?\n", []child{{NodeText, "What is it?"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := MustParse([]byte(tc.source))
			var heading *Node
			for _, c := range doc.Root.Children {
				if c.Type == NodeHeading {
					heading = c
				}
			}
			if heading == nil || len(heading.Children) != len(tc.want) {
				t.Fatalf("heading children: want %d, tree:\n%s", len(tc.want), DumpTreeForSnapshot(doc.Root))
			}
			from := strings.Index(string(doc.Source), "#")
			for i, w := range tc.want {
				got := heading.Children[i]
				wr, end := wantRange(t, doc.Source, w.raw, from)
				from = end
				if got.Type != w.typ {
					t.Errorf("child %d: type %v, want %v", i, got.Type, w.typ)
				}
				if got.Range != wr {
					t.Errorf("child %d (%v %q): range %+v, want %+v", i, got.Type, got.Literal, got.Range, wr)
				}
			}
		})
	}
}

func TestEntityLineFeedInHeadingStaysInText(t *testing.T) {
	doc := MustParse([]byte("# &#10; ?\n"))
	heading := doc.Root.Children[0]
	if len(heading.Children) != 1 || heading.Children[0].Type != NodeText || heading.Children[0].Literal != "\n ?" {
		t.Fatalf("want one text node \"\\n ?\", got:\n%s", DumpTreeForSnapshot(doc.Root))
	}
}

// A decoded entity must not move the positions of the lines after it.
func TestEntityBeforeLineBreakKeepsLaterRanges(t *testing.T) {
	for _, src := range []string{"&amp;\nx\n===\n", "***\n\n&amp;\nx\n===\n", "&amp;!\nx\n===\n", "***\n\n&amp;!\nx\n===\n"} {
		doc := MustParse([]byte(src))
		found := false
		doc.Root.Walk(func(n *Node) bool {
			if n.Type == NodeText && n.Literal == "x" {
				found = true
				want, _ := wantRange(t, doc.Source, "x", 0)
				if n.Range != want {
					t.Errorf("%q: x has range %+v, want %+v", src, n.Range, want)
				}
			}
			return true
		})
		if !found {
			t.Errorf("%q: no text x in:\n%s", src, DumpTreeForSnapshot(doc.Root))
		}
	}
}

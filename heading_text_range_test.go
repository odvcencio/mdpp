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

func TestATXHeadingRawTextHTMLKeepsSourceRange(t *testing.T) {
	doc := MustParse([]byte("# <script>x</script> y\n"))
	heading := doc.Root.Children[0]
	if heading.Type != NodeHeading || len(heading.Children) == 0 {
		t.Fatalf("unexpected tree:\n%s", DumpTreeForSnapshot(doc.Root))
	}
	for _, c := range heading.Children {
		if c.Range.EndByte <= c.Range.StartByte {
			t.Fatalf("%v %q has the empty range %+v", c.Type, c.Literal, c.Range)
		}
	}
}

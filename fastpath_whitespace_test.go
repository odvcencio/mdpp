package mdpp

import (
	"strings"
	"testing"
)

// The fast block path may treat only Markdown's own whitespace (space and tab)
// as syntax. Any other Unicode whitespace or control character is text, and a
// chunk that holds one next to a syntax-bearing position goes to the full
// parser. These tests insert such characters at every position of a set of
// block forms and compare the fast and full readings.

// stray characters that Unicode counts as whitespace or that act as line
// separators, but that Markdown does not.
var strayWhitespace = []string{"\u00a0", "\u2003", "\u0085", "\u2028", "\u3000", "\f", "\v"}

// whitespaceBaseBlocks are blocks whose syntax depends on line edges, markers,
// pipes and fences. Each is preceded by a thematic break so that the rest of
// the document takes the fast path.
var whitespaceBaseBlocks = []string{
	"~~~\ncode\n~~~\nafter",
	"~~~\n~~~\n~~~\n~~~\n",
	"```\nx\n```\n",
	"|h|\n|-|\n|x|\n",
	"|a|b|\n|:-|-:|\n|1|2|\n",
	"# head\ntext\n",
	"## head ##\n",
	"- a\n- b\n",
	"1. a\n2. b\n",
	"- [x] a\n",
	"> quote\n> more\n",
	"a\nb\n",
	"a\n===\n",
	"***\n",
	"[^1]: note\n",
	"[a]: /u\n",
	"<div>\nx\n</div>\n",
}

func whitespaceVariantInputs() []string {
	var out []string
	for _, base := range whitespaceBaseBlocks {
		for _, stray := range strayWhitespace {
			for pos := 0; pos <= len(base); pos++ {
				if pos > 0 && pos < len(base) && base[pos]&0xC0 == 0x80 {
					continue
				}
				out = append(out, "***\n"+base[:pos]+stray+base[pos:])
			}
		}
	}
	return out
}

func TestFastPathTreatsOnlySpaceAndTabAsSyntax(t *testing.T) {
	inputs := whitespaceVariantInputs()
	bad := fastPathDifferences(inputs)
	for i, src := range bad {
		if i >= 10 {
			t.Errorf("%d inputs differ in total", len(bad))
			break
		}
		t.Errorf("fast and full readings differ for %q\nfast:\n%s\nfull:\n%s", src, parseWithFastPath(src, true), parseWithFastPath(src, false))
	}
	t.Logf("%d inputs compared", len(inputs))
}

// The two inputs of the review, with their exact expected trees.
func TestNonbreakingSpaceIsNotFenceOrCellSyntax(t *testing.T) {
	fence := "***\n~~~\n~~~\u00a0\n~~~\n~~~\n"
	doc, err := Parse([]byte(fence))
	if err != nil {
		t.Fatal(err)
	}
	// The nonbreaking space keeps line 3 from closing the fence: it is code
	// text. Line 4 closes the fence; line 5 opens a new, empty one.
	var codes []*Node
	for _, c := range doc.Root.Children {
		if c.Type == NodeCodeBlock {
			codes = append(codes, c)
		}
	}
	if len(codes) != 2 || codes[0].Literal != "~~~\u00a0\n" || codes[0].Range.StartByte != 4 || codes[0].Range.EndByte != 18 {
		t.Fatalf("fence closed at a line that ends in a nonbreaking space: %s", fullNodeDump(doc.Root))
	}

	table := "***\n|h|\n|-|\n|\u00a0x|\n"
	doc, err = Parse([]byte(table))
	if err != nil {
		t.Fatal(err)
	}
	var texts []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		if n.Type == NodeText {
			texts = append(texts, n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Root)
	found := false
	for _, n := range texts {
		if strings.Contains(n.Literal, "x") {
			found = true
			raw := string(doc.Source[n.Range.StartByte:n.Range.EndByte])
			if raw != n.Literal {
				t.Errorf("text %q has range %v covering %q", n.Literal, n.Range, raw)
			}
		}
	}
	if !found {
		t.Fatalf("no text node with x: %s", fullNodeDump(doc.Root))
	}
}

func TestFastPathAcceptsStrayWhitespaceOnlyInsideWords(t *testing.T) {
	cases := []struct {
		src    string
		accept bool
	}{
		{"a\u00a0b\n", true},
		{"a \u00a0b\n", true},
		{"\u00a0a\n", false},
		{"a\u00a0\n", false},
		{"a.\u00a0b\n", true},
		{"~~~\u00a0x\n", false},
		{"#\u00a0x\n", false},
		{">\u00a0x\n", false},
		{"`\u00a0b\u00a0`\n", true},
		{"*\u00a0a\u00a0*\n", true},
		{"[^1\u00a0]: x\n", false},
		{"[a]\u00a0b\n", true},
		{"|\u00a0x|\n", false},
		{"~~~\u00a0\n", false},
		{"a\fb\n", false},
		{"a\vb\n", false},
		{"a\x00b\n", false},
		{"plain text\n", true},
	}
	for _, c := range cases {
		if got := fastChunkBytesAccepted([]byte(c.src)); got != c.accept {
			t.Errorf("fastChunkBytesAccepted(%q) = %v, want %v", c.src, got, c.accept)
		}
	}
}

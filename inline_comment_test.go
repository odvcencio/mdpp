package mdpp

import "testing"

// inlineCommentParsers are the parsers that the comment test runs against.
// The GFM profile adds its own entry in its own test file.
var inlineCommentParsers = []struct {
	name string
	fn   func([]byte) (*Document, error)
}{
	{"default profile", Parse},
}

// Inline HTML comments in every form the specification allows (CommonMark 6.6;
// GFM example 645) give one HTMLInline node whose literal and range are the
// comment, in every profile and on both block paths.
func TestInlineHTMLCommentForms(t *testing.T) {
	cases := []struct {
		name, src string
		want      []string // literals of the HTMLInline nodes, in order
		start     []int    // their start bytes
	}{
		{"empty comment", "x <!-->\n", []string{"<!-->"}, []int{2}},
		{"dash comment", "x <!--->\n", []string{"<!--->"}, []int{2}},
		{"specification 645", "foo <!--> foo -->\n\nfoo <!---> foo -->\n", []string{"<!-->", "<!--->"}, []int{4, 23}},
		{"plain", "a <!-- c --> b\n", []string{"<!-- c -->"}, []int{2}},
		{"double hyphen in the text", "foo <!-- this is a --\ncomment - with hyphens -->\n", []string{"<!-- this is a --\ncomment - with hyphens -->"}, []int{4}},
		{"after emphasis", "*x* <!-- --\n-->\n", []string{"<!-- --\n-->"}, []int{4}},
		{"two comments", "x <!-- a -- b --> y <!-- c -->\n", []string{"<!-- a -- b -->", "<!-- c -->"}, []int{2, 20}},
		{"in a heading", "# h <!-->\n", []string{"<!-->"}, []int{4}},
		{"in a table cell", "|h|\n|-|\n|x <!--->|\n", []string{"<!--->"}, []int{11}},
		{"in a list item", "- a <!-->\n", []string{"<!-->"}, []int{4}},
		{"in link text", "[a <!--> b](/u)\n", []string{"<!-->"}, []int{3}},
		{"after a break (fast path)", "***\nx <!-->\n", []string{"<!-->"}, []int{6}},
	}
	for _, parse := range inlineCommentParsers {
		for _, tc := range cases {
			doc, err := parse.fn([]byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			nodes := doc.Root.Find(NodeHTMLInline)
			if len(nodes) != len(tc.want) {
				t.Errorf("%s (%s): %d HTMLInline nodes, want %d:\n%s", tc.name, parse.name, len(nodes), len(tc.want), fullNodeDump(doc.Root))
				continue
			}
			for i, n := range nodes {
				if n.Literal != tc.want[i] || n.Range.StartByte != tc.start[i] || n.Range.EndByte != tc.start[i]+len(tc.want[i]) ||
					string(doc.Source[n.Range.StartByte:n.Range.EndByte]) != tc.want[i] {
					t.Errorf("%s (%s): node %d is %q at %v, want %q at %d", tc.name, parse.name, i, n.Literal, n.Range, tc.want[i], tc.start[i])
				}
			}
		}
	}
}

// A comment that lies inside a code span, behind a backslash, or inside a raw
// tag is not a comment.
func TestInlineHTMLCommentHiddenPositions(t *testing.T) {
	for _, src := range []string{
		"`<!-->` and `<!-- c -->`\n",
		"\\<!--> x\n",
		"<a title=\"<!-- c -->\"> x\n",
		"&lt;!--> x\n",
		"<!-- unterminated\n",
	} {
		doc := MustParse([]byte(src))
		for _, n := range doc.Root.Find(NodeHTMLInline) {
			if n.Literal == "<!-->" || n.Literal == "<!-- c -->" || n.Literal == "<!-- unterminated" {
				t.Errorf("%q: %q read as a comment:\n%s", src, n.Literal, fullNodeDump(doc.Root))
			}
		}
	}
}

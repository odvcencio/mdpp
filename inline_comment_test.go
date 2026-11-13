package mdpp

import (
	"strings"
	"testing"
	"time"
)

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

// htmlInlineSpans returns the HTMLInline nodes of a document and fails the test
// when one holds text that is not the source at its range: a comment node must
// never show text that the source does not hold.
func htmlInlineSpans(t *testing.T, parse func([]byte) (*Document, error), src string) []*Node {
	t.Helper()
	doc, err := parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	nodes := doc.Root.Find(NodeHTMLInline)
	for _, n := range nodes {
		if got := string(doc.Source[n.Range.StartByte:n.Range.EndByte]); got != n.Literal {
			t.Errorf("%.60q: HTMLInline %q at %v, but the source there is %q", src, n.Literal, n.Range, got)
		}
	}
	return nodes
}

// The three inputs of the fifth review: a comment next to a tag that looks like
// a stand-in, a comment longer than one chunk, and more comments than the old
// stand-in namespace held.
func TestInlineCommentReviewInputs(t *testing.T) {
	for _, parse := range inlineCommentParsers {
		nodes := htmlInlineSpans(t, parse.fn, "x <!--><q00>\n")
		if len(nodes) != 2 || nodes[0].Literal != "<!-->" || nodes[1].Literal != "<q00>" ||
			nodes[0].Range.StartByte != 2 || nodes[1].Range.StartByte != 7 {
			t.Errorf("%s: <!--><q00>: got %d nodes", parse.name, len(nodes))
		}

		long := "<!--" + strings.Repeat("a", 312) + "-->"
		nodes = htmlInlineSpans(t, parse.fn, "x "+long+"\n")
		if len(nodes) != 1 || nodes[0].Literal != long || nodes[0].Range.StartByte != 2 || nodes[0].Range.EndByte != 2+len(long) {
			t.Errorf("%s: 321-byte comment: got %d nodes", parse.name, len(nodes))
		}
		// With spaces inside, the chunker would find boundaries in it.
		spaced := "<!-- " + strings.Repeat("word ", 90) + "-->"
		nodes = htmlInlineSpans(t, parse.fn, "start "+strings.Repeat("filler text. ", 30)+spaced+" end\n")
		if len(nodes) != 1 || nodes[0].Literal != spaced {
			t.Errorf("%s: long comment with spaces: got %d nodes", parse.name, len(nodes))
		}

		for _, n := range []int{1296, 1297, 1298} {
			nodes = htmlInlineSpans(t, parse.fn, "x "+strings.Repeat("<!--> ", n)+"\n")
			if len(nodes) != n {
				t.Errorf("%s: %d comments: got %d nodes", parse.name, n, len(nodes))
			}
		}
		// A line over 16 KiB with inline punctuation is not read at all (it gets
		// a diagnostic); 5,000 comments in one paragraph are spread over lines.
		nodes = htmlInlineSpans(t, parse.fn, "x\n"+strings.Repeat("y "+strings.Repeat("<!--> ", 100)+"\n", 50))
		if len(nodes) != 5000 {
			t.Errorf("%s: 5000 comments: got %d nodes", parse.name, len(nodes))
		}
	}
}

// inlineCommentPrecedenceCases lists inputs and the comments in them.
var inlineCommentPrecedenceCases = []struct {
	name, src string
	want      []string
}{
	{"code span", "`<!-- c -->` x\n", nil},
	{"code span, double backticks", "``<!--> ` <!--->`` x\n", nil},
	{"comment after a code span", "`a` <!-->\n", []string{"<!-->"}},
	{"unclosed backtick does not hide", "` <!-->\n", []string{"<!-->"}},
	{"backslash before the opener", "\\<!--> x\n", nil},
	{"backslash does not escape in a code span", "`\\` <!--> x\n", []string{"<!-->"}},
	{"link destination in angle brackets", "[a](<!-- c -->) x\n", nil},
	{"link destination", "[a](/u<!--) x -->\n", nil},
	{"link title", "[a](/u \"<!-- c -->\") x\n", nil},
	{"link title with single quotes", "[a](/u '<!-->') x\n", nil},
	{"image destination", "![a](<!--> \"t\") x\n", nil},
	{"link text", "[a <!--> b](/u)\n", []string{"<!-->"}},
	{"after a link", "[a](/u) <!--> x\n", []string{"<!-->"}},
	{"no link: brackets without parenthesis", "[a] <!--> (b)\n", []string{"<!-->"}},
	{"no link: unclosed destination", "[a](<!-->\n", []string{"<!-->"}},
	{"attribute value", "<a title=\"<!-- c -->\"> x\n", []string{"<a title=\"<!-- c -->\">"}},
	{"attribute value in single quotes", "<a title='<!-->'> x\n", []string{"<a title='<!-->'>"}},
	{"processing instruction", "<?php <!--> ?> x\n", []string{"<?php <!--> ?>"}},
	{"declaration", "<!DOCTYPE <!--> x\n", []string{"<!DOCTYPE <!-->"}},
	{"cdata", "<![CDATA[ <!--> ]]> x\n", []string{"<![CDATA[ <!--> ]]>"}},
	{"comment hides what is inside it", "x <!-- `a --> b` <!--> c\n", []string{"<!-- `a -->", "<!-->"}},
	{"unterminated", "<!-- unterminated\n", nil},
	{"entity", "&lt;!--> x\n", nil},
}

// A comment opener is not a comment inside a code span, a link destination or
// title, an HTML attribute value, a processing instruction, a declaration, a
// CDATA section, or behind a backslash. A comment in link text is a comment.
func TestInlineCommentPrecedence(t *testing.T) {
	cases := inlineCommentPrecedenceCases

	// Through the parser, in every profile: the comment nodes are the comments.
	for _, parse := range inlineCommentParsers {
		for _, tc := range cases {
			var got []string
			for _, n := range htmlInlineSpans(t, parse.fn, tc.src) {
				if strings.HasPrefix(n.Literal, "<!--") {
					got = append(got, n.Literal)
				}
			}
			var want []string
			for _, w := range tc.want {
				if strings.HasPrefix(w, "<!--") {
					want = append(want, w)
				}
			}
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("%s (%s): comment nodes %q, want %q", tc.name, parse.name, got, want)
			}
		}
	}
}

// The scanner is linear: twice the comments cost about twice the work. The
// check counts allocations, which do not depend on the machine; the measured
// time is only logged.
func TestInlineCommentWorkIsLinear(t *testing.T) {
	build := func(n int) []byte {
		return []byte("x\n" + strings.Repeat("y "+strings.Repeat("<!--> a [b](c \"d\") `e` ", 20)+"\n", n/20))
	}
	small, large := build(5000), build(10000)
	var smallTime, largeTime time.Duration
	smallAllocs := testing.AllocsPerRun(1, func() {
		start := time.Now()
		MustParse(small)
		smallTime = time.Since(start)
	})
	largeAllocs := testing.AllocsPerRun(1, func() {
		start := time.Now()
		MustParse(large)
		largeTime = time.Since(start)
	})
	t.Logf("5000 comments: %d allocations, %v; 10000 comments: %d allocations, %v (directional)", int(smallAllocs), smallTime, int(largeAllocs), largeTime)
	if ratio := largeAllocs / smallAllocs; ratio > 2.5 {
		t.Errorf("allocations grew %.2fx for twice the comments", ratio)
	}
}

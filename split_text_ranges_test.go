package mdpp

import (
	"strconv"
	"strings"
	"testing"
)

// Splitting a text node in one extension pass must keep the raw source text of
// each fragment, so that the next pass can map its matches to the source. Here
// the math pass splits "$x$&amp;^y^" and the superscript pass then works on the
// fragment "&^y^", whose source is "&amp;^y^".
func TestSplitTextKeepsRawSourceForLaterPasses(t *testing.T) {
	cases := []struct {
		src    string
		kind   NodeType
		source string
		start  int
	}{
		{"$x$&amp;^y^\n", NodeSuperscript, "^y^", 8},
		{"$x$&amp;^y^\n", NodeMathInline, "$x$", 0},
		{"&amp;$x$&amp;^y^\n", NodeSuperscript, "^y^", 13},
		{"&lt;^a^ $b$ &gt;^c^\n", NodeSuperscript, "^a^", 4},
		{"&lt;^a^ $b$ &gt;^c^\n", NodeSuperscript, "^c^", 16},
		{"\\&amp;$x$^y^\n", NodeSuperscript, "^y^", 9},
	}
	for _, tc := range cases {
		doc := MustParse([]byte(tc.src))
		found := false
		for _, n := range doc.Root.Find(tc.kind) {
			if n.Range.StartByte != tc.start {
				continue
			}
			found = true
			if !n.HasSourceRange() {
				t.Errorf("%q: %v at %d has no source range:\n%s", tc.src, tc.kind, tc.start, fullNodeDump(doc.Root))
				continue
			}
			if got := string(doc.Source[n.Range.StartByte:n.Range.EndByte]); got != tc.source {
				t.Errorf("%q: %v covers %q, want %q", tc.src, tc.kind, got, tc.source)
			}
		}
		if !found {
			t.Errorf("%q: no %v starting at %d:\n%s", tc.src, tc.kind, tc.start, fullNodeDump(doc.Root))
		}
	}
}

// A literal that holds "&amp;" because the source escaped the ampersand
// ("\\&amp;") must not be decoded a second time when its text node is split.
func TestSplitTextDoesNotDecodeTwice(t *testing.T) {
	doc := MustParse([]byte("$x$ \\&amp; ^y^\n"))
	var texts []string
	walkNodes(doc.Root, func(n *Node, parent *Node, index int) bool {
		if n.Type == NodeText {
			texts = append(texts, n.Literal)
		}
		return true
	})
	if got := strings.Join(texts, ""); got != " &amp; " {
		t.Errorf("text after the math is %q, want %q (the escaped ampersand decoded once)\n%s", got, " &amp; ", fullNodeDump(doc.Root))
	}
}

// The work of mapping spans to the source must grow with the text, not with the
// product of the text and the number of matches ("&amp;" makes the literal differ
// from the source text, which needs the raw-offset table).
func TestSplitTextWorkIsLinear(t *testing.T) {
	measure := func(n int) (bytes, builds int64, srcLen int) {
		src := "&amp;" + strings.Repeat("$x$", n) + "\n"
		textSplitWork.bytes.Store(0)
		textSplitWork.builds.Store(0)
		doc := MustParse([]byte(src))
		maths := doc.Root.Find(NodeMathInline)
		if len(maths) != n {
			t.Fatalf("n=%d: %d math nodes", n, len(maths))
		}
		last := maths[len(maths)-1]
		if !last.HasSourceRange() || last.Range.StartByte != 5+3*(n-1) {
			t.Fatalf("n=%d: last math node has range %v", n, last.Range)
		}
		return textSplitWork.bytes.Load(), textSplitWork.builds.Load(), len(src)
	}
	b1, t1, len1 := measure(1000)
	b2, t2, len2 := measure(10000)
	t.Logf("n=1000: %d bytes scanned, %d offset tables, source %d bytes", b1, t1, len1)
	t.Logf("n=10000: %d bytes scanned, %d offset tables, source %d bytes", b2, t2, len2)
	for _, c := range []struct {
		n             int
		bytes, builds int64
		srcLen        int
	}{{1000, b1, t1, len1}, {10000, b2, t2, len2}} {
		if c.builds > 2 {
			t.Errorf("n=%d: %d offset tables built, want at most 2 (one per text node)", c.n, c.builds)
		}
		if c.bytes > 3*int64(c.srcLen) {
			t.Errorf("n=%d: scanned %d bytes for a %d-byte source, want at most 3x", c.n, c.bytes, c.srcLen)
		}
	}
}

func BenchmarkSplitTextManyMatches(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		src := []byte("&amp;" + strings.Repeat("$x$", n) + "\n")
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			for i := 0; i < b.N; i++ {
				MustParse(src)
			}
		})
	}
}

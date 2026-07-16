package mdpp

import (
	"strings"
	"testing"
)

// TestBlockquoteContinuationMarkers proves multi-line blockquotes never leak
// their continuation `> ` markers into text nodes. The grammar's
// block_quote_marker/block_continuation tokens cover only the first line; the
// continuation prefixes sit inside the paragraph's inline byte range and used
// to surface as literal "> " runs after soft breaks (caught rendering a
// gosx-slides deck: "and > **grammargen**"). Both parse paths are covered:
// the bare-blockquote fast path and (via the leading heading) the
// tree-sitter slow path where the leak lived.
func TestBlockquoteContinuationMarkers(t *testing.T) {
	cases := []struct{ name, src string }{
		{"fast path plain", "> line one\n> line two continues\n"},
		{"slow path plain", "# H\n\n> line one\n> line two continues\n"},
		{"slow path inline markup", "# H\n\n> alpha **bold** and\n> **beta** gamma from\n> delta epsilon.\n"},
		{"slow path nested quote intact", "# H\n\n> outer line\n> > nested quote\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			var text strings.Builder
			doc.Root.Walk(func(n *Node) bool {
				if n.Type == NodeText {
					text.WriteString(n.Literal)
					text.WriteString("|")
				}
				return true
			})
			if got := text.String(); strings.Contains(got, ">") {
				t.Fatalf("continuation '>' marker leaked into text nodes: %q", got)
			}
		})
	}
}

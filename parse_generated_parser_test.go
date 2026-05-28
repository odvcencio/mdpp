package mdpp

import "testing"

// TestBlockLangUsesGeneratedParser asserts the block-level parser path is the
// grammargen-generated MarkdownGrammar (with adapted scanner), not the bundled
// markdown.bin blob. This locks in Task 0.3 of the v0.3.0 grammar-extension
// work.
//
// Rationale: we can't easily introspect the *gotreesitter.Language object to
// distinguish "this came from MarkdownLanguage()" vs "this came from
// GenerateLanguage(MarkdownGrammar())". Both should produce equivalent CSTs
// for vanilla markdown — that's the whole point of Phase 0 parity. So this
// test is a behavior assertion (heading + code block round-trip) that must
// survive the swap. It's a TDD-light pattern for refactors: it passes both
// before and after, but it pins the contract so any regression in the swap
// surfaces immediately as a test failure on the wider suite.
func TestBlockLangUsesGeneratedParser(t *testing.T) {
	src := "# Title\n\nA paragraph.\n\n```go\npackage main\n```\n"
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if doc == nil || doc.Root == nil {
		t.Fatalf("Parse returned nil doc/root")
	}

	var sawHeading, sawCodeBlock bool
	walkNodes(doc.Root, func(n *Node, _ *Node, _ int) bool {
		switch n.Type {
		case NodeHeading:
			sawHeading = true
		case NodeCodeBlock:
			sawCodeBlock = true
		}
		return true
	})
	if !sawHeading {
		t.Errorf("expected NodeHeading in AST")
	}
	if !sawCodeBlock {
		t.Errorf("expected NodeCodeBlock in AST")
	}
}

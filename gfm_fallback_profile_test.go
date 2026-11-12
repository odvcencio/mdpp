package mdpp

import (
	"context"
	"strings"
	"testing"
)

// The complexity fallback and the block-recovery parse read part of a document
// again. They must read it in the profile of the document, and spend its
// budget.

func TestLongLineFallbackKeepsTheGFMProfile(t *testing.T) {
	long := "*" + strings.Repeat("a", 16384) + "\n"
	cases := []struct {
		name, src string
		check     func(t *testing.T, doc *Document, gfm bool)
	}{
		{"front matter is not read", "---\nt: x\n# h\n---\n\n" + long, func(t *testing.T, doc *Document, gfm bool) {
			if gfm {
				if got := countNodes(doc.Root, NodeFrontmatter); got != 0 {
					t.Errorf("GFM tree has %d front matter nodes", got)
				}
				if got := countNodes(doc.Root, NodeHeading); got != 1 {
					t.Errorf("GFM tree has %d headings, want the one in `# h`:\n%s", got, fullNodeDump(doc.Root))
				}
			} else if got := countNodes(doc.Root, NodeFrontmatter); got != 1 {
				t.Errorf("default tree has %d front matter nodes, want 1", got)
			}
		}},
		{"math is not read", "$x$\n\n" + long, func(t *testing.T, doc *Document, gfm bool) {
			got := countNodes(doc.Root, NodeMathInline)
			if gfm && got != 0 {
				t.Errorf("GFM tree has %d MathInline nodes", got)
			}
			if !gfm && got != 1 {
				t.Errorf("default tree has %d MathInline nodes, want 1", got)
			}
		}},
	}
	for _, tc := range cases {
		for _, profile := range []ParseProfile{ProfileMarkdownPlus, ProfileGFM} {
			doc, err := ParseWithOptions([]byte(tc.src), ParseOptions{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, doc, profile == ProfileGFM)
			found := false
			for _, d := range doc.Diagnostics() {
				found = found || d.Code == "MDPP-PARSE-004"
			}
			if !found {
				t.Errorf("%s (profile %v): no complexity diagnostic", tc.name, profile)
			}
			if doc.Profile() != profile {
				t.Errorf("%s: profile %v, want %v", tc.name, doc.Profile(), profile)
			}
		}
	}
}

// The text between two blocks that the tree leaves unconverted is read again as
// blocks of its own. The input holds such text ("- $x$", a break, "]") before a
// link label that the grammar cuts in two.
func TestBlockRecoveryKeepsTheGFMProfile(t *testing.T) {
	src := "- $x$\n***\n]\n# h\n[a \\[b](/u)\n\n"
	doc, err := ParseWithOptions([]byte(src), ParseOptions{Profile: ProfileGFM})
	if err != nil {
		t.Fatal(err)
	}
	if got := countNodes(doc.Root, NodeMathInline); got != 0 {
		t.Errorf("GFM tree has %d MathInline nodes:\n%s", got, fullNodeDump(doc.Root))
	}
	if got := countNodes(doc.Root, NodeHeading); got != 1 {
		t.Errorf("GFM tree has %d headings, want 1", got)
	}
}

// A recovery parse spends the budget of the document: when it is spent, the
// recovery does not run again with a fresh one.
func TestBlockRecoverySharesTheBudget(t *testing.T) {
	text := "- a\n***\n]\n"
	source := []byte(text)
	ctx := &parseCtx{budget: newParseBudget(context.Background(), len(source), ParseOptions{})}
	var nodes []*Node
	if !appendRecoveredBlockText(&nodes, text, source, 0, ctx) {
		t.Fatalf("recovery did not run with budget left")
	}
	ctx = &parseCtx{budget: newParseBudget(context.Background(), len(source), ParseOptions{})}
	ctx.budget.hit = "parser work budget"
	nodes = nil
	if appendRecoveredBlockText(&nodes, text, source, 0, ctx) {
		t.Errorf("recovery ran after the budget was spent")
	}
}

package fmt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/mdpp"
)

// Parent paragraph ranges use both inclusive and exclusive EndLine shapes in
// real frontmatter-heavy documents. The formatter derives the physical span
// from text and soft-break children so the final continuation is neither left
// behind nor appended again by every subsequent pass.
func TestFormatFrontmatterWrappedParagraphGolden(t *testing.T) {
	input := readFormatFixture(t, "frontmatter-wrap.input.md")
	want := readFormatFixture(t, "frontmatter-wrap.golden.md")

	got := input
	for pass := 1; pass <= 3; pass++ {
		var err error
		got, err = Format(got)
		if err != nil {
			t.Fatalf("Format pass %d: %v", pass, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("Format pass %d did not match golden output\nwant:\n%s\ngot:\n%s", pass, want, got)
		}
	}

	fragment := []byte("Normal stdout mode must emit a complete document on every successful pass.")
	if count := bytes.Count(got, fragment); count != 1 {
		t.Fatalf("final wrapped fragment appears %d times, want 1\nout:\n%s", count, got)
	}
}

func TestFormatUnwrapsListContinuationOnce(t *testing.T) {
	input := []byte("- A list item wraps onto\n  a continuation line.\n- The next item stays separate.\n")
	want := []byte("- A list item wraps onto a continuation line.\n- The next item stays separate.\n")

	once, err := Format(input)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if !bytes.Equal(once, want) {
		t.Fatalf("Format list continuation\nwant:\n%s\ngot:\n%s", want, once)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatalf("Format twice: %v", err)
	}
	if !bytes.Equal(twice, want) {
		t.Fatalf("Format list continuation is not idempotent\nwant:\n%s\ngot:\n%s", want, twice)
	}
}

func TestUnwrapSimpleParagraphDoesNotTrustParentEndLine(t *testing.T) {
	paragraph := &mdpp.Node{
		Type:  mdpp.NodeParagraph,
		Range: mdpp.Range{StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 1},
		Children: []*mdpp.Node{
			{Type: mdpp.NodeText, Literal: "first line"},
			{Type: mdpp.NodeSoftBreak},
			{Type: mdpp.NodeText, Literal: "second line"},
		},
	}
	root := &mdpp.Node{Type: mdpp.NodeDocument, Children: []*mdpp.Node{paragraph}}
	lines := []formattedLine{
		{text: "first line", sourceLine: 1},
		{text: "second line", sourceLine: 2},
	}

	got := unwrapSimpleParagraphs(root, lines, []string{"first line", "second line"})
	if len(got) != 1 || got[0].text != "first line second line" || got[0].sourceLine != 1 {
		t.Fatalf("unwrap with unreliable parent EndLine = %#v", got)
	}
}

// Blank-line-terminated paragraphs (exclusive EndLine) must still unwrap
// without eating the following line.
func TestFormatUnwrapBlankTerminatedParagraph(t *testing.T) {
	src := []byte(`---
mdpp: "0.1"
id: concept.repro
type: concept
---

A paragraph that wraps to
a second line.

Next paragraph stays.
`)
	once, err := Format(src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if !bytes.Contains(once, []byte("Next paragraph stays.")) {
		t.Errorf("following paragraph was eaten:\n%s", once)
	}
	if n := bytes.Count(once, []byte("a second line.")); n != 1 {
		t.Errorf("wrapped fragment count = %d, want 1:\n%s", n, once)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatalf("Format twice: %v", err)
	}
	if !bytes.Equal(once, twice) {
		t.Errorf("Format not idempotent.\nonce:\n%s\ntwice:\n%s", once, twice)
	}
}

func readFormatFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "testdata", "format", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

package mdpp

import (
	"strings"
	"testing"
)

// parseDump parses src and returns the snapshot dump of the resulting AST.
func parseDump(t *testing.T, src string) string {
	t.Helper()
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return DumpTreeForSnapshot(doc.AST())
}

// parseSlidesDump parses src, splits it into a deck, and dumps the AST.
func parseSlidesDump(t *testing.T, src string) string {
	t.Helper()
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	SplitSlides(doc)
	return DumpTreeForSnapshot(doc.AST())
}

// --- Step 11/12: slide split ---

// A top-level thematic break (`---` surrounded by blank lines) splits the
// document into NodeSlide containers. Note: a bare `a\n---\nb` is a setext
// heading per CommonMark, NOT a thematic break, so we use the blank-line
// form which parses to NodeThematicBreak.
func TestSlideSplitThematicBreak(t *testing.T) {
	got := parseSlidesDump(t, "a\n\n---\n\nb\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Slide)`,
		`    (Paragraph)`,
		`      (Text "a")`,
		`  (Slide)`,
		`    (Paragraph)`,
		`      (Text "b")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("slide split drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// A document with no separators yields a single slide wrapping all content.
func TestSlideSingleWhenNoSeparator(t *testing.T) {
	got := parseSlidesDump(t, "# Title\n\nbody\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Slide)`,
		`    (Heading level="1")`,
		`      (Text "Title")`,
		`    (Paragraph)`,
		`      (Text "body")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("single-slide drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// A `---` inside a fenced code block must NOT split slides: the fence is its
// own top-level node, so top-level-only splitting leaves it untouched.
func TestSlideSeparatorInsideFenceDoesNotSplit(t *testing.T) {
	got := parseSlidesDump(t, "```\na\n---\nb\n```\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Slide)`,
		"    (CodeBlock \"a\\n---\\nb\\n\")",
		``,
	}, "\n")
	if got != want {
		t.Errorf("fence separator split incorrectly\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// Document frontmatter must stay a sibling at the document level (or inside
// the first slide as the deck-level frontmatter), and must NOT be swallowed
// such that it disappears. Here we assert it remains the first child of the
// document, ahead of any slide.
func TestSlideKeepsDocumentFrontmatter(t *testing.T) {
	got := parseSlidesDump(t, "---\ntitle: Deck\n---\n\nfirst\n\n---\n\nsecond\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Frontmatter "title: Deck")`,
		`  (Slide)`,
		`    (Paragraph)`,
		`      (Text "first")`,
		`  (Slide)`,
		`    (Paragraph)`,
		`      (Text "second")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("document frontmatter handling drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// Per-slide frontmatter: a YAML block immediately after a separator becomes
// that slide's frontmatter, stored in Attrs["frontmatter"], not a child.
func TestSlidePerSlideFrontmatter(t *testing.T) {
	got := parseSlidesDump(t, "first\n\n---\n\n```yaml\nlayout: center\n```\n\nsecond\n")
	// The yaml fence immediately after the separator is lifted onto the slide
	// as frontmatter; the remaining content stays as children.
	want := strings.Join([]string{
		`(Document)`,
		`  (Slide)`,
		`    (Paragraph)`,
		`      (Text "first")`,
		`  (Slide frontmatter="layout: center\n")`,
		`    (Paragraph)`,
		`      (Text "second")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("per-slide frontmatter drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

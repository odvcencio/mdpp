package fmt

import (
	"bytes"
	"testing"
)

// Paragraph shapes whose Range.EndLine semantics differ (lazy continuations
// and tightly-packed list paragraphs report inclusive ends; blank-line
// terminated paragraphs exclusive ones). The unwrap pass must not leave the
// final physical line behind. Regression for the graft-corruption bug where
// every format pass appended another copy of the last wrapped fragment.
func TestFormatUnwrapLazyContinuationIdempotent(t *testing.T) {
	src := []byte(`---
mdpp: "0.1"
id: concept.repro
type: concept
---

## Milestones
  [[other-doc]].
## Risks
- **First bullet**: wraps once and is
  terminated by the next bullet.
- **Lazy bullet** has a continuation that
drops its indent and then another line
  that keeps it, ending the paragraph cleanly.
- **Last bullet**: wraps across one,
  two,
  three continuation lines.
`)
	once, err := Format(src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatalf("Format twice: %v", err)
	}
	if !bytes.Equal(once, twice) {
		t.Errorf("Format not idempotent.\nonce:\n%s\ntwice:\n%s", once, twice)
	}
	for _, fragment := range []string{
		"ending the paragraph cleanly.",
		"terminated by the next bullet.",
		"three continuation lines.",
	} {
		if n := bytes.Count(twice, []byte(fragment)); n != 1 {
			t.Errorf("fragment %q duplicated: %d occurrences\nout:\n%s", fragment, n, twice)
		}
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

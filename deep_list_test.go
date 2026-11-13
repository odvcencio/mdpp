package mdpp

import "testing"

// deepListParsers are the parsers that the deep-list test runs against. The GFM
// profile adds its own entry in its own test file.
var deepListParsers = []struct {
	name string
	fn   func([]byte) (*Document, error)
	// buildsPlain: the profile has the deep-list builder (a shortcut), so a list
	// of plain items is built. The GFM profile has no shortcut: the grammar
	// cannot read the list, and the whole list is reported.
	buildsPlain bool
}{
	{"default profile", Parse, true},
	{"gfm profile", func(src []byte) (*Document, error) { return ParseWithOptions(src, ParseOptions{Profile: ProfileGFM}) }, false},
}

const deepListTail = "\n  - a\n    - a\n      - a\n        - a\n          - a\n"

// The deep-list builder (a list nested more than four levels, which the
// grammar cannot read) reads item text as inline text. An item that holds a
// block must not be built that way: the list is left to the grammar path, which
// reports it as unrecognized over its whole range. The review's three inputs:
// a task item, a heading and a block comment.
func TestDeepListWithBlocksIsReportedNotMisread(t *testing.T) {
	for _, parse := range deepListParsers {
		firsts := []string{"- [x] a", "- [ ] a", "- # h", "- <!-- c -->"}
		if parse.buildsPlain {
			// The shortcut's own guard: every first item that could be a block.
			firsts = append(firsts, "- > q", "- ```", "- ---", "- - a", "- 1. a", "- [a]: /u", "- | a |", "- :::", "- $$")
		}
		for _, first := range firsts {
			src := first + deepListTail
			doc, err := parse.fn([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			covered := false
			for _, d := range doc.Diagnostics() {
				if d.Range.StartByte == 0 && d.Range.EndByte >= len(src)-1 {
					covered = true
				}
			}
			if !covered {
				t.Errorf("%s: %q: no diagnostic covers the list:\n%s", parse.name, src, fullNodeDump(doc.Root))
			}
			if len(doc.Root.Find(NodeListItem)) > 0 && len(doc.Diagnostics()) == 0 {
				t.Errorf("%s: %q: list items without a diagnostic:\n%s", parse.name, src, fullNodeDump(doc.Root))
			}
		}
	}
}

// A deep list of plain items is still built, without a diagnostic, including
// items that only look like block syntax at a glance.
func TestDeepListOfPlainItemsIsStillBuilt(t *testing.T) {
	for _, parse := range deepListParsers {
		for _, first := range []string{"- a", "- *a* b", "- **a**", "- +1 vote", "- 2024 a", "- 1.5 a", "- a <!-- c --> b", "- a # b", "- -a"} {
			src := first + deepListTail
			doc, err := parse.fn([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			if !parse.buildsPlain {
				covered := false
				for _, d := range doc.Diagnostics() {
					if d.Range.StartByte == 0 && d.Range.EndByte >= len(src)-1 {
						covered = true
					}
				}
				if !covered {
					t.Errorf("%s: %q: no diagnostic covers the list:\n%s", parse.name, src, fullNodeDump(doc.Root))
				}
				continue
			}
			if len(doc.Diagnostics()) != 0 || len(doc.Root.Find(NodeListItem)) != 6 {
				t.Errorf("%s: %q: %d items, diagnostics %v:\n%s", parse.name, src, len(doc.Root.Find(NodeListItem)), doc.Diagnostics(), fullNodeDump(doc.Root))
			}
		}
	}
}

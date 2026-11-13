package mdpp

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// rangeExempt reports whether a node has no source text of its own: a table
// of contents is built from the headings, and its list, items and links point
// at no bytes. Such a node has the zero Range (HasSourceRange is false).
func rangeExempt(n *Node, ancestors []*Node) bool {
	// A padding cell that a merged table adds to a short row claims no source.
	if n.Type == NodeTableCell && n.Range == (Range{}) && len(n.Children) == 0 {
		return true
	}
	for _, a := range ancestors {
		if a.Type == NodeTableOfContents {
			return true
		}
	}
	return false
}

// checkNodeRanges returns every node whose range is empty or lies outside
// its parent's range.
func checkNodeRanges(doc *Document) []string {
	var problems []string
	var visit func(n *Node, parent *Node, ancestors []*Node)
	visit = func(n *Node, parent *Node, ancestors []*Node) {
		if !rangeExempt(n, ancestors) && n.Type != NodeDocument {
			r := n.Range
			switch {
			case r.StartLine == 0 || (r.EndByte <= r.StartByte && !(n.Type == NodeText && n.Literal == "" && r.EndByte == r.StartByte)):
				problems = append(problems, fmt.Sprintf("%v %q has the empty range %+v", n.Type, n.Literal, r))
			case r.StartByte < 0 || r.StartByte > r.EndByte:
				problems = append(problems, fmt.Sprintf("%v %q range %d-%d is reversed", n.Type, n.Literal, r.StartByte, r.EndByte))
			case r.EndByte > len(doc.Source):
				problems = append(problems, fmt.Sprintf("%v %q ends at %d, past the source (%d bytes)", n.Type, n.Literal, r.EndByte, len(doc.Source)))
			case !pointMatches(doc.Source, r.StartByte, r.StartLine, r.StartCol):
				problems = append(problems, fmt.Sprintf("%v %q starts at byte %d but says line %d column %d", n.Type, n.Literal, r.StartByte, r.StartLine, r.StartCol))
			case !pointMatches(doc.Source, r.EndByte, r.EndLine, r.EndCol):
				problems = append(problems, fmt.Sprintf("%v %q ends at byte %d but says line %d column %d", n.Type, n.Literal, r.EndByte, r.EndLine, r.EndCol))
			case parent != nil && parent.Type != NodeDocument && parent.Range.StartLine != 0 &&
				(r.StartByte < parent.Range.StartByte || r.EndByte > parent.Range.EndByte):
				problems = append(problems, fmt.Sprintf("%v %q range %d-%d lies outside its parent %v %d-%d", n.Type, n.Literal, r.StartByte, r.EndByte, parent.Type, parent.Range.StartByte, parent.Range.EndByte))
			}
		}
		for _, c := range n.Children {
			visit(c, n, append(ancestors, n))
		}
	}
	visit(doc.Root, nil, nil)
	return problems
}

// pointMatches reports whether the 1-indexed line and byte column are those of
// the byte offset in source.
func pointMatches(source []byte, offset, line, col int) bool {
	if offset < 0 || offset > len(source) {
		return false
	}
	wantLine := 1 + strings.Count(string(source[:offset]), "\n")
	wantCol := offset - (strings.LastIndex(string(source[:offset]), "\n") + 1) + 1
	return line == wantLine && col == wantCol
}

// cachedDefaultRanges contains only assertion results, not parser trees. It is
// populated in the ordinary single-count full suite when the earlier range
// test parses the same sources, and is checked in source corpus order below.
var cachedDefaultRanges map[string][]string

const sourceRangeChecked = "\x00source-range-checked"
const fastComparisonChecked = "\x00fast-comparison-checked"
const sharedFastEnv = "MDPP_INTERNAL_SHARED_FAST"

func sharedDefaultRangeEnabled() bool {
	return (flag.Lookup("test.run").Value.String() == "" && flag.Lookup("test.count").Value.String() == "1") || os.Getenv("MDPP_INTERNAL_COMBINED_RANGES") == "1"
}

// Full-suite corpus jobs can run together: both readers consume the published
// caches only after the range producer completes. Selective runs use their
// independent paths unless explicitly exercising this scheduling in a test.
var sharedRangeStarted = make(chan struct{})
var sharedRangeDone = make(chan struct{})

func parallelCorpusSuiteEnabled() bool {
	if flag.Lookup("test.count").Value.String() != "1" {
		return false
	}
	if os.Getenv("MDPP_INTERNAL_PARALLEL_SHARE") == "1" {
		return true
	}
	skip := flag.Lookup("test.skip")
	return flag.Lookup("test.run").Value.String() == "" && (skip == nil || skip.Value.String() == "")
}

// Only wait if the producer is already scheduled: this cannot consume the
// last parallel test slot while the producer is still waiting for one.
func awaitStartedRangeProducer() bool {
	select {
	case <-sharedRangeStarted:
		<-sharedRangeDone
		return true
	default:
		return false
	}
}

func TestNodeRangesPointIntoSource(t *testing.T) {
	parallel := parallelCorpusSuiteEnabled()
	if parallel {
		t.Parallel()
		close(sharedRangeStarted)
		defer close(sharedRangeDone)
	}
	inputs := append(specAndConformanceInputs(t), differentialInputs()...)
	originalCount := len(inputs)
	combine := sharedDefaultRangeEnabled() || parallel
	shareFast := os.Getenv(sharedFastEnv) == "1" || (flag.Lookup("test.run").Value.String() == "" && flag.Lookup("test.count").Value.String() == "1")
	if parallel {
		shareFast = true
	}
	var first map[string]int
	if combine {
		first = make(map[string]int, len(inputs))
		for i, s := range inputs {
			if _, ok := first[s]; !ok {
				first[s] = i
			}
		}
		for _, s := range rangeInputs(t) {
			if _, ok := first[s]; !ok {
				first[s] = len(inputs)
				inputs = append(inputs, s)
			}
		}
	}
	var shardEnv []string
	if combine {
		shardEnv = append(shardEnv, "MDPP_INTERNAL_COMBINED_RANGES=1")
	}
	if shareFast {
		shardEnv = append(shardEnv, sharedFastEnv+"=1")
	}
	results, child := corpusShardMap(t, inputs, func(i int, src string) ([]string, error) {
		doc, err := Parse([]byte(src))
		if err != nil {
			return nil, err
		}
		var point string
		if i < originalCount {
			if problems := checkNodeRanges(doc); len(problems) > 0 {
				point = problems[0]
			}
		}
		out := []string{point}
		if combine && first[src] == i {
			if shareFast && i < originalCount {
				same := sameFullPathAsDocument(src, doc, os.Getenv("MDPP_FASTPATH_NORANGES") != "")
				verdict := "different"
				if same {
					verdict = "same"
				}
				out = append(out, fastComparisonChecked, verdict)
			}
			problems := checkRangeConsistency(doc, src)
			out = append(out, sourceRangeChecked)
			out = append(out, problems...)
		}
		return out, nil
	}, shardEnv...)
	if child {
		return
	}
	failures := map[string]string{}
	if combine {
		cachedDefaultRanges = make(map[string][]string, len(first))
		if shareFast {
			cachedFastPathComparisons = make(map[string]bool, len(first))
			cachedFastPathNoRanges = os.Getenv("MDPP_FASTPATH_NORANGES") != ""
		}
	}
	for i, problems := range results {
		if len(problems) == 0 {
			t.Fatalf("missing range result for input %d", i)
		}
		if i < originalCount && problems[0] != "" {
			failures[inputs[i]] = problems[0]
		}
		if combine && first[inputs[i]] == i {
			pos := 1
			if shareFast && i < originalCount {
				if len(problems) < pos+2 || problems[pos] != fastComparisonChecked || (problems[pos+1] != "same" && problems[pos+1] != "different") {
					t.Fatalf("fast comparison assertion missing for input %d", i)
				}
				cachedFastPathComparisons[inputs[i]] = problems[pos+1] == "same"
				pos += 2
			}
			if len(problems) <= pos || problems[pos] != sourceRangeChecked {
				t.Fatalf("source range assertion missing for input %d", i)
			}
			cachedDefaultRanges[inputs[i]] = problems[pos+1:]
		}
	}
	if combine {
		t.Logf("verified %d shared source range assertion markers", len(cachedDefaultRanges))
		if shareFast {
			t.Logf("verified %d shared differential assertion markers", len(cachedFastPathComparisons))
		}
	}
	if len(failures) == 0 {
		return
	}
	var keys []string
	for k := range failures {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) < len(keys[j]) })
	var sb strings.Builder
	for i, k := range keys {
		if i == 12 {
			break
		}
		fmt.Fprintf(&sb, "\n  %q: %s", k, failures[k])
	}
	t.Errorf("%d of %d inputs have a node with a bad range, for example:%s", len(failures), originalCount, sb.String())
}

// Text nodes whose literal is plain source text must cover exactly that
// text. These cases were built with no range before: fast-path list items and
// table cells, the nested list builder, footnote definition bodies, loose
// paragraphs and block quote content.
func TestPlainTextNodesCoverTheirSource(t *testing.T) {
	cases := []struct{ name, src string }{
		{"fast list item", "<!-- c -->\n\n- ab cd\n- ef\n"},
		{"fast ordered list item", "***\n\n3. ab cd\n4. ef\n"},
		{"fast table cell", "***\n\n| h1 | h2 |\n|---|---|\n| x y |  z |\n"},
		{"nested list builder", "- a1\n  - b1\n    - c1\n      - d1\n        - e1\n          - f1\n"},
		{"footnote body alone", "[^1]: some note\n"},
		{"footnote body after paragraph", "text\n\n[^1]: some note\n"},
		{"footnote body with link", "***\n\n[^1]: see [the docs](https://x.y) now\n\nafter\n"},
		{"loose paragraph", "***\n\n[link \\[bar](/uri)\n"},
		{"block quote content", "> ab\n> cd\n"},
		{"nested block quote", "> > ab\n> cd\n"},
		{"quote after paragraph", "***\n\nPa\n> q\n> r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := MustParse([]byte(tc.src))
			if problems := checkNodeRanges(doc); len(problems) > 0 {
				t.Errorf("bad ranges in %q: %v\n%s", tc.src, problems, DumpTreeForSnapshot(doc.Root))
			}
			doc.Root.Walk(func(n *Node) bool {
				if n.Type != NodeText || n.Range.StartLine == 0 || strings.ContainsAny(n.Literal, "\\&") {
					return true
				}
				if got := string(doc.Source[n.Range.StartByte:n.Range.EndByte]); got != n.Literal {
					t.Errorf("text %q has range %d-%d covering %q", n.Literal, n.Range.StartByte, n.Range.EndByte, got)
				}
				return true
			})
			texts := 0
			doc.Root.Walk(func(n *Node) bool {
				if n.Type == NodeText {
					texts++
				}
				return true
			})
			if texts == 0 {
				t.Errorf("no text nodes in %q", tc.src)
			}
		})
	}
}

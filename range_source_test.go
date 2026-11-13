package mdpp

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// These tests check that every node's Range points at bytes of the source that
// was passed in, and that the bytes look like the node: a heading starts with
// its marker or ends with an underline, a task item starts at its list marker,
// and so on. A node that has no source text of its own must say so with
// HasSourceRange, and its subtree is skipped.

var (
	rangeQuotePrefix  = `[ \t>]*`
	rangeListMarker   = `(?:[-*+]|\d{1,9}[.)])`
	reRangeATX        = regexp.MustCompile(`^` + rangeQuotePrefix + `(?:` + rangeListMarker + `[ \t]+)*` + rangeQuotePrefix + `#{1,6}(?:[ \t\n]|$)`)
	reRangeUnderline  = regexp.MustCompile(`^[ \t>]*(?:=+|-+)[ \t]*$`)
	reRangeListItem   = regexp.MustCompile(`^` + rangeQuotePrefix + `(?:` + rangeListMarker + `)(?:[ \t\n]|$)`)
	reRangeFence      = regexp.MustCompile("^" + rangeQuotePrefix + "(?:" + rangeListMarker + "[ \t]+)*" + rangeQuotePrefix + "(?:```|~~~)")
	reRangeIndented   = regexp.MustCompile(`^[ \t>]*(?:` + rangeListMarker + `)?(?:    |\t| {1,3}\t)`)
	reRangeThematic   = regexp.MustCompile(`^` + rangeQuotePrefix + `(?:` + rangeListMarker + `[ \t]+)*(?:-[ \t]*-[ \t]*-|\*[ \t]*\*[ \t]*\*|_[ \t]*_[ \t]*_)`)
	reRangeHTMLStart  = regexp.MustCompile(`^[ \t>]*(?:` + rangeListMarker + `[ \t]+)*[ \t>]*<`)
	reRangeQuoteStart = regexp.MustCompile(`^[ \t]*>`)
)

// profileParser parses src in one profile; the tests below run in each.
type profileParser struct {
	name  string
	parse func(src string) (*Document, error)
}

var rangeTestProfiles = []profileParser{
	{"default", func(src string) (*Document, error) { return Parse([]byte(src)) }},
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// lineColOf returns the 1-indexed line and byte column of an offset.
func lineColOf(src []byte, off int) (int, int) {
	line := 1 + bytes.Count(src[:off], []byte{'\n'})
	col := off - (bytes.LastIndexByte(src[:off], '\n') + 1) + 1
	return line, col
}

// checkRangeConsistency walks doc and describes every node whose range does
// not match the source.
func checkRangeConsistency(doc *Document, input string) []string {
	var problems []string
	orig := normalizeLineEndings([]byte(input))
	if !bytes.HasPrefix(doc.Source, orig) || len(doc.Source) > len(orig)+1 {
		problems = append(problems, fmt.Sprintf("Document.Source %q is not the input %q", doc.Source, orig))
		return problems
	}
	src := doc.Source
	bad := func(n *Node, format string, args ...any) {
		problems = append(problems, fmt.Sprintf("%v %q [%d-%d]: %s", n.Type, n.Literal, n.Range.StartByte, n.Range.EndByte, fmt.Sprintf(format, args...)))
	}
	var visit func(n *Node, parent *Node, synthesized bool)
	visit = func(n *Node, parent *Node, synthesized bool) {
		if n.Type == NodeTableOfContents {
			synthesized = true
		}
		if n.Type != NodeDocument && !n.HasSourceRange() {
			if !synthesized {
				bad(n, "has no source range, and it is not part of a synthesized subtree")
			}
			// Synthesized: no range is claimed. The marker must be the whole
			// zero value, so that no half-set range hides.
			if n.Range != (Range{}) {
				bad(n, "HasSourceRange is false but the range is %+v", n.Range)
			}
			return
		}
		r := n.Range
		if n.Type != NodeDocument {
			switch {
			case r.StartByte < 0 || r.EndByte < r.StartByte || r.EndByte > len(src):
				bad(n, "range lies outside the source (%d bytes)", len(src))
				return
			case r.EndByte == r.StartByte && n.Type != NodeTableCell && n.Type != NodeText:
				bad(n, "empty range")
				return
			}
			if line, col := lineColOf(src, r.StartByte); line != r.StartLine || col != r.StartCol {
				bad(n, "start line:col %d:%d, but byte %d is at %d:%d", r.StartLine, r.StartCol, r.StartByte, line, col)
			}
			if line, col := lineColOf(src, r.EndByte); line != r.EndLine || col != r.EndCol {
				bad(n, "end line:col %d:%d, but byte %d is at %d:%d", r.EndLine, r.EndCol, r.EndByte, line, col)
			}
			if parent != nil && parent.Type != NodeDocument && parent.HasSourceRange() &&
				(r.StartByte < parent.Range.StartByte || r.EndByte > parent.Range.EndByte) {
				bad(n, "lies outside its parent %v [%d-%d]", parent.Type, parent.Range.StartByte, parent.Range.EndByte)
			}
			text := string(src[r.StartByte:r.EndByte])
			// Block ranges may start mid-line (inside a quote or a list item), so the
			// first line is the line the node starts on, from its first byte on.
			switch n.Type {
			case NodeHeading:
				if !reRangeATX.MatchString(text) && !reRangeUnderline.MatchString(lastLine(text)) {
					bad(n, "bytes %q do not start with a heading marker or end with an underline", text)
				}
			case NodeListItem, NodeTaskListItem:
				if !reRangeListItem.MatchString(text) {
					bad(n, "bytes %q do not start at a list marker", text)
				} else if n.Type == NodeTaskListItem && !regexp.MustCompile(`^[^\[]*\[[ xX]\]`).MatchString(firstLine(text)) {
					bad(n, "bytes %q do not hold a task box on the first line", text)
				}
			case NodeCodeBlock:
				// Inside a quote or an item the range may start after the container's indentation.
				// The literal of an all-indented document keeps its leading blank lines, and so does the range.
				body := text
				if strings.HasPrefix(n.Literal, "\n") {
					for {
						line, rest, ok := strings.Cut(body, "\n")
						if !ok || strings.TrimSpace(line) != "" {
							break
						}
						body = rest
					}
				}
				if !reRangeFence.MatchString(body) && !reRangeIndented.MatchString(body) && (parent == nil || parent.Type == NodeDocument) {
					bad(n, "bytes %q start with neither a fence nor an indentation", text)
				}
			case NodeThematicBreak:
				if !reRangeThematic.MatchString(text) {
					bad(n, "bytes %q are not a thematic break", text)
				}
			case NodeBlockquote:
				if !reRangeQuoteStart.MatchString(text) && !strings.Contains(firstLine(text), ">") {
					bad(n, "bytes %q do not start with '>'", text)
				}
			case NodeHTMLBlock:
				if !reRangeHTMLStart.MatchString(text) {
					bad(n, "bytes %q do not start with '<'", text)
				}
			case NodeTable:
				if !strings.Contains(firstLine(text), "|") {
					bad(n, "bytes %q: the first line has no '|'", text)
				}
			case NodeCodeSpan:
				if !strings.Contains(text, "`") {
					bad(n, "bytes %q hold no backtick", text)
				}
			case NodeImage:
				if !strings.HasPrefix(text, "!") {
					bad(n, "bytes %q do not start with '!'", text)
				}
			case NodeFrontmatter:
				if !strings.HasPrefix(text, "---") && !strings.HasPrefix(text, "+++") {
					bad(n, "bytes %q do not start with a front matter fence", text)
				}
			case NodeText:
				if !strings.ContainsAny(text, "\\&\n\t") && text != n.Literal {
					bad(n, "bytes %q differ from the literal", text)
				}
			}
		}
		prevStart := -1
		for _, c := range n.Children {
			if c.HasSourceRange() && n.Type != NodeDocument {
				if c.Range.StartByte < prevStart {
					bad(c, "starts before the previous sibling")
				}
				prevStart = c.Range.StartByte
			}
			visit(c, n, synthesized)
		}
	}
	visit(doc.Root, nil, false)
	return problems
}

// rangeReviewInputs are the inputs from review that once had zero or shifted
// ranges, plus one document that uses each Markdown++ extension once.
var rangeReviewInputs = []string{
	"\n> - [X] A",
	"> - [X] A",
	"> [!NOTE]\nx\n## Evidence\nObserved.",
	"> [!NOTE]\nx\n\n## Evidence\nObserved.",
	"## Summary\n<!-- describe -->\n## Checks\n\n> - [X] A",
	"Before.\n<H2 class=\"x\">Extra</H2>",
	":::note\n## Extra\n:::",
	"- a\n\nText\n- b",
	"---\ntitle: x\n---\n\n## H",
	"> [!WARNING]\nline one\nline two\n\nafter\n\n## H\n",
	"para\n\n> [!TIP]\n> x\n\n> [!NOTE]\ny\n\nend",
	"---\ntitle: t\n---\n\n> [!NOTE]\nx\n\n## Evidence\n",
	"Term\n: Definition one\n: Definition two\n\nOther\n: x\n",
	"# T\n\n[[toc]]\n\n## A\n\n### B\n",
	"text[^1]\n\n[^1]: note with *emphasis*\n",
	"E = $a^2$ and $$\nx\n$$\n\nx^2^ and H~2~O :smile: [[embed:https://example.com/v]]",
	"- a\n\ntext\n- b\n\ntext\n- c\n",
	"1. a\n\ntext\n2. b\n",
	"> [Heading]\n> body\n",
	"```mermaid\ngraph TD\nA-->B\n```\n",
	"<Card title=\"x\" />\n\nHello {name}\n",
	"::: warning title=\"w\"\ninner *text*\n:::\n\nafter\n",
	"- [x] done\n- [ ] todo\n  - [X] nested\n",
	"> - [ ] q1\n> - [x] q2\n>   text\n",
	"1. one\n   - [ ] sub\n2. two\n",
	"| a | b |\n|---|---|\n| 1 | 2 |\n",
	"~~strike~~ <https://auto.link> www.example.com\n",
	"    code\n\n- item\n\n      code in item\n",
	"> ```\n> fence in quote\n> ```\n",
	"Setext\n======\n\nSetext 2\n--------\n\n> Q\n> ==\n",
}

func rangeInputs(t testing.TB) []string {
	inputs := append([]string(nil), rangeReviewInputs...)
	inputs = append(inputs, conformanceInputs(t)...)
	inputs = append(inputs, specAndConformanceInputs(t)...)
	inputs = append(inputs, differentialInputs()...)
	seen := map[string]bool{}
	out := inputs[:0]
	for _, in := range inputs {
		if !seen[in] {
			seen[in] = true
			out = append(out, in)
		}
	}
	return out
}

func TestNodeRangesMatchOriginalSource(t *testing.T) {
	inputs := rangeInputs(t)
	for _, p := range rangeTestProfiles {
		t.Run(p.name, func(t *testing.T) {
			// All problems of an input are collected and grouped by kind (node type
			// and message), and the shortest input of each kind is reported.
			kinds := map[string]string{}
			counts := map[string]int{}
			bad := 0
			var results [][]string
			if p.name == "default" && cachedDefaultRanges != nil {
				results = make([][]string, len(inputs))
				for i, in := range inputs {
					var ok bool
					results[i], ok = cachedDefaultRanges[in]
					if !ok {
						t.Fatalf("source range input %d missing from cache", i)
					}
				}
				cachedDefaultRanges = nil
				t.Logf("checked %d source range inputs using shared default-profile parses", len(inputs))
			} else {
				var child bool
				results, child = corpusShardMap(t, inputs, func(_ int, in string) ([]string, error) {
					doc, err := p.parse(in)
					if err != nil {
						return nil, err
					}
					return checkRangeConsistency(doc, in), nil
				})
				if child {
					return
				}
			}
			for i, problems := range results {
				in := inputs[i]
				if len(problems) > 0 {
					bad++
				}
				seenKind := map[string]bool{}
				for _, pr := range problems {
					f := strings.Fields(pr)
					kind := f[0] + " " + strings.Join(f[len(f)-3:], " ")
					if strings.Contains(pr, "has no source range") {
						kind = f[0] + " has no source range"
					}
					if seenKind[kind] {
						continue
					}
					seenKind[kind] = true
					counts[kind]++
					if cur, ok := kinds[kind]; !ok || len(in) < len(cur) {
						kinds[kind] = in
						kinds[kind+"\x00"] = pr
					}
				}
			}
			if bad == 0 {
				return
			}
			var names []string
			for k := range counts {
				names = append(names, k)
			}
			sort.Strings(names)
			var sb strings.Builder
			for _, k := range names {
				fmt.Fprintf(&sb, "\n  %d x %s, e.g. %q: %s", counts[k], k, kinds[k], kinds[k+"\x00"])
			}
			t.Errorf("%d of %d inputs have a node whose range does not match the source:%s", bad, len(inputs), sb.String())
		})
	}
}

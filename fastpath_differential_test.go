package mdpp

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// The fast block path (parseFastBlockChunk) is an optimisation. It must read a
// chunk exactly as the full parser does: same node types, attributes, literals
// and ranges. These tests parse each input twice, once with the fast path and
// once with it disabled, and compare the trees.

var dumpWithoutRanges bool

func fullNodeDump(n *Node) string {
	var sb strings.Builder
	writeFullNode(&sb, n, 0)
	return sb.String()
}

func writeFullNode(sb *strings.Builder, n *Node, depth int) {
	fmt.Fprintf(sb, "%s%s", strings.Repeat(" ", depth), n.Type)
	keys := make([]string, 0, len(n.Attrs))
	for k := range n.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(sb, " %s=%q", k, n.Attrs[k])
	}
	r := n.Range
	if dumpWithoutRanges {
		fmt.Fprintf(sb, " %q\n", n.Literal)
	} else if n.Type == NodeFootnoteDef {
		// The grammar path ends a footnote definition at the line end or at the
		// next line, depending on what follows; only the end fields are not
		// compared. The start is.
		fmt.Fprintf(sb, " %q [%d-* %d:%d-*]\n", n.Literal, r.StartByte, r.StartLine, r.StartCol)
	} else {
		fmt.Fprintf(sb, " %q [%d-%d %d:%d-%d:%d]\n", n.Literal, r.StartByte, r.EndByte, r.StartLine, r.StartCol, r.EndLine, r.EndCol)
	}
	for _, c := range n.Children {
		writeFullNode(sb, c, depth+1)
	}
}

func parseTreeWithFastPath(src string, fast bool) *Node {
	fastBlockChunkDisabled = !fast
	defer func() { fastBlockChunkDisabled = false }()
	doc, err := Parse([]byte(src))
	if err != nil || doc == nil {
		return nil
	}
	return doc.Root
}

func parseWithFastPath(src string, fast bool) (dump string) {
	fastBlockChunkDisabled = !fast
	defer func() { fastBlockChunkDisabled = false }()
	doc, err := Parse([]byte(src))
	if err != nil || doc == nil {
		return "error"
	}
	return fullNodeDump(doc.Root)
}

// fastPathDifferences returns the inputs, from a list, whose fast and full
// readings differ.
func fastPathDifferences(inputs []string) []string {
	var bad []string
	seen := map[string]bool{}
	for _, src := range inputs {
		if seen[src] {
			continue
		}
		seen[src] = true
		if parseWithFastPath(src, true) != parseWithFastPath(src, false) {
			bad = append(bad, src)
		}
	}
	return bad
}

var differentialBlocks = []struct{ name, text string }{
	{"para1", "Pa"}, {"para2", "Pa\nPb"}, {"para3", "Pa\nPb\nPc"},
	{"atx", "## H"}, {"atx-question", "## What is it?"},
	{"setext=", "S\n==="}, {"setext-", "S\n---"}, {"setext2", "S\nT\n==="}, {"setext3", "S\nT\nU\n---"},
	{"break*", "***"}, {"break_", "___"}, {"break-", "---"}, {"break- - -", "- - -"},
	{"fence`", "```\ncode\n```"}, {"fence~", "~~~\ncode\n~~~"}, {"fence-open", "```\ncode"},
	{"indented", "    code"},
	{"html1", "<pre>\nx\n</pre>"}, {"html2", "<!-- c -->"}, {"html3", "<?php x ?>"}, {"html4", "<!DOCTYPE html>"},
	{"html5", "<![CDATA[ x ]]>"}, {"html6", "<div>\nx\n</div>"}, {"html7", "<span>x</span>"},
	{"quote", "> q"}, {"quote2", "> q\n> r"},
	{"bullet", "- a"}, {"bullet2", "- a\n- b"}, {"bullet-loose", "- a\n\n- b"}, {"star", "* a"},
	{"ordered", "1. a"}, {"ordered2", "1. a\n2. b"}, {"ordered-start", "2. a"},
	{"table", "| h |\n|---|\n| x |"}, {"task", "- [ ] t"},
	{"frontmatter", "---\ntitle: x\n---"}, {"footnote", "[^1]: note"}, {"refdef", "[a]: /u"},
}

// differentialTripleBlocks is the subset used for ordered triples.
var differentialTripleBlocks = []string{"para2", "atx", "setext=", "break*", "break-", "fence`", "indented", "html6", "html2", "quote", "bullet2", "ordered", "table"}

// reviewedBoundaryInputs are paragraph, list, quote and table boundaries found
// by review that once read differently on the two paths. The leading break
// sends the rest of each document through the fast path.
var reviewedBoundaryInputs = []string{
	"***\na\n***", "***\na\n___", "***\na\n-\t-\t-", "***\na\n<p>", "***\na\n<x>",
	"***\n-", "***\na\n-\tb", "***\na\n1) b", "***\n1) b", "***\n- a\nb", "***\n>a\nb",
	"***\na\n- b\nc", "***\na|b\n-|-", "***\n|a|\n|-|\nb", "***\na\n    >b",
	"***\na\n    ~~~\n    ~~~", "***\na\n=\n", "***\n&amp;\nx\n=", "***\n&#10;\nx\n=",
	"***\r\na\r\n***\r\n", "***\na\n  ---\n", "***\na\n--- \n", "***\na  \n",
	// Container and boundary probes from the second review.
	"***\n- 1) a\n", "***\n- -\n", "***\n- #\n", "***\n- [a]: /u\n", "***\n> - a\nb\n", "***\n- > a\nb\n",
	"***\n0. a\n* b\n", "***\n|h|\n|-|\n|h|\n|-|\n# h\n", "***\n|h|\n|-|\n| |\n", "***\n~~~\n~~~x\n~~~\n~~~\n",
	"***\n```\n```x\n```\n", "***\n> a\n> - b\nc\n", "# &#10; !\n", "&amp;!\nx\n===\n",
}

func differentialInputs() []string {
	text := map[string]string{}
	for _, b := range differentialBlocks {
		text[b.name] = b.text
	}
	inputs := append([]string(nil), reviewedBoundaryInputs...)
	seps := []string{"\n", "\n\n"}
	for _, a := range differentialBlocks {
		inputs = append(inputs, a.text+"\n")
		for _, b := range differentialBlocks {
			for _, s := range seps {
				inputs = append(inputs, a.text+s+b.text+"\n")
			}
		}
	}
	for _, an := range differentialTripleBlocks {
		for _, bn := range differentialTripleBlocks {
			for _, cn := range differentialTripleBlocks {
				for _, s1 := range seps {
					for _, s2 := range seps {
						inputs = append(inputs, text[an]+s1+text[bn]+s2+text[cn]+"\n")
					}
				}
			}
		}
	}
	// The same block combinations with CRLF line ends.
	m := len(inputs)
	for i := 0; i < m; i++ {
		if strings.Contains(inputs[i], "\n") && !strings.Contains(inputs[i], "\r") {
			inputs = append(inputs, strings.ReplaceAll(inputs[i], "\n", "\r\n"))
		}
	}
	// Unicode whitespace and control characters at every position of the
	// block forms whose syntax depends on line edges, markers and pipes.
	inputs = append(inputs, whitespaceVariantInputs()...)
	// Each input also follows a leading HTML comment and a leading break, the
	// two places where the rest of a document takes the fast path.
	n := len(inputs)
	for i := 0; i < n; i++ {
		inputs = append(inputs, "<!-- c -->\n\n"+inputs[i], "***\n\n"+inputs[i])
	}
	return inputs
}

func specAndConformanceInputs(t testing.TB) []string {
	var inputs []string
	for _, file := range []string{"commonmark-0.31.2.json", "gfm-0.29.json"} {
		data, err := specFiles.ReadFile("testdata/spec/" + file)
		if err != nil {
			t.Fatal(err)
		}
		var examples []markdownSpecExample
		if err := json.Unmarshal(data, &examples); err != nil {
			t.Fatal(err)
		}
		for _, e := range examples {
			inputs = append(inputs, e.Markdown, "<!-- c -->\n\n"+e.Markdown, "***\n\n"+e.Markdown)
		}
	}
	for _, input := range conformanceInputs(t) {
		inputs = append(inputs, input, "<!-- c -->\n\n"+input, "***\n\n"+input)
	}
	return inputs
}

func conformanceInputs(t testing.TB) []string {
	entries, err := os.ReadDir("examples/conformance")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile("examples/conformance/" + e.Name() + "/input.md")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(b))
	}
	return out
}

func TestFastPathMatchesFullParser(t *testing.T) {
	inputs := append(specAndConformanceInputs(t), differentialInputs()...)
	dumpWithoutRanges = os.Getenv("MDPP_FASTPATH_NORANGES") != ""
	defer func() { dumpWithoutRanges = false }()
	bad := fastPathDifferences(inputs)
	if path := os.Getenv("MDPP_FASTPATH_REPORT"); path != "" {
		sort.Slice(bad, func(i, j int) bool {
			if len(bad[i]) != len(bad[j]) {
				return len(bad[i]) < len(bad[j])
			}
			return bad[i] < bad[j]
		})
		var sb strings.Builder
		fmt.Fprintf(&sb, "%d inputs, %d differences\n", len(inputs), len(bad))
		for _, b := range bad {
			fmt.Fprintf(&sb, "%q\n", b)
		}
		_ = os.WriteFile(path, []byte(sb.String()), 0o644)
	}
	reported := 0
	for _, src := range bad {
		if reported < 20 {
			t.Errorf("fast and full readings differ for %q\nfast:\n%s\nfull:\n%s", src, parseWithFastPath(src, true), parseWithFastPath(src, false))
		}
		reported++
	}
	if reported > 20 {
		t.Errorf("%d inputs differ in total", reported)
	}
	distinct := map[string]bool{}
	for _, src := range inputs {
		distinct[src] = true
	}
	t.Logf("%d entries, %d distinct inputs compared", len(inputs), len(distinct))
}

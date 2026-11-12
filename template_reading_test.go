package mdpp

import (
	"strings"
	"testing"
)

// A tool that reads a pull request template, or a pull request body that was
// made from one, needs three kinds of node with exact byte ranges: headings
// (with their level), task items (with their checked state) and HTML comments
// (block and inline). This test checks them, with LF and with CRLF line ends,
// in the GFM profile and in the default profile.

const pullRequestTemplate = "<!-- Thank you for the change. Fill in each section. -->\n" +
	"## Summary\n" +
	"\n" +
	"Short description. <!-- one sentence -->\n" +
	"\n" +
	"## Checklist\n" +
	"\n" +
	"- [x] Tests added\n" +
	"- [ ] Docs updated\n" +
	"  - [X] Nested item done\n" +
	"1. [ ] Ordered item\n" +
	"\n" +
	"### Notes <!-->\n" +
	"<!--\n" +
	"hidden\n" +
	"lines -->\n" +
	"Closes #1 <!--->\n"

func TestPullRequestTemplateNodesHaveExactRanges(t *testing.T) {
	type want struct {
		text    string // the source text of the node, without its line end
		level   string
		checked string
	}
	headings := []want{{"## Summary", "2", ""}, {"## Checklist", "2", ""}, {"### Notes <!-->", "3", ""}}
	tasks := []want{
		{"- [x] Tests added", "", "true"},
		{"- [ ] Docs updated\n  - [X] Nested item done", "", "false"}, // the parent item holds the nested one
		{"- [X] Nested item done", "", "true"},
		{"1. [ ] Ordered item", "", "false"},
	}
	blocks := []string{"<!-- Thank you for the change. Fill in each section. -->", "<!--\nhidden\nlines -->"}
	inline := []string{"<!-- one sentence -->", "<!-->", "<!--->"}

	for _, eol := range []string{"\n", "\r\n"} {
		input := strings.ReplaceAll(pullRequestTemplate, "\n", eol)
		for _, profile := range []ParseProfile{ProfileMarkdownPlus, ProfileGFM} {
			doc, err := ParseWithOptions([]byte(input), ParseOptions{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			label := func(kind string) string {
				return kind + " (profile " + profile.String() + ", " + map[string]string{"\n": "LF", "\r\n": "CRLF"}[eol] + ")"
			}
			if !doc.StructureCertified() {
				t.Errorf("%s: not certified: %s", label("template"), diagnosticSummary(doc))
			}
			// exact checks the range of n against the source text, in
			// Document.Source and, through OriginalRange, in the input.
			exact := func(kind string, n *Node, text string) {
				t.Helper()
				src := string(doc.Source)
				index := strings.Index(src, strings.ReplaceAll(text, "\n", "\n"))
				if index < 0 || n.Range.StartByte != index {
					t.Errorf("%s: %q starts at byte %d, want %d", label(kind), text, n.Range.StartByte, index)
					return
				}
				got := src[n.Range.StartByte:n.Range.EndByte]
				if strings.TrimRight(got, "\n") != text {
					t.Errorf("%s: range covers %q, want %q", label(kind), got, text)
				}
				if line := 1 + strings.Count(src[:index], "\n"); n.Range.StartLine != line {
					t.Errorf("%s: %q starts on line %d, want %d", label(kind), text, n.Range.StartLine, line)
				}
				start, end, ok := doc.OriginalRange([]byte(input), n.Range)
				if !ok {
					t.Errorf("%s: no mapping to the input", label(kind))
					return
				}
				if want := strings.ReplaceAll(text, "\n", eol); strings.TrimRight(input[start:end], "\r\n") != want {
					t.Errorf("%s: input bytes %q, want %q", label(kind), input[start:end], want)
				}
			}

			gotHeadings := doc.Root.Find(NodeHeading)
			if len(gotHeadings) != len(headings) {
				t.Fatalf("%s: %d headings", label("headings"), len(gotHeadings))
			}
			for i, w := range headings {
				if gotHeadings[i].Attrs["level"] != w.level {
					t.Errorf("%s: heading %d has level %q, want %q", label("headings"), i, gotHeadings[i].Attrs["level"], w.level)
				}
				exact("heading", gotHeadings[i], w.text)
			}
			gotTasks := doc.Root.Find(NodeTaskListItem)
			if len(gotTasks) != len(tasks) {
				t.Fatalf("%s: %d task items: %s", label("tasks"), len(gotTasks), fullNodeDump(doc.Root))
			}
			for i, w := range tasks {
				if gotTasks[i].Attrs["checked"] != w.checked {
					t.Errorf("%s: task item %d has checked=%q, want %q", label("tasks"), i, gotTasks[i].Attrs["checked"], w.checked)
				}
				first := strings.SplitN(w.text, "\n", 2)[0]
				n := gotTasks[i]
				if got := strings.TrimRight(string(doc.Source[n.Range.StartByte:n.Range.EndByte]), "\n"); !strings.HasPrefix(got, first) {
					t.Errorf("%s: task item %d covers %q, want it to start with %q", label("tasks"), i, got, first)
				}
				exact("task item", n, w.text)
			}
			gotBlocks := doc.Root.Find(NodeHTMLBlock)
			if len(gotBlocks) != len(blocks) {
				t.Fatalf("%s: %d HTML blocks: %s", label("blocks"), len(gotBlocks), fullNodeDump(doc.Root))
			}
			for i, w := range blocks {
				exact("HTML comment block", gotBlocks[i], w)
			}
			gotInline := doc.Root.Find(NodeHTMLInline)
			if len(gotInline) != len(inline) {
				t.Fatalf("%s: %d inline HTML nodes: %s", label("inline"), len(gotInline), fullNodeDump(doc.Root))
			}
			for i, w := range inline {
				if gotInline[i].Literal != w {
					t.Errorf("%s: literal %q, want %q", label("inline"), gotInline[i].Literal, w)
				}
				exact("inline HTML comment", gotInline[i], w)
			}
		}
	}
}

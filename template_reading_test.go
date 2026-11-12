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

// The inputs of the fifth review, read in the GFM profile (the grammar path) and
// in the default profile, for the three kinds of node the tool needs. Each
// input is either read exactly, with the counts below and no uncertain range, or
// it carries a diagnostic that covers the text that was not read structurally.
func TestReviewInputsForHeadingsTasksAndComments(t *testing.T) {
	tail := "\n  - a\n    - a\n      - a\n        - a\n          - a\n"
	comments := func(doc *Document) int {
		n := 0
		for _, node := range doc.Root.Find(NodeHTMLInline) {
			if strings.HasPrefix(node.Literal, "<!--") {
				n++
			}
		}
		for _, node := range doc.Root.Find(NodeHTMLBlock) {
			if strings.HasPrefix(node.Literal, "<!--") {
				n++
			}
		}
		return n
	}
	exact := []struct {
		name, src                string
		headings, tasks, comment int
	}{
		{"compact comments next to a tag", "x <!--><q00>\n", 0, 0, 1},
		{"321-byte comment", "x <!--" + strings.Repeat("a", 312) + "-->\n", 0, 0, 1},
		{"1,297 compact comments", "x " + strings.Repeat("<!--> ", 1297) + "\n", 0, 0, 1297},
		{"tab after the checkbox", "- [x]\ta\n", 0, 1, 0},
		{"comment, heading, task", "<!-- c -->\n## S\n- [ ] t\n", 1, 1, 1},
		{"comment inside a code span", "`<!-- c -->` and # not a heading\n", 0, 0, 0},
		{"definition then text", "[foo]: /url\n\n[foo] <!-- c -->\n", 0, 0, 1},
	}
	for _, profile := range []ParseProfile{ProfileMarkdownPlus, ProfileGFM} {
		for _, tc := range exact {
			doc, err := ParseWithOptions([]byte(tc.src), ParseOptions{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			if !doc.StructureCertified() {
				t.Errorf("%s (%v): not certified: %s", tc.name, profile, diagnosticSummary(doc))
			}
			if h, k, c := len(doc.Root.Find(NodeHeading)), len(doc.Root.Find(NodeTaskListItem)), comments(doc); h != tc.headings || k != tc.tasks || c != tc.comment {
				t.Errorf("%s (%v): %d headings, %d task items, %d comments; want %d, %d, %d:\n%.2000s", tc.name, profile, h, k, c, tc.headings, tc.tasks, tc.comment, fullNodeDump(doc.Root))
			}
		}
	}
	uncertain := []struct {
		name, src  string
		start, end int
		profiles   []ParseProfile
	}{
		{"deep list with a task item", "- [x] a" + tail, 0, 58, []ParseProfile{ProfileMarkdownPlus, ProfileGFM}},
		{"deep list with a heading", "- # h" + tail, 0, 56, []ParseProfile{ProfileMarkdownPlus, ProfileGFM}},
		{"deep list with a block comment", "- <!-- c -->" + tail, 0, 62, []ParseProfile{ProfileMarkdownPlus, ProfileGFM}},
		{"quote with a discarded emphasis", "> *x* <a http://a\n", 2, 17, []ParseProfile{ProfileMarkdownPlus, ProfileGFM}},
		{"heading line inside an HTML block", "Before.\n<h2>\n# h\n", 8, 17, []ParseProfile{ProfileGFM}},
		{"heading line inside a pre block", "<pre>\n# h\n</pre>\n", 0, 17, []ParseProfile{ProfileGFM}},
		{"heading after an indented closing fence", "```\naaa\n    ```\n# h\n", 0, 20, []ParseProfile{ProfileGFM}},
		{"heading made of a definition and an underline", "[foo]: /url\n===\n[foo]\n", 0, 16, []ParseProfile{ProfileGFM}},
	}
	for _, tc := range uncertain {
		for _, profile := range tc.profiles {
			doc, err := ParseWithOptions([]byte(tc.src), ParseOptions{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			if doc.StructureCertified() || !covers(doc.UncertifiedRanges(), tc.start, tc.end) {
				t.Errorf("%s (%v): uncertain ranges %s do not cover [%d,%d):\n%.1500s", tc.name, profile, diagnosticSummary(doc), tc.start, tc.end, fullNodeDump(doc.Root))
			}
		}
	}
}

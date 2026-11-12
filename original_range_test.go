package mdpp

import "testing"

// Ranges index Document.Source, in which CRLF and CR are LF. OriginalRange maps
// them to the bytes of the input. The three constructs a template reader needs
// (heading, task item, HTML comment) are checked with CRLF, CR and mixed line
// ends, and in the GFM profile.
func TestOriginalRangeMapsHeadingTaskAndCommentToInputBytes(t *testing.T) {
	inputs := map[string]string{
		"CRLF":   "# Title\r\n\r\n- [x] done\r\n- [ ] todo\r\n\r\n<!-- note -->\r\n\r\ntext <!--> end\r\n",
		"CR":     "# Title\r\r- [x] done\r- [ ] todo\r\r<!-- note -->\r\rtext <!--> end\r",
		"mixed":  "# Title\r\n\n- [x] done\r- [ ] todo\n\r\n<!-- note -->\n\rtext <!--> end\r\n",
		"no end": "# Title\r\n\r\n- [x] done\r\n- [ ] todo\r\n\r\n<!-- note -->\r\n\r\ntext <!--> end",
	}
	for name, input := range inputs {
		for _, profile := range []ParseProfile{ProfileMarkdownPlus, ProfileGFM} {
			doc, err := ParseWithOptions([]byte(input), ParseOptions{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			if !doc.SourceHadCarriageReturns() {
				t.Fatalf("%s: carriage returns not reported", name)
			}
			check := func(kind string, n *Node, wantStart string) {
				t.Helper()
				start, end, ok := doc.OriginalRange([]byte(input), n.Range)
				if !ok {
					t.Errorf("%s/%v %s: no mapping", name, profile, kind)
					return
				}
				got := input[start:end]
				// The mapped input text, read the way Parse reads line ends,
				// is the text of the normalized range.
				if want := string(doc.Source[n.Range.StartByte:min(n.Range.EndByte, len(doc.Source))]); string(normalizeLineEndings([]byte(got))) != want {
					t.Errorf("%s/%v %s: input bytes %q normalize to %q, Source range is %q", name, profile, kind, got, normalizeLineEndings([]byte(got)), want)
				}
				if len(got) < len(wantStart) || got[:len(wantStart)] != wantStart {
					t.Errorf("%s/%v %s: input bytes %q do not start with %q", name, profile, kind, got, wantStart)
				}
			}
			headings := doc.Root.Find(NodeHeading)
			if len(headings) != 1 {
				t.Fatalf("%s/%v: %d headings", name, profile, len(headings))
			}
			check("heading", headings[0], "# Title")
			tasks := doc.Root.Find(NodeTaskListItem)
			if len(tasks) != 2 || tasks[0].Attrs["checked"] != "true" || tasks[1].Attrs["checked"] != "false" {
				t.Fatalf("%s/%v: task items %v", name, profile, tasks)
			}
			check("task item 1", tasks[0], "- [x] done")
			check("task item 2", tasks[1], "- [ ] todo")
			blocks := doc.Root.Find(NodeHTMLBlock)
			if len(blocks) != 1 {
				t.Fatalf("%s/%v: %d HTML blocks", name, profile, len(blocks))
			}
			check("HTML comment block", blocks[0], "<!-- note -->")
			inline := doc.Root.Find(NodeHTMLInline)
			if len(inline) != 1 || inline[0].Literal != "<!-->" {
				t.Fatalf("%s/%v: inline HTML %v", name, profile, inline)
			}
			check("inline comment", inline[0], "<!-->")
			if s, e, _ := doc.OriginalRange([]byte(input), inline[0].Range); input[s:e] != "<!-->" {
				t.Errorf("%s/%v: inline comment maps to %q", name, profile, input[s:e])
			}
		}
	}
}

// The heading of the report: "# h\r\n" ends at byte 4 of Source and byte 5 of
// the input; and an input that Parse did not read is refused.
func TestOriginalRangeCRLFHeadingEnd(t *testing.T) {
	input := []byte("# h\r\n")
	doc := MustParse(input)
	h := doc.Root.Find(NodeHeading)[0]
	if h.Range.EndByte != 4 {
		t.Fatalf("heading ends at byte %d of Source", h.Range.EndByte)
	}
	if s, e, ok := doc.OriginalRange(input, h.Range); !ok || s != 0 || e != 5 {
		t.Errorf("OriginalRange = %d, %d, %v; want 0, 5, true", s, e, ok)
	}
	if _, _, ok := doc.OriginalRange([]byte("# x\r\n"), h.Range); ok {
		t.Errorf("a different input was accepted")
	}
	lf := MustParse([]byte("# h\n"))
	if s, e, ok := lf.OriginalRange([]byte("# h\n"), lf.Root.Find(NodeHeading)[0].Range); !ok || s != 0 || e != 4 {
		t.Errorf("LF input: %d, %d, %v", s, e, ok)
	}
}

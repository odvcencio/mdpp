package mdpp

import (
	"strings"
	"testing"
)

func TestReviewGFMFenceLoweringPreservesCode(t *testing.T) {
	for _, tc := range []struct{ name, open, inner, close string }{
		{"mixed_tilde", "```", "~~~", "```"},
		{"mixed_backtick", "~~~", "```", "~~~"},
		{"shorter_run", "````", "```", "````"},
		{"closer_with_text", "```", "``` trailing", "```"},
		{"longer_closer", "```", "~~~", "````"},
		{"three_space_closer", "```", "~~~", "   ```"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.inner + "\n<script>\nhello\n</script>\nworld\n"
			src := tc.open + "\n" + body + tc.close + "\n"
			doc, err := ParseWithOptions([]byte(src), ParseOptions{Profile: ProfileGFM})
			if err != nil {
				t.Fatal(err)
			}
			nodes := doc.Root.Find(NodeCodeBlock)
			if len(nodes) != 1 {
				t.Fatalf("code nodes=%d", len(nodes))
			}
			if nodes[0].Literal != body {
				t.Errorf("code literal %q, want %q", nodes[0].Literal, body)
			}
			if string(doc.Source) != src {
				t.Errorf("source %q, want %q", doc.Source, src)
			}
			if nodes[0].Range.StartByte != 0 || nodes[0].Range.EndByte != len(src) {
				t.Errorf("range %+v, source len=%d", nodes[0].Range, len(src))
			}
			for _, d := range doc.Diagnostics() {
				if d.Code == DiagHTMLBlockUnread || d.Code == DiagBlockMisread {
					t.Errorf("false diagnostic %s %+v", d.Code, d.Range)
				}
			}
		})
	}
}
func TestReviewGFMFenceLoweringStillDetectsRealHTML(t *testing.T) {
	src := "```\n~~~\ncode\n````\n<script>\nx\n</script>\n# after\n"
	lowered, inserts := gfmLowerSource([]byte(src))
	if len(inserts) != 1 || !strings.Contains(string(lowered), "</script>\n\n\n# after") {
		t.Fatalf("real HTML insertion missing: positions=%v source=%q", inserts, lowered)
	}
	doc, err := ParseWithOptions([]byte(src), ParseOptions{Profile: ProfileGFM})
	if err != nil {
		t.Fatal(err)
	}
	codes := doc.Root.Find(NodeCodeBlock)
	if len(codes) != 1 || codes[0].Literal != "~~~\ncode\n" {
		t.Fatalf("code=%v", codes)
	}
	if !strings.Contains(string(doc.Source), "</script>\n# after") {
		t.Fatalf("source %q", doc.Source)
	}
}

func TestReviewGFMFenceLoweringIndentedCloserReportsGrammarLimit(t *testing.T) {
	src := "```\n    ```\n<script>\nhello\n</script>\nworld\n```\n"
	lowered, inserts := gfmLowerSource([]byte(src))
	if len(inserts) != 0 || string(lowered) != src {
		t.Errorf("code modified before parse: inserts=%v source=%q", inserts, lowered)
	}
	doc, err := ParseWithOptions([]byte(src), ParseOptions{Profile: ProfileGFM})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range doc.Diagnostics() {
		if d.Code == DiagBlockMisread {
			found = true
		}
	}
	if !found {
		t.Errorf("missing documented indented-fence warning: %v", doc.Diagnostics())
	}
}

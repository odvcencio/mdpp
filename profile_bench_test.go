package mdpp

import (
	"fmt"
	"strings"
	"testing"
)

// proseDocument builds a document of n sections of ordinary prose: a heading,
// two paragraphs with inline markup, a list with a task item and a quote.
func proseDocument(n int) []byte {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "## Section %d\n\n", i)
		sb.WriteString("This paragraph has *emphasis*, **strong text**, `code` and a [link](https://example.com/a) in it.\nIt continues on a second line with more words to read.\n\n")
		sb.WriteString("A second paragraph, with an autolink <https://example.com> and a footnote-free sentence.\n\n")
		sb.WriteString("- first item\n- [x] done task\n- [ ] open task\n\n")
		sb.WriteString("> a quoted line\n> and its continuation\n\n")
	}
	return []byte(sb.String())
}

// BenchmarkParseProseByProfile measures prose parsing in both profiles. The GFM
// profile takes no shortcut (parseCtx.shortcut), so it pays for the grammar
// path on every block. The numbers are directional: they depend on the machine.
func BenchmarkParseProseByProfile(b *testing.B) {
	for _, size := range []int{10, 100, 400} {
		src := proseDocument(size)
		for _, p := range []struct {
			name    string
			profile ParseProfile
		}{{"default", ProfileMarkdownPlus}, {"gfm", ProfileGFM}} {
			b.Run(fmt.Sprintf("%dKB/%s", len(src)/1024, p.name), func(b *testing.B) {
				b.SetBytes(int64(len(src)))
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := ParseWithOptions(src, ParseOptions{Profile: p.profile}); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

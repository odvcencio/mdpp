package mdpp

import (
	"strings"
	"testing"
)

func TestReviewParagraphFootnoteDoesNotInterrupt(t *testing.T) {
	for _, prefix := range []string{"<!-- c -->\n\n", "***\n\n"} {
		for _, lines := range []int{1, 2, 3} {
			for _, sep := range []string{"\n", "\r\n"} {
				text := strings.Join([]string{"Pa", "Pb", "Pc"}[:lines], sep) + sep + "[^1]: note" + sep
				source := prefix + text
				fast, full := parseWithFastPath(source, true), parseWithFastPath(source, false)
				if fast != full {
					t.Errorf("source=%q\nfast=%s\nfull=%s", source, fast, full)
				}
			}
		}
	}
}

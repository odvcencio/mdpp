package mdpp

import (
	"strings"
	"testing"
	"time"
)

func TestManyUnmatchedLinkBracketsStayFastAndPreserveText(t *testing.T) {
	for _, opener := range []string{"[", "!["} {
		t.Run(opener, func(t *testing.T) {
			source := strings.Repeat(opener, 100000)
			start := time.Now()
			doc, err := Parse([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if elapsed := time.Since(start); elapsed >= time.Second {
				t.Fatalf("100000 unmatched openers took %s", elapsed)
			}
			got, err := Render(doc, RenderOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), source) {
				t.Fatal("unmatched openers lost text")
			}
		})
	}
}

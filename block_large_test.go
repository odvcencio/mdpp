package mdpp

import (
	"strings"
	"testing"
	"time"
)

func TestLargeHTMLAndBlockquoteBlocks(t *testing.T) {
	for _, line := range []string{"<div>\n", ">\n"} {
		t.Run(strings.TrimSpace(line), func(t *testing.T) {
			source := []byte(strings.Repeat(line, 100_000))
			start := time.Now()
			doc, err := Parse(source)
			if err != nil || doc == nil || doc.Root == nil {
				t.Fatalf("Parse: document=%v, error=%v", doc, err)
			}
			if elapsed := time.Since(start); elapsed >= time.Second {
				t.Fatalf("100,000 lines of %q took %s", line, elapsed)
			}
		})
	}
}

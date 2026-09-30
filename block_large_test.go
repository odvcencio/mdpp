package mdpp

import (
	"strings"
	"testing"
	"time"
)

// Large block runs must parse in time linear in their size. An absolute time
// cap depends on the machine and on -race (about 10x slower), so this compares
// the best of three parses at N lines with the best of three at 4N lines. Linear
// growth gives a ratio near 4; quadratic growth gives 16. A ratio above 9 fails.
func TestLargeHTMLAndBlockquoteBlocks(t *testing.T) {
	parseBest := func(source []byte) time.Duration {
		best := time.Duration(1<<63 - 1)
		for range 3 {
			start := time.Now()
			doc, err := Parse(source)
			if err != nil || doc == nil || doc.Root == nil {
				t.Fatalf("Parse: document=%v, error=%v", doc, err)
			}
			if elapsed := time.Since(start); elapsed < best {
				best = elapsed
			}
		}
		return best
	}
	for _, line := range []string{"<div>\n", ">\n"} {
		t.Run(strings.TrimSpace(line), func(t *testing.T) {
			small := parseBest([]byte(strings.Repeat(line, 25_000)))
			large := parseBest([]byte(strings.Repeat(line, 100_000)))
			// Very fast parses are dominated by timer and allocation noise.
			if large < 50*time.Millisecond {
				return
			}
			if ratio := float64(large) / float64(small); ratio > 9 {
				t.Fatalf("100,000 lines of %q took %s, %.1fx the time of 25,000 lines (%s); parse time is not linear", line, large, ratio, small)
			}
		})
	}
}

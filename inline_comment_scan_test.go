package mdpp

import (
	"strings"
	"testing"
	"time"
)

// The scanner decides what is a comment, so the cases are checked on it directly:
// the grammar reads some of the other raw HTML forms as text.
func TestInlineCommentScannerPrecedence(t *testing.T) {
	for _, tc := range inlineCommentPrecedenceCases {
		// The scanner decides, so it is checked directly: the grammar reads some
		// of the other raw HTML forms as text, which is not what is tested here.
		text := strings.TrimSuffix(tc.src, "\n")
		spans, _ := findInlineComments(text)
		var got []string
		for _, span := range spans {
			got = append(got, text[span[0]:span[1]])
		}
		var want []string
		for _, w := range tc.want {
			if strings.HasPrefix(w, "<!--") {
				want = append(want, w)
			}
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: scanner found %q, want %q", tc.name, got, want)
		}
	}
}

func TestInlineCommentScannerIsLinear(t *testing.T) {
	// The scanner alone, on text that tries every failing search many times.
	hostile := strings.Repeat("`a ``b ```c <!-- <? <![CDATA[ <!A [x](y \"z [x](y 'z [x](y (z ", 2000)
	a1 := testing.AllocsPerRun(1, func() { findInlineComments(hostile[:len(hostile)/2]) })
	a2 := testing.AllocsPerRun(1, func() { findInlineComments(hostile) })
	if a2 > 2.5*a1+10 {
		t.Errorf("scanner allocations grew from %v to %v for twice the text", a1, a2)
	}
	// Time, best of three: four times the text must cost well under the 16
	// times that quadratic work would.
	best := func(text string) time.Duration {
		min := time.Hour
		for i := 0; i < 3; i++ {
			start := time.Now()
			findInlineComments(text)
			if d := time.Since(start); d < min {
				min = d
			}
		}
		return min
	}
	small, large := best(hostile[:len(hostile)/4]), best(hostile)
	t.Logf("scanner: %d bytes %v, %d bytes %v (directional)", len(hostile)/4, small, len(hostile), large)
	if large > 10*small+time.Millisecond {
		t.Errorf("scanner time grew from %v to %v for four times the text", small, large)
	}
}

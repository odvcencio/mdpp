package mdpp

import "testing"

func TestReviewTableSetextFastParity(t *testing.T) {
	for _, src := range []string{"***\n\n| a | b |\n| --- | --- |\n| c | d |\n| e | f |\n===\n", "***\n\n| a | b |\n| --- | --- |\n| c | d |\n---\n"} {
		got, want := parseWithFastPath(src, true), parseWithFastPath(src, false)
		if got != want {
			t.Errorf("source=%q\nfast:\n%s\nfull:\n%s", src, got, want)
		}
	}
}

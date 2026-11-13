package mdpp

import "testing"

func TestFastPathMatchesFullParser(t *testing.T) {
	t.Parallel()
	differentialProfile = ProfileMarkdownPlus
	checkFastPathMatchesFullParser(t)
}

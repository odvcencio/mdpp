package mdpp

import "testing"

func TestUnresolvedReferenceRenderUnchangedWithoutComments(t *testing.T) {
	cases := map[string]string{"[x *y*]": "<p>[x *y*]</p>\n", "[x **y**][id]": "<p>[x **y**][id]</p>\n"}
	for input, want := range cases {
		if got := RenderString(input); got != want {
			t.Errorf("%q: got %q want %q", input, got, want)
		}
	}
}

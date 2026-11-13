package mdpp

import (
	"strings"
	"testing"
)

func TestCommentSemantics(t *testing.T) {
	cases := []struct{ text, want string }{
		{"~~a *b <!--> c* d~~", "<del>a <em>b &lt;!--&gt; c</em> d</del>"},
		{"*a <!--> `*x*` b*", "<em>a &lt;!--&gt; <code>*x*</code> b</em>"},
		{"*a <!--> \\* b*", "<em>a &lt;!--&gt; * b</em>"},
		{"*a <!--> [b][id] c*\n\n[id]: /url", "<em>a &lt;!--&gt; <a href=\"/url\">b</a> c</em>"},
		{"*a <!--> [b](/url) c*", "<em>a &lt;!--&gt; <a href=\"/url\">b</a> c</em>"},
		{"*a <!--> <b> c*", "<em>a &lt;!--&gt; &lt;b&gt; c</em>"},
		{"*a <!--> b* ~z~", "<em>a &lt;!--&gt; b</em> <sub>z</sub>"},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got := RenderString(tc.text)
			if !strings.Contains(got, tc.want) {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

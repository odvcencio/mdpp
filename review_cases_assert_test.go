package mdpp

import (
	"strings"
	"testing"
)

func TestReviewCasesAssert(t *testing.T) {
	cases := []struct{ in, want string }{
		{"~~a ~x~ <!--> b~~", "<del>a <sub>x</sub> &lt;!--&gt; b</del>"},
		{"~~a <!--> ~x~ b~~", "<del>a &lt;!--&gt; <sub>x</sub> b</del>"},
		{"*a <!--> unmatched ` b*", "<em>a &lt;!--&gt; unmatched ` b</em>"},
		{"*a <!--> unmatched < b*", "<em>a &lt;!--&gt; unmatched &lt; b</em>"},
		{"*a <!--> unmatched [ b*", "<em>a &lt;!--&gt; unmatched [ b</em>"},
		{"*a <!--> [x *y*] b*", "<em>a &lt;!--&gt; [x <em>y</em>] b</em>"},
		{"*a <!--> [x *y*][id] b*\n\n[id]: /u", "<em>a &lt;!--&gt; <a href=\"/u\">x <em>y</em></a> b</em>"},
		{"*a <!--> [x *y*](/u) b*", "<em>a &lt;!--&gt; <a href=\"/u\">x <em>y</em></a> b</em>"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := RenderString(tc.in); !strings.Contains(got, tc.want) {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

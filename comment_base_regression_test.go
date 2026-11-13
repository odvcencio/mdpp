package mdpp

import (
	"strings"
	"testing"
)

func TestCommentPrePRFormattingRegressions(t *testing.T) {
	text := "prefix " + strings.Repeat("*", 322) + "a <!--> b" + strings.Repeat("*", 322)
	if got := strings.Count(RenderString(text), "<strong>"); got != 161 {
		t.Errorf("deep strong count=%d want=161", got)
	}
	if got := RenderString("*a <!--> <a href=\" b*"); !strings.Contains(got, "<em>a &lt;!--&gt; &lt;a href=&#34; b</em>") {
		t.Errorf("malformed HTML output %q", got)
	}
}

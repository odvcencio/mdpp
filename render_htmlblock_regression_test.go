package mdpp

import (
	"strings"
	"testing"
)

// TestRenderFencedCodeAfterHTMLBlock guards a v0.4.0 regression where an HTML
// block (type 6/7, e.g. <aside>) followed by a blank line and a fenced code
// block caused the html_block to over-consume the blank line, so the fence
// opener rendered as literal text and its body as a paragraph (empty
// <pre><code></code></pre>). Fixed in gotreesitter by de-merging the
// html_block terminator blank line; this is the downstream guard.
func TestRenderFencedCodeAfterHTMLBlock(t *testing.T) {
	src := "<aside class=\"note\">trusted html</aside>\n\n```go\nfunc main() {}\n```\n"
	r := NewRenderer(
		WithHighlightCode(true),
		WithUnsafeHTML(true),
		WithHardWraps(true),
	)
	html := r.Render(MustParse([]byte(src)))

	if !strings.Contains(html, `<code class="language-go">`) {
		t.Errorf("expected highlighted go code fence after HTML block, got:\n%s", html)
	}
	if strings.Contains(html, "<pre><code></code></pre>") {
		t.Errorf("fence body was dropped (empty code block) after HTML block:\n%s", html)
	}
	if !strings.Contains(html, `<aside class="note">trusted html</aside>`) {
		t.Errorf("expected the raw HTML aside to be preserved, got:\n%s", html)
	}
}

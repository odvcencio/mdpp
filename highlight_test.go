package mdpp

import (
	"strings"
	"testing"
)

func TestHighlightGoCode(t *testing.T) {
	r := NewRenderer(WithHighlightCode(true))
	html := r.RenderString("```go\npackage main\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```")
	assertContains(t, html, `<pre><code class="language-go">`)
	assertContains(t, html, `<span class="hl-keyword">package</span>`)
	assertContains(t, html, `<span class="hl-keyword">func</span>`)
	assertContains(t, html, `<span class="hl-function">main</span>`)
	assertContains(t, html, `<span class="hl-string">&#34;hi&#34;</span>`)
	assertContains(t, html, "</code></pre>")
}

func TestHighlightGoFenceInfoStringUsesLanguageToken(t *testing.T) {
	r := NewRenderer(WithHighlightCode(true))
	html := r.RenderString("```go title=\"example.go\"\nfunc main() {}\n```")
	assertContains(t, html, `<pre><code class="language-go">`)
	assertContains(t, html, `<span class="hl-keyword">func</span>`)
	assertContains(t, html, `<span class="hl-function">main</span>`)
}

func TestHighlightUnknownLanguage(t *testing.T) {
	r := NewRenderer(WithHighlightCode(true))
	html := r.RenderString("```unknownlang\nhello\n```")
	if !strings.Contains(html, "hello") {
		t.Fatal("expected unhighlighted content")
	}
}

func TestHighlightDisabled(t *testing.T) {
	r := NewRenderer(WithHighlightCode(false))
	html := r.RenderString("```go\nfunc main() {}\n```")
	if strings.Contains(html, `class="hl-`) {
		t.Fatal("expected no highlighting when disabled")
	}
}

func TestHighlightPython(t *testing.T) {
	r := NewRenderer(WithHighlightCode(true))
	html := r.RenderString("```python\ndef hello():\n    pass\n```")
	if !strings.Contains(html, "<span") {
		t.Fatalf("expected highlighted Python, got: %s", html)
	}
}

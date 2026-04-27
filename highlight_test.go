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

func TestHighlightGoTypeConversionDoesNotSwallowString(t *testing.T) {
	r := NewRenderer(WithHighlightCode(true))
	html := r.RenderString("```go\nsrc := []byte(\"package main\\nfunc main() {}\\n\")\nlang := grammars.GoLanguage()\ntree, _ := gotreesitter.NewParser(lang).MustParse(src)\n```")
	assertContains(t, html, `<span class="hl-string">&#34;package main\nfunc main() {}\n&#34;</span>`)
	assertContains(t, html, `<span class="hl-function">GoLanguage</span>`)
	assertContains(t, html, `<span class="hl-function">NewParser</span>`)
	assertContains(t, html, `<span class="hl-function">MustParse</span>`)
	assertNotContains(t, html, `<span class="hl-type">[]byte(&#34;`)
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

func TestHighlightUsesCanonicalClassifierForLanguageAliases(t *testing.T) {
	r := NewRenderer(WithHighlightCode(true))
	html := r.RenderString("```py\nprint(\"x\")\n```")
	assertContains(t, html, `<code class="language-py">`)
	assertContains(t, html, `<span class="hl-function">print</span>`)
	assertContains(t, html, `<span class="hl-string">&#34;x&#34;</span>`)
}

func TestHighlightCacheReusesResult(t *testing.T) {
	const src = "func main() { fmt.Println(\"x\") }"
	first, ok1 := highlightCode("go", src)
	if !ok1 || first == "" {
		t.Fatalf("first highlight failed: ok=%v html=%q", ok1, first)
	}

	key := hlCacheKey("go", src)
	entry, hit := highlightHTMLCache.get(key)
	if !hit {
		t.Fatal("expected cache entry after first highlight")
	}
	if entry.html != first || !entry.ok {
		t.Fatalf("cached entry diverges: ok=%v html=%q", entry.ok, entry.html)
	}

	second, ok2 := highlightCode("go", src)
	if !ok2 || second != first {
		t.Fatalf("second highlight diverged from first:\n  first=%q\n  second=%q", first, second)
	}
}

func TestHighlightCacheNegativeResultReused(t *testing.T) {
	// Unknown language should be cached as a negative result so we don't
	// re-probe grammars.DetectLanguageByName on every render.
	first, ok1 := highlightCode("definitely-not-a-language", "x := 1")
	if ok1 || first != "" {
		t.Fatalf("expected unknown-language miss, got ok=%v html=%q", ok1, first)
	}
	key := hlCacheKey("definitely-not-a-language", "x := 1")
	entry, hit := highlightHTMLCache.get(key)
	if !hit {
		t.Fatal("expected negative cache entry")
	}
	if entry.ok {
		t.Fatal("negative cache entry should have ok=false")
	}
}

func TestHighlightGoClassifierRejectsSelectorReceiver(t *testing.T) {
	// fmt.Println(): the receiver `fmt` is an identifier under selector_expression
	// and must NOT be classified as hl-function. Only the field_identifier
	// `Println` should.
	r := NewRenderer(WithHighlightCode(true))
	html := r.RenderString("```go\nfmt.Println(\"x\")\n```")
	assertContains(t, html, `<span class="hl-function">Println</span>`)
	assertNotContains(t, html, `<span class="hl-function">fmt</span>`)
}

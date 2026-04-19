package mdpp

import (
	"strings"
	"testing"
)

// Security tests verify that user-supplied content never escapes as live
// markup at any render surface — headings, paragraphs, lists, tables,
// blockquotes, admonition titles and bodies, footnote definitions, and
// code blocks. Under the default (safe) renderer, no <script> tag should
// ever survive; under WithUnsafeHTML(true), the admonition title stays
// escaped because title rendering forces unsafeHTML=false.

const xssNeedle = "<script>alert(1)</script>"
const xssEscaped = "&lt;script&gt;alert(1)&lt;/script&gt;"

// assertNoLiveScript asserts html contains no live <script> tag.
func assertNoLiveScript(t *testing.T, html string) {
	t.Helper()
	if strings.Contains(html, xssNeedle) {
		t.Fatalf("live <script> tag survived rendering:\n%s", html)
	}
}

func TestSecurity_ScriptInHeading(t *testing.T) {
	html := NewRenderer().RenderString("## " + xssNeedle + "\n")
	assertNoLiveScript(t, html)
	assertContains(t, html, xssEscaped)
}

func TestSecurity_ScriptInParagraph(t *testing.T) {
	html := NewRenderer().RenderString("body " + xssNeedle + " tail\n")
	assertNoLiveScript(t, html)
}

func TestSecurity_ScriptInListItem(t *testing.T) {
	html := NewRenderer().RenderString("- " + xssNeedle + "\n")
	assertNoLiveScript(t, html)
}

func TestSecurity_ScriptInTableCell(t *testing.T) {
	src := "| col |\n|-----|\n| " + xssNeedle + " |\n"
	html := NewRenderer().RenderString(src)
	assertNoLiveScript(t, html)
}

func TestSecurity_ScriptInBlockquote(t *testing.T) {
	html := NewRenderer().RenderString("> " + xssNeedle + "\n")
	assertNoLiveScript(t, html)
}

func TestSecurity_ScriptInAdmonitionBody(t *testing.T) {
	html := NewRenderer().RenderString("> [!NOTE]\n> " + xssNeedle + "\n")
	assertNoLiveScript(t, html)
}

func TestSecurity_ScriptInAdmonitionTitleUnderUnsafeHTML(t *testing.T) {
	// Even with WithUnsafeHTML(true), the admonition title path forces
	// unsafeHTML=false on the sub-renderer. Verify that contract holds.
	html := NewRenderer(WithUnsafeHTML(true)).RenderString("> [!NOTE] " + xssNeedle + "\n> body\n")
	if strings.Contains(html, `<p class="admonition-title">`+xssNeedle) {
		t.Fatalf("live <script> survived in admonition title under unsafeHTML=true:\n%s", html)
	}
}

func TestSecurity_ScriptInFootnoteDefinition(t *testing.T) {
	src := "Text[^x]\n\n[^x]: " + xssNeedle + "\n"
	html := NewRenderer().RenderString(src)
	assertNoLiveScript(t, html)
}

func TestSecurity_ScriptInCodeBlockAlwaysEscaped(t *testing.T) {
	src := "```html\n" + xssNeedle + "\n```\n"
	html := NewRenderer().RenderString(src)
	// Code blocks always escape < and >, even when a highlighter runs.
	assertNoLiveScript(t, html)
	assertContains(t, html, "&lt;script&gt;")
}

func TestSecurity_ImageSrcEscaped(t *testing.T) {
	// Quote-breaking attempt in image src.
	src := `![alt](x.png" onerror="alert(1))`
	html := NewRenderer().RenderString(src)
	// The onerror= attribute must not survive as an attribute.
	if strings.Contains(html, `onerror="alert(1)"`) {
		t.Fatalf("onerror attribute survived:\n%s", html)
	}
}

func TestSecurity_LinkHrefEscaped(t *testing.T) {
	// Quote-breaking in href.
	src := `[x](https://a.com" onmouseover="alert(1))`
	html := NewRenderer().RenderString(src)
	if strings.Contains(html, `onmouseover="alert(1)"`) {
		t.Fatalf("onmouseover attribute survived:\n%s", html)
	}
}

func TestSecurity_InlineHTMLDefaultEscaped(t *testing.T) {
	for _, payload := range []string{
		`<img src=x onerror=alert(1)>`,
		`<iframe src="javascript:alert(1)"></iframe>`,
		`<svg onload=alert(1)>`,
	} {
		html := NewRenderer().RenderString(payload)
		if strings.Contains(html, payload) {
			t.Fatalf("payload survived under safe HTML: %q\n%s", payload, html)
		}
	}
}

func TestSecurity_RenderIsDeterministic(t *testing.T) {
	// Quick sanity: same input renders the same output across calls.
	src := "# Hello\n\nBody *em* and `code`.\n\n> [!NOTE]\n> note\n"
	r := NewRenderer(WithHeadingIDs(true), WithWrapEmoji(true))
	a := r.RenderString(src)
	b := r.RenderString(src)
	if a != b {
		t.Fatalf("non-deterministic render")
	}
}

package mdpp

import (
	"strings"
	"testing"

	nethtml "golang.org/x/net/html"
)

func TestURLPolicyAdversarialVectors(t *testing.T) {
	tests := []struct {
		name           string
		source         string
		trustedBlocked int
	}{
		{"x1", "[x](javascript:alert(1))", 1},
		{"x2", "[x](JaVaScRiPt:alert(1))", 1},
		{"x3", "[x](java&#115;cript:alert(1))", 0},
		{"x4", "[x](&#x6A;avascript:alert(1))", 0},
		{"x5", "![x](javascript:alert(1))", 1},
		{"x6", "<javascript:alert(1)>\n", 1},
		{"x7", "[x]\n\n[x]: javascript:alert(1)\n", 0},
		{"x8", "<img src=x onerror=alert(1)>\n", 0},
		{"x9", "[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)", 1},
		{"x10", "[x](vbscript:msgbox(1))", 1},
		{"x11", "[x](/u \"a\\\" onmouseover=\\\"alert(1)\")", 0},
		{"x12", "# \\\"<script>alert(1)</script>\n", 0},
		{"x13", "$<img src=x onerror=alert(1)>$\n", 0},
		{"x14", "$$\n<script>alert(1)</script>\n$$\n", 0},
		{"x15", "[[embed:javascript:alert(1)]]\n", 2},
		{"x16", "[[embed:https://evil.example/\\\"><script>alert(1)</script>]]\n", 0},
		{"x17", "```js\\\" onmouseover=\\\"alert(1)\ncode\n```\n", 0},
		{"x18", "```mermaid\n<script>alert(1)</script>\n```\n", 0},
		{"x19", "> [!NOTE] <script>alert(1)</script>\n", 0},
		{"x20", ":::warning <script>alert(1)</script>\nbody\n:::\n", 0},
		{"x21", "x[^a\"><script>]\n\n[^a\"><script>]: note\n", 0},
		{"xss", "<javascript:alert(document.domain)>\n\n[click](<javascript:alert(document.domain)>)\n\n[[embed:javascript:alert(document.domain)]]\n", 3},
		{"sec56-autolink", "<javascript:alert(document.domain)>\n", 1},
		{"sec56-embed", "[[embed:javascript:alert(document.domain)]]\n", 2},
		{"sec56-inline-link", "[x](javascript:alert(1))", 1},
		{"sec56-image", "![x](javascript:alert(1))", 1},
		{"sec56-vbscript", "[x](vbscript:msgbox(1))", 1},
		{"sec56-data-html", "[x](data:text/html;base64,aGVsbG8=)", 1},
		{"obf-mixed-case", "[x](JaVaScRiPt:alert(1))", 1},
		{"obf-leading-space", `<a href=" javascript:alert(1)">x</a>`, 1},
		{"obf-tab", "<a href=\"java\tscript:alert(1)\">x</a>", 1},
		{"obf-entity-letter", `<a href="&#x6A;avascript:alert(1)">x</a>`, 1},
		{"obf-colon", `<a href="javascript&colon;alert(1)">x</a>`, 1},
		{"obf-newline", "<a href=\"java\nscript:alert(1)\">x</a>", 1},
		{"obf-svg-data", "![x](data:image/svg+xml,%3Csvg%3E)", 1},
		{"obf-data-href", "[x](data:image/png;base64,AAAA)", 1},
		{"harmless-percent-scheme", "[x](%6Aavascript:alert(1))", 0},
	}
	if len(tests) != 37 {
		t.Fatalf("URL vector table has %d cases, want 37", len(tests))
	}
	modes := []struct {
		name string
		opts RenderOptions
	}{
		{name: "safe"},
		{name: "sanitize", opts: RenderOptions{UnsafeHTML: true, Sanitize: true}},
		{name: "trusted", opts: RenderOptions{UnsafeHTML: true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range modes {
				t.Run(mode.name, func(t *testing.T) {
					doc := MustParse([]byte(tc.source))
					out, err := Render(doc, mode.opts)
					if err != nil {
						t.Fatal(err)
					}
					got := countBlockedURLAttributes(string(out))
					want := 0
					if mode.name == "trusted" {
						want = tc.trustedBlocked
					}
					if got != want {
						t.Fatalf("blocked URL attributes = %d, want %d; output: %s", got, want, out)
					}
				})
			}
		})
	}
}

func TestDefaultURLPolicyNormalizesBrowserSchemeInput(t *testing.T) {
	tests := []struct {
		name string
		kind URLKind
		raw  string
		want string
		ok   bool
	}{
		{"javascript", URLKindLink, "javascript:alert(1)", "", false},
		{"mixed case", URLKindLink, "JaVaScRiPt:alert(1)", "", false},
		{"leading controls", URLKindLink, " \x00javascript:alert(1) ", "", false},
		{"embedded tab", URLKindLink, "java\tscript:alert(1)", "", false},
		{"embedded newline", URLKindLink, "java\nscript:alert(1)", "", false},
		{"entity letter", URLKindLink, "&#x6A;avascript:alert(1)", "", false},
		{"entity colon", URLKindLink, "javascript&colon;alert(1)", "", false},
		{"vbscript", URLKindLink, "VBSCRIPT:msgbox(1)", "", false},
		{"file", URLKindLink, "file:///tmp/x", "", false},
		{"data html", URLKindLink, "data:text/html,hi", "", false},
		{"svg data image", URLKindImageSrc, "data:image/svg+xml,%3Csvg%3E", "", false},
		{"raster data image", URLKindImageSrc, "data:image/avif;base64,AA", "data:image/avif;base64,AA", true},
		{"raster data href", URLKindLink, "data:image/png;base64,AA", "", false},
		{"hypha scheme", URLKindLink, "hypha://m31labs/page", "hypha://m31labs/page", true},
		{"http", URLKindLink, "http://example.com", "http://example.com", true},
		{"https", URLKindLink, "https://example.com", "https://example.com", true},
		{"mailto", URLKindLink, "mailto:team@example.com", "mailto:team@example.com", true},
		{"tel", URLKindLink, "tel:+12025550123", "tel:+12025550123", true},
		{"raster png image", URLKindImageSrc, "data:image/png;base64,AA", "data:image/png;base64,AA", true},
		{"raster gif image", URLKindImageSrc, "data:image/gif;base64,AA", "data:image/gif;base64,AA", true},
		{"raster jpeg image", URLKindImageSrc, "data:image/jpeg;base64,AA", "data:image/jpeg;base64,AA", true},
		{"raster webp image", URLKindImageSrc, "data:image/webp;base64,AA", "data:image/webp;base64,AA", true},
		{"percent encoded relative", URLKindLink, "%6Aavascript:alert(1)", "%6Aavascript:alert(1)", true},
		{"fragment", URLKindLink, "#section", "#section", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := defaultURLPolicy(tc.kind, tc.raw)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("defaultURLPolicy(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestParsedHTMLURLsAreNotEntityDecodedTwice(t *testing.T) {
	if _, ok := defaultParsedURLPolicy(URLKindLink, "javascript:alert(1)"); ok {
		t.Fatal("parsed script URL was allowed")
	}
	if got, ok := defaultParsedURLPolicy(URLKindLink, "&#x6A;avascript:alert(1)"); !ok || got != "&#x6A;avascript:alert(1)" {
		t.Fatalf("parsed URL was decoded twice: (%q, %v)", got, ok)
	}
	got, err := Render(MustParse([]byte(`<a href="&amp;#x6A;avascript:alert(1)">relative</a>`)), RenderOptions{UnsafeHTML: true, Sanitize: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `href="&amp;#x6A;avascript:alert(1)"`) {
		t.Fatalf("sanitizer changed a browser-relative URL: %s", got)
	}
}

func TestURLPolicyCoversCustomRendererAttributes(t *testing.T) {
	r := NewRenderer(WithNodeRenderer(NodeParagraph, func(_ *Renderer, b *strings.Builder, _ *Node) bool {
		b.WriteString(`<a href="javascript:alert(1)">link</a><img src="vbscript:msgbox(1)" />`)
		b.WriteString(`<img srcset="https://ok.example/a 1x, javascript:alert(1) 2x" />`)
		b.WriteString(`<video poster="file:///tmp/poster"><source src="javascript:alert(1)" /></video>`)
		b.WriteString(`<form action="javascript:alert(1)"><button formaction="vbscript:msgbox(1)">go</button></form>`)
		b.WriteString(`<svg><use xlink:href="javascript:alert(1)"></use></svg>`)
		b.WriteString(`<div data-src="javascript:alert(1)"></div>`)
		return true
	}))
	out := r.RenderString("custom renderer output")
	if got := countBlockedURLAttributes(out); got != 0 {
		t.Fatalf("custom renderer emitted %d blocked URL attributes: %s", got, out)
	}
	for _, want := range []string{`href=""`, `src=""`, `srcset=""`, `poster=""`, `action=""`, `formaction=""`, `xlink:href=""`, `data-src=""`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected filtered attribute %s in %s", want, out)
		}
	}
}

func TestBlockedURLsKeepLinkAndImageText(t *testing.T) {
	source := "[link text](javascript:alert(1))\n\n![image text](vbscript:msgbox(1))\n\n[[embed:javascript:alert(1)]]"
	got := NewRenderer().RenderString(source)
	for _, want := range []string{`<a href="">link text</a>`, `<img src="" alt="image text" />`, `data-src=""`, `>javascript:alert(1)</a>`} {
		if !strings.Contains(got, want) {
			t.Errorf("safe output missing %q: %s", want, got)
		}
	}
}

func TestRasterDataURLsAreAllowedOnlyForImageSources(t *testing.T) {
	source := "![raster](data:image/png;base64,AAAA)\n\n[link](data:image/png;base64,AAAA)"
	got := NewRenderer().RenderString(source)
	if !strings.Contains(got, `src="data:image/png;base64,AAAA"`) {
		t.Fatalf("raster data image source was blocked: %s", got)
	}
	if !strings.Contains(got, `href=""`) || countBlockedURLAttributes(got) != 0 {
		t.Fatalf("raster data URL was allowed outside image src: %s", got)
	}
}

func TestPublicRenderNodeHelpersApplyURLPolicy(t *testing.T) {
	r := NewRenderer()
	link := &Node{Type: NodeLink, Attrs: map[string]string{"href": "javascript:alert(1)"}, Children: []*Node{textNode("text")}}
	var nodeOutput strings.Builder
	r.RenderNodeInto(&nodeOutput, link)
	if countBlockedURLAttributes(nodeOutput.String()) != 0 || !strings.Contains(nodeOutput.String(), `href=""`) {
		t.Fatalf("RenderNodeInto bypassed URL policy: %s", nodeOutput.String())
	}
	var childrenOutput strings.Builder
	r.RenderChildrenInto(&childrenOutput, &Node{Type: NodeDocument, Children: []*Node{link}})
	if countBlockedURLAttributes(childrenOutput.String()) != 0 || !strings.Contains(childrenOutput.String(), `href=""`) {
		t.Fatalf("RenderChildrenInto bypassed URL policy: %s", childrenOutput.String())
	}
}

func TestRenderWithFragmentsAppliesURLPolicy(t *testing.T) {
	doc := MustParse([]byte("[x](javascript:alert(1))\n\n![alt](data:image/svg+xml,%3Csvg%3E)"))
	html, fragments := NewRenderer().RenderWithFragments(doc)
	if countBlockedURLAttributes(html) != 0 {
		t.Fatalf("RenderWithFragments emitted a blocked URL: %s", html)
	}
	for _, fragment := range fragments {
		if got := countBlockedURLAttributes(fragment.HTML); got != 0 {
			t.Errorf("fragment %d emitted %d blocked URL attributes: %s", fragment.Index, got, fragment.HTML)
		}
	}
}

func TestReferenceTOCAndFootnoteURLAttributesPassThroughPolicy(t *testing.T) {
	source := "# Section\n\n[[toc]]\n\n[bad][unsafe]\n\n[^note]: footnote\n\nRef[^note].\n\n[unsafe]: javascript:alert(1)\n"
	got := NewRenderer().RenderString(source)
	if countBlockedURLAttributes(got) != 0 {
		t.Fatalf("renderer emitted a blocked reference, TOC, or footnote URL: %s", got)
	}
	for _, want := range []string{`href="#section"`, `href=""`, `href="#fn-note"`, `href="#fnref-note"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered output missing %q: %s", want, got)
		}
	}
}

func TestURLPolicyCoversContainerAndComponentRendererOutput(t *testing.T) {
	componentRenderer := NewRenderer(WithNodeRenderer(NodeComponent, func(_ *Renderer, b *strings.Builder, _ *Node) bool {
		b.WriteString(`<a href="javascript:alert(1)">component</a>`)
		return true
	}))
	componentHTML := componentRenderer.RenderString("<Card />")
	if countBlockedURLAttributes(componentHTML) != 0 || !strings.Contains(componentHTML, `href=""`) {
		t.Fatalf("component renderer bypassed URL policy: %s", componentHTML)
	}

	containerRenderer := NewRenderer(WithContainerRenderer(func(_ *Node, body string) string {
		return `<video poster="file:///tmp/poster"><source src="javascript:alert(1)"></video>` + body
	}))
	containerHTML := containerRenderer.RenderString(":::custom\nbody\n:::")
	if countBlockedURLAttributes(containerHTML) != 0 || !strings.Contains(containerHTML, `poster=""`) || !strings.Contains(containerHTML, `src=""`) {
		t.Fatalf("container renderer bypassed URL policy: %s", containerHTML)
	}
}

func TestURLPolicyCanOnlyNarrowBuiltInPolicy(t *testing.T) {
	var calls int
	opts := RenderOptions{URLPolicy: func(kind URLKind, raw string) (string, bool) {
		calls++
		if kind == URLKindLink && strings.HasPrefix(raw, "hypha:") {
			return "", false
		}
		return raw, true
	}}
	out, err := Render(MustParse([]byte("[blocked](javascript:alert(1)) and [local](hypha://m31labs/x)")), opts)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("custom URL policy called %d times, want 1 (built-in blocked URLs are rejected first)", calls)
	}
	if countBlockedURLAttributes(string(out)) != 0 || !strings.Contains(string(out), `href=""`) {
		t.Fatalf("custom policy did not empty the URL attribute: %s", out)
	}

	allowUnsafe := NewRenderer(WithURLPolicy(func(_ URLKind, _ string) (string, bool) {
		return "javascript:alert(1)", true
	}))
	if got := countBlockedURLAttributes(allowUnsafe.RenderString("[x](https://example.com)")); got != 0 {
		t.Fatalf("custom policy re-enabled a blocked scheme: %s", allowUnsafe.RenderString("[x](https://example.com)"))
	}
}

func TestSanitizeHTMLAllowListAndGFMTagFilter(t *testing.T) {
	source := `<p onclick="alert(1)"><a href="&#x6A;avascript:alert(1)" onmouseover="alert(1)">link</a><a href="&amp;#x6A;avascript:alert(3)">relative</a><img src="data:image/svg+xml,%3Csvg%3E" onerror="alert(1)"><script>alert(2)</script><style>bad</style><iframe src="https://evil.example">frame</iframe><object>object text</object><embed src="javascript:alert(1)"><form><b>form text</b></form><base href="https://evil.example"><meta http-equiv="refresh"><link rel="stylesheet" href="https://evil.example"><title>title text</title><textarea><img src=x></textarea><b class="kept">safe</b></p>`
	out, err := Render(MustParse([]byte(source)), RenderOptions{UnsafeHTML: true, Sanitize: true})
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, forbidden := range []string{"onclick", "onmouseover", "onerror", "<script", "alert(2)", "<style", "<iframe", "<object", "object text", "<embed", "<form", "form text", "<base", "<meta", "<link", `src="data:image/svg+xml`} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(forbidden)) {
			t.Errorf("sanitized HTML retained %q: %s", forbidden, got)
		}
	}
	for _, want := range []string{`href=""`, `href="&amp;#x6A;avascript:alert(3)"`, `src=""`, `&lt;title>`, `&lt;textarea>`, `<b class="kept">safe</b>`} {
		if !strings.Contains(got, want) {
			t.Errorf("sanitized HTML missing %q: %s", want, got)
		}
	}
	if countBlockedURLAttributes(got) != 0 {
		t.Fatalf("sanitized HTML emitted a blocked URL: %s", got)
	}
}

func TestSanitizeModeAppliesCompleteGFMTagFilter(t *testing.T) {
	for _, tag := range []string{"title", "textarea", "xmp", "noembed", "noframes", "plaintext"} {
		t.Run(tag, func(t *testing.T) {
			source := "<" + tag + "><img src=javascript:alert(1)></" + tag + ">"
			if tag == "plaintext" {
				source = "<plaintext><img src=javascript:alert(1)>"
			}
			got, err := Render(MustParse([]byte(source)), RenderOptions{UnsafeHTML: true, Sanitize: true})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), "&lt;"+tag+">") {
				t.Fatalf("GFM tag filter did not escape <%s: %s", tag, got)
			}
			if strings.Contains(string(got), `<img src="javascript:`) {
				t.Fatalf("sanitizer emitted an unsafe descendant URL: %s", got)
			}
		})
	}
}

func TestSanitizeModeFiltersURLsWhenUnsafeHTMLIsFalse(t *testing.T) {
	got, err := Render(MustParse([]byte("[x](javascript:alert(1))\n\n<script>still escaped</script>")), RenderOptions{Sanitize: true})
	if err != nil {
		t.Fatal(err)
	}
	if countBlockedURLAttributes(string(got)) != 0 || !strings.Contains(string(got), `href=""`) {
		t.Fatalf("Sanitize did not apply URL policy in safe raw-HTML mode: %s", got)
	}
}

func TestTrustedModeKeepsRawHTMLAndUnsafeURLs(t *testing.T) {
	source := "[x](javascript:alert(1))\n\n<script>alert(2)</script>"
	got, err := Render(MustParse([]byte(source)), RenderOptions{UnsafeHTML: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `href="javascript:alert(1"`) || !strings.Contains(string(got), `<script>alert(2)</script>`) {
		t.Fatalf("trusted mode changed existing output: %s", got)
	}
}

func FuzzSafeRenderNeverEmitsBlockedURLAttributes(f *testing.F) {
	for _, seed := range []string{
		`[x](javascript:alert(1))`,
		`![x](data:image/svg+xml,%3Csvg%3E)`,
		`<a href="java&#x73;cript:alert(1)">x</a>`,
		`<img src="data:image/png;base64,AA">`,
		`[x](%6Aavascript:alert(1))`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		// Keep this renderer-property target below the parser's separate 2 KiB
		// segmented-input threshold; parser resource budgets are tested elsewhere.
		if len(source) > 1024 {
			t.Skip()
		}
		out := NewRenderer().RenderString(source)
		if got := countBlockedURLAttributes(out); got != 0 {
			t.Fatalf("safe renderer emitted %d blocked URL attributes for %q: %s", got, source, out)
		}
	})
}

func countBlockedURLAttributes(source string) int {
	z := nethtml.NewTokenizer(strings.NewReader(source))
	count := 0
	for {
		typ := z.Next()
		if typ == nethtml.ErrorToken {
			return count
		}
		if typ != nethtml.StartTagToken && typ != nethtml.SelfClosingTagToken {
			continue
		}
		token := z.Token()
		for _, attr := range token.Attr {
			kind, ok := urlKindForAttribute(token.Data, attr.Key)
			if !ok {
				continue
			}
			if _, allowed := filterURLAttribute(nil, kind, attr.Val); !allowed {
				count++
			}
		}
	}
}

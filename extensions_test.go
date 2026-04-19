package mdpp

import (
	"strings"
	"testing"
)

func assertContains(t *testing.T, html, substr string) {
	t.Helper()
	if !strings.Contains(html, substr) {
		t.Fatalf("expected %q in:\n%s", substr, html)
	}
}

func assertNotContains(t *testing.T, html, substr string) {
	t.Helper()
	if strings.Contains(html, substr) {
		t.Fatalf("did not expect %q in:\n%s", substr, html)
	}
}

// --- Admonitions ---

func TestAdmonitionNote(t *testing.T) {
	html := NewRenderer().RenderString("> [!NOTE]\n> This is a note")
	assertContains(t, html, `class="admonition admonition-note"`)
	assertContains(t, html, `class="admonition-title"`)
	assertContains(t, html, "NOTE")
	assertContains(t, html, "This is a note")
}

func TestAdmonitionWarning(t *testing.T) {
	html := NewRenderer().RenderString("> [!WARNING]\n> Be careful")
	assertContains(t, html, `admonition-warning`)
	assertContains(t, html, "Be careful")
}

func TestAdmonitionCustomTitle(t *testing.T) {
	html := NewRenderer().RenderString("> [!WARNING] Deployment caveat\n> Be careful")
	assertContains(t, html, `admonition-warning`)
	assertContains(t, html, `<p class="admonition-title">Deployment caveat</p>`)
	assertContains(t, html, "Be careful")
	assertNotContains(t, html, "<p>Deployment caveat")
}

func TestAdmonitionCustomTitleRendersEmojiShortcodes(t *testing.T) {
	html := NewRenderer(WithWrapEmoji(true)).RenderString("> [!NOTE] Taking notes :sweat_smile:\n> Body")
	assertContains(t, html, `<p class="admonition-title">Taking notes <span class="emoji" role="img" aria-label="sweat_smile">😅</span></p>`)
	assertContains(t, html, "Body")
}

func TestAdmonitionCustomTitleRendersInlineCode(t *testing.T) {
	html := NewRenderer().RenderString("> [!TIP] Use `mdpp` *carefully*\n> Body")
	assertContains(t, html, `<p class="admonition-title">Use <code>mdpp</code> carefully</p>`)
	assertContains(t, html, "Body")
}

func TestAdmonitionCustomTitleEscapesHTML(t *testing.T) {
	html := NewRenderer().RenderString("> [!NOTE] <script>alert(1)</script>\n> Body")
	assertContains(t, html, `&lt;script&gt;alert(1)&lt;/script&gt;`)
	assertNotContains(t, html, `<script>alert(1)</script>`)
}

func TestAdmonitionCustomTitleEscapesHTMLEvenWhenUnsafeHTMLIsEnabled(t *testing.T) {
	html := NewRenderer(WithUnsafeHTML(true)).RenderString("> [!NOTE] <script>alert(1)</script>\n> <em>Body</em>")
	assertContains(t, html, `&lt;script&gt;alert(1)&lt;/script&gt;`)
	assertContains(t, html, `<em>Body</em>`)
	assertNotContains(t, html, `<p class="admonition-title"><script>alert(1)</script></p>`)
}

func TestAdmonitionAllowsUnquotedBody(t *testing.T) {
	html := NewRenderer().RenderString("> [!NOTE]\nThis belongs to the note.\n\nOutside.")
	assertContains(t, html, `class="admonition admonition-note"`)
	assertContains(t, html, "This belongs to the note.")
	assertNotContains(t, html, "&gt; This belongs")
	assertContains(t, html, "<p>Outside.</p>")
}

func TestAdmonitionCustomTitleAllowsUnquotedBody(t *testing.T) {
	html := NewRenderer().RenderString("> [!TIP] Shortcut\nThis belongs to the tip.\n\nOutside.")
	assertContains(t, html, `<p class="admonition-title">Shortcut</p>`)
	assertContains(t, html, "This belongs to the tip.")
	assertContains(t, html, "<p>Outside.</p>")
}

func TestAdmonitionTip(t *testing.T) {
	html := NewRenderer().RenderString("> [!TIP]\n> A helpful tip")
	assertContains(t, html, `admonition-tip`)
	assertContains(t, html, "A helpful tip")
}

func TestAdmonitionImportant(t *testing.T) {
	html := NewRenderer().RenderString("> [!IMPORTANT]\n> Very important")
	assertContains(t, html, `admonition-important`)
}

func TestAdmonitionCaution(t *testing.T) {
	html := NewRenderer().RenderString("> [!CAUTION]\n> Proceed with caution")
	assertContains(t, html, `admonition-caution`)
}

func TestBlockquoteNotAdmonition(t *testing.T) {
	html := NewRenderer().RenderString("> Just a normal quote")
	assertContains(t, html, "<blockquote>")
	assertNotContains(t, html, "admonition")
}

func TestBlockquoteBracketHeadingIsNotLink(t *testing.T) {
	html := NewRenderer().RenderString("> [Being Defensive on HN... :sweat_smile:]\nLet's just say it was a wake-up call")
	assertContains(t, html, "<blockquote>")
	assertContains(t, html, "<strong>Being Defensive on HN... 😅</strong>")
	assertContains(t, html, "Let&#39;s just say it was a wake-up call")
	assertNotContains(t, html, `<a href="">`)
	assertNotContains(t, html, "[Being Defensive")
}

func TestBlockquoteBracketHeadingStripsQuotedContinuationMarkers(t *testing.T) {
	html := NewRenderer().RenderString("> [Being Defensive on HN... :sweat_smile:]\n> Let's just say it was a wake-up call")
	assertContains(t, html, "<blockquote>")
	assertContains(t, html, "<strong>Being Defensive on HN... 😅</strong>")
	assertContains(t, html, "Let&#39;s just say it was a wake-up call")
	assertNotContains(t, html, "&gt; Let&#39;s")
	assertNotContains(t, html, `<a href="">`)
}

func TestBlockquoteBracketHeadingKeepsApostrophes(t *testing.T) {
	html := NewRenderer().RenderString("> [Why I'm writing this at all]\n> Not to re-litigate the thread.")
	assertContains(t, html, "<blockquote>")
	assertContains(t, html, "<strong>Why I&#39;m writing this at all</strong>")
	assertContains(t, html, "Not to re-litigate the thread.")
	assertNotContains(t, html, "<strong>&#39;</strong>")
	assertNotContains(t, html, `[Why I&#39;m writing this at all]`)
}

func TestAdmonitionTypesEmitSemanticClasses(t *testing.T) {
	for _, tc := range []struct {
		source string
		class  string
		title  string
	}{
		{"> [!NOTE]\n> body", `class="admonition admonition-note"`, "NOTE"},
		{"> [!TIP]\n> body", `class="admonition admonition-tip"`, "TIP"},
		{"> [!IMPORTANT]\n> body", `class="admonition admonition-important"`, "IMPORTANT"},
		{"> [!WARNING]\n> body", `class="admonition admonition-warning"`, "WARNING"},
		{"> [!CAUTION]\n> body", `class="admonition admonition-caution"`, "CAUTION"},
	} {
		html := NewRenderer().RenderString(tc.source)
		assertContains(t, html, tc.class)
		assertContains(t, html, `<p class="admonition-title">`+tc.title+`</p>`)
	}
}

// --- Task Lists ---

func TestTaskListChecked(t *testing.T) {
	html := NewRenderer().RenderString("- [x] Done\n- [ ] Todo")
	assertContains(t, html, `type="checkbox"`)
	assertContains(t, html, `checked`)
	assertContains(t, html, `class="task-list-item"`)
	assertContains(t, html, "Done")
	assertContains(t, html, "Todo")
	assertContains(t, html, `<li class="task-list-item"><input type="checkbox" disabled checked />Done</li>`)
	assertContains(t, html, `<li class="task-list-item"><input type="checkbox" disabled />Todo</li>`)
	assertNotContains(t, html, `<input type="checkbox" disabled checked /><p>`)
}

func TestTaskListUnchecked(t *testing.T) {
	html := NewRenderer().RenderString("- [ ] Not done")
	assertContains(t, html, `class="task-list-item"`)
	assertContains(t, html, `disabled`)
	assertContains(t, html, "Not done")
	assertContains(t, html, `<li class="task-list-item"><input type="checkbox" disabled />Not done</li>`)
}

func TestNormalListNotTask(t *testing.T) {
	html := NewRenderer().RenderString("- Normal item")
	assertContains(t, html, "<li>")
	assertNotContains(t, html, "task-list-item")
}

// --- Footnotes ---

func TestFootnote(t *testing.T) {
	html := NewRenderer().RenderString("Text[^1]\n\n[^1]: Footnote content")
	assertContains(t, html, `class="footnote-ref"`)
	assertContains(t, html, `href="#fn-1"`)
	assertContains(t, html, `id="fnref-1"`)
	assertContains(t, html, "Footnote content")
	assertContains(t, html, `class="footnotes"`)
}

func TestFootnoteMultiple(t *testing.T) {
	html := NewRenderer().RenderString("A[^a] B[^b]\n\n[^a]: First\n\n[^b]: Second")
	assertContains(t, html, `href="#fn-a"`)
	assertContains(t, html, `href="#fn-b"`)
	assertContains(t, html, "First")
	assertContains(t, html, "Second")
}

func TestFootnoteDefinitionMarkdownLink(t *testing.T) {
	html := NewRenderer().RenderString("Text[^repo]\n\n[^repo]: [gotreesitter](https://github.com/odvcencio/gotreesitter)")
	assertContains(t, html, `<a href="https://github.com/odvcencio/gotreesitter">gotreesitter</a>`)
	assertNotContains(t, html, `[gotreesitter](https://github.com/odvcencio/gotreesitter)`)
}

func TestFootnoteAdjacentDefinitionsAndEmptyUnused(t *testing.T) {
	html := NewRenderer().RenderString(strings.Join([]string{
		"One[^one]. Two[^two].",
		"",
		"[^one]: [One](https://example.com/one)",
		"[^two]: [Disaster, but lucky still](https://link-to-article)",
		"[^unused]:",
	}, "\n"))
	assertContains(t, html, `<a href="https://example.com/one">One</a>`)
	assertContains(t, html, `<a href="https://link-to-article">Disaster, but lucky still</a>`)
	assertNotContains(t, html, `id="fn-unused"`)
	assertNotContains(t, html, `3:`)
}

func TestFootnoteHyphenatedID(t *testing.T) {
	html := NewRenderer().RenderString("Text[^note-one]\n\n[^note-one]: Hyphenated")
	assertContains(t, html, `href="#fn-note-one"`)
	assertContains(t, html, `id="fnref-note-one"`)
	assertContains(t, html, "Hyphenated")
}

// --- Math ---

func TestMathInline(t *testing.T) {
	html := NewRenderer().RenderString("The formula $E = mc^{2}$ is famous")
	assertContains(t, html, `class="math-inline"`)
	assertContains(t, html, `<sup>2</sup>`)
}

func TestMathBlock(t *testing.T) {
	html := NewRenderer().RenderString("$$E = mc^{2}$$")
	assertContains(t, html, `class="math-block"`)
	assertContains(t, html, `<sup>2</sup>`)
}

func TestMathFraction(t *testing.T) {
	html := NewRenderer().RenderString(`$$\frac{a}{b}$$`)
	assertContains(t, html, `math-frac-num`)
	assertContains(t, html, `math-frac-den`)
}

func TestMathNumericFraction(t *testing.T) {
	html := NewRenderer().RenderString(`Cold drops by $\frac{574}{245} \approx 2.34$.`)
	assertContains(t, html, `<span class="math-frac-num">574</span>`)
	assertContains(t, html, `<span class="math-frac-den">245</span>`)
	assertContains(t, html, `≈`)
	assertNotContains(t, html, `\frac574245`)
}

func TestMathGreekLetters(t *testing.T) {
	html := NewRenderer().RenderString(`$\alpha + \beta$`)
	assertContains(t, html, "α")
	assertContains(t, html, "β")
}

func TestMathSqrt(t *testing.T) {
	html := NewRenderer().RenderString(`$\sqrt{x}$`)
	assertContains(t, html, "√")
}

func TestMathBlackboardBold(t *testing.T) {
	html := NewRenderer().RenderString(`$\mathbb{R}$`)
	assertContains(t, html, "ℝ")
}

func TestMathNotTriggeredInCode(t *testing.T) {
	// Dollar signs inside code spans should not be treated as math
	html := NewRenderer().RenderString("`$not math$`")
	assertNotContains(t, html, "math-inline")
}

// --- Superscript / Subscript ---

func TestSuperscript(t *testing.T) {
	html := NewRenderer().RenderString("x^2^")
	assertContains(t, html, "<sup>2</sup>")
}

func TestSubscript(t *testing.T) {
	html := NewRenderer().RenderString("H~2~O")
	assertContains(t, html, "<sub>2</sub>")
}

func TestSuperscriptInSentence(t *testing.T) {
	html := NewRenderer().RenderString("The value is x^n^ where n is large")
	assertContains(t, html, "<sup>n</sup>")
	assertContains(t, html, "The value is x")
	assertContains(t, html, " where n is large")
}

func TestSubscriptInSentence(t *testing.T) {
	html := NewRenderer().RenderString("Water is H~2~O")
	assertContains(t, html, "<sub>2</sub>")
	assertContains(t, html, "Water is H")
}

// --- Strikethrough (GFM, should already work via tree-sitter) ---

func TestStrikethrough(t *testing.T) {
	html := NewRenderer().RenderString("~~deleted~~")
	assertContains(t, html, "<del>")
	assertContains(t, html, "deleted")
}

// --- Table of Contents ---

func TestTOCDirectiveEmitsNav(t *testing.T) {
	src := "[[toc]]\n\n## Alpha\n\n## Beta\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `<nav class="mdpp-toc"`)
	assertContains(t, html, `href="#alpha"`)
	assertContains(t, html, `href="#beta"`)
	assertContains(t, html, "Alpha")
	assertContains(t, html, "Beta")
}

func TestTOCDirectiveCaseInsensitive(t *testing.T) {
	html := NewRenderer().RenderString("[[TOC]]\n\n## Alpha\n")
	assertContains(t, html, `<nav class="mdpp-toc"`)
	assertContains(t, html, `href="#alpha"`)
}

func TestTOCDirectiveNestsByHeadingLevel(t *testing.T) {
	src := "[[toc]]\n\n## Alpha\n\n### Alpha One\n\n### Alpha Two\n\n## Beta\n"
	html := NewRenderer().RenderString(src)
	assertContains(t, html, `<nav class="mdpp-toc"`)
	// Alpha and Beta are both H2, so each should appear in the root list;
	// the two H3s should nest inside Alpha's <li>.
	idxAlpha := strings.Index(html, `href="#alpha"`)
	idxAlphaOne := strings.Index(html, `href="#alpha-one"`)
	idxBeta := strings.Index(html, `href="#beta"`)
	if !(idxAlpha < idxAlphaOne && idxAlphaOne < idxBeta) {
		t.Fatalf("expected alpha < alpha-one < beta in TOC, got:\n%s", html)
	}
	// There should be a nested <ul> for the H3s.
	assertContains(t, html, "<ul>")
}

func TestTOCDirectiveEmptyWithNoHeadings(t *testing.T) {
	html := NewRenderer().RenderString("[[toc]]\n\nJust prose.\n")
	assertContains(t, html, `<nav class="mdpp-toc"`)
	assertNotContains(t, html, "<ul>")
}

func TestTOCDirectiveInlineIsLiteral(t *testing.T) {
	// An inline occurrence (not on its own line) must remain literal text.
	html := NewRenderer().RenderString("See [[toc]] below.\n\n## Alpha\n")
	assertNotContains(t, html, `<nav class="mdpp-toc"`)
}

func TestTOCDirectiveIgnoredInCodeFence(t *testing.T) {
	src := "```\n[[toc]]\n```\n\n## Alpha\n"
	html := NewRenderer().RenderString(src)
	assertNotContains(t, html, `<nav class="mdpp-toc"`)
	assertContains(t, html, "[[toc]]")
}

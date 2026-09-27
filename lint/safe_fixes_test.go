package lint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/mdpp"
	mdppfmt "m31labs.dev/mdpp/fmt"
)

var lintFixSafetyFixtures = [][]byte{
	[]byte("---\ntitle: Safe fix fixture\n---\n\nA two-space hard break  \nkeeps its line break.\nA backslash hard break\\\nkeeps its line break too.\nAn ordinary trailing space is removed. \n\nInline `https://code.example.test`suffix and [a destination](https://destination.example.test).\n\n```Go\nfenced code with trailing spaces  \n[foo]: https://fenced.example.test\nhttps://fenced.example.test/code\n```\n\n$$\nmath with trailing spaces  \nhttps://math.example.test\n$$\n\n<div>raw HTML with trailing spaces  \nhttps://html.example.test</div>\n\n    indented code with trailing spaces  \n    [bar]: https://indented.example.test\n    https://indented.example.test/code\n\n[stale]: https://unused.example.test\n"),
	[]byte("Text [guide][guide].\n\n[guide]: https://example.test/guide\n"),
	[]byte("---\ntitle: Protected reference lookalikes\n[frontmatter]: https://frontmatter.example.test\n---\n\n$$\n[math]: https://math.example.test\n$$\n\n<div>\n[html]: https://html.example.test\n</div>\n"),
	[]byte("- a\n- b\n\n  [ref]: /url\n- d\n"),
	[]byte("* 0\n  "),
	[]byte("* 0  \n  "),
	[]byte("* 0 \n\n"),
	[]byte("[0]:0"),
	[]byte("[0 \n "),
	[]byte("* 0\n \n0"),
	[]byte("- a\n- b\n\n  \nref]: /\nr|\n- "),
	[]byte("0\x00[0]:00"),
	[]byte("0\x00 \n0"),
	[]byte("*\n\x00\x00\x00\n \n0"),
	[]byte("0*\r \n0"),
	[]byte("A|\n- \n00"),
}

func TestLintFixesPreserveMeaningAndStayOutsideProtectedRanges(t *testing.T) {
	for i, src := range lintFixSafetyFixtures {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			fixed := applyAllLintFixes(t, src)
			assertLintFixMeaning(t, src, fixed)
		})
	}
}

func TestLintFixesPreserveCommonMarkExamples(t *testing.T) {
	path := filepath.Join("..", "testdata", "spec", "commonmark-0.31.2.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("CommonMark examples are not available in this checkout")
		}
		t.Fatalf("read CommonMark examples: %v", err)
	}
	var examples []struct {
		Example  int    `json:"example"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatalf("decode CommonMark examples: %v", err)
	}
	for _, example := range examples {
		example := example
		t.Run(fmt.Sprintf("example_%d", example.Example), func(t *testing.T) {
			src := []byte(example.Markdown)
			fixed := applyAllLintFixes(t, src)
			assertLintFixMeaning(t, src, fixed)
		})
	}
}

func TestLintFixesPreserveFormatterMeaningFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "fmt", "testdata", "meaning", "*.md"))
	if err != nil {
		t.Fatalf("list formatter meaning fixtures: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("formatter meaning fixtures are not available in this checkout")
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read formatter meaning fixture: %v", err)
			}
			fixed := applyAllLintFixes(t, src)
			if !mdppfmt.PreservesMeaning(src, fixed) {
				t.Fatalf("lint fixes changed formatter meaning for %s", path)
			}
		})
	}
}

func TestLintRawLineRulesSkipProtectedRanges(t *testing.T) {
	src := []byte("---\nmdpp: 9.9  \n---\n\nplain trailing space \n\nA two-space hard break  \nnext line.\n\\\nnext line after backslash.\n\n`inline  https://span.example.test *x* _y_`\n\n```Go\nfenced trailing spaces  \n[unused]: https://fenced.example.test\nhttps://fenced.example.test\n* one\n- two\n**x** _y_\n|---|---|\n| x | y |\n\n\n```\n\n$$\nmath trailing spaces  \nhttps://math.example.test\n$$\n\n<div>HTML trailing spaces  \nhttps://html.example.test</div>\n\n    indented trailing spaces  \n    [indented]: https://indented.example.test\n    https://indented.example.test\n    * one\n    - two\n    **x** _y_\n")
	diags := Lint(mdpp.MustParse(src))
	var md009 int
	for _, diag := range diags {
		switch diag.Code {
		case "MD009":
			md009++
			if diag.Range.StartLine != 5 {
				t.Fatalf("MD009 should only flag the ordinary trailing space, got line %d: %#v", diag.Range.StartLine, diags)
			}
		case "MD004", "MD012", "MD034", "MD049", "MDPP105", "MDPP201", "MDPP203", "MDPP300":
			t.Fatalf("raw-line rule %s should skip protected AST ranges: %#v", diag.Code, diags)
		}
	}
	if md009 != 1 {
		t.Fatalf("expected one ordinary MD009 finding, got %d: %#v", md009, diags)
	}
}

func TestLintMD034SkipsInlineCodeAndLinkDestinations(t *testing.T) {
	src := "`https://code.example.test`suffix [label](https://destination.example.test) <https://autolink.example.test> https://plain.example.test\n"
	diags := Lint(mdpp.MustParse([]byte(src)))
	var bareURLs []Diagnostic
	for _, diag := range diags {
		if diag.Code == "MD034" {
			bareURLs = append(bareURLs, diag)
		}
	}
	wantCol := strings.Index(src, "https://plain.example.test") + 1
	if len(bareURLs) != 1 || bareURLs[0].Range.StartCol != wantCol {
		t.Fatalf("expected only the prose URL to trigger MD034, got %#v", bareURLs)
	}
}

func TestLinkReferenceDefinitionsComeFromAST(t *testing.T) {
	doc := mdpp.MustParse([]byte("[Guide]: https://example.test/guide\n\nRead [guide][guide].\n"))
	defs := doc.Root.Find(mdpp.NodeLinkReferenceDefinition)
	if len(defs) != 1 || defs[0].Attr("label") != "guide" || defs[0].Attr("href") != "https://example.test/guide" {
		t.Fatalf("expected parsed reference definition node, got %#v", defs)
	}
	diags := Lint(doc)
	if diag := findLintCode(diags, "MDPP105"); diag != nil {
		t.Fatalf("used AST reference should not be reported as unused: %#v", diag)
	}
	if diag := findLintCode(diags, "MDPP106"); diag != nil {
		t.Fatalf("defined AST reference should not be reported missing: %#v", diag)
	}
}

func TestUnusedListReferenceDefinitionHasNoUnsafeFix(t *testing.T) {
	src := []byte("- a\n- b\n\n  [ref]: /url\n- d\n")
	doc := mdpp.MustParse(src)
	var found *Diagnostic
	diags := Lint(doc)
	for i := range diags {
		if diags[i].Code == "MDPP105" {
			found = &diags[i]
			break
		}
	}
	if found == nil || found.Fix != nil {
		t.Fatalf("expected unused nested reference to be reported without a fix, got %#v", found)
	}
	assertLintFixMeaning(t, src, applyAllLintFixes(t, src))
}

func TestUnusedEOFReferenceDefinitionHasNoUnsafeFix(t *testing.T) {
	src := []byte("0\x00[0]:00")
	for _, diag := range Lint(mdpp.MustParse(src)) {
		if diag.Code == "MDPP105" {
			if diag.Fix != nil {
				t.Fatalf("terminal reference definition should not have an automatic fix, got %#v", diag.Fix)
			}
			assertLintFixMeaning(t, src, applyAllLintFixes(t, src))
			return
		}
	}
	t.Fatal("expected unused terminal reference diagnostic")
}

func TestUnusedEOFReferenceDefinitionRangeIncludesLastByte(t *testing.T) {
	src := []byte("[ref]: /url")
	diag := findLintCode(Lint(mdpp.MustParse(src)), "MDPP105")
	if diag == nil {
		t.Fatal("expected unused reference diagnostic")
	}
	if diag.Range.StartByte != 0 || diag.Range.EndByte != len(src) {
		t.Fatalf("expected EOF range to include the entire definition, got %#v for %q", diag.Range, src)
	}
	if diag.Fix != nil {
		t.Fatalf("terminal reference definition should not have an automatic fix, got %#v", diag.Fix)
	}
}

func TestLintWithParserRecoveryOffersNoFixes(t *testing.T) {
	src := []byte("0\x00 \n0")
	doc := mdpp.MustParse(src)
	if len(doc.Diagnostics()) == 0 {
		t.Fatal("expected parser recovery diagnostic")
	}
	for _, diag := range Lint(doc) {
		if diag.Fix != nil {
			t.Fatalf("parser recovery source should not receive automatic fixes, got %#v", diag)
		}
	}
	assertLintFixMeaning(t, src, applyAllLintFixes(t, src))
}

func TestLintWithNormalizedLineEndingsOffersNoFixes(t *testing.T) {
	src := []byte("Text with trailing space \r\nNext.\r\n")
	doc := mdpp.MustParse(src)
	if !doc.SourceHadCarriageReturns() {
		t.Fatal("expected source line endings to be normalized")
	}
	for _, diag := range Lint(doc) {
		if diag.Fix != nil {
			t.Fatalf("normalized source should not receive byte-offset fixes, got %#v", diag.Fix)
		}
	}
	assertLintFixMeaning(t, src, applyAllLintFixes(t, src))
}

func TestLintTrailingWhitespaceAtEOFHasNoUnsafeFix(t *testing.T) {
	src := []byte("* 0\n  ")
	diags := Lint(mdpp.MustParse(src))
	for _, diag := range diags {
		if diag.Code == "MD009" {
			if diag.Fix != nil {
				t.Fatalf("expected trailing whitespace at EOF to have no fix, got %#v", diag.Fix)
			}
			assertLintFixMeaning(t, src, applyAllLintFixes(t, src))
			return
		}
	}
	t.Fatalf("expected MD009 for trailing whitespace at EOF, got %#v", diags)
}

func TestLintMD009PreservesNestedHardBreak(t *testing.T) {
	src := []byte("* 0  \n  ")
	diags := Lint(mdpp.MustParse(src))
	for _, diag := range diags {
		if diag.Code == "MD009" && diag.Range.StartLine == 1 {
			t.Fatalf("hard-break marker was flagged by MD009: %#v", diag)
		}
	}
	assertLintFixMeaning(t, src, applyAllLintFixes(t, src))
}

func TestLintMD009DoesNotFixRenderedTrailingTextInList(t *testing.T) {
	src := []byte("* 0 \n\n")
	for _, diag := range Lint(mdpp.MustParse(src)) {
		if diag.Code == "MD009" {
			if diag.Fix != nil {
				t.Fatalf("list trailing text should not have an automatic fix, got %#v", diag.Fix)
			}
			assertLintFixMeaning(t, src, applyAllLintFixes(t, src))
			return
		}
	}
	t.Fatal("expected MD009 for trailing text in list")
}

func TestLintMD009DoesNotFixWhitespaceRenderedAsText(t *testing.T) {
	src := []byte("[0 \n ")
	diags := Lint(mdpp.MustParse(src))
	for _, diag := range diags {
		if diag.Code == "MD009" && diag.Range.StartLine == 1 {
			if diag.Fix != nil {
				t.Fatalf("rendered trailing text should not have an automatic fix, got %#v", diag.Fix)
			}
			assertLintFixMeaning(t, src, applyAllLintFixes(t, src))
			return
		}
	}
	t.Fatalf("expected MD009 for trailing text, got %#v", diags)
}

func TestLintMD009DoesNotFixWhitespaceInsideSoftBreak(t *testing.T) {
	src := []byte("* 0\n \n0")
	diags := Lint(mdpp.MustParse(src))
	for _, diag := range diags {
		if diag.Code == "MD009" && diag.Range.StartLine == 2 {
			if diag.Fix != nil {
				t.Fatalf("soft-break whitespace should not have an automatic fix, got %#v", diag.Fix)
			}
			assertLintFixMeaning(t, src, applyAllLintFixes(t, src))
			return
		}
	}
	t.Fatalf("expected MD009 for soft-break whitespace, got %#v", diags)
}

func FuzzLintFixesPreserveMeaning(f *testing.F) {
	for _, src := range lintFixSafetyFixtures {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 16_384 {
			t.Skip()
		}
		doc, err := mdpp.Parse(src)
		if err != nil || doc == nil || doc.Root == nil {
			t.Skip()
		}
		fixed := applyAllLintFixes(t, src)
		assertLintFixMeaning(t, src, fixed)
	})
}

func applyAllLintFixes(t *testing.T, src []byte) []byte {
	t.Helper()
	doc, err := mdpp.Parse(src)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}
	var edits []TextEdit
	for _, diag := range Lint(doc) {
		if diag.Fix != nil {
			edits = append(edits, *diag.Fix)
		}
	}
	assertFixesOutsideProtectedRanges(t, doc, edits)
	return applyTextEdits(t, src, edits...)
}

func assertFixesOutsideProtectedRanges(t *testing.T, doc *mdpp.Document, edits []TextEdit) {
	t.Helper()
	var protected []mdpp.Range
	doc.Root.Walk(func(n *mdpp.Node) bool {
		switch n.Type {
		case mdpp.NodeCodeBlock, mdpp.NodeCodeSpan, mdpp.NodeDiagram, mdpp.NodeMathInline, mdpp.NodeMathBlock,
			mdpp.NodeHTMLBlock, mdpp.NodeHTMLInline, mdpp.NodeFrontmatter, mdpp.NodeAutoEmbed:
			protected = append(protected, n.Range)
		}
		return true
	})
	for _, edit := range edits {
		for _, r := range protected {
			if edit.Range.StartByte < r.EndByte && edit.Range.EndByte > r.StartByte {
				t.Fatalf("lint fix overlaps protected source: fix=%#v range=%#v", edit.Range, r)
			}
		}
	}
}

func assertLintFixMeaning(t *testing.T, before, after []byte) {
	t.Helper()
	if !mdppfmt.PreservesMeaning(before, after) {
		t.Fatalf("lint fixes changed rendered meaning\nbefore: %q\nafter:  %q", before, after)
	}
}

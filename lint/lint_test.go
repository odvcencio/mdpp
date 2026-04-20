package lint

import (
	"testing"

	"github.com/odvcencio/mdpp"
)

func TestLintUndefinedFootnote(t *testing.T) {
	doc := mdpp.MustParse([]byte("Text[^missing].\n"))
	diags := Lint(doc)
	assertLintCode(t, diags, "MDPP100")
}

func TestLintDuplicateHeadingIncludesRelatedInfo(t *testing.T) {
	doc := mdpp.MustParse([]byte("## Results\n\n## Results\n"))
	diags := Lint(doc)
	diag := findLintCode(diags, "MDPP103")
	if diag == nil {
		t.Fatalf("expected MDPP103, got %#v", diags)
	}
	if len(diag.Related) != 1 || diag.Related[0].Range.StartLine != 1 {
		t.Fatalf("expected related location for first heading, got %#v", diag.Related)
	}
}

func TestLintDirectives(t *testing.T) {
	doc := mdpp.MustParse([]byte("[[toc]]\n\n[[toc]]\n\n[[embed:https://example.invalid/video]]\n"))
	diags := Lint(doc)
	assertLintCode(t, diags, "MDPP108")
	assertLintCode(t, diags, "MDPP109")
	assertLintCode(t, diags, "MDPP110")
}

func TestLintAllowsDirectMediaGenericEmbed(t *testing.T) {
	doc := mdpp.MustParse([]byte("[[embed:https://media.example.com/demo.mp4]]\n"))
	diags := Lint(doc)
	if findLintCode(diags, "MDPP110") != nil {
		t.Fatalf("direct media fallback should not warn as an unknown provider: %#v", diags)
	}
}

func TestLintSuppressionNextLine(t *testing.T) {
	doc := mdpp.MustParse([]byte("<!-- mdpp-disable-next-line MD034 -->\nhttp://example.com\n"))
	diags := Lint(doc)
	if diag := findLintCode(diags, "MD034"); diag != nil {
		t.Fatalf("expected MD034 to be suppressed, got %#v", diags)
	}
}

func TestLintIgnoresBareURLInsideCode(t *testing.T) {
	doc := mdpp.MustParse([]byte("```text\nhttp://example.com\n_maybe_\n```\n\nhttp://example.org\n"))
	diags := Lint(doc)
	count := 0
	for _, diag := range diags {
		if diag.Code == "MD034" {
			count++
			if diag.Range.StartLine != 6 {
				t.Fatalf("expected bare URL diagnostic on prose line, got line %d", diag.Range.StartLine)
			}
		}
		if diag.Code == "MD049" {
			t.Fatalf("did not expect emphasis style diagnostic from code fence: %#v", diags)
		}
	}
	if count != 1 {
		t.Fatalf("expected one MD034 diagnostic, got %d: %#v", count, diags)
	}
}

func TestLintFixesTrailingWhitespaceFenceLanguageAndUnusedRef(t *testing.T) {
	doc := mdpp.MustParse([]byte("text  \n\n```Go\nx\n```\n\n[stale]: https://example.com\n"))
	diags := Lint(doc)

	trailing := findLintCode(diags, "MD009")
	if trailing == nil || trailing.Fix == nil || trailing.Fix.NewText != "" {
		t.Fatalf("expected MD009 delete fix, got %#v", trailing)
	}

	fence := findLintCode(diags, "MDPP300")
	if fence == nil || fence.Fix == nil || fence.Fix.NewText != "go" {
		t.Fatalf("expected MDPP300 lowercase fix, got %#v", fence)
	}

	unused := findLintCode(diags, "MDPP105")
	if unused == nil || unused.Fix == nil || unused.Fix.Range.EndLine != 8 {
		t.Fatalf("expected MDPP105 line delete fix, got %#v", unused)
	}
}

func TestLintAutolinkNeedsDescriptiveText(t *testing.T) {
	doc := mdpp.MustParse([]byte("<https://example.com>\n"))
	diags := Lint(doc)
	assertLintCode(t, diags, "MDPP201")
	if findLintCode(diags, "MD034") != nil {
		t.Fatalf("did not expect MD034 for autolink, got %#v", diags)
	}
}

func TestLintReferenceDefinitionURLIsStructured(t *testing.T) {
	doc := mdpp.MustParse([]byte("Read [the guide][guide] and [Shortcut].\n\n[guide]: https://example.com/guide\n[shortcut]: https://example.com/shortcut\n"))
	diags := Lint(doc)
	if findLintCode(diags, "MD034") != nil {
		t.Fatalf("reference definition URL should not be a bare URL diagnostic: %#v", diags)
	}
	if findLintCode(diags, "MDPP105") != nil {
		t.Fatalf("reference definitions used by full and shortcut links should not be unused: %#v", diags)
	}
}

func assertLintCode(t *testing.T, diags []Diagnostic, code string) {
	t.Helper()
	if findLintCode(diags, code) == nil {
		t.Fatalf("expected %s, got %#v", code, diags)
	}
}

func findLintCode(diags []Diagnostic, code string) *Diagnostic {
	for i := range diags {
		if diags[i].Code == code {
			return &diags[i]
		}
	}
	return nil
}

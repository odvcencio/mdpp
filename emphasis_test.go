package mdpp

import (
	"strings"
	"testing"
)

func TestCommonMarkEmphasisDelimiterProcessing(t *testing.T) {
	tests := []struct {
		markdown string
		want     string
	}{
		{"***foo***", "<p><em><strong>foo</strong></em></p>\n"},
		{"*foo **bar***", "<p><em>foo <strong>bar</strong></em></p>\n"},
		{"_foo_bar_", "<p><em>foo_bar</em></p>\n"},
		{"foo_bar_baz", "<p>foo_bar_baz</p>\n"},
		{"foo***bar***baz", "<p>foo<em><strong>bar</strong></em>baz</p>\n"},
		{"a\u00a0*κόσμος*\u00a0b", "<p>a\u00a0<em>κόσμος</em>\u00a0b</p>\n"},
		{"*a**b*", "<p><em>a**b</em></p>\n"},
	}
	for _, tt := range tests {
		t.Run(tt.markdown, func(t *testing.T) {
			got := NewRenderer().RenderString(tt.markdown)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("RenderString(%q) = %q, want containing %q", tt.markdown, got, tt.want)
			}
		})
	}
}

// Regression tests for two review findings: a closer must not pair across an
// unresolved delimiter span (CommonMark 0.31.2 example 470), and a hard break
// inside emphasis must survive (example 638).
func TestCommonMarkEmphasisRegressions(t *testing.T) {
	for _, tt := range []struct{ markdown, want string }{
		{"*foo __bar *baz bim__ bam*\n", "<p><em>foo <strong>bar *baz bim</strong> bam</em></p>\n"},
		{"*foo  \nbar*\n", "<p><em>foo<br />\nbar</em></p>\n"},
	} {
		doc, err := Parse([]byte(tt.markdown))
		if err != nil {
			t.Fatal(err)
		}
		got, err := Render(doc, RenderOptions{UnsafeHTML: true, HeadingIDs: false})
		if err != nil {
			t.Fatal(err)
		}
		if normalizeSpecHTML(string(got)) != normalizeSpecHTML(tt.want) {
			t.Errorf("Render(%q) = %q, want %q", tt.markdown, got, tt.want)
		}
	}
}

// Newlines inside emphasis must become soft breaks (or hard wraps) like
// newlines elsewhere in the paragraph.
func TestEmphasisNewlinesFollowHardWraps(t *testing.T) {
	doc, err := Parse([]byte("*foo\nbar*\n"))
	if err != nil {
		t.Fatal(err)
	}
	soft, err := Render(doc, RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(soft), "<em>foo\nbar</em>") {
		t.Fatalf("soft render = %q", soft)
	}
	hard, err := Render(doc, RenderOptions{HardWraps: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hard), "<em>foo<br") || !strings.Contains(string(hard), "bar</em>") {
		t.Fatalf("hard-wrap render = %q, want a <br> inside the emphasis", hard)
	}
}

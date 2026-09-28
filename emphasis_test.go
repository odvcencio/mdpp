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

package lint

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/mdpp"
)

func TestLintImplementedRulesPositiveCases(t *testing.T) {
	tests := []struct {
		code string
		src  string
	}{
		{code: "MD004", src: "- one\n* two\n"},
		{code: "MD009", src: "text  \n"},
		{code: "MD012", src: "alpha\n\n\n\nbeta\n"},
		{code: "MD034", src: "http://example.com\n"},
		{code: "MD045", src: "![](image.png)\n"},
		{code: "MD049", src: "*one* and _two_\n"},
		{code: "MDPP100", src: "Text[^missing].\n"},
		{code: "MDPP101", src: "[^note]: definition only\n"},
		{code: "MDPP102", src: "[jump](#missing)\n"},
		{code: "MDPP103", src: "# Results\n\n## Results\n"},
		{code: "MDPP104", src: ":::unknown\nbody\n:::\n"},
		{code: "MDPP105", src: "[ref]: https://example.com\n"},
		{code: "MDPP106", src: "[x][missing]\n"},
		{code: "MDPP107", src: "---\nmdpp: 9.9\n---\n# Content\n"},
		{code: "MDPP108", src: "[[toc]]\n\n[[toc]]\n"},
		{code: "MDPP109", src: "[[toc]]\n"},
		{code: "MDPP110", src: "[[embed:https://example.invalid/video]]\n"},
		{code: "MDPP111", src: "[[embed:not-a-url]]\n"},
		{code: "MDPP200", src: "# A\n\n### B\n"},
		{code: "MDPP201", src: "<https://example.com>\n"},
		{code: "MDPP202", src: "[](/x)\n"},
		{code: "MDPP203", src: "|---|---|\n| a | b |\n"},
		{code: "MDPP300", src: "```Go\nx\n```\n"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.code, func(t *testing.T) {
			diags := Lint(mdpp.MustParse([]byte(tc.src)))
			assertLintCode(t, diags, tc.code)
		})
	}
}

func TestLintConformanceCorpusClean(t *testing.T) {
	root := filepath.Join("..", "examples", "conformance")
	var inputs []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d != nil && d.IsDir() {
			return nil
		}
		if filepath.Base(path) == "input.md" {
			inputs = append(inputs, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk corpus: %v", err)
	}
	sort.Strings(inputs)
	for _, path := range inputs {
		path := path
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			diags := Lint(mdpp.MustParse(src))
			if len(diags) != 0 {
				t.Fatalf("%s: expected no diagnostics, got %#v", path, diags)
			}
		})
	}
}

func TestLintFixesApplyCleanly(t *testing.T) {
	tests := []struct {
		code string
		src  string
	}{
		{code: "MD009", src: "text  \n"},
		{code: "MDPP105", src: "[ref]: https://example.com\n"},
		{code: "MDPP300", src: "```Go\nx\n```\n"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.code, func(t *testing.T) {
			diags := Lint(mdpp.MustParse([]byte(tc.src)))
			diag := findLintCode(diags, tc.code)
			if diag == nil {
				t.Fatalf("expected %s in %#v", tc.code, diags)
			}
			if diag.Fix == nil {
				t.Fatalf("expected fix for %s, got %#v", tc.code, diag)
			}
			fixed := applyTextEdits(t, []byte(tc.src), *diag.Fix)
			diagsAfter := Lint(mdpp.MustParse(fixed))
			if len(diagsAfter) != 0 {
				t.Fatalf("expected fixes to clean source, got %#v", diagsAfter)
			}
		})
	}
}

func TestLintPerformance100kChars(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping perf test in short mode")
	}
	doc := mdpp.MustParse([]byte(strings.Repeat("a", 100_000)))
	start := time.Now()
	diags := Lint(doc)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics on plain text, got %#v", diags)
	}
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Fatalf("linting 100k chars took %s, want <= 50ms", elapsed)
	}
}

func applyTextEdits(t *testing.T, src []byte, edits ...TextEdit) []byte {
	t.Helper()
	if len(edits) == 0 {
		return append([]byte(nil), src...)
	}
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].Range.StartByte == edits[j].Range.StartByte {
			return edits[i].Range.EndByte > edits[j].Range.EndByte
		}
		return edits[i].Range.StartByte > edits[j].Range.StartByte
	})
	out := append([]byte(nil), src...)
	for _, edit := range edits {
		start := edit.Range.StartByte
		end := edit.Range.EndByte
		if start < 0 || end < start || end > len(out) {
			t.Fatalf("invalid edit range %+v for source len %d", edit.Range, len(out))
		}
		next := make([]byte, 0, len(out)-(end-start)+len(edit.NewText))
		next = append(next, out[:start]...)
		next = append(next, edit.NewText...)
		next = append(next, out[end:]...)
		out = next
	}
	return out
}

package fmt

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/odvcencio/mdpp"
)

func TestFormatConformanceCorpusIdempotentAndStable(t *testing.T) {
	root := filepath.Join("..", "examples", "conformance")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read conformance corpus: %v", err)
	}

	var cases []string
	for _, entry := range entries {
		if entry.IsDir() {
			cases = append(cases, entry.Name())
		}
	}
	sort.Strings(cases)

	if len(cases) < 30 {
		t.Fatalf("conformance corpus has %d cases, want at least 30", len(cases))
	}

	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(root, name)
			inputPath := filepath.Join(dir, "input.md")
			expectedPath := filepath.Join(dir, "expected.html")

			input, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read input: %v", err)
			}
			expectedHTML, err := os.ReadFile(expectedPath)
			if err != nil {
				t.Fatalf("read expected HTML: %v", err)
			}

			formatted1, err := Format(input)
			if err != nil {
				t.Fatalf("format input: %v", err)
			}
			formatted2, err := Format(formatted1)
			if err != nil {
				t.Fatalf("format formatted input: %v", err)
			}
			if !bytes.Equal(formatted1, formatted2) {
				t.Fatalf("format not idempotent\nfirst:\n%s\nsecond:\n%s", formatted1, formatted2)
			}

			renderer := mdpp.NewRenderer()
			originalHTML := renderer.RenderString(string(input))
			formattedHTML := renderer.RenderString(string(formatted1))
			if originalHTML != formattedHTML {
				t.Fatalf("rendered HTML changed after formatting\noriginal:\n%s\nformatted:\n%s", originalHTML, formattedHTML)
			}
			if formattedHTML != string(expectedHTML) {
				t.Fatalf("rendered HTML mismatch with conformance expectation\nexpected:\n%s\nformatted:\n%s", expectedHTML, formattedHTML)
			}
		})
	}
}

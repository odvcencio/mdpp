package mdpp

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

var updateConformance = flag.Bool("update", false, "update examples/conformance expected HTML files")

func TestConformanceCorpus(t *testing.T) {
	root := filepath.Join("examples", "conformance")
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
			readmePath := filepath.Join(dir, "README.md")

			requireRegularFile(t, inputPath)
			requireRegularFile(t, readmePath)

			input, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read input: %v", err)
			}
			got, err := Render(MustParse(input), RenderOptions{})
			if err != nil {
				t.Fatalf("render: %v", err)
			}

			if *updateConformance {
				if err := os.WriteFile(expectedPath, got, 0o644); err != nil {
					t.Fatalf("update expected: %v", err)
				}
			}

			requireRegularFile(t, expectedPath)
			expected, err := os.ReadFile(expectedPath)
			if err != nil {
				t.Fatalf("read expected: %v", err)
			}
			if !bytes.Equal(got, expected) {
				t.Fatalf("rendered HTML mismatch\nexpected:\n%s\nactual:\n%s", expected, got)
			}
		})
	}
}

func requireRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("missing required file %s: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("required file %s is a directory", path)
	}
}

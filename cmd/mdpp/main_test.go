package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/mdpp"
)

func TestRenderWritesHTMLToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"render"}, strings.NewReader("# Hello\n"), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if got, want := stdout.String(), "<h1 id=\"hello\">Hello</h1>\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRenderWritesOutputFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.html")
	var stdout, stderr bytes.Buffer
	code := run([]string{"render", "-o", out}, strings.NewReader("# Hello\n"), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(`<h1 id="hello">Hello</h1>`)) {
		t.Fatalf("output file missing rendered heading: %s", got)
	}
}

func TestParseJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"parse", "--json"}, strings.NewReader("# Hello\n"), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	var payload struct {
		File string `json:"file"`
		Root struct {
			Type string `json:"type"`
		} `json:"root"`
		Headings []mdpp.Heading `json:"headings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v\n%s", err, stdout.String())
	}
	if payload.File != "<stdin>" || payload.Root.Type != "Document" || len(payload.Headings) != 1 || payload.Headings[0].Text != "Hello" {
		t.Fatalf("unexpected parse payload: %+v", payload)
	}
}

func TestFormatStdoutIsStableAcrossRepeatedPasses(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt"}, strings.NewReader("Title\n=====\n"), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if got, want := stdout.String(), "# Title\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"fmt"}, strings.NewReader("# Title\n"), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if got, want := stdout.String(), "# Title\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	canonical := stdout.String()
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"format"}, strings.NewReader(canonical), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("format alias exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.String() != canonical {
		t.Fatalf("format alias stdout = %q, want %q", stdout.String(), canonical)
	}
}

func TestFormatWriteAliasesAreIdempotentAndPreserveMode(t *testing.T) {
	input := readCLIFormatFixture(t, "frontmatter-wrap.input.md")
	want := readCLIFormatFixture(t, "frontmatter-wrap.golden.md")

	for _, writeFlag := range []string{"-w", "--write"} {
		writeFlag := writeFlag
		t.Run(writeFlag, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "document.md")
			if err := os.WriteFile(path, input, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o750); err != nil {
				t.Fatal(err)
			}
			originalInfo, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}

			var stdout, stderr bytes.Buffer
			code := run([]string{"fmt", writeFlag, path}, strings.NewReader(""), &stdout, &stderr)
			if code != exitOK {
				t.Fatalf("first write exit code = %d, stderr = %s", code, stderr.String())
			}
			if got := strings.TrimSpace(stdout.String()); got != path {
				t.Fatalf("first write stdout = %q, want %q", got, path)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("formatted file did not match golden output\nwant:\n%s\ngot:\n%s", want, got)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got, wantMode := info.Mode().Perm(), os.FileMode(0o750); got != wantMode {
				t.Fatalf("formatted file mode = %o, want %o", got, wantMode)
			}
			if os.SameFile(originalInfo, info) {
				t.Fatal("formatted file was modified in place, want atomic replacement")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
				t.Fatalf("temporary files remain after atomic replacement: %v", entries)
			}

			stdout.Reset()
			stderr.Reset()
			code = run([]string{"fmt", writeFlag, path}, strings.NewReader(""), &stdout, &stderr)
			if code != exitOK {
				t.Fatalf("second write exit code = %d, stderr = %s", code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("second write stdout = %q, want empty no-op", stdout.String())
			}
			got, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("second write changed the canonical file\nwant:\n%s\ngot:\n%s", want, got)
			}

			stdout.Reset()
			stderr.Reset()
			code = run([]string{"fmt", "--check", path}, strings.NewReader(""), &stdout, &stderr)
			if code != exitOK || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("check after write: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestFormatWriteFollowsSymlinkWithoutReplacingIt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	link := filepath.Join(dir, "document.md")
	if err := os.WriteFile(target, []byte("Title\n=====\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--write", link}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("write replaced symlink: mode=%v", info.Mode())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte("# Title\n"); !bytes.Equal(got, want) {
		t.Fatalf("formatted symlink target = %q, want %q", got, want)
	}
}

func TestFormatCheckAndDiff(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--check", "--stdin-filepath", "doc.md"}, strings.NewReader("Title\n=====\n"), &stdout, &stderr)
	if code != exitFindings {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "doc.md" {
		t.Fatalf("stdout = %q, want doc.md", got)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"fmt", "--diff"}, strings.NewReader("Title\n=====\n"), &stdout, &stderr)
	if code != exitFindings {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "--- <stdin>") || !strings.Contains(stdout.String(), "+# Title") {
		t.Fatalf("unexpected diff output: %q", stdout.String())
	}
}

func TestFormatExitSemantics(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		stdin      string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "clean check", args: []string{"fmt", "--check"}, stdin: "# Title\n", wantCode: exitOK},
		{name: "findings", args: []string{"fmt", "--check", "--stdin-filepath", "doc.md"}, stdin: "Title\n=====\n", wantCode: exitFindings, wantStdout: "doc.md\n"},
		{name: "conflicting modes", args: []string{"fmt", "--check", "--diff"}, stdin: "Title\n=====\n", wantCode: exitError, wantStderr: "mutually exclusive"},
		{name: "write stdin", args: []string{"fmt", "--write", "-"}, stdin: "Title\n=====\n", wantCode: exitError, wantStderr: "cannot be used with stdin"},
		{name: "missing input", args: []string{"fmt", filepath.Join(t.TempDir(), "missing.md")}, wantCode: exitError, wantStderr: "read"},
		{name: "help", args: []string{"fmt", "--help"}, wantCode: exitOK, wantStderr: "Usage of mdpp fmt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, strings.NewReader(tt.stdin), &stdout, &stderr)
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d; stdout=%q stderr=%q", code, tt.wantCode, stdout.String(), stderr.String())
			}
			if tt.wantStdout != "" && stdout.String() != tt.wantStdout {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStdout == "" && tt.name != "help" && stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestLintJSONReturnsFindingsExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"lint", "--json"}, strings.NewReader("http://example.com\n"), &stdout, &stderr)
	if code != exitFindings {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	var diagnostics []struct {
		File string `json:"file"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &diagnostics); err != nil {
		t.Fatalf("lint json: %v\n%s", err, stdout.String())
	}
	if len(diagnostics) != 1 || diagnostics[0].File != "<stdin>" || diagnostics[0].Code != "MD034" {
		t.Fatalf("unexpected diagnostics: %+v", diagnostics)
	}
}

func TestLintFormatAndFilters(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"lint", "--format=github", "--severity=warning"}, strings.NewReader("http://example.com\n\n![ ](x.png)\n"), &stdout, &stderr)
	if code != exitFindings {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "MD034") {
		t.Fatalf("severity filter should exclude info diagnostic: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "::warning") || !strings.Contains(stdout.String(), "MD045") {
		t.Fatalf("expected GitHub warning annotation, got %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"lint", "--json", "--rules=MD034"}, strings.NewReader("http://example.com\n\n![ ](x.png)\n"), &stdout, &stderr)
	if code != exitFindings {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "MD045") || !strings.Contains(stdout.String(), "MD034") {
		t.Fatalf("rule filter output = %q", stdout.String())
	}
}

func TestLintExitSemantics(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		stdin      string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "clean", args: []string{"lint"}, stdin: "# Title\n", wantCode: exitOK},
		{name: "clean json", args: []string{"lint", "--json"}, stdin: "# Title\n", wantCode: exitOK, wantStdout: "[]\n"},
		{name: "findings", args: []string{"lint"}, stdin: "http://example.com\n", wantCode: exitFindings, wantStdout: "MD034"},
		{name: "invalid severity", args: []string{"lint", "--severity=bogus"}, wantCode: exitError, wantStderr: "unknown severity"},
		{name: "help", args: []string{"lint", "--help"}, wantCode: exitOK, wantStderr: "Usage of mdpp lint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, strings.NewReader(tt.stdin), &stdout, &stderr)
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d; stdout=%q stderr=%q", code, tt.wantCode, stdout.String(), stderr.String())
			}
			if tt.wantStdout != "" && !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Fatalf("stdout = %q, want substring %q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStdout == "" && stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestLintFixPreservesModeAndReportsOriginalFindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.md")
	if err := os.WriteFile(path, []byte("Text with trailing spaces.  \n"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o750); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"lint", "--fix", path}, strings.NewReader(""), &stdout, &stderr)
	if code != exitFindings {
		t.Fatalf("exit code = %d, want %d; stdout=%q stderr=%q", code, exitFindings, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "MD009") {
		t.Fatalf("stdout = %q, want original MD009 finding", stdout.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte("Text with trailing spaces.\n"); !bytes.Equal(got, want) {
		t.Fatalf("fixed file = %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o750); got != want {
		t.Fatalf("fixed file mode = %o, want %o", got, want)
	}
}

func TestVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), mdpp.Version) || !strings.Contains(stdout.String(), mdpp.SpecVersion) {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func readCLIFormatFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "format", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

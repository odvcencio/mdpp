package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/mdpp"
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

func TestFormatStdoutAndWrite(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt"}, strings.NewReader("Title\n=====\n"), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if got, want := stdout.String(), "# Title\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}

	path := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(path, []byte("1) item\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"fmt", "-w", path}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), path; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "1. item\n" {
		t.Fatalf("formatted file = %q", got)
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

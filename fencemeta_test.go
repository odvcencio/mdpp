package mdpp

import (
	"strings"
	"testing"
)

// --- Step 15: code-fence highlight meta ---

// A fence info string `go {1-3|5}` keeps language="go" and lifts the brace
// content into Attrs["highlights"]="1-3|5".
func TestFenceHighlightMeta(t *testing.T) {
	got := parseDump(t, "```go {1-3|5}\nx := 1\n```\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (CodeBlock highlights="1-3|5" language="go" "x := 1\n")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("fence highlight meta drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// A simple single-range highlight.
func TestFenceHighlightSingleRange(t *testing.T) {
	got := parseDump(t, "```go {2}\nx := 1\n```\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (CodeBlock highlights="2" language="go" "x := 1\n")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("single-range highlight drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// A fence with no brace meta is unchanged: no highlights attr, language only.
// This is the backward-compat case — the overwhelming majority of fences.
func TestFenceNoMetaUnchanged(t *testing.T) {
	got := parseDump(t, "```go\nx := 1\n```\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (CodeBlock language="go" "x := 1\n")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("plain fence changed\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// Other info-string tokens (e.g. a title=) between the language and the brace
// block are tolerated: language stays the first token and highlights still
// captures the brace content.
func TestFenceHighlightWithOtherTokens(t *testing.T) {
	got := parseDump(t, "```js title=\"a.js\" {2,4}\nx\n```\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (CodeBlock highlights="2,4" language="js" "x\n")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("fence with extra tokens drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

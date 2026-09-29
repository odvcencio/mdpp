package mdpp

import (
	"strings"
	"testing"
)

// The fence language is decoded once. A doubly escaped diagram name is literal
// text, not the diagram renderer's trigger, on both the fast fence path and the
// grammar path.
func TestFenceLanguageIsDecodedOnce(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		diagram      bool
	}{
		{"single reference reaches the diagram renderer", "```&#109;ermaid\ngraph TD; A-->B\n```\n", true},
		{"double reference stays a code block", "```&amp;#109;ermaid\ngraph TD; A-->B\n```\n", false},
		{"double reference after a list (grammar path)", "- item\n\n```&amp;#109;ermaid\ngraph TD; A-->B\n```\n", false},
		{"plain name", "```mermaid\ngraph TD; A-->B\n```\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Parse([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			out, err := Render(doc, RenderOptions{})
			if err != nil {
				t.Fatal(err)
			}
			html := string(out)
			isDiagram := strings.Contains(html, "mdpp-diagram")
			if tc.diagram && !isDiagram {
				t.Fatalf("want the diagram path, got:\n%s", html)
			}
			if !tc.diagram && (isDiagram || !strings.Contains(html, "<pre")) {
				t.Fatalf("want an ordinary code block, got:\n%s", html)
			}
		})
	}
}

package mdpp

import (
	"strings"
	"testing"
)

// --- Step 13/14: inline component + expression recognition ---

// A self-closing uppercase component becomes a NodeComponent carrying its name
// and the raw props string. tree-sitter parses `<Counter initial={3}/>` as a
// single Text node, so this is pure post-parse reinterpretation.
func TestComponentSelfClosingWithProps(t *testing.T) {
	got := parseDump(t, "<Counter initial={3}/>\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Paragraph)`,
		`    (Component name="Counter" props="initial={3}")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("self-closing component drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// A bare self-closing component with no props.
func TestComponentSelfClosingNoProps(t *testing.T) {
	got := parseDump(t, "<Box/>\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Paragraph)`,
		`    (Component name="Box")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("bare self-closing component drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// A paired uppercase component wraps its inner content as children. tree-sitter
// parses the open/close as HTMLInline nodes; we fold the run between a matching
// <Name> … </Name> pair into a NodeComponent.
func TestComponentPairedWithChildren(t *testing.T) {
	got := parseDump(t, "<Note>inner</Note>\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Paragraph)`,
		`    (Component name="Note")`,
		`      (Text "inner")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("paired component drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// A component embedded mid-paragraph keeps the surrounding text intact.
func TestComponentInlineAmidText(t *testing.T) {
	got := parseDump(t, "pre <Counter/> post\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Paragraph)`,
		`    (Text "pre ")`,
		`    (Component name="Counter")`,
		`    (Text " post")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("inline component drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// An interpolation `{expr}` becomes a NodeExpression carrying the inner source.
func TestExpressionBasic(t *testing.T) {
	got := parseDump(t, "value is {x} here\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Paragraph)`,
		`    (Text "value is ")`,
		`    (Expression "x")`,
		`    (Text " here")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("expression drifted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// --- Backward-compat negatives (the spec's highest-risk gate) ---

// `a < b` is arithmetic/comparison prose, not a tag — stays Text.
func TestBackcompatLessThanProse(t *testing.T) {
	got := parseDump(t, "a < b\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Paragraph)`,
		`    (Text "a < b")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("`a < b` was reinterpreted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// `x<y && y>z` must not be parsed as a tag or component — stays Text.
func TestBackcompatAngleComparisons(t *testing.T) {
	got := parseDump(t, "x<y && y>z\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Paragraph)`,
		`    (Text "x<y && y>z")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("`x<y && y>z` was reinterpreted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// A lowercase HTML tag is ordinary HTML, never a GoSX component. The opening
// tag remains Text (tree-sitter's choice) and the closing remains HTMLInline;
// neither becomes a NodeComponent.
func TestBackcompatLowercaseHTMLTag(t *testing.T) {
	got := parseDump(t, `<span class="x">hi</span>`+"\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Paragraph)`,
		`    (Text "<span class=\"x\">hi")`,
		`    (HTMLInline "</span>")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("lowercase HTML tag was reinterpreted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// An escaped brace `\{...\}` must stay literal text. The backslash escapes
// split the run at the brace boundaries, so no single text node holds a
// complete `{…}` and the expression pass never fires.
func TestBackcompatEscapedBraces(t *testing.T) {
	got := parseDump(t, `literal \{not an expr\}`+"\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Paragraph)`,
		`    (Text "literal ")`,
		`    (Text "{not an expr")`,
		`    (Text "}")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("escaped braces were reinterpreted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// A self-closing LOWERCASE tag stays HTMLInline, not a component.
func TestBackcompatLowercaseSelfClosing(t *testing.T) {
	got := parseDump(t, "<br/>\n")
	want := strings.Join([]string{
		`(Document)`,
		`  (Paragraph)`,
		`    (HTMLInline "<br/>")`,
		``,
	}, "\n")
	if got != want {
		t.Errorf("lowercase self-closing tag was reinterpreted\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// --- Render hosts ---

// A NodeComponent renders to a stable <gosx-component> host carrying the name
// and props so the document survives a standalone render without the island
// runtime.
func TestComponentRendersHost(t *testing.T) {
	html := NewRenderer().RenderString("<Counter initial={3}/>")
	assertContains(t, html, `<gosx-component data-name="Counter"`)
	assertContains(t, html, `data-props="initial={3}"`)
}

// A NodeExpression renders to a stable <gosx-expr> host carrying the source.
func TestExpressionRendersHost(t *testing.T) {
	html := NewRenderer().RenderString("hi {name} there")
	assertContains(t, html, `<gosx-expr>name</gosx-expr>`)
}

// Backward-compat at the render layer: prose `a < b` renders unchanged (the
// `<` is HTML-escaped, no component host appears).
func TestBackcompatLessThanRendersUnchanged(t *testing.T) {
	html := NewRenderer().RenderString("a < b")
	assertContains(t, html, "a &lt; b")
	assertNotContains(t, html, "gosx-component")
	assertNotContains(t, html, "gosx-expr")
}

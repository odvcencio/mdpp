package mdpp

import (
	"regexp"
	"strings"
)

// --- gosx-slides inline component + expression recognition (Track D) ---
//
// These two passes run inside the default postProcess pipeline because they
// are backward-compatible: they only fire on unambiguous MDX-style constructs
// (an uppercase-led tag, a single-level brace interpolation) and leave all
// ordinary Markdown untouched. The disambiguation rules, in order of how the
// underlying tree-sitter parse presents each construct, are:
//
//  1. Self-closing component with props — `<Counter initial={3}/>`. The `{…}`
//     props defeat tree-sitter's html_tag rule, so the whole thing arrives as
//     one Text node. componentSelfClosingRe rewrites it.
//  2. Self-closing component without `{…}` props — `<Box/>`, `<Counter/>`.
//     tree-sitter recognizes these as a single HTMLInline. We rewrite the
//     HTMLInline iff the tag name is uppercase.
//  3. Paired component — `<Note>…</Note>`. tree-sitter emits the open and
//     close as two HTMLInline siblings with the inner content between them.
//     We fold a matching uppercase <Name>…</Name> run into one NodeComponent.
//
// Lowercase tags (`<div>`, `<br/>`, `<span class="x">`) are ordinary HTML and
// are never reinterpreted. `a < b` and `x<y && y>z` never form a tag, so they
// stay Text. The expression pass requires a complete `{…}` inside a single
// Text node; a `\{`/`\}` escape splits the run at the brace, so escaped braces
// can never match.

// componentNamePat is the MDX component-name rule: an uppercase initial,
// followed by identifier chars and optional dotted member access (<Foo.Bar/>).
const componentNamePat = `[A-Z][A-Za-z0-9_.]*`

// componentSelfClosingRe matches a self-closing component embedded in plain
// text. The props group is everything between the name and `/>`, excluding
// angle brackets so it cannot run across adjacent tags.
var componentSelfClosingRe = regexp.MustCompile(`<(` + componentNamePat + `)((?:\s[^<>]*?)?)\s*/>`)

// htmlInlineSelfClosingRe matches an HTMLInline literal that is a single
// self-closing tag, capturing the tag name and the inner props.
var htmlInlineSelfClosingRe = regexp.MustCompile(`^<([A-Za-z][A-Za-z0-9_.-]*)((?:\s[^<>]*?)?)\s*/>$`)

// htmlInlineOpenRe matches an HTMLInline literal that is a single opening tag
// (not self-closing), capturing the tag name and the inner props.
var htmlInlineOpenRe = regexp.MustCompile(`^<([A-Za-z][A-Za-z0-9_.-]*)((?:\s[^<>]*?)?)\s*>$`)

// htmlInlineCloseRe matches an HTMLInline literal that is a single closing tag.
var htmlInlineCloseRe = regexp.MustCompile(`^</([A-Za-z][A-Za-z0-9_.-]*)\s*>$`)

// expressionRe matches a single-level `{expr}` interpolation inside one Text
// node. The inner group is non-empty and contains no braces, so `{}` is left
// alone and nested braces match the innermost pair.
var expressionRe = regexp.MustCompile(`\{([^{}]+)\}`)

// isComponentName reports whether name is a GoSX component name (uppercase
// initial) rather than an ordinary HTML tag name.
func isComponentName(name string) bool {
	if name == "" {
		return false
	}
	c := name[0]
	return c >= 'A' && c <= 'Z'
}

// processComponents reinterprets uppercase-led inline tags as NodeComponent.
// It runs three sub-passes: the self-closing-in-text rewrite, the
// self-closing-HTMLInline rewrite, and the paired-HTMLInline fold.
func processComponents(root *Node) {
	if root == nil {
		return
	}
	// 1. Self-closing components that arrived as Text (props with braces).
	processInlinePattern(root, componentSelfClosingRe, func(match []string) *Node {
		name := match[1]
		if !isComponentName(name) {
			return nil // leave lowercase tags as text
		}
		return newComponentNode(name, strings.TrimSpace(match[2]))
	})

	// 2 & 3. HTMLInline-based components (self-closing void tags and paired
	// open/close runs). These need sibling context, so they walk node lists.
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if len(n.Children) > 0 {
			n.Children = foldComponentChildren(n.Children)
		}
		return true
	})
}

// newComponentNode builds a NodeComponent, recording props only when present.
func newComponentNode(name, props string) *Node {
	c := &Node{Type: NodeComponent, Attrs: map[string]string{"name": name}}
	if props != "" {
		c.Attrs["props"] = props
	}
	return c
}

// foldComponentChildren rewrites a single node's child slice: self-closing
// uppercase HTMLInline tags become NodeComponent leaves, and a matching
// uppercase <Name>…</Name> HTMLInline pair (with depth handling for same-name
// nesting) becomes a NodeComponent wrapping the folded inner children.
func foldComponentChildren(children []*Node) []*Node {
	out := make([]*Node, 0, len(children))
	for i := 0; i < len(children); i++ {
		child := children[i]
		if child == nil || child.Type != NodeHTMLInline {
			out = append(out, child)
			continue
		}

		// Self-closing: <Name.../>
		if m := htmlInlineSelfClosingRe.FindStringSubmatch(child.Literal); m != nil {
			if isComponentName(m[1]) {
				comp := newComponentNode(m[1], strings.TrimSpace(m[2]))
				comp.Range = child.Range
				out = append(out, comp)
				continue
			}
			out = append(out, child)
			continue
		}

		// Opening tag: <Name ...> — look for the matching close.
		if m := htmlInlineOpenRe.FindStringSubmatch(child.Literal); m != nil && isComponentName(m[1]) {
			name := m[1]
			closeIdx := matchingCloseIndex(children, i, name)
			if closeIdx > i {
				comp := newComponentNode(name, strings.TrimSpace(m[2]))
				comp.Range = child.Range
				// Recurse into the inner run so nested components fold too.
				comp.Children = foldComponentChildren(children[i+1 : closeIdx])
				out = append(out, comp)
				i = closeIdx
				continue
			}
			// No matching close — leave the opening tag as-is.
			out = append(out, child)
			continue
		}

		out = append(out, child)
	}
	return out
}

// matchingCloseIndex finds the index in children of the </name> that closes the
// <name> opening tag at openIdx, accounting for same-name nesting. Returns -1
// when there is no matching close in this slice.
func matchingCloseIndex(children []*Node, openIdx int, name string) int {
	depth := 0
	for j := openIdx + 1; j < len(children); j++ {
		c := children[j]
		if c == nil || c.Type != NodeHTMLInline {
			continue
		}
		if om := htmlInlineOpenRe.FindStringSubmatch(c.Literal); om != nil && om[1] == name {
			depth++
			continue
		}
		if cm := htmlInlineCloseRe.FindStringSubmatch(c.Literal); cm != nil && cm[1] == name {
			if depth == 0 {
				return j
			}
			depth--
		}
	}
	return -1
}

// processExpressions reinterprets `{expr}` interpolations inside Text nodes as
// NodeExpression leaves. It runs after processComponents so a component's props
// (stored as an opaque string on the NodeComponent, not as children) are never
// scanned for expressions.
func processExpressions(root *Node) {
	if root == nil {
		return
	}
	processInlinePattern(root, expressionRe, func(match []string) *Node {
		return &Node{Type: NodeExpression, Literal: match[1]}
	})
}

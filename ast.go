package mdpp

import (
	"strconv"
	"strings"
)

// NodeType classifies an AST node.
type NodeType int

const (
	NodeDocument NodeType = iota
	NodeHeading
	NodeParagraph
	NodeCodeBlock
	NodeBlockquote
	NodeList
	NodeListItem
	NodeTable
	NodeTableRow
	NodeTableCell
	NodeThematicBreak
	NodeLink
	NodeImage
	NodeEmphasis
	NodeStrong
	NodeStrikethrough
	NodeCodeSpan
	NodeText
	NodeSoftBreak
	NodeHardBreak
	NodeHTMLBlock
	NodeHTMLInline
	NodeFootnoteRef
	NodeFootnoteDef
	NodeMathInline
	NodeMathBlock
	NodeAdmonition
	NodeDefinitionList
	NodeDefinitionTerm
	NodeDefinitionDesc
	NodeSuperscript
	NodeSubscript
	NodeTaskListItem
	NodeFrontmatter
	NodeTableOfContents
	NodeAutoEmbed
	NodeEmoji
	NodeDiagram
	NodeContainerDirective
)

// Node is a single element in the Markdown AST.
type Node struct {
	Type     NodeType
	Children []*Node
	Literal  string
	Attrs    map[string]string
	Range    Range
}

// Range identifies a node's byte span in Document.Source.
// Lines and columns are 1-indexed; columns are byte columns.
type Range struct {
	StartByte int
	EndByte   int
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// Severity classifies parse diagnostics.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityError
)

// Diagnostic is a recoverable parser finding attached to a source range.
type Diagnostic struct {
	Code     string
	Severity Severity
	Message  string
	Range    Range
}

// linkRefDef is a parsed [label]: href "title" definition.
type linkRefDef struct {
	href  string
	title string
}

// Document is the top-level result of parsing a Markdown source.
type Document struct {
	Root            *Node
	Source          []byte
	frontmatterData map[string]any
	linkRefDefs     map[string]linkRefDef
	diagnostics     []Diagnostic
}

// AST returns the root node.
func (d *Document) AST() *Node {
	if d == nil {
		return nil
	}
	return d.Root
}

// Frontmatter returns the parsed frontmatter metadata, if any.
func (d *Document) Frontmatter() map[string]any {
	return d.frontmatterData
}

// Diagnostics returns recoverable parse diagnostics.
func (d *Document) Diagnostics() []Diagnostic {
	if d == nil || len(d.diagnostics) == 0 {
		return nil
	}
	out := make([]Diagnostic, len(d.diagnostics))
	copy(out, d.diagnostics)
	return out
}

// FormatVersion returns the frontmatter mdpp version, if declared.
func (d *Document) FormatVersion() string {
	if d == nil || d.frontmatterData == nil {
		return ""
	}
	switch v := d.frontmatterData["mdpp"].(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

// TOCEntry represents a single table-of-contents heading.
type TOCEntry struct {
	Level int
	ID    string
	Text  string
}

// collectNodesText extracts plain text from a slice of nodes.
func collectNodesText(nodes []*Node) string {
	var sb strings.Builder
	for _, n := range nodes {
		sb.WriteString(collectNodeText(n))
	}
	return sb.String()
}

// textNode creates a leaf text node.
func textNode(text string) *Node {
	return &Node{Type: NodeText, Literal: text}
}

// newNode creates a node of the given type with optional children.
func newNode(typ NodeType, children ...*Node) *Node {
	return &Node{Type: typ, Children: children}
}

// Attr returns an attribute value or "" if absent.
func (n *Node) Attr(key string) string {
	if n == nil || n.Attrs == nil {
		return ""
	}
	return n.Attrs[key]
}

// HasAttr reports whether an attribute key is present.
func (n *Node) HasAttr(key string) bool {
	if n == nil || n.Attrs == nil {
		return false
	}
	_, ok := n.Attrs[key]
	return ok
}

// Level returns a heading level, or 0 for non-heading nodes.
func (n *Node) Level() int {
	if n == nil || n.Type != NodeHeading {
		return 0
	}
	level, _ := strconv.Atoi(n.Attr("level"))
	return level
}

// Text returns collected plain text for this node.
func (n *Node) Text() string {
	return collectNodeText(n)
}

// Walk visits this node and descendants in pre-order.
func (n *Node) Walk(visit func(n *Node) bool) {
	if n == nil || visit == nil {
		return
	}
	if !visit(n) {
		return
	}
	for _, child := range n.Children {
		child.Walk(visit)
	}
}

// Find returns descendants, including this node, with the requested type.
func (n *Node) Find(typ NodeType) []*Node {
	var out []*Node
	n.Walk(func(cur *Node) bool {
		if cur.Type == typ {
			out = append(out, cur)
		}
		return true
	})
	return out
}

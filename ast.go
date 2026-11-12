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
	// gosx-slides parse seams (Track D). Produced post-parse by reinterpreting
	// already-parsed nodes; the grammar is unchanged.
	NodeSlide      // a deck slide: top-level content grouped between `---` separators
	NodeComponent  // an inline GoSX component: <Name .../> or <Name>…</Name>
	NodeExpression // an inline expression interpolation: {expr}
	NodeLinkReferenceDefinition
)

// Node is a single element in the Markdown AST.
type Node struct {
	Type     NodeType
	Children []*Node
	Literal  string
	Attrs    map[string]string
	Range    Range

	// raw is the source text of a Text node whose Literal was decoded from it
	// (escapes and entities), so that splitting at line ends uses source
	// positions. It is empty when Literal is the source text.
	raw string
}

// Range identifies a node's byte span in Document.Source.
// Lines and columns are 1-indexed; columns are byte columns.
//
// Document.Source is the input with line endings normalised to LF and a
// final newline added when the input had none. Every range indexes that text,
// whatever rewriting a parse pass did on the way: no range refers to a lowered
// or stripped copy of the input.
//
// A node that has no source text of its own (the table of contents that
// "[[toc]]" produces, with its list, items and links) has the zero Range.
// Because lines are 1-indexed, StartLine is never zero for a node that has
// source text, so the zero value is a marker a caller can test with
// Node.HasSourceRange. A zero-width node that does sit in the source (an empty
// table cell) has a non-zero StartLine. The parser never gives a synthesized
// node a guessed range.
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
//
// # Diagnostics and exact readings
//
// Parse never fails on a source: when a part of it exceeds a limit or the
// grammar cannot read it, the parser keeps that part as text and records a
// Diagnostic with the Range of the source it affects. The tree is then a
// reading of the document in which that range holds no structure. The codes
// that say so are DiagPanic (MDPP-PARSE-000), DiagContainerAutoClosed (002),
// DiagDepthExceeded (003), DiagComplexityLimit (004), DiagBudgetExceeded (005),
// DiagSyntaxRecovery (006) and DiagNoTree (007). A caller that needs an exact
// structural reading, for example to find every heading, list item, task item
// or HTML tag in a text, must:
//
//  1. check Document.StructureCertified, or read Document.UncertifiedRanges;
//  2. treat the nodes that overlap an uncertified range as text, and refuse to
//     conclude from the tree that the range holds no heading, task item, link
//     or HTML (a block kept as text has none);
//  3. treat a document with an uncertified range as unreadable when the caller
//     cannot accept a partial answer, and report the diagnostic.
//
// Diagnostic.StructureUncertain is true for each of these codes. Every range
// is in Document.Source, like the ranges of nodes. Each fallback in the block
// and inline readers records a diagnostic, and the range covers everything the
// fallback kept as text, including structure outside the place where the
// grammar reported its error.
//
// A certified document is one in which the parser took no fallback. It does not
// say that the grammar's reading agrees with the specification for every
// input: where the grammar misreads a construct without reporting an error,
// there is no diagnostic to give. testdata/spec/known-failures.json lists the
// specification examples that read differently.
type Document struct {
	Root            *Node
	Source          []byte
	frontmatterData map[string]any
	linkRefDefs     map[string]linkRefDef
	diagnostics     []Diagnostic
	sourceHadCR     bool
	profile         ParseProfile
}

// Profile returns the dialect that the document was parsed as.
func (d *Document) Profile() ParseProfile {
	if d == nil {
		return ProfileMarkdownPlus
	}
	return d.profile
}

// SourceHadCarriageReturns reports whether the input to Parse contained CR
// bytes. Parse normalizes CRLF and lone CR to LF, so byte ranges cannot be
// applied directly to the original byte slice when this returns true.
func (d *Document) SourceHadCarriageReturns() bool {
	return d != nil && d.sourceHadCR
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

// Diagnostics returns recoverable parse diagnostics. See Document for what a
// caller that needs an exact structural reading does with them.
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

// HasSourceRange reports whether the node's Range points at bytes of
// Document.Source. It is false for a node that the parser synthesized and that
// has no source text of its own; such a node has the zero Range. Callers that
// map a node to the source must test it first.
func (n *Node) HasSourceRange() bool {
	return n != nil && n.Range.StartLine > 0
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

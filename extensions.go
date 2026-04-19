package mdpp

import (
	"regexp"
	"strings"
)

// postProcess walks the AST after parsing and applies Markdown++ transformations.
// Task lists and subscripts are handled during parsing (convertListItem and
// strikethrough detection respectively), so they are not processed here.
func postProcess(doc *Document) {
	if doc == nil || doc.Root == nil {
		return
	}
	flattenDocumentNodes(doc.Root)
	processAdmonitions(doc.Root)
	processBlockquoteHeadings(doc.Root)
	footnoteDefs := processFootnotes(doc.Root)
	processInlineMath(doc.Root)
	processSuperscripts(doc.Root)
	processEmojiShortcodes(doc.Root)

	// Append referenced footnote definitions at the end of the document.
	for _, def := range footnoteDefs {
		if def != nil {
			doc.Root.Children = append(doc.Root.Children, def)
		}
	}

	// Resolve [text][ref] / [ref][] / [ref] references against the
	// link_reference_definition map captured during Parse. Must run
	// before processTOC (TOC consumers do not depend on it) but after
	// admonition + footnote processing so footnote shortcut links are
	// not mis-resolved as link refs.
	processReferenceLinks(doc)

	// Definition lists: recognize `Term\n: Def` paragraphs and merge
	// adjacent term/description pairs into a single <dl>.
	processDefinitionLists(doc.Root)

	// Auto-embeds: `[[embed:url]]` on its own line → NodeAutoEmbed.
	processAutoEmbeds(doc.Root)

	// TOC runs last so every heading in the final AST is accounted for.
	processTOC(doc)
}

// processReferenceLinks walks the AST and, for every NodeLink that has a
// `ref` attribute but no resolved `href`, looks up the corresponding
// link_reference_definition and fills in href + title. Collapsed and
// shortcut links fall back to using the link text as the lookup key
// when no explicit label is present.
func processReferenceLinks(doc *Document) {
	if doc == nil || doc.Root == nil || len(doc.linkRefDefs) == 0 {
		return
	}
	walkNodes(doc.Root, func(n *Node, parent *Node, index int) bool {
		if n.Type != NodeLink || n.Attrs == nil {
			return true
		}
		if n.Attrs["href"] != "" {
			return true
		}
		// Ignore shortcut links that carry the [!TYPE] / [^id] raw marker —
		// those have already been consumed by admonition/footnote processors
		// or remain as literal text. Only reference-style links we care about
		// either have a `ref` attr or are collapsed/shortcut over plain text.
		raw := n.Attrs["raw"]
		if strings.HasPrefix(raw, "[!") || strings.HasPrefix(raw, "[^") {
			return true
		}
		label := n.Attrs["ref"]
		if label == "" {
			// Collapsed / shortcut form: the link text IS the label.
			label = collectNodeText(n)
		}
		label = normalizeLinkLabel(label)
		if def, ok := doc.linkRefDefs[label]; ok {
			n.Attrs["href"] = def.href
			if def.title != "" {
				n.Attrs["title"] = def.title
			}
			delete(n.Attrs, "ref")
			delete(n.Attrs, "raw")
		}
		return true
	})
}

func flattenDocumentNodes(root *Node) {
	if root == nil {
		return
	}
	for i := 0; i < len(root.Children); i++ {
		child := root.Children[i]
		if child == nil {
			continue
		}
		if child.Type == NodeDocument {
			replacement := append([]*Node{}, child.Children...)
			root.Children = append(root.Children[:i], append(replacement, root.Children[i+1:]...)...)
			i--
			continue
		}
		flattenDocumentNodes(child)
	}
}

// --- Admonitions ---

var admonitionRawRe = regexp.MustCompile(`^\[!(NOTE|WARNING|TIP|IMPORTANT|CAUTION)\]$`)

// processAdmonitions converts blockquotes starting with [!TYPE] into admonition nodes.
// tree-sitter parses "[!NOTE]" as a shortcut_link with raw="[!NOTE]" and link text "!".
// The AST looks like: Blockquote > Paragraph > [Link(raw="[!NOTE]"), Text("\n> content")]
func processAdmonitions(root *Node) {
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if n.Type != NodeBlockquote || len(n.Children) == 0 {
			return true
		}

		firstChild := n.Children[0]
		if firstChild.Type != NodeParagraph || len(firstChild.Children) == 0 {
			return true
		}

		// Look for a Link node with raw attr matching [!TYPE]
		firstNode := firstChild.Children[0]
		if firstNode.Type != NodeLink {
			return true
		}
		raw := firstNode.Attrs["raw"]
		if raw == "" {
			return true
		}
		match := admonitionRawRe.FindStringSubmatch(raw)
		if match == nil {
			return true
		}

		adType := strings.ToLower(match[1])

		// Build admonition node
		adm := &Node{
			Type:  NodeAdmonition,
			Attrs: map[string]string{"type": adType},
		}

		// Remove the Link node from the first paragraph's children
		firstChild.Children = firstChild.Children[1:]

		if caption, rest := extractAdmonitionCaption(firstChild.Children); caption != "" {
			adm.Attrs["title"] = caption
			firstChild.Children = rest
		}
		firstChild.Children = cleanAdmonitionLeadingChildren(firstChild.Children)

		// If first paragraph still has content, include it
		startIdx := 0
		if len(firstChild.Children) == 0 {
			startIdx = 1
		}

		adm.Children = make([]*Node, len(n.Children[startIdx:]))
		copy(adm.Children, n.Children[startIdx:])

		// Replace the blockquote with the admonition in the parent
		if parent != nil {
			parent.Children[index] = adm
		}

		return true
	})
}

func extractAdmonitionCaption(children []*Node) (string, []*Node) {
	if len(children) == 0 {
		return "", children
	}
	var caption strings.Builder
	var rest []*Node
	foundBreak := false
	for i, child := range children {
		if child == nil {
			continue
		}
		switch child.Type {
		case NodeSoftBreak, NodeHardBreak:
			rest = append(rest, children[i+1:]...)
			foundBreak = true
		case NodeText:
			text := child.Literal
			if caption.Len() == 0 {
				text = strings.TrimLeft(text, " \t")
			}
			if text == "" && caption.Len() == 0 {
				continue
			}
			line, tail, hasBreak := strings.Cut(text, "\n")
			caption.WriteString(line)
			if hasBreak {
				if tail != "" {
					clone := *child
					clone.Literal = tail
					rest = append(rest, &clone)
				}
				rest = append(rest, children[i+1:]...)
				foundBreak = true
			}
		default:
			caption.WriteString(admonitionCaptionSource(child))
		}
		if foundBreak {
			break
		}
	}
	title := strings.TrimSpace(caption.String())
	if title == "" {
		return "", children
	}
	return title, rest
}

func admonitionCaptionSource(n *Node) string {
	if n == nil {
		return ""
	}
	switch n.Type {
	case NodeText, NodeHTMLInline, NodeHTMLBlock, NodeMathInline, NodeMathBlock, NodeSuperscript, NodeSubscript, NodeEmoji:
		return n.Literal
	case NodeCodeSpan:
		return "`" + n.Literal + "`"
	default:
		var out strings.Builder
		for _, child := range n.Children {
			out.WriteString(admonitionCaptionSource(child))
		}
		return out.String()
	}
}

func cleanAdmonitionLeadingChildren(children []*Node) []*Node {
	cleaned := make([]*Node, 0, len(children))
	leading := true
	for _, child := range children {
		if child == nil {
			continue
		}
		if leading && (child.Type == NodeSoftBreak || child.Type == NodeHardBreak) {
			continue
		}
		if leading && child.Type == NodeText {
			clone := *child
			clone.Literal = cleanAdmonitionText(clone.Literal)
			if clone.Literal == "" {
				continue
			}
			cleaned = append(cleaned, &clone)
			leading = false
			continue
		}
		cleaned = append(cleaned, child)
		leading = false
	}
	return cleaned
}

func cleanAdmonitionText(text string) string {
	text = strings.TrimLeft(text, "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "> ")
		lines[i] = strings.TrimPrefix(lines[i], ">")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// --- Quote headings ---

var bracketedQuoteHeadingRe = regexp.MustCompile(`^\[[^\^!][\s\S]*\]$`)

func processBlockquoteHeadings(root *Node) {
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if n.Type != NodeBlockquote || len(n.Children) == 0 {
			return true
		}
		firstChild := n.Children[0]
		if firstChild.Type != NodeParagraph || len(firstChild.Children) == 0 {
			return true
		}
		firstNode := firstChild.Children[0]
		if firstNode.Type != NodeLink || firstNode.Attrs["href"] != "" {
			return true
		}
		raw := firstNode.Attrs["raw"]
		if !bracketedQuoteHeadingRe.MatchString(raw) {
			return true
		}
		firstChild.Children[0] = newNode(NodeStrong, firstNode.Children...)
		firstChild.Children = cleanBlockquoteHeadingContinuation(firstChild.Children)
		return true
	})
}

func cleanBlockquoteHeadingContinuation(children []*Node) []*Node {
	cleaned := make([]*Node, 0, len(children))
	for i, child := range children {
		if child == nil {
			continue
		}
		if i == 0 || child.Type != NodeText {
			cleaned = append(cleaned, child)
			continue
		}
		clone := *child
		clone.Literal = cleanBlockquoteContinuationText(clone.Literal)
		if clone.Literal == "" {
			continue
		}
		cleaned = append(cleaned, &clone)
	}
	return cleaned
}

func cleanBlockquoteContinuationText(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "> ")
		lines[i] = strings.TrimPrefix(lines[i], ">")
	}
	return strings.Join(lines, "\n")
}

// --- Footnotes ---

var footnoteRefRawRe = regexp.MustCompile(`^\[\^([A-Za-z0-9_-]+)\]$`)

// processFootnotes scans for footnote references and definitions.
// Definitions may be:
//  1. NodeFootnoteDef already created by the parser from link_reference_definition
//  2. Paragraphs containing Link(raw="[^id]") followed by Text(": content")
//
// References are Link nodes with raw="[^id]".
func processFootnotes(root *Node) []*Node {
	defs := make(map[string]*Node)
	var defOrder []string
	var defIndices []int

	// First pass: collect footnote definitions from document children.
	for i, child := range root.Children {
		// Case 1: already a NodeFootnoteDef from parser
		if child.Type == NodeFootnoteDef {
			id := child.Attrs["id"]
			if id != "" {
				if _, exists := defs[id]; !exists {
					defOrder = append(defOrder, id)
				}
				defs[id] = child
			}
			defIndices = append(defIndices, i)
			continue
		}

		// Case 2: paragraph containing Link(raw="[^id]") + Text(": content")
		if child.Type != NodeParagraph || len(child.Children) == 0 {
			continue
		}
		firstNode := child.Children[0]
		if firstNode.Type != NodeLink {
			continue
		}
		raw := firstNode.Attrs["raw"]
		if raw == "" {
			continue
		}
		match := footnoteRefRawRe.FindStringSubmatch(raw)
		if match == nil {
			continue
		}
		defChildren, ok := footnoteDefinitionChildren(child.Children[1:])
		if !ok {
			continue
		}
		defNode := &Node{
			Type:     NodeFootnoteDef,
			Attrs:    map[string]string{"id": match[1]},
			Children: defChildren,
		}
		if _, exists := defs[match[1]]; !exists {
			defOrder = append(defOrder, match[1])
		}
		defs[match[1]] = defNode
		defIndices = append(defIndices, i)
	}

	// Remove definition nodes from root (in reverse order to preserve indices).
	for j := len(defIndices) - 1; j >= 0; j-- {
		idx := defIndices[j]
		root.Children = append(root.Children[:idx], root.Children[idx+1:]...)
	}

	// Second pass: convert footnote references in all nodes.
	// Replace Link nodes with raw="[^id]" with FootnoteRef nodes.
	var refOrder []string
	seenRefs := make(map[string]struct{})
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if len(n.Children) == 0 {
			return true
		}
		for i, child := range n.Children {
			if child.Type != NodeLink {
				continue
			}
			raw := child.Attrs["raw"]
			if raw == "" {
				continue
			}
			match := footnoteRefRawRe.FindStringSubmatch(raw)
			if match == nil {
				continue
			}
			id := match[1]
			if _, seen := seenRefs[id]; !seen {
				refOrder = append(refOrder, id)
				seenRefs[id] = struct{}{}
			}
			n.Children[i] = &Node{
				Type:  NodeFootnoteRef,
				Attrs: map[string]string{"id": id},
			}
		}
		return true
	})

	ordered := make([]*Node, 0, len(refOrder))
	for _, id := range refOrder {
		if def := defs[id]; def != nil {
			ordered = append(ordered, def)
		}
	}
	if len(ordered) > 0 {
		return ordered
	}
	for _, id := range defOrder {
		if def := defs[id]; def != nil && len(def.Children) > 0 {
			ordered = append(ordered, def)
		}
	}
	return ordered
}

func footnoteDefinitionChildren(children []*Node) ([]*Node, bool) {
	if len(children) == 0 {
		return nil, false
	}
	first := children[0]
	if first.Type != NodeText || !strings.HasPrefix(first.Literal, ":") {
		return nil, false
	}

	out := make([]*Node, 0, len(children))
	firstText := strings.TrimPrefix(first.Literal, ":")
	firstText = strings.TrimPrefix(firstText, " ")
	firstText = strings.TrimPrefix(firstText, "\t")
	if firstText != "" {
		clone := *first
		clone.Literal = firstText
		out = append(out, &clone)
	}
	out = append(out, children[1:]...)
	return out, true
}

// --- Math ---

var mathBlockRe = regexp.MustCompile(`^\$\$([\s\S]+?)\$\$$`)
var mathInlineRe = regexp.MustCompile(`\$([^\$\n]+?)\$`)

// processInlineMath converts $...$ to inline math and $$...$$ paragraphs to block math.
func processInlineMath(root *Node) {
	// First: handle block math — paragraphs that are entirely $$...$$
	for i, child := range root.Children {
		if child.Type != NodeParagraph {
			continue
		}
		text := collectNodeText(child)
		text = strings.TrimSpace(text)
		match := mathBlockRe.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		root.Children[i] = &Node{
			Type:    NodeMathBlock,
			Literal: strings.TrimSpace(match[1]),
		}
	}

	// Second: handle inline math $...$
	processInlinePattern(root, mathInlineRe, func(match []string) *Node {
		return &Node{
			Type:    NodeMathInline,
			Literal: match[1],
		}
	})
}

// --- Superscript ---

var superscriptRe = regexp.MustCompile(`\^([^\^\s]+?)\^`)

// processSuperscripts converts ^text^ patterns in text nodes to superscript nodes.
func processSuperscripts(root *Node) {
	processInlinePattern(root, superscriptRe, func(match []string) *Node {
		return &Node{
			Type:    NodeSuperscript,
			Literal: match[1],
		}
	})
}

// --- Auto-embeds ---

var autoEmbedDirectiveRe = regexp.MustCompile(`^\[\[embed:\s*(\S.*?)\s*\]\]$`)

// processAutoEmbeds replaces a top-level paragraph whose trimmed surface
// text matches `[[embed:<url>]]` with a NodeAutoEmbed carrying the url as
// the `src` attribute and the provider name (if recognizable) as `provider`.
// Inline occurrences remain literal.
func processAutoEmbeds(root *Node) {
	if root == nil {
		return
	}
	for i, child := range root.Children {
		if child == nil || child.Type != NodeParagraph {
			continue
		}
		text := strings.TrimSpace(collectSurfaceText(child))
		match := autoEmbedDirectiveRe.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		url := match[1]
		embed := &Node{
			Type:  NodeAutoEmbed,
			Attrs: map[string]string{"src": url, "provider": detectEmbedProvider(url)},
		}
		root.Children[i] = embed
	}
}

// detectEmbedProvider returns a short provider hint for the given URL.
// Used as a rendering hook for themes; returns "" when unknown.
func detectEmbedProvider(url string) string {
	lower := strings.ToLower(url)
	switch {
	case strings.Contains(lower, "youtube.com/"), strings.Contains(lower, "youtu.be/"):
		return "youtube"
	case strings.Contains(lower, "vimeo.com/"):
		return "vimeo"
	case strings.Contains(lower, "github.com/"):
		return "github"
	case strings.Contains(lower, "twitter.com/"), strings.Contains(lower, "x.com/"):
		return "twitter"
	case strings.Contains(lower, "spotify.com/"), strings.Contains(lower, "open.spotify.com/"):
		return "spotify"
	case strings.Contains(lower, "soundcloud.com/"):
		return "soundcloud"
	case strings.Contains(lower, "codepen.io/"):
		return "codepen"
	case strings.Contains(lower, "codesandbox.io/"):
		return "codesandbox"
	}
	return ""
}

// --- Definition Lists ---

// processDefinitionLists detects paragraphs of the form
//
//	Term
//	: Definition
//	[: Another def]
//
// and replaces them with a NodeDefinitionList. Adjacent matching
// paragraphs in the same block coalesce into one <dl>.
//
// tree-sitter-markdown does not recognize this PHP-Markdown-Extra /
// pandoc-style syntax; it emits a single paragraph with soft breaks.
// We re-split the paragraph's surface text on newlines to recover the
// structure.
func processDefinitionLists(root *Node) {
	if root == nil {
		return
	}
	out := make([]*Node, 0, len(root.Children))
	var currentList *Node
	for _, child := range root.Children {
		if child == nil {
			continue
		}
		if child.Type != NodeParagraph {
			if currentList != nil {
				out = append(out, currentList)
				currentList = nil
			}
			out = append(out, child)
			continue
		}
		term, descs, ok := splitDefinitionParagraph(child)
		if !ok {
			if currentList != nil {
				out = append(out, currentList)
				currentList = nil
			}
			out = append(out, child)
			continue
		}
		if currentList == nil {
			currentList = &Node{Type: NodeDefinitionList}
		}
		currentList.Children = append(currentList.Children, term)
		currentList.Children = append(currentList.Children, descs...)
	}
	if currentList != nil {
		out = append(out, currentList)
	}
	root.Children = out
}

// splitDefinitionParagraph inspects a paragraph's inline children and
// returns a DefinitionTerm + DefinitionDesc chain if the paragraph matches
// `Term\n: Def [\n: Def ...]`. Works on AST children directly so inline
// formatting (emphasis, code, links, emoji) is preserved instead of
// flattened to text.
func splitDefinitionParagraph(para *Node) (*Node, []*Node, bool) {
	// Partition children at soft/hard break boundaries.
	segments := [][]*Node{{}}
	for _, c := range para.Children {
		if c == nil {
			continue
		}
		if c.Type == NodeSoftBreak || c.Type == NodeHardBreak {
			segments = append(segments, nil)
			continue
		}
		segments[len(segments)-1] = append(segments[len(segments)-1], c)
	}
	if len(segments) < 2 {
		return nil, nil, false
	}
	termChildren := segments[0]
	if len(termChildren) == 0 {
		return nil, nil, false
	}
	var descs []*Node
	for _, seg := range segments[1:] {
		if len(seg) == 0 {
			return nil, nil, false
		}
		head := seg[0]
		if head.Type != NodeText {
			return nil, nil, false
		}
		lit := strings.TrimLeft(head.Literal, " \t")
		switch {
		case lit == ":":
			// The colon is its own text node — drop it and left-trim the
			// next text child so the description starts cleanly.
			seg = seg[1:]
			if len(seg) > 0 && seg[0] != nil && seg[0].Type == NodeText {
				clone := *seg[0]
				clone.Literal = strings.TrimLeft(clone.Literal, " \t")
				seg[0] = &clone
			}
		case lit == ": ":
			seg = seg[1:]
		case strings.HasPrefix(lit, ": "):
			clone := *head
			clone.Literal = strings.TrimLeft(lit[2:], " \t")
			seg = append([]*Node{&clone}, seg[1:]...)
		default:
			return nil, nil, false
		}
		descs = append(descs, &Node{Type: NodeDefinitionDesc, Children: seg})
	}
	if len(descs) == 0 {
		return nil, nil, false
	}
	term := &Node{Type: NodeDefinitionTerm, Children: termChildren}
	return term, descs, true
}

// --- Table of Contents ---

var tocDirectiveRe = regexp.MustCompile(`^\[\[[Tt][Oo][Cc]\]\]$`)

// processTOC replaces any top-level paragraph whose trimmed text is exactly
// [[toc]] (case-insensitive) with a NodeTableOfContents populated from the
// document's headings. The directive must sit on a line by itself; inline
// occurrences are left as literal text.
//
// tree-sitter-markdown parses "[[toc]]" as Text("[") + Link(raw="[toc]") +
// Text("]"), so we reconstruct the paragraph's surface text using link raw
// attributes rather than relying on collectNodeText (which descends into
// link children and loses the brackets).
func processTOC(doc *Document) {
	if doc == nil || doc.Root == nil {
		return
	}
	var headings []Heading
	collectHeadings(doc.Root, &headings)
	for i, child := range doc.Root.Children {
		if child == nil || child.Type != NodeParagraph {
			continue
		}
		text := strings.TrimSpace(collectSurfaceText(child))
		if !tocDirectiveRe.MatchString(text) {
			continue
		}
		doc.Root.Children[i] = buildTOCNode(headings)
	}
}

// collectSurfaceText reconstructs the raw-ish surface text of a node subtree,
// preferring a link's raw attribute over its rendered children so shortcut-link
// brackets survive round-trip for directive matching.
func collectSurfaceText(n *Node) string {
	var sb strings.Builder
	collectSurfaceTextInto(n, &sb)
	return sb.String()
}

func collectSurfaceTextInto(n *Node, sb *strings.Builder) {
	if n == nil {
		return
	}
	switch n.Type {
	case NodeText, NodeCodeSpan:
		sb.WriteString(n.Literal)
		return
	case NodeSoftBreak, NodeHardBreak:
		sb.WriteByte('\n')
		return
	case NodeLink:
		if raw := n.Attrs["raw"]; raw != "" {
			sb.WriteString(raw)
			return
		}
	}
	for _, c := range n.Children {
		collectSurfaceTextInto(c, sb)
	}
}

// buildTOCNode builds a NodeTableOfContents containing a nested NodeList that
// mirrors the heading hierarchy. Empty heading sets produce an empty TOC node
// so the renderer can still emit a <nav> wrapper if desired.
func buildTOCNode(headings []Heading) *Node {
	toc := &Node{Type: NodeTableOfContents}
	if len(headings) == 0 {
		return toc
	}
	minLevel := headings[0].Level
	for _, h := range headings[1:] {
		if h.Level < minLevel {
			minLevel = h.Level
		}
	}
	rootList := &Node{Type: NodeList}
	listAtLevel := map[int]*Node{minLevel: rootList}
	lastItemAtLevel := map[int]*Node{}
	for _, h := range headings {
		lvl := h.Level
		list := listAtLevel[lvl]
		if list == nil {
			for p := lvl - 1; p >= minLevel; p-- {
				if parent, ok := lastItemAtLevel[p]; ok {
					sub := &Node{Type: NodeList}
					parent.Children = append(parent.Children, sub)
					list = sub
					listAtLevel[lvl] = sub
					break
				}
			}
			if list == nil {
				list = rootList
				listAtLevel[lvl] = rootList
			}
		}
		item := tocListItem(h)
		list.Children = append(list.Children, item)
		lastItemAtLevel[lvl] = item
		for k := range listAtLevel {
			if k > lvl {
				delete(listAtLevel, k)
				delete(lastItemAtLevel, k)
			}
		}
	}
	toc.Children = []*Node{rootList}
	return toc
}

func tocListItem(h Heading) *Node {
	link := &Node{
		Type:     NodeLink,
		Attrs:    map[string]string{"href": "#" + h.ID},
		Children: []*Node{textNode(h.Text)},
	}
	return &Node{Type: NodeListItem, Children: []*Node{link}}
}

// --- Helpers ---

// walkNodes performs a depth-first walk of the AST, calling fn for each node.
// fn receives the node, its parent, and the index within the parent's children.
// If fn returns false, children of that node are not visited.
func walkNodes(root *Node, fn func(n *Node, parent *Node, index int) bool) {
	walkNodesRecursive(root, nil, 0, fn)
}

func walkNodesRecursive(n *Node, parent *Node, index int, fn func(*Node, *Node, int) bool) {
	if n == nil {
		return
	}
	if !fn(n, parent, index) {
		return
	}
	for i := 0; i < len(n.Children); i++ {
		walkNodesRecursive(n.Children[i], n, i, fn)
	}
}

// processInlinePattern scans all text nodes in the AST for a regex pattern,
// splitting text nodes to insert new nodes created by the factory function.
func processInlinePattern(root *Node, re *regexp.Regexp, factory func(match []string) *Node) {
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if len(n.Children) == 0 {
			return true
		}

		newChildren := make([]*Node, 0, len(n.Children))
		changed := false
		for _, child := range n.Children {
			if child.Type != NodeText {
				newChildren = append(newChildren, child)
				continue
			}
			split := splitTextByPattern(child.Literal, re, factory)
			if len(split) == 1 && split[0].Type == NodeText {
				newChildren = append(newChildren, child)
				continue
			}
			newChildren = append(newChildren, split...)
			changed = true
		}

		if changed {
			n.Children = newChildren
		}

		return true
	})
}

// splitTextByPattern splits a text string into alternating text and newly
// created nodes based on a regex pattern.
func splitTextByPattern(text string, re *regexp.Regexp, factory func(match []string) *Node) []*Node {
	locs := re.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return []*Node{textNode(text)}
	}

	var nodes []*Node
	cursor := 0

	for _, loc := range locs {
		if loc[0] > cursor {
			nodes = append(nodes, textNode(text[cursor:loc[0]]))
		}

		match := make([]string, len(loc)/2)
		for i := 0; i < len(loc)/2; i++ {
			if loc[i*2] >= 0 && loc[i*2+1] >= 0 {
				match[i] = text[loc[i*2]:loc[i*2+1]]
			}
		}

		nodes = append(nodes, factory(match))
		cursor = loc[1]
	}

	if cursor < len(text) {
		nodes = append(nodes, textNode(text[cursor:]))
	}

	return nodes
}

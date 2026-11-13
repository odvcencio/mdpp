package mdpp

import (
	"bytes"
	"regexp"
	"strings"
	"sync/atomic"
)

// postProcess walks the AST after parsing and applies Markdown++ transformations.
// Task lists and subscripts are handled during parsing (convertListItem and
// strikethrough detection respectively), so they are not processed here.
func postProcess(doc *Document) {
	if doc == nil || doc.Root == nil {
		return
	}
	flattenDocumentNodes(doc.Root)
	mergeAdjacentTables(doc)
	walkNodes(doc.Root, func(n *Node, parent *Node, index int) bool {
		if n.Type == NodeBlockquote {
			stripQuoteContinuationMarkers(n)
		}
		return true
	})
	repairStandaloneHTMLBlocks(doc)
	trimHeadingRanges(doc)
	extendIndentedCodeRanges(doc)
	annotateListTightness(doc)
	repairListCodeTabs(doc)
	processAdmonitions(doc.Root)
	processBlockquoteHeadings(doc.Root, doc.Source)
	footnoteDefs := processFootnotes(doc.Root)
	processInlineMath(doc.Root, doc.Source)
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

	// Stitch interrupted lists: tree-sitter-markdown (per CommonMark) treats
	// a blank-line-separated, non-indented paragraph between two list items
	// as a list-terminating paragraph, emitting two separate <ol>/<ul>s with
	// an orphan <p> in between. Restore the ergonomic "continuation belongs
	// to the preceding item" behavior authors expect.
	mergeInterruptedLists(doc.Root)

	// Definition lists: recognize `Term\n: Def` paragraphs and merge
	// adjacent term/description pairs into a single <dl>.
	processDefinitionLists(doc.Root)

	// Auto-embeds: `[[embed:url]]` on its own line → NodeAutoEmbed.
	processAutoEmbeds(doc.Root)

	// TOC runs last so every heading in the final AST is accounted for.
	processTOC(doc)

	// gosx-slides inline parse seams (Track D). These only fire on unambiguous
	// MDX constructs (uppercase-led tags, single-level brace interpolation), so
	// ordinary Markdown is untouched and existing output is unchanged.
	// Components run before expressions so component props are not scanned as
	// interpolations. Slide splitting is deliberately NOT here — it changes the
	// document shape and is opt-in via SplitSlides / Document.Slides.
	processComponents(doc.Root)
	processExpressions(doc.Root)
}

// processReferenceLinks walks the AST and, for every NodeLink that has a
// `ref` attribute but no resolved `href`, looks up the corresponding
// link_reference_definition and fills in href + title. Collapsed and
// shortcut links fall back to using the link text as the lookup key
// when no explicit label is present.
func processReferenceLinks(doc *Document) {
	if doc == nil || doc.Root == nil {
		return
	}
	hasComments := bytes.Contains(doc.Source, []byte("<!--"))
	if len(doc.linkRefDefs) == 0 && !hasComments {
		return
	}
	if len(doc.linkRefDefs) > 0 {
		walkNodes(doc.Root, func(n *Node, parent *Node, index int) bool {
			if (n.Type != NodeLink && n.Type != NodeImage) || n.Attrs == nil {
				return true
			}
			if _, inline := n.Attrs["href"]; inline {
				return true
			}
			if _, inline := n.Attrs["src"]; inline {
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
				label = n.Attrs["label"]
				if label == "" {
					label = collectNodeText(n)
				}
			}
			label = normalizeLinkLabel(label)
			if def, ok := doc.linkRefDefs[label]; ok {
				n.Attrs["resolved-ref"] = label
				if n.Type == NodeImage {
					n.Attrs["src"] = def.href
				} else {
					n.Attrs["href"] = def.href
				}
				if def.title != "" {
					n.Attrs["title"] = def.title
				}
				delete(n.Attrs, "ref")
				delete(n.Attrs, "raw")
			}
			return true
		})
	}
	if !hasComments {
		return
	}
	// Materialize unresolved marked reference links as source-backed child
	// nodes so ordinary AST copies and JSON round trips retain their output.
	walkNodes(doc.Root, func(n *Node, _ *Node, _ int) bool {
		if n.Type != NodeLink || n.referenceSuffix == nil {
			return true
		}
		suffix := *n.referenceSuffix
		n.referenceSuffix = nil
		if _, resolved := n.Attrs["href"]; resolved {
			return true
		}
		start, end := n.Range.StartByte, n.Range.EndByte
		children := make([]*Node, 0, len(n.Children)+2)
		children = append(children, textNodeRange("[", sourceRange(doc.Source, start, start+1)))
		children = append(children, n.Children...)
		suffixStart := end - len(suffix) - 1
		children = append(children, textNodeRange("]"+suffix, sourceRange(doc.Source, suffixStart, end)))
		n.Children = children
		delete(n.Attrs, "raw")
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
			Range: n.Range,
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
					captionWasEmpty := caption.Len() == len(line)
					rest = append(rest, cleanedTextCopy(child, suffixCleaner(func(s string) string {
						if captionWasEmpty {
							s = strings.TrimLeft(s, " \t")
						}
						_, after, _ := strings.Cut(s, "\n")
						return after
					})))
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
			clone := cleanedTextCopy(child, cleanAdmonitionTextAt)
			if clone.Literal == "" {
				continue
			}
			cleaned = append(cleaned, clone)
			leading = false
			continue
		}
		cleaned = append(cleaned, child)
		leading = false
	}
	return cleaned
}

func cleanAdmonitionText(text string) string {
	cleaned, _ := cleanAdmonitionTextAt(text)
	return cleaned
}

// cleanAdmonitionTextAt cleans text as cleanAdmonitionText does and also
// returns the offset in text of the first byte that is kept.
func cleanAdmonitionTextAt(text string) (string, int) {
	trimmed := strings.TrimLeft(text, "\n")
	from := len(text) - len(trimmed)
	lines := strings.Split(trimmed, "\n")
	for i, line := range lines {
		stripped := strings.TrimPrefix(line, "> ")
		stripped = strings.TrimPrefix(stripped, ">")
		if i == 0 {
			from += len(line) - len(stripped)
		}
		lines[i] = stripped
	}
	joined := strings.Join(lines, "\n")
	cleaned := strings.TrimLeft(joined, " \t")
	from += len(joined) - len(cleaned)
	if strings.TrimSpace(cleaned) == "" {
		return "", from
	}
	return cleaned, from
}

// --- Quote headings ---

var bracketedQuoteHeadingRe = regexp.MustCompile(`^\[[^\^!][\s\S]*\]$`)

func processBlockquoteHeadings(root *Node, source []byte) {
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
		strong := newNode(NodeStrong, parseInline(bracketedQuoteHeadingText(raw), nil)...)
		// The label sits one byte after the link's "[" when the link covers
		// exactly the bracketed source; the parsed inline nodes then get ranges.
		if r := firstNode.Range; r.StartLine != 0 && r.EndByte <= len(source) && string(source[r.StartByte:r.EndByte]) == raw {
			strong.Range = r
			strong.Children = parseInlineAt(bracketedQuoteHeadingText(raw), source, r.StartByte+1, nil)
		}
		firstChild.Children[0] = strong
		firstChild.Children = cleanBlockquoteHeadingContinuation(firstChild.Children)
		return true
	})
}

func bracketedQuoteHeadingText(raw string) string {
	if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
		return raw
	}
	return raw[1 : len(raw)-1]
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
		clone := cleanedTextCopy(child, cleanBlockquoteContinuationTextAt)
		if clone.Literal == "" {
			continue
		}
		cleaned = append(cleaned, clone)
	}
	return cleaned
}

func cleanBlockquoteContinuationText(text string) string {
	cleaned, _ := cleanBlockquoteContinuationTextAt(text)
	return cleaned
}

// cleanBlockquoteContinuationTextAt cleans text as
// cleanBlockquoteContinuationText does and also returns the offset in text of
// the first byte that is kept.
func cleanBlockquoteContinuationTextAt(text string) (string, int) {
	lines := strings.Split(text, "\n")
	from := 0
	for i, line := range lines {
		stripped := strings.TrimPrefix(line, "> ")
		stripped = strings.TrimPrefix(stripped, ">")
		if i == 0 {
			from = len(line) - len(stripped)
		}
		lines[i] = stripped
	}
	return strings.Join(lines, "\n"), from
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
			Range:    child.Range,
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
			if child.Type == NodeFootnoteRef {
				id := child.Attrs["id"]
				if id != "" {
					if _, seen := seenRefs[id]; !seen {
						refOrder = append(refOrder, id)
						seenRefs[id] = struct{}{}
					}
				}
				continue
			}
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
				Range: child.Range,
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
		out = append(out, cleanedTextCopy(first, suffixCleaner(func(s string) string {
			s = strings.TrimPrefix(s, ":")
			s = strings.TrimPrefix(s, " ")
			return strings.TrimPrefix(s, "\t")
		})))
	}
	out = append(out, children[1:]...)
	return out, true
}

// --- Math ---

var mathBlockRe = regexp.MustCompile(`^\$\$([\s\S]+?)\$\$$`)
var mathInlineRe = regexp.MustCompile(`\$([^\$\n]+?)\$`)

// processInlineMath converts $...$ to inline math and $$...$$ paragraphs to block math.
func processInlineMath(root *Node, source []byte) {
	// First: handle block math — paragraphs that are entirely $$...$$
	for i, child := range root.Children {
		if child.Type != NodeParagraph {
			continue
		}
		text := strings.TrimSpace(collectNodeText(child))
		if !strings.HasPrefix(text, "$$") || !strings.HasSuffix(text, "$$") {
			continue
		}
		if child.Range.StartLine != 0 && child.Range.StartByte >= 0 && child.Range.EndByte <= len(source) && child.Range.EndByte >= child.Range.StartByte {
			text = string(source[child.Range.StartByte:child.Range.EndByte])
		}
		text = strings.TrimSpace(text)
		match := mathBlockRe.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		root.Children[i] = &Node{
			Type:    NodeMathBlock,
			Literal: strings.TrimSpace(match[1]),
			Range:   child.Range,
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

// --- List stitching ---

// mergeInterruptedLists walks top-level children looking for the pattern
//
//	List₁  →  Paragraph (one or more)  →  List₂
//
// where both lists have the same marker type (both ordered or both
// unordered). The paragraphs are folded into List₁'s last item as
// continuation children and List₂'s items are appended to List₁. This
// makes multi-paragraph list items "just work" without the 3-space
// continuation indent CommonMark requires, and without forcing authors
// to use the trailing-space soft-break hack.
//
// Merging stops at any other block type (heading, code block, blockquote,
// admonition, table, etc.) so non-list content still properly breaks the
// list context.
func mergeInterruptedLists(root *Node) {
	if root == nil {
		return
	}
	for i := 0; i < len(root.Children); i++ {
		list := root.Children[i]
		if list == nil || list.Type != NodeList {
			continue
		}
		// Scan forward through a run of paragraphs until we find the next
		// block. If that block is a compatible NodeList, merge.
		j := i + 1
		for j < len(root.Children) && root.Children[j] != nil &&
			root.Children[j].Type == NodeParagraph {
			j++
		}
		if j == i+1 || j >= len(root.Children) {
			continue
		}
		next := root.Children[j]
		if next == nil || next.Type != NodeList {
			continue
		}
		if !sameListKind(list, next) {
			continue
		}

		// Attach paragraphs to the last item of `list`.
		lastItem := lastListItem(list)
		if lastItem == nil {
			continue
		}
		for k := i + 1; k < j; k++ {
			lastItem.Children = append(lastItem.Children, root.Children[k])
		}
		// The folded paragraphs now belong to the item, and the second
		// list's items to the first list: their ranges must cover them.
		lastItem.Range = extendRangeTo(lastItem.Range, root.Children[j-1].Range)
		list.Range = extendRangeTo(list.Range, next.Range)
		// Append `next`'s items to `list`.
		list.Children = append(list.Children, next.Children...)
		// Drop the paragraphs and the second list from root.
		root.Children = append(root.Children[:i+1], root.Children[j+1:]...)
		// Re-check at same index in case another list follows (chain merging).
		i--
	}
}

// extendRangeTo returns r grown to end where end ends. A range without a
// position stays without one.
func extendRangeTo(r, end Range) Range {
	if r.StartLine == 0 || end.StartLine == 0 || end.EndByte <= r.EndByte {
		return r
	}
	r.EndByte, r.EndLine, r.EndCol = end.EndByte, end.EndLine, end.EndCol
	return r
}

// sameListKind returns true when both lists are both ordered (or both
// unordered). A list is ordered if its `ordered` attribute is "true".
func sameListKind(a, b *Node) bool {
	if a == nil || b == nil {
		return false
	}
	aOrdered := a.Attrs != nil && a.Attrs["ordered"] == "true"
	bOrdered := b.Attrs != nil && b.Attrs["ordered"] == "true"
	return aOrdered == bOrdered
}

// lastListItem returns the last NodeListItem (or NodeTaskListItem) child
// of a NodeList, or nil if none exists.
func lastListItem(list *Node) *Node {
	for i := len(list.Children) - 1; i >= 0; i-- {
		c := list.Children[i]
		if c != nil && (c.Type == NodeListItem || c.Type == NodeTaskListItem) {
			return c
		}
	}
	return nil
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
			Range: child.Range,
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
	return "generic"
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
		if currentList.Range.StartLine == 0 {
			currentList.Range = child.Range
		} else {
			currentList.Range = extendRangeTo(currentList.Range, child.Range)
		}
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
				seg[0] = cleanedTextCopy(seg[0], suffixCleaner(func(s string) string {
					return strings.TrimLeft(s, " \t")
				}))
			}
		case lit == ": ":
			seg = seg[1:]
		case strings.HasPrefix(lit, ": "):
			clone := cleanedTextCopy(head, suffixCleaner(func(s string) string {
				s = strings.TrimLeft(s, " \t")
				return strings.TrimLeft(strings.TrimPrefix(s, ": "), " \t")
			}))
			seg = append([]*Node{clone}, seg[1:]...)
		default:
			return nil, nil, false
		}
		descs = append(descs, &Node{Type: NodeDefinitionDesc, Children: seg, Range: spanOfNodes(seg)})
	}
	if len(descs) == 0 {
		return nil, nil, false
	}
	term := &Node{Type: NodeDefinitionTerm, Children: termChildren, Range: spanOfNodes(termChildren)}
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
		if child == nil {
			continue
		}
		if child.Type == NodeTableOfContents {
			toc := buildTOCNode(headings)
			toc.Range = child.Range
			doc.Root.Children[i] = toc
			continue
		}
		if child.Type != NodeParagraph {
			continue
		}
		text := strings.TrimSpace(collectSurfaceText(child))
		if !tocDirectiveRe.MatchString(text) {
			continue
		}
		toc := buildTOCNode(headings)
		toc.Range = child.Range
		doc.Root.Children[i] = toc
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
			split := splitTextByPattern(child, re, factory)
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

// textSplitWork counts the work that splitting text nodes does, so that a test
// can show it grows with the text and not with the product of the text and the
// number of matches. bytes is the number of source bytes that were scanned to
// find line and column positions; builds is the number of times a raw-offset
// table was built (once per split text node).
var textSplitWork struct {
	bytes  atomic.Int64
	builds atomic.Int64
}

// textSpanMapper maps byte spans of a text node's literal to ranges in the
// source. It does the work that depends on the whole node once: it checks that
// the source text and the range agree, builds the table that maps decoded
// offsets back to raw offsets, and then moves one position cursor forward as
// the spans, which a caller asks for in increasing order, advance.
type textSpanMapper struct {
	base    Range
	src     string
	offsets []int // decoded offset -> raw offset; nil when literal == raw text
	ok      bool

	at, line, col int // the cursor: a source offset and its line and column
}

func newTextSpanMapper(n *Node) *textSpanMapper {
	m := &textSpanMapper{base: n.Range, src: n.Literal}
	if n.raw != "" {
		m.src = n.raw
	}
	if n.Range.StartLine == 0 || n.Range.EndByte-n.Range.StartByte != len(m.src) {
		return m
	}
	if n.raw != "" {
		if decodeMarkdownText(n.raw) != n.Literal {
			return m
		}
		m.offsets = literalRawOffsets(n.raw)
		textSplitWork.builds.Add(1)
	}
	m.ok = true
	m.at, m.line, m.col = 0, n.Range.StartLine, n.Range.StartCol
	return m
}

// position returns the line and column of source offset off (relative to the
// start of the node's source text), moving the cursor there.
func (m *textSpanMapper) position(off int) (int, int) {
	if off < m.at {
		m.at, m.line, m.col = 0, m.base.StartLine, m.base.StartCol
	}
	textSplitWork.bytes.Add(int64(off - m.at))
	m.line, m.col = advanceTextPosition(m.line, m.col, m.src[m.at:off])
	m.at = off
	return m.line, m.col
}

// span maps the literal's byte offsets a..b to a range in the source, and
// returns the raw source text of that part of the literal.
func (m *textSpanMapper) span(a, b int) (Range, string) {
	if !m.ok {
		return Range{}, ""
	}
	if m.offsets != nil {
		a, b = m.offsets[a], m.offsets[b]
	}
	if a < 0 || b < a || b > len(m.src) {
		return Range{}, ""
	}
	startLine, startCol := m.position(a)
	endLine, endCol := m.position(b)
	return Range{
		StartByte: m.base.StartByte + a,
		EndByte:   m.base.StartByte + b,
		StartLine: startLine,
		StartCol:  startCol,
		EndLine:   endLine,
		EndCol:    endCol,
	}, m.src[a:b]
}

// splitTextByPattern splits a text node into alternating text and newly
// created nodes based on a regex pattern. Each new node's range is the part of
// the text node's source text that it came from. When the literal is not the
// source text (escapes and entities were decoded, or line prefixes were
// removed) the offsets are mapped through the source text, and a node whose
// source text cannot be established has no range. A text fragment keeps the
// raw source text of its part, so that a later pass can map it again.
func splitTextByPattern(n *Node, re *regexp.Regexp, factory func(match []string) *Node) []*Node {
	text, base := n.Literal, n.Range
	locs := re.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return []*Node{textNodeRange(text, base)}
	}

	mapper := newTextSpanMapper(n)
	// fragment makes a text node for text[a:b] that keeps its raw source.
	fragment := func(a, b int) *Node {
		r, raw := mapper.span(a, b)
		node := &Node{Type: NodeText, Literal: text[a:b], Range: r}
		if r.StartLine != 0 && raw != node.Literal {
			node.raw = raw
		}
		return node
	}

	var nodes []*Node
	cursor := 0

	for _, loc := range locs {
		if loc[0] > cursor {
			nodes = append(nodes, fragment(cursor, loc[0]))
		}

		match := make([]string, len(loc)/2)
		for i := 0; i < len(loc)/2; i++ {
			if loc[i*2] >= 0 && loc[i*2+1] >= 0 {
				match[i] = text[loc[i*2]:loc[i*2+1]]
			}
		}

		node := factory(match)
		if node != nil {
			node.Range, _ = mapper.span(loc[0], loc[1])
			nodes = append(nodes, node)
		}
		cursor = loc[1]
	}

	if cursor < len(text) {
		nodes = append(nodes, fragment(cursor, len(text)))
	}

	return nodes
}

// cleanedTextCopy returns a copy of the text node n whose literal is what
// clean makes of it, with a range that covers the kept source text. clean
// returns the cleaned literal and the offset in its argument of the first kept
// byte; it must only delete text before that byte and at the starts of the
// following lines, so the node's range runs from that byte to the end of the
// node's source text. When the offset cannot be established (the source text
// and the range disagree) the copy has no range, which a caller can test with
// HasSourceRange.
func cleanedTextCopy(n *Node, clean func(string) (string, int)) *Node {
	literal, from := clean(n.Literal)
	clone := *n
	clone.Literal = literal
	clone.Range = Range{}
	clone.raw = ""
	if literal == "" || n.Range.StartLine == 0 {
		return &clone
	}
	src := n.Literal
	if n.raw != "" {
		src = n.raw
	}
	if n.Range.EndByte-n.Range.StartByte != len(src) {
		return &clone
	}
	if n.raw != "" {
		cleanedRaw, rawFrom := clean(src)
		if decodeMarkdownText(cleanedRaw) != literal {
			return &clone
		}
		if cleanedRaw != literal {
			clone.raw = cleanedRaw
		}
		from = rawFrom
	}
	if from < 0 || from >= len(src) {
		return &clone
	}
	clone.Range = textSliceRange(n.Range, src, from, len(src))
	return &clone
}

// suffixCleaner adapts a function that only trims the start of a string.
func suffixCleaner(trim func(string) string) func(string) (string, int) {
	return func(s string) (string, int) {
		cleaned := trim(s)
		return cleaned, len(s) - len(cleaned)
	}
}

// spanOfNodes returns the range from the start of the first node that has a
// source range to the end of the last one. It is the zero Range when none has.
func spanOfNodes(nodes []*Node) Range {
	var span Range
	for _, n := range nodes {
		if n == nil || !n.HasSourceRange() {
			continue
		}
		if span.StartLine == 0 {
			span = n.Range
			continue
		}
		if n.Range.StartByte < span.StartByte {
			span.StartByte, span.StartLine, span.StartCol = n.Range.StartByte, n.Range.StartLine, n.Range.StartCol
		}
		if n.Range.EndByte > span.EndByte {
			span.EndByte, span.EndLine, span.EndCol = n.Range.EndByte, n.Range.EndLine, n.Range.EndCol
		}
	}
	return span
}

// trimHeadingRanges ends a heading at its last line. The grammar can end a
// heading in a list item after the indentation of the next line ("Bar\n  ---\n  "),
// which is not part of the heading.
func trimHeadingRanges(doc *Document) {
	var visit func(n *Node)
	visit = func(n *Node) {
		for _, c := range n.Children {
			switch c.Type {
			case NodeHeading:
				trimHeadingRange(doc, c)
			case NodeBlockquote, NodeList, NodeListItem, NodeTaskListItem, NodeAdmonition, NodeContainerDirective, NodeFootnoteDef:
				visit(c)
			}
		}
	}
	visit(doc.Root)
}

func trimHeadingRange(doc *Document, n *Node) {
	if n.Range.StartLine == 0 || n.Range.EndByte > len(doc.Source) {
		return
	}
	start, end := n.Range.StartByte, n.Range.EndByte
	cut := end
	for cut > start && (doc.Source[cut-1] == ' ' || doc.Source[cut-1] == '\t') {
		cut--
	}
	if cut < end && cut > start && doc.Source[cut-1] == '\n' {
		n.Range = sourceRange(doc.Source, start, cut)
	}
}

// extendIndentedCodeRanges starts a top-level indented code block at the
// beginning of its first line. The grammar starts the node after the
// indentation, but the indentation is what makes the block.
func extendIndentedCodeRanges(doc *Document) {
	for _, n := range doc.Root.Children {
		if n == nil || n.Type != NodeCodeBlock || n.Range.StartLine == 0 || n.Range.StartCol == 1 || n.Range.EndByte > len(doc.Source) {
			continue
		}
		start := n.Range.StartByte
		lineStart := start
		for lineStart > 0 && (doc.Source[lineStart-1] == ' ' || doc.Source[lineStart-1] == '\t') {
			lineStart--
		}
		if lineStart == 0 || doc.Source[lineStart-1] == '\n' {
			n.Range = sourceRange(doc.Source, lineStart, n.Range.EndByte)
		}
	}
}

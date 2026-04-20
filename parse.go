package mdpp

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

var (
	admonitionMarkerLineRe    = regexp.MustCompile(`^(\s*>\s*)\[!(NOTE|WARNING|TIP|IMPORTANT|CAUTION)\](?:\s+.*)?$`)
	footnoteDefinitionRawRe   = regexp.MustCompile(`^ {0,3}\[\^([A-Za-z0-9_-]+)\]:[ \t]*(.*)$`)
	footnoteDefinitionLabelRe = regexp.MustCompile(`^\[\^([A-Za-z0-9_-]+)\]$`)
	inlineMarkdownLinkRe      = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s][^)]*)\)`)
)

type headingTextRepair struct {
	ordinal int
	text    string
}

// Cached languages, initialised once.
var (
	mdLangOnce   sync.Once
	mdLang       *gotreesitter.Language
	mdInlineOnce sync.Once
	mdInlineLang *gotreesitter.Language

	mdEntry       *grammars.LangEntry
	mdInlineEntry *grammars.LangEntry
)

func blockLang() *gotreesitter.Language {
	mdLangOnce.Do(func() {
		mdLang = grammars.MarkdownLanguage()
		mdEntry = grammars.DetectLanguageByName("markdown")
	})
	return mdLang
}

func inlineLang() *gotreesitter.Language {
	mdInlineOnce.Do(func() {
		mdInlineLang = grammars.MarkdownInlineLanguage()
		mdInlineEntry = grammars.DetectLanguageByName("markdown_inline")
	})
	return mdInlineLang
}

// Parse parses Markdown source into a Document AST.
func Parse(source []byte) (doc *Document, err error) {
	defer func() {
		if r := recover(); r != nil {
			src := normalizeLineEndings(append([]byte(nil), source...))
			root := &Node{
				Type:     NodeDocument,
				Range:    sourceRange(src, 0, len(src)),
				Children: []*Node{textNodeRange(string(src), sourceRange(src, 0, len(src)))},
			}
			doc = &Document{
				Root:   root,
				Source: src,
				diagnostics: []Diagnostic{{
					Code:     "MDPP-PARSE-000",
					Severity: SeverityError,
					Message:  "parser recovered from panic",
					Range:    sourceRange(src, 0, len(src)),
				}},
			}
			err = nil
		}
	}()
	return parseDocument(source), nil
}

// MustParse parses Markdown source and panics only if Parse returns an error.
func MustParse(source []byte) *Document {
	doc, err := Parse(source)
	if err != nil {
		panic(err)
	}
	return doc
}

func parseDocument(source []byte) *Document {
	source = normalizeLineEndings(source)
	source = lowerMarkdownPlusSource(source)
	// tree-sitter markdown requires a trailing newline for correct parsing.
	if len(source) > 0 && source[len(source)-1] != '\n' {
		source = append(source, '\n')
	}

	if doc := parseContainerDocument(source); doc != nil {
		return doc
	}

	// Pure all-indented documents are mis-parsed by tree-sitter-markdown
	// (punctuation leaves instead of an indented_code_block). Fall back to
	// a synthetic NodeCodeBlock when every non-blank line in the source is
	// indented 4+ spaces or a leading tab.
	if doc := parseAllIndentedDocument(source); doc != nil {
		return doc
	}

	// tree-sitter-markdown caps list nesting at 4 levels and emits an ERROR
	// wrapping the whole document beyond that. Detect pure-list documents
	// with deeper nesting and reconstruct the tree from indent levels.
	if doc := parseDeepNestedListDocument(source); doc != nil {
		return doc
	}
	if doc := parseSimpleBlockquoteDocument(source); doc != nil {
		return doc
	}
	parseSource, headingRepairs := protectSlowATXHeadingPunctuation(source)

	lang := blockLang()
	if lang == nil {
		return &Document{Root: &Node{Type: NodeDocument, Range: sourceRange(source, 0, len(source))}, Source: source}
	}

	tree, err := parsePooled(lang, mdEntry, parseSource)
	if err != nil || tree == nil {
		return &Document{Root: &Node{Type: NodeDocument, Range: sourceRange(source, 0, len(source))}, Source: source}
	}
	defer tree.Release()

	bt := gotreesitter.Bind(tree)
	root := convertBlock(bt, bt.RootNode(), source)
	if root == nil {
		root = &Node{Type: NodeDocument, Range: sourceRange(source, 0, len(source))}
	}
	repairProtectedHeadings(root, headingRepairs)
	doc := &Document{Root: root, Source: source}
	doc.linkRefDefs = collectLinkRefDefs(bt, bt.RootNode())
	doc.extractFrontmatter()
	postProcess(doc)
	return doc
}

// collectLinkRefDefs walks the tree-sitter AST gathering every
// link_reference_definition that is not a footnote definition. Keys are
// lowercased, whitespace-normalized labels per CommonMark.
func collectLinkRefDefs(bt *gotreesitter.BoundTree, root *gotreesitter.Node) map[string]linkRefDef {
	out := make(map[string]linkRefDef)
	var walk func(n *gotreesitter.Node)
	walk = func(n *gotreesitter.Node) {
		if n == nil {
			return
		}
		if bt.NodeType(n) == "link_reference_definition" {
			raw := strings.TrimRight(bt.NodeText(n), "\n")
			// Skip footnote defs.
			if footnoteDefinitionRawRe.MatchString(raw) {
				return
			}
			var label, dest, title string
			for i := 0; i < n.ChildCount(); i++ {
				c := n.Child(i)
				switch bt.NodeType(c) {
				case "link_label":
					label = bt.NodeText(c)
				case "link_destination":
					dest = bt.NodeText(c)
				case "link_title":
					title = stripQuotes(bt.NodeText(c))
				}
			}
			if footnoteDefinitionLabelRe.MatchString(label) {
				return
			}
			label = normalizeLinkLabel(label)
			if label != "" && dest != "" {
				out[label] = linkRefDef{href: dest, title: title}
			}
			return
		}
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return out
}

// normalizeLinkLabel lower-cases the label and collapses whitespace,
// matching CommonMark's link label matching rules. Input may include
// surrounding brackets ("[Foo]" or "Foo") — both are handled.
func normalizeLinkLabel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	s = strings.ToLower(s)
	return strings.Join(strings.Fields(s), " ")
}

func lowerMarkdownPlusSource(source []byte) []byte {
	if !strings.Contains(string(source), "[!") {
		return source
	}
	lines := strings.Split(string(source), "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	inAdmonition := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		}
		if inFence {
			out = append(out, line)
			continue
		}
		if admonitionMarkerLineRe.MatchString(line) {
			out = append(out, line)
			inAdmonition = true
			continue
		}
		if inAdmonition {
			if trimmed == "" {
				inAdmonition = false
				out = append(out, line)
				continue
			}
			if strings.HasPrefix(strings.TrimLeft(line, " \t"), ">") {
				out = append(out, line)
				continue
			}
			out = append(out, "> "+line)
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

func parseSimpleBlockquoteDocument(source []byte) *Document {
	text := strings.TrimRight(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	if text == "" || !strings.Contains(text, ">") {
		return nil
	}

	lines := strings.Split(text, "\n")
	contentLines := make([]string, 0, len(lines))
	sawQuote := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			contentLines = append(contentLines, "")
			continue
		}
		content, ok := stripBlockquoteMarker(line)
		if !ok {
			return nil
		}
		if strings.HasPrefix(strings.TrimSpace(content), "[") {
			// Defer admonitions and bracketed blockquote headings to the
			// slow path which has dedicated post-processors.
			return nil
		}
		contentLines = append(contentLines, content)
		sawQuote = true
	}
	if !sawQuote {
		return nil
	}

	// Recursively parse the stripped content so nested block structures
	// (nested blockquotes, lists, code fences, headings) survive. The
	// recursive Parse runs its own postProcess; wrapping here must not
	// re-run it or footnote / emoji processors would double-fire.
	inner := MustParse([]byte(strings.Join(contentLines, "\n") + "\n"))
	quote := &Node{Type: NodeBlockquote, Range: sourceRange(source, 0, len(source))}
	if inner != nil && inner.Root != nil {
		quote.Children = inner.Root.Children
		clearNodeRanges(quote)
		quote.Range = sourceRange(source, 0, len(source))
	}
	doc := &Document{Root: &Node{Type: NodeDocument, Children: []*Node{quote}, Range: sourceRange(source, 0, len(source))}, Source: source}
	doc.extractFrontmatter()
	return doc
}

func stripBlockquoteMarker(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, ">") {
		return "", false
	}
	content := strings.TrimPrefix(trimmed, ">")
	if strings.HasPrefix(content, " ") || strings.HasPrefix(content, "\t") {
		content = content[1:]
	}
	return content, true
}

type sourceLine struct {
	start int
	end   int
	next  int
	text  string
}

func parseContainerDocument(source []byte) *Document {
	if !bytes.Contains(source, []byte(":::")) {
		return nil
	}
	lines := sourceLines(source)
	children, diagnostics, found := parseContainerChildren(source, lines, 0, len(lines), 0, len(source))
	if !found {
		return nil
	}
	root := &Node{Type: NodeDocument, Children: children, Range: sourceRange(source, 0, len(source))}
	doc := &Document{Root: root, Source: source, diagnostics: diagnostics}
	doc.extractFrontmatter()
	processTOC(doc)
	return doc
}

func sourceLines(source []byte) []sourceLine {
	lines := make([]sourceLine, 0, bytes.Count(source, []byte{'\n'})+1)
	for start := 0; start < len(source); {
		end := start
		for end < len(source) && source[end] != '\n' {
			end++
		}
		next := end
		if next < len(source) && source[next] == '\n' {
			next++
		}
		lines = append(lines, sourceLine{
			start: start,
			end:   end,
			next:  next,
			text:  string(source[start:end]),
		})
		start = next
	}
	return lines
}

func parseContainerChildren(source []byte, lines []sourceLine, from int, to int, chunkStart int, chunkEnd int) ([]*Node, []Diagnostic, bool) {
	var children []*Node
	var diagnostics []Diagnostic
	found := false
	cursor := chunkStart
	inFence := false

	for i := from; i < to; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line.text)
		if isMarkdownFenceLine(trimmed) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		info, ok := parseContainerOpenLine(line.text)
		if !ok {
			continue
		}
		found = true
		children = appendParsedChunk(children, source[cursor:line.start], source, cursor)

		closeIndex := -1
		depth := 1
		bodyFence := false
		for j := i + 1; j < to; j++ {
			bodyLine := lines[j]
			bodyTrimmed := strings.TrimSpace(bodyLine.text)
			if isMarkdownFenceLine(bodyTrimmed) {
				bodyFence = !bodyFence
				continue
			}
			if bodyFence {
				continue
			}
			if isContainerCloseLine(bodyLine.text, info.fenceLen) {
				depth--
				if depth == 0 {
					closeIndex = j
					break
				}
				continue
			}
			if nested, ok := parseContainerOpenLine(bodyLine.text); ok && nested.fenceLen == info.fenceLen {
				depth++
			}
		}

		bodyStart := line.next
		bodyEnd := chunkEnd
		closeEnd := chunkEnd
		nextLine := to
		if closeIndex >= 0 {
			bodyEnd = lines[closeIndex].start
			closeEnd = lines[closeIndex].next
			nextLine = closeIndex + 1
		} else {
			diagnostics = append(diagnostics, Diagnostic{
				Code:     "MDPP-PARSE-002",
				Severity: SeverityWarning,
				Message:  "container directive auto-closed at end of document",
				Range:    sourceRange(source, line.start, line.end),
			})
		}

		bodyDoc := parseDocument(source[bodyStart:bodyEnd])
		var bodyChildren []*Node
		if bodyDoc != nil && bodyDoc.Root != nil {
			bodyChildren = append(bodyChildren, bodyDoc.Root.Children...)
			for _, child := range bodyChildren {
				shiftNodeRanges(child, source, bodyStart)
			}
		}

		container := &Node{
			Type:     NodeContainerDirective,
			Children: bodyChildren,
			Attrs:    info.attrs,
			Range:    sourceRange(source, line.start, closeEnd),
		}
		children = append(children, container)
		diagnostics = append(diagnostics, bodyDocDiagnostics(bodyDoc, source, bodyStart)...)
		cursor = closeEnd
		i = nextLine - 1
	}

	children = appendParsedChunk(children, source[cursor:chunkEnd], source, cursor)
	return children, diagnostics, found
}

func appendParsedChunk(children []*Node, chunk []byte, source []byte, offset int) []*Node {
	if strings.TrimSpace(string(chunk)) == "" {
		return children
	}
	doc := parseDocument(chunk)
	if doc == nil || doc.Root == nil {
		return children
	}
	for _, child := range doc.Root.Children {
		shiftNodeRanges(child, source, offset)
		children = append(children, child)
	}
	return children
}

func bodyDocDiagnostics(doc *Document, source []byte, offset int) []Diagnostic {
	if doc == nil || len(doc.diagnostics) == 0 {
		return nil
	}
	out := make([]Diagnostic, len(doc.diagnostics))
	copy(out, doc.diagnostics)
	for i := range out {
		out[i].Range = sourceRange(source, offset+out[i].Range.StartByte, offset+out[i].Range.EndByte)
	}
	return out
}

func shiftNodeRanges(root *Node, source []byte, offset int) {
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if n.Range.StartLine != 0 {
			n.Range = sourceRange(source, offset+n.Range.StartByte, offset+n.Range.EndByte)
		}
		return true
	})
}

type containerOpenInfo struct {
	fenceLen int
	attrs    map[string]string
}

func parseContainerOpenLine(line string) (containerOpenInfo, bool) {
	if !strings.HasPrefix(line, ":::") {
		return containerOpenInfo{}, false
	}
	i := 0
	for i < len(line) && line[i] == ':' {
		i++
	}
	rest := strings.TrimSpace(line[i:])
	if rest == "" || strings.HasPrefix(rest, ":") {
		return containerOpenInfo{}, false
	}
	nameEnd := 0
	for nameEnd < len(rest) {
		c := rest[nameEnd]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (nameEnd > 0 && ((c >= '0' && c <= '9') || c == '_' || c == '-')) {
			nameEnd++
			continue
		}
		break
	}
	if nameEnd == 0 {
		return containerOpenInfo{}, false
	}
	name := strings.ToLower(rest[:nameEnd])
	raw := strings.TrimSpace(rest[nameEnd:])
	attrs := map[string]string{"name": name, "raw": raw}
	parseContainerInfoAttrs(attrs, raw)
	return containerOpenInfo{fenceLen: i, attrs: attrs}, true
}

func isContainerCloseLine(line string, fenceLen int) bool {
	if len(line) != fenceLen {
		return false
	}
	for i := 0; i < len(line); i++ {
		if line[i] != ':' {
			return false
		}
	}
	return true
}

func parseContainerInfoAttrs(attrs map[string]string, raw string) {
	rest := strings.TrimSpace(raw)
	if rest == "" {
		return
	}
	if strings.HasPrefix(rest, "\"") {
		if end := strings.Index(rest[1:], "\""); end >= 0 {
			attrs["title"] = rest[1 : end+1]
			rest = strings.TrimSpace(rest[end+2:])
		}
	}
	if start := strings.Index(rest, "{"); start >= 0 {
		if end := strings.LastIndex(rest, "}"); end > start {
			if title := strings.TrimSpace(rest[:start]); title != "" && attrs["title"] == "" {
				attrs["title"] = strings.Trim(title, `"`)
			}
			parseContainerAttrGroup(attrs, rest[start+1:end])
			return
		}
	}
	if attrs["title"] == "" {
		attrs["title"] = strings.Trim(rest, `"`)
	}
}

func parseContainerAttrGroup(attrs map[string]string, group string) {
	var classes []string
	extra := map[string]string{}
	for _, field := range strings.Fields(group) {
		switch {
		case strings.HasPrefix(field, "#") && len(field) > 1:
			attrs["id"] = strings.TrimPrefix(field, "#")
		case strings.HasPrefix(field, ".") && len(field) > 1:
			classes = append(classes, strings.TrimPrefix(field, "."))
		case strings.Contains(field, "="):
			key, value, _ := strings.Cut(field, "=")
			key = strings.TrimSpace(key)
			value = strings.Trim(strings.TrimSpace(value), `"`)
			if key != "" {
				extra[key] = value
			}
		}
	}
	if len(classes) > 0 {
		attrs["class"] = strings.Join(classes, " ")
	}
	if len(extra) > 0 {
		data, err := json.Marshal(extra)
		if err == nil {
			attrs["attrs"] = string(data)
		}
	}
}

func protectSlowATXHeadingPunctuation(source []byte) ([]byte, []headingTextRepair) {
	var protected []byte
	var repairs []headingTextRepair
	inFence := false
	headingOrdinal := 0
	previousLineCanBeSetextHeading := false

	for start := 0; start < len(source); {
		end := start
		for end < len(source) && source[end] != '\n' {
			end++
		}
		line := source[start:end]
		trimmed := strings.TrimSpace(string(line))

		if isMarkdownFenceLine(trimmed) {
			inFence = !inFence
			previousLineCanBeSetextHeading = false
		} else if inFence {
			previousLineCanBeSetextHeading = false
		} else if text, punctOffset, ok := slowATXHeadingPunctuation(line); ok {
			if protected == nil {
				protected = append([]byte(nil), source...)
			}
			protected[start+punctOffset] = '0'
			repairs = append(repairs, headingTextRepair{ordinal: headingOrdinal, text: text})
			headingOrdinal++
			previousLineCanBeSetextHeading = false
		} else if isATXHeadingLine(line) {
			headingOrdinal++
			previousLineCanBeSetextHeading = false
		} else if isSetextUnderlineLine(line) && previousLineCanBeSetextHeading {
			headingOrdinal++
			previousLineCanBeSetextHeading = false
		} else {
			previousLineCanBeSetextHeading = trimmed != ""
		}

		if end == len(source) {
			break
		}
		start = end + 1
	}
	if protected == nil {
		return source, nil
	}
	return protected, repairs
}

func slowATXHeadingPunctuation(line []byte) (string, int, bool) {
	textStart, textEnd, ok := atxHeadingTextRange(line)
	if !ok || textStart >= textEnd {
		return "", 0, false
	}
	punctOffset := textEnd - 1
	switch line[punctOffset] {
	case '.', '?', '!':
	default:
		return "", 0, false
	}
	text := string(line[textStart:textEnd])
	if !strings.ContainsAny(text, " \t") {
		return "", 0, false
	}
	return text, punctOffset, true
}

func isMarkdownFenceLine(line string) bool {
	return strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~")
}

// parseAllIndentedDocument returns a Document containing a single
// NodeCodeBlock when every non-blank line in source is indented 4+ spaces
// or a leading tab. tree-sitter-markdown does not emit indented_code_block
// for this shape; CommonMark spec does (§ 4.4 allows them at document
// start).
func parseAllIndentedDocument(source []byte) *Document {
	text := strings.TrimRight(string(source), "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	sawContent := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "\t") {
			return nil
		}
		sawContent = true
	}
	if !sawContent {
		return nil
	}
	stripped := make([]string, len(lines))
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "    "):
			stripped[i] = line[4:]
		case strings.HasPrefix(line, "\t"):
			stripped[i] = line[1:]
		default:
			stripped[i] = line
		}
	}
	code := &Node{
		Type:    NodeCodeBlock,
		Literal: strings.TrimRight(strings.Join(stripped, "\n"), "\n") + "\n",
		Attrs:   map[string]string{},
		Range:   sourceRange(source, 0, len(source)),
	}
	doc := &Document{Root: &Node{Type: NodeDocument, Children: []*Node{code}, Range: sourceRange(source, 0, len(source))}, Source: source}
	doc.extractFrontmatter()
	return doc
}

// parseDeepNestedListDocument handles pure-list documents whose nesting
// depth exceeds tree-sitter-markdown's supported limit (4 levels). When
// every non-blank source line is a list item and at least one item is at
// depth 5 or deeper, rebuild the list tree from indentation levels.
func parseDeepNestedListDocument(source []byte) *Document {
	text := strings.TrimRight(string(source), "\n")
	if text == "" {
		return nil
	}
	type listLine struct {
		level  int
		marker byte
		text   string
	}
	lines := strings.Split(text, "\n")
	items := make([]listLine, 0, len(lines))
	maxLevel := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		level := 0
		rest := line
		for strings.HasPrefix(rest, "  ") {
			level++
			rest = rest[2:]
		}
		// Reject if the "level" prefix mixed with other whitespace or a non-list line.
		if len(rest) < 2 {
			return nil
		}
		marker := rest[0]
		if marker != '-' && marker != '*' && marker != '+' {
			return nil
		}
		if rest[1] != ' ' {
			return nil
		}
		items = append(items, listLine{level: level, marker: marker, text: rest[2:]})
		if level > maxLevel {
			maxLevel = level
		}
	}
	if len(items) == 0 || maxLevel < 5 {
		// Let tree-sitter handle everything else it's equipped to handle.
		return nil
	}

	// Build nested list tree using a stack keyed by indent level.
	type listFrame struct {
		level int
		list  *Node
		last  *Node // last list item at this level
	}
	rootList := &Node{Type: NodeList, Attrs: map[string]string{}, Range: sourceRange(source, 0, len(source))}
	stack := []listFrame{{level: -1, list: rootList}}
	for _, it := range items {
		// Pop frames deeper than current level.
		for len(stack) > 1 && stack[len(stack)-1].level >= it.level {
			stack = stack[:len(stack)-1]
		}
		target := stack[len(stack)-1]
		// If current level is strictly deeper than the parent's, nest a
		// new list under the parent's last item.
		for target.level < it.level-1 {
			// Fill intermediate levels with a pass-through item.
			panic("unreachable: items are validated to have level <= top stack level + 1")
		}
		if target.level < it.level {
			if target.last == nil {
				// No parent item to nest under — treat as root list.
				target = stack[0]
			} else {
				sub := &Node{Type: NodeList, Attrs: map[string]string{}}
				target.last.Children = append(target.last.Children, sub)
				target = listFrame{level: it.level - 1, list: sub}
			}
		}
		para := &Node{Type: NodeParagraph, Children: parseInline(it.text, source)}
		item := &Node{Type: NodeListItem, Children: []*Node{para}}
		target.list.Children = append(target.list.Children, item)
		if target.level == it.level-1 || target.list == rootList {
			stack = append(stack, listFrame{level: it.level, list: target.list, last: item})
		} else {
			stack[len(stack)-1].last = item
		}
	}
	doc := &Document{Root: &Node{Type: NodeDocument, Children: []*Node{rootList}, Range: sourceRange(source, 0, len(source))}, Source: source}
	postProcess(doc)
	return doc
}

// normalizeLineEndings converts CRLF and lone CR to LF so downstream
// processing (tree-sitter parse, postProcess regexes, render output)
// does not leak carriage returns into the rendered HTML.
func normalizeLineEndings(source []byte) []byte {
	if bytes.IndexByte(source, '\r') < 0 {
		return source
	}
	out := make([]byte, 0, len(source))
	for i := 0; i < len(source); i++ {
		if source[i] == '\r' {
			if i+1 < len(source) && source[i+1] == '\n' {
				continue
			}
			out = append(out, '\n')
			continue
		}
		out = append(out, source[i])
	}
	return out
}

func newNodeFromTree(typ NodeType, n *gotreesitter.Node, children ...*Node) *Node {
	node := newNode(typ, children...)
	node.Range = treeNodeRange(n)
	return node
}

func applyTreeRange(node *Node, n *gotreesitter.Node) *Node {
	if node != nil {
		node.Range = treeNodeRange(n)
	}
	return node
}

func treeNodeRange(n *gotreesitter.Node) Range {
	if n == nil {
		return Range{}
	}
	start := n.StartPoint()
	end := n.EndPoint()
	return Range{
		StartByte: int(n.StartByte()),
		EndByte:   int(n.EndByte()),
		StartLine: int(start.Row) + 1,
		StartCol:  int(start.Column) + 1,
		EndLine:   int(end.Row) + 1,
		EndCol:    int(end.Column) + 1,
	}
}

func sourceRange(source []byte, start int, end int) Range {
	if source == nil || start < 0 || end < start {
		return Range{}
	}
	if start > len(source) {
		start = len(source)
	}
	if end > len(source) {
		end = len(source)
	}
	startLine, startCol := sourceLineCol(source, start)
	endLine, endCol := sourceLineCol(source, end)
	return Range{
		StartByte: start,
		EndByte:   end,
		StartLine: startLine,
		StartCol:  startCol,
		EndLine:   endLine,
		EndCol:    endCol,
	}
}

func sourceLineCol(source []byte, offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	line, col := 1, 1
	for i := 0; i < offset; i++ {
		if source[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}

func inlineSpanRange(source []byte, baseOffset int, start int, end int) Range {
	if baseOffset < 0 {
		return Range{}
	}
	return sourceRange(source, baseOffset+start, baseOffset+end)
}

func textSliceRange(base Range, text string, start int, end int) Range {
	if base.StartLine == 0 || start < 0 || end < start {
		return Range{}
	}
	if start > len(text) {
		start = len(text)
	}
	if end > len(text) {
		end = len(text)
	}
	startLine, startCol := advanceTextPosition(base.StartLine, base.StartCol, text[:start])
	endLine, endCol := advanceTextPosition(base.StartLine, base.StartCol, text[:end])
	return Range{
		StartByte: base.StartByte + start,
		EndByte:   base.StartByte + end,
		StartLine: startLine,
		StartCol:  startCol,
		EndLine:   endLine,
		EndCol:    endCol,
	}
}

func advanceTextPosition(line int, col int, text string) (int, int) {
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}

func textNodeRange(text string, r Range) *Node {
	node := textNode(text)
	node.Range = r
	return node
}

func clearNodeRanges(root *Node) {
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		n.Range = Range{}
		return true
	})
}

func convertCodeBlock(bt *gotreesitter.BoundTree, n *gotreesitter.Node, typ string) *Node {
	cb := newNodeFromTree(NodeCodeBlock, n)
	cb.Attrs = make(map[string]string)
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		switch bt.NodeType(child) {
		case "info_string":
			langNode := findChild(bt, child, "language")
			if langNode != nil {
				cb.Attrs["language"] = strings.TrimSpace(bt.NodeText(langNode))
			} else {
				cb.Attrs["language"] = strings.TrimSpace(bt.NodeText(child))
			}
		case "code_fence_content":
			cb.Literal = bt.NodeText(child)
		}
	}
	if typ == "indented_code_block" {
		cb.Literal = stripIndentedCodeBlock(bt.NodeText(n))
	}
	return codeBlockToDiagram(cb)
}

// stripIndentedCodeBlock removes the 4-space (or tab) indent from each line
// of an indented code block and trims the trailing blank line tree-sitter
// includes in the node text.
func stripIndentedCodeBlock(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "    "):
			lines[i] = line[4:]
		case strings.HasPrefix(line, "\t"):
			lines[i] = line[1:]
		}
	}
	out := strings.Join(lines, "\n")
	return strings.TrimRight(out, "\n") + "\n"
}

func isATXHeadingLine(line []byte) bool {
	_, _, ok := atxHeadingTextRange(line)
	return ok
}

func atxHeadingTextRange(line []byte) (int, int, bool) {
	lineEnd := len(line)
	if lineEnd > 0 && line[lineEnd-1] == '\r' {
		lineEnd--
	}
	i := 0
	for i < lineEnd && line[i] == ' ' {
		i++
	}
	if i > 3 {
		return 0, 0, false
	}
	hashStart := i
	for i < lineEnd && line[i] == '#' {
		i++
	}
	hashes := i - hashStart
	if hashes == 0 || hashes > 6 {
		return 0, 0, false
	}
	if i < lineEnd && line[i] != ' ' && line[i] != '\t' {
		return 0, 0, false
	}
	for i < lineEnd && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	textStart := i
	textEnd := trimRightSpaceBytes(line, textStart, lineEnd)
	if textStart >= textEnd {
		return 0, 0, false
	}

	j := textEnd - 1
	for j >= textStart && line[j] == '#' {
		j--
	}
	if j >= textStart && j < textEnd-1 && (line[j] == ' ' || line[j] == '\t') {
		textEnd = trimRightSpaceBytes(line, textStart, j+1)
	}
	if textStart >= textEnd {
		return 0, 0, false
	}
	return textStart, textEnd, true
}

func trimRightSpaceBytes(line []byte, start int, end int) int {
	for end > start && (line[end-1] == ' ' || line[end-1] == '\t') {
		end--
	}
	return end
}

func isSetextUnderlineLine(line []byte) bool {
	lineEnd := len(line)
	if lineEnd > 0 && line[lineEnd-1] == '\r' {
		lineEnd--
	}
	i := 0
	for i < lineEnd && line[i] == ' ' {
		i++
	}
	if i > 3 {
		return false
	}
	if i >= lineEnd || (line[i] != '=' && line[i] != '-') {
		return false
	}
	marker := line[i]
	for i < lineEnd && line[i] == marker {
		i++
	}
	for i < lineEnd && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i == lineEnd
}

func repairProtectedHeadings(root *Node, repairs []headingTextRepair) {
	if root == nil || len(repairs) == 0 {
		return
	}
	headingOrdinal := 0
	repairIndex := 0
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if n.Type != NodeHeading {
			return true
		}
		if repairIndex < len(repairs) && repairs[repairIndex].ordinal == headingOrdinal {
			n.Children = parseInline(repairs[repairIndex].text, nil)
			repairIndex++
		}
		headingOrdinal++
		return true
	})
}

// convertBlock recursively converts a block-level tree-sitter node into an AST Node.
func convertBlock(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) *Node {
	if n == nil {
		return nil
	}
	typ := bt.NodeType(n)

	switch typ {
	case "document":
		doc := newNodeFromTree(NodeDocument, n)
		doc.Children = convertBlockChildren(bt, n, source)
		return doc

	case "section":
		// section is a wrapper in tree-sitter-markdown; flatten its children.
		// For simple documents, tree-sitter may omit wrapper nodes and place
		// children (e.g. list_item, fenced_code_block_delimiter) directly
		// under section. We detect these patterns and synthesise the wrapper.
		if synth := synthesiseSectionContent(bt, n, source); synth != nil {
			return synth
		}
		nodes := convertBlockChildren(bt, n, source)
		if len(nodes) == 1 {
			return nodes[0]
		}
		// Return a pseudo-document to hold multiple section children;
		// the caller will merge them.
		wrapper := newNodeFromTree(NodeDocument, n)
		wrapper.Children = nodes
		return wrapper

	case "atx_heading", "setext_heading":
		heading := newNodeFromTree(NodeHeading, n)
		level := headingLevel(bt, n)
		if heading.Attrs == nil {
			heading.Attrs = make(map[string]string)
		}
		heading.Attrs["level"] = levelStr(level)
		if text, start, ok := extractHeadingTextSpan(bt, n); ok && text != "" {
			heading.Children = append(heading.Children, parseInlineAt(text, source, start)...)
		}
		return heading

	case "paragraph":
		nodeText := strings.TrimRight(bt.NodeText(n), "\n")
		if footnoteDefs := convertFootnoteDefinitionParagraph(nodeText, source); footnoteDefs != nil {
			applyTreeRange(footnoteDefs, n)
			return footnoteDefs
		}

		para := newNodeFromTree(NodeParagraph, n)
		nodeStart := n.StartByte()

		cursor := uint32(0) // relative to nodeStart
		textLen := uint32(len(nodeText))
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			cs := child.StartByte()
			ce := child.EndByte()
			// Ensure offsets are within the paragraph and relative to nodeStart
			if cs < nodeStart {
				cs = nodeStart
			}
			if ce < nodeStart {
				continue
			}
			childStart := cs - nodeStart
			childEnd := ce - nodeStart
			if childStart > textLen {
				childStart = textLen
			}
			if childEnd > textLen {
				childEnd = textLen
			}

			// Gap text before this child
			if childStart > cursor {
				gap := nodeText[cursor:childStart]
				if gap != "" {
					para.Children = append(para.Children, parseInlineAt(gap, source, int(nodeStart+childStart))...)
				}
			}

			if bt.NodeType(child) == "inline" {
				para.Children = append(para.Children, parseInlineAt(bt.NodeText(child), source, int(child.StartByte()))...)
			}
			if childEnd > cursor {
				cursor = childEnd
			}
		}
		// Trailing gap text after last child
		if cursor < textLen {
			gap := nodeText[cursor:]
			if strings.TrimSpace(gap) != "" {
				para.Children = append(para.Children, parseInlineAt(gap, source, int(nodeStart+cursor))...)
			}
		}
		// Split text nodes on newlines → insert NodeSoftBreak
		para.Children = splitTextNewlines(para.Children)
		return para

	case "fenced_code_block", "indented_code_block":
		return convertCodeBlock(bt, n, typ)

	case "block_quote":
		bq := newNodeFromTree(NodeBlockquote, n)
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			childType := bt.NodeType(child)
			if childType == "block_quote_marker" || childType == "block_continuation" {
				continue
			}
			if converted := convertBlock(bt, child, source); converted != nil {
				bq.Children = append(bq.Children, converted)
			}
		}
		return bq

	case "list":
		list := newNodeFromTree(NodeList, n)
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			if bt.NodeType(child) == "list_item" {
				applyListMarkerAttrs(bt, list, child)
				if converted := convertListItem(bt, child, source); converted != nil {
					list.Children = append(list.Children, converted)
				}
			}
		}
		return list

	case "thematic_break":
		return newNodeFromTree(NodeThematicBreak, n)

	case "pipe_table":
		return convertTable(bt, n, source)

	case "html_block":
		block := newNodeFromTree(NodeHTMLBlock, n)
		block.Literal = bt.NodeText(n)
		return block

	case "link_reference_definition":
		raw := strings.TrimRight(bt.NodeText(n), "\n")
		if match := footnoteDefinitionRawRe.FindStringSubmatch(raw); match != nil {
			fn := newNodeFromTree(NodeFootnoteDef, n)
			fn.Attrs = map[string]string{"id": match[1]}
			if strings.TrimSpace(match[2]) != "" {
				fn.Children = append(fn.Children, parseFootnoteDefinitionInline(match[2], source)...)
			}
			return fn
		}

		// Detect footnote definitions: [^id]: content
		var label, dest string
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := bt.NodeType(child)
			switch ct {
			case "link_label":
				label = bt.NodeText(child)
			case "link_destination":
				dest = bt.NodeText(child)
			}
		}
		// Footnote defs have labels like [^id]
		if match := footnoteDefinitionLabelRe.FindStringSubmatch(label); match != nil {
			fn := newNodeFromTree(NodeFootnoteDef, n)
			fn.Attrs = map[string]string{"id": match[1]}
			if strings.TrimSpace(dest) != "" {
				fn.Children = append(fn.Children, parseFootnoteDefinitionInline(dest, source)...)
			}
			return fn
		}
		// Regular link reference definitions — skip (handled by tree-sitter linking)
		return nil

	default:
		// Skip node types we don't map (block_continuation, markers, etc.)
		return nil
	}
}

func parseFootnoteDefinitionInline(text string, source []byte) []*Node {
	matches := inlineMarkdownLinkRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return parseInline(text, source)
	}

	nodes := make([]*Node, 0, len(matches)*2+1)
	cursor := 0
	for _, match := range matches {
		if match[0] > cursor {
			nodes = append(nodes, parseInline(text[cursor:match[0]], source)...)
		}
		link := newNode(NodeLink)
		link.Attrs = map[string]string{"href": text[match[4]:match[5]]}
		link.Children = append(link.Children, textNode(text[match[2]:match[3]]))
		nodes = append(nodes, link)
		cursor = match[1]
	}
	if cursor < len(text) {
		nodes = append(nodes, parseInline(text[cursor:], source)...)
	}
	return nodes
}

func convertFootnoteDefinitionParagraph(text string, source []byte) *Node {
	if !strings.Contains(text, "[^") {
		return nil
	}
	lines := strings.Split(text, "\n")
	defs := make([]*Node, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		match := footnoteDefinitionRawRe.FindStringSubmatch(line)
		if match == nil {
			return nil
		}
		def := newNode(NodeFootnoteDef)
		def.Attrs = map[string]string{"id": match[1]}
		if strings.TrimSpace(match[2]) != "" {
			def.Children = append(def.Children, parseFootnoteDefinitionInline(match[2], source)...)
		}
		defs = append(defs, def)
	}
	if len(defs) == 0 {
		return nil
	}
	if len(defs) == 1 {
		return defs[0]
	}
	doc := newNode(NodeDocument)
	doc.Children = defs
	return doc
}

// convertListItem converts a list_item node into a NodeListItem.
func convertListItem(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) *Node {
	item := newNodeFromTree(NodeListItem, n)
	isTask := false
	checked := false
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		childType := bt.NodeType(child)
		// Detect task list markers from tree-sitter
		if childType == "task_list_marker_checked" {
			isTask = true
			checked = true
			continue
		}
		if childType == "task_list_marker_unchecked" {
			isTask = true
			checked = false
			continue
		}
		// Skip markers and continuations
		if strings.HasPrefix(childType, "list_marker") || childType == "block_continuation" {
			continue
		}
		if converted := convertBlock(bt, child, source); converted != nil {
			item.Children = append(item.Children, converted)
		}
	}
	if isTask {
		item.Type = NodeTaskListItem
		if item.Attrs == nil {
			item.Attrs = make(map[string]string)
		}
		if checked {
			item.Attrs["checked"] = "true"
		} else {
			item.Attrs["checked"] = "false"
		}
	}
	return item
}

func applyListMarkerAttrs(bt *gotreesitter.BoundTree, list *Node, item *gotreesitter.Node) {
	if list.Attrs != nil && list.Attrs["ordered"] != "" {
		return
	}
	marker := listItemMarker(bt, item)
	if marker == nil {
		return
	}
	if !isOrderedListMarker(bt.NodeType(marker)) {
		return
	}
	if list.Attrs == nil {
		list.Attrs = make(map[string]string)
	}
	list.Attrs["ordered"] = "true"
	if start := orderedListStart(bt.NodeText(marker)); start != "" && start != "1" {
		list.Attrs["start"] = start
	}
}

func listItemMarker(bt *gotreesitter.BoundTree, item *gotreesitter.Node) *gotreesitter.Node {
	for i := 0; i < item.ChildCount(); i++ {
		child := item.Child(i)
		if strings.HasPrefix(bt.NodeType(child), "list_marker") {
			return child
		}
	}
	return nil
}

func isOrderedListMarker(markerType string) bool {
	switch markerType {
	case "list_marker_dot", "list_marker_parenthesis",
		"list_marker_decimal_period", "list_marker_decimal_paren", "list_marker_decimal_parens",
		"list_marker_lower_alpha_period", "list_marker_lower_alpha_paren", "list_marker_lower_alpha_parens",
		"list_marker_upper_alpha_period", "list_marker_upper_alpha_paren", "list_marker_upper_alpha_parens",
		"list_marker_lower_roman_period", "list_marker_lower_roman_paren", "list_marker_lower_roman_parens",
		"list_marker_upper_roman_period", "list_marker_upper_roman_paren", "list_marker_upper_roman_parens":
		return true
	default:
		return false
	}
}

func orderedListStart(marker string) string {
	marker = strings.TrimSpace(marker)
	start := 0
	for i := 0; i < len(marker); i++ {
		if marker[i] < '0' || marker[i] > '9' {
			break
		}
		start = start*10 + int(marker[i]-'0')
	}
	if start == 0 {
		return ""
	}
	return strconv.Itoa(start)
}

// convertTable converts a pipe_table node into a NodeTable with rows,
// cells, and per-column alignment. Alignment is read from the delimiter
// row (`:---`, `:---:`, `---:`) and stored on the NodeTable as a
// comma-separated `align` attribute (values: "", "left", "center",
// "right"). The renderer applies per-cell text-align from this list.
func convertTable(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) *Node {
	table := newNodeFromTree(NodeTable, n)
	var aligns []string
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		childType := bt.NodeType(child)
		switch childType {
		case "pipe_table_header", "pipe_table_row":
			row := newNodeFromTree(NodeTableRow, child)
			for j := 0; j < child.ChildCount(); j++ {
				cell := child.Child(j)
				if bt.NodeType(cell) == "pipe_table_cell" {
					c := newNodeFromTree(NodeTableCell, cell)
					raw := bt.NodeText(cell)
					start, end := trimSpaceSpan(raw)
					text := raw[start:end]
					if text != "" {
						c.Children = append(c.Children, parseInlineAt(text, source, int(cell.StartByte())+start)...)
					}
					row.Children = append(row.Children, c)
				}
			}
			table.Children = append(table.Children, row)
		case "pipe_table_delimiter_row":
			aligns = readDelimiterRowAligns(bt, child)
		}
	}
	if table.Attrs == nil {
		table.Attrs = map[string]string{}
	}
	if len(aligns) > 0 {
		table.Attrs["align"] = strings.Join(aligns, ",")
	}
	return table
}

// readDelimiterRowAligns extracts per-column alignment from a
// pipe_table_delimiter_row. Returns a slice whose entries are "left",
// "center", "right", or "" (no alignment specified).
func readDelimiterRowAligns(bt *gotreesitter.BoundTree, delim *gotreesitter.Node) []string {
	var aligns []string
	for j := 0; j < delim.ChildCount(); j++ {
		cell := delim.Child(j)
		if bt.NodeType(cell) != "pipe_table_delimiter_cell" {
			continue
		}
		left, right := false, false
		for k := 0; k < cell.ChildCount(); k++ {
			switch bt.NodeType(cell.Child(k)) {
			case "pipe_table_align_left":
				left = true
			case "pipe_table_align_right":
				right = true
			}
		}
		switch {
		case left && right:
			aligns = append(aligns, "center")
		case right:
			aligns = append(aligns, "right")
		case left:
			aligns = append(aligns, "left")
		default:
			aligns = append(aligns, "")
		}
	}
	return aligns
}

// parseInline parses inline markdown text using the markdown_inline grammar.
func parseInline(text string, source []byte) []*Node {
	return parseInlineAt(text, source, -1)
}

func parseInlineAt(text string, source []byte, baseOffset int) []*Node {
	if len(text) > maxInlineParseChunk {
		chunks := splitInlineParseChunks(text, maxInlineParseChunk)
		if len(chunks) > 1 {
			nodes := make([]*Node, 0, len(chunks))
			offset := 0
			for _, chunk := range chunks {
				chunkBase := -1
				if baseOffset >= 0 {
					chunkBase = baseOffset + offset
				}
				nodes = append(nodes, parseInlineWithRecoveryAt(chunk, source, chunkBase, true)...)
				offset += len(chunk)
			}
			return nodes
		}
	}
	return parseInlineWithRecoveryAt(text, source, baseOffset, true)
}

const maxInlineParseChunk = 320

func splitInlineParseChunks(text string, max int) []string {
	if max < 80 || len(text) <= max {
		return nil
	}
	chunks := make([]string, 0, len(text)/max+1)
	start := 0
	for len(text)-start > max {
		cut := inlineParseChunkCut(text, start, max)
		if cut <= start {
			return nil
		}
		chunks = append(chunks, text[start:cut])
		start = cut
	}
	if start < len(text) {
		chunks = append(chunks, text[start:])
	}
	return chunks
}

func inlineParseChunkCut(text string, start int, max int) int {
	limit := start + max
	if limit >= len(text) {
		return len(text)
	}
	floor := start + max/2
	for i := limit; i > floor; i-- {
		if text[i-1] == ' ' && isPreferredInlineChunkBoundary(text, start, i) {
			return i
		}
	}
	for i := limit; i > floor; i-- {
		if text[i-1] == ' ' && isSafeInlineChunk(text[start:i]) {
			return i
		}
	}
	return -1
}

func isPreferredInlineChunkBoundary(text string, start int, cut int) bool {
	if cut <= start || !isSafeInlineChunk(text[start:cut]) {
		return false
	}
	i := cut - 2
	for i >= start && (text[i] == ' ' || text[i] == '\t') {
		i--
	}
	if i < start {
		return false
	}
	switch text[i] {
	case '.', '!', '?', ';', ':', ',':
		return true
	default:
		return false
	}
}

func isSafeInlineChunk(chunk string) bool {
	return countUnescaped(chunk, '`')%2 == 0 &&
		countUnescaped(chunk, '[') == countUnescaped(chunk, ']') &&
		countUnescaped(chunk, '(') == countUnescaped(chunk, ')')
}

func countUnescaped(text string, marker byte) int {
	count := 0
	escaped := false
	for i := 0; i < len(text); i++ {
		if escaped {
			escaped = false
			continue
		}
		if text[i] == '\\' {
			escaped = true
			continue
		}
		if text[i] == marker {
			count++
		}
	}
	return count
}

func parseInlineWithRecovery(text string, source []byte, recoverSuffix bool) []*Node {
	return parseInlineWithRecoveryAt(text, source, -1, recoverSuffix)
}

func parseInlineWithRecoveryAt(text string, source []byte, baseOffset int, recoverSuffix bool) []*Node {
	lang := inlineLang()
	if lang == nil {
		return []*Node{textNodeRange(text, inlineSpanRange(source, baseOffset, 0, len(text)))}
	}

	src := []byte(text)
	tree, err := parsePooled(lang, mdInlineEntry, src)
	if err != nil || tree == nil {
		return []*Node{textNodeRange(text, inlineSpanRange(source, baseOffset, 0, len(text)))}
	}
	defer tree.Release()

	bt := gotreesitter.Bind(tree)
	root := bt.RootNode()
	nodes := convertInlineChildren(bt, root, source, baseOffset)
	if root != nil {
		start := int(root.StartByte())
		end := int(root.EndByte())
		if start > 0 && start <= len(src) {
			prefix := textNodeRange(string(src[:start]), inlineSpanRange(source, baseOffset, 0, start))
			nodes = append([]*Node{prefix}, nodes...)
		}
		if end >= 0 && end < len(src) {
			suffix := string(src[end:])
			if recoverSuffix {
				suffixBase := -1
				if baseOffset >= 0 {
					suffixBase = baseOffset + end
				}
				nodes = append(nodes, parseInlineWithRecoveryAt(suffix, source, suffixBase, false)...)
			} else {
				appendTextRange(&nodes, suffix, inlineSpanRange(source, baseOffset, end, len(src)))
			}
		}
	}
	return splitTextNewlines(nodes)
}

func convertBlockChildren(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) []*Node {
	var nodes []*Node
	var loose strings.Builder
	looseAttach := false
	separatedByBlankLine := false

	flushLoose := func() {
		if loose.Len() == 0 {
			return
		}
		appendLooseBlockText(&nodes, loose.String(), looseAttach, source)
		loose.Reset()
		looseAttach = false
		separatedByBlankLine = false
	}

	appendLooseRaw := func(raw string) {
		if raw == "" {
			return
		}
		if containsBlankLine(raw) {
			if loose.Len() > 0 {
				loose.WriteString(raw)
				flushLoose()
			}
			separatedByBlankLine = true
			return
		}
		if loose.Len() == 0 {
			looseAttach = canAttachLooseText(nodes, separatedByBlankLine)
		}
		loose.WriteString(raw)
	}

	nodeStart := int(n.StartByte())
	nodeEnd := int(n.EndByte())
	if bt.NodeType(n) == "document" {
		nodeEnd = len(source)
	}
	if nodeStart < 0 {
		nodeStart = 0
	}
	if nodeEnd > len(source) {
		nodeEnd = len(source)
	}
	if nodeEnd < nodeStart {
		nodeEnd = nodeStart
	}
	cursor := nodeStart

	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		childStart := int(child.StartByte())
		childEnd := int(child.EndByte())
		if childStart < nodeStart {
			childStart = nodeStart
		}
		if childStart > nodeEnd {
			childStart = nodeEnd
		}
		if childEnd < childStart {
			childEnd = childStart
		}
		if childEnd > nodeEnd {
			childEnd = nodeEnd
		}
		if childStart > cursor {
			appendLooseRaw(string(source[cursor:childStart]))
		}

		if converted := convertBlock(bt, child, source); converted != nil {
			flushLoose()
			appendBlockNode(&nodes, converted)
			separatedByBlankLine = false
		} else if childEnd > childStart {
			appendLooseRaw(string(source[childStart:childEnd]))
		}
		if childEnd > cursor {
			cursor = childEnd
		}
	}
	if cursor < nodeEnd {
		appendLooseRaw(string(source[cursor:nodeEnd]))
	}
	flushLoose()

	return nodes
}

func appendBlockNode(nodes *[]*Node, n *Node) {
	if n == nil {
		return
	}
	if n.Type == NodeDocument {
		*nodes = append(*nodes, n.Children...)
		return
	}
	*nodes = append(*nodes, n)
}

func canAttachLooseText(nodes []*Node, separatedByBlankLine bool) bool {
	if separatedByBlankLine || len(nodes) == 0 {
		return false
	}
	return nodes[len(nodes)-1].Type == NodeParagraph
}

func appendLooseBlockText(nodes *[]*Node, text string, attach bool, source []byte) {
	segments := looseParagraphSegments(text)
	for i, segment := range segments {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" {
			continue
		}
		if def := convertFootnoteDefinitionParagraph(trimmed, source); def != nil {
			appendBlockNode(nodes, def)
			continue
		}
		if attach && i == 0 && len(*nodes) > 0 && (*nodes)[len(*nodes)-1].Type == NodeParagraph {
			last := (*nodes)[len(*nodes)-1]
			last.Children = append(last.Children, parseInline(segment, source)...)
			last.Children = splitTextNewlines(last.Children)
			continue
		}
		para := newNode(NodeParagraph)
		para.Children = append(para.Children, parseInline(trimmed, source)...)
		para.Children = splitTextNewlines(para.Children)
		*nodes = append(*nodes, para)
	}
}

func looseParagraphSegments(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	segments := make([]string, 0, 1)
	var current strings.Builder
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if current.Len() > 0 {
				segments = append(segments, current.String())
				current.Reset()
			}
			continue
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	return segments
}

func containsBlankLine(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] != '\n' {
			continue
		}
		for j := i + 1; j < len(text); j++ {
			switch text[j] {
			case ' ', '\t', '\r':
				continue
			case '\n':
				return true
			default:
				j = len(text)
			}
		}
	}
	return false
}

// convertInlineChildren walks an inline tree-sitter node and converts
// its children into AST nodes, collecting text runs from unnamed/leaf nodes.
func convertInlineChildren(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte, baseOffset int) []*Node {
	if n == nil {
		return nil
	}
	var nodes []*Node
	typ := bt.NodeType(n)

	switch typ {
	case "inline":
		// Root inline node — process children, collecting text spans
		nodes = collectInlineChildren(bt, n, source, baseOffset)

	case "strong_emphasis":
		strong := newNode(NodeStrong)
		strong.Range = inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))
		strong.Children = collectInlineTextOnly(bt, n, source, baseOffset)
		nodes = append(nodes, strong)

	case "emphasis":
		em := newNode(NodeEmphasis)
		em.Range = inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))
		em.Children = collectInlineTextOnly(bt, n, source, baseOffset)
		nodes = append(nodes, em)

	case "strikethrough":
		raw := bt.NodeText(n)
		if strings.HasPrefix(raw, "~~") {
			// Double tilde: true strikethrough.
			// tree-sitter nests ~~x~~ as outer(~~) > inner(~x~),
			// so we extract text content directly to avoid converting
			// the inner node to subscript.
			s := newNode(NodeStrikethrough)
			s.Range = inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))
			s.Children = collectStrikethroughText(bt, n, source, baseOffset)
			nodes = append(nodes, s)
		} else {
			// Single tilde: subscript (~text~)
			sub := newNode(NodeSubscript)
			sub.Range = inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))
			content := collectStrikethroughText(bt, n, source, baseOffset)
			sub.Literal = collectNodesText(content)
			nodes = append(nodes, sub)
		}

	case "inline_link":
		link := newNode(NodeLink)
		link.Range = inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))
		if link.Attrs == nil {
			link.Attrs = make(map[string]string)
		}
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := bt.NodeType(child)
			switch ct {
			case "link_text":
				// Re-parse the link text through the inline grammar so emphasis,
				// code spans, emoji, etc. inside link text survive as structure.
				childBase := -1
				if baseOffset >= 0 {
					childBase = baseOffset + int(child.StartByte())
				}
				link.Children = append(link.Children, parseInlineAt(bt.NodeText(child), source, childBase)...)
			case "link_destination":
				link.Attrs["href"] = bt.NodeText(child)
			case "link_title":
				link.Attrs["title"] = stripQuotes(bt.NodeText(child))
			}
		}
		nodes = append(nodes, link)

	case "full_reference_link", "collapsed_reference_link", "shortcut_link":
		link := newNode(NodeLink)
		link.Range = inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))
		if link.Attrs == nil {
			link.Attrs = make(map[string]string)
		}
		// Capture full raw text for shortcut links so post-processing
		// can detect footnote refs ([^id]) and admonition markers ([!TYPE]).
		if typ == "shortcut_link" {
			link.Attrs["raw"] = bt.NodeText(n)
		}
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := bt.NodeType(child)
			switch ct {
			case "link_text":
				childBase := -1
				if baseOffset >= 0 {
					childBase = baseOffset + int(child.StartByte())
				}
				link.Children = append(link.Children, parseInlineAt(bt.NodeText(child), source, childBase)...)
			case "link_label":
				link.Attrs["ref"] = bt.NodeText(child)
			}
		}
		nodes = append(nodes, link)

	case "image":
		img := newNode(NodeImage)
		img.Range = inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))
		if img.Attrs == nil {
			img.Attrs = make(map[string]string)
		}
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := bt.NodeType(child)
			switch ct {
			case "image_description":
				img.Attrs["alt"] = bt.NodeText(child)
			case "link_destination":
				img.Attrs["src"] = bt.NodeText(child)
			case "link_title":
				img.Attrs["title"] = stripQuotes(bt.NodeText(child))
			}
		}
		nodes = append(nodes, img)

	case "code_span":
		cs := newNode(NodeCodeSpan)
		cs.Range = inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))
		// Extract text between delimiters
		cs.Literal = extractCodeSpanText(bt, n)
		nodes = append(nodes, cs)

	case "uri_autolink", "email_autolink":
		// tree-sitter-markdown wraps the URL in angle brackets: <https://x>.
		raw := bt.NodeText(n)
		url := strings.TrimPrefix(strings.TrimSuffix(raw, ">"), "<")
		href := url
		if typ == "email_autolink" {
			href = "mailto:" + url
		}
		link := newNode(NodeLink)
		link.Range = inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))
		link.Attrs = map[string]string{"href": href}
		link.Children = []*Node{textNodeRange(url, inlineSpanRange(source, baseOffset, int(n.StartByte())+1, int(n.EndByte())-1))}
		nodes = append(nodes, link)

	case "hard_line_break":
		nodes = append(nodes, &Node{Type: NodeHardBreak, Range: inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))})

	case "backslash_escape":
		text := bt.NodeText(n)
		if len(text) > 1 {
			nodes = append(nodes, textNodeRange(text[1:], inlineSpanRange(source, baseOffset, int(n.StartByte())+1, int(n.EndByte()))))
		}

	case "html_tag":
		hi := newNode(NodeHTMLInline)
		hi.Range = inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))
		hi.Literal = bt.NodeText(n)
		nodes = append(nodes, hi)

	default:
		// Leaf text or unnamed punctuation — handled by parent's collector
		text := bt.NodeText(n)
		if text != "" {
			nodes = append(nodes, textNodeRange(text, inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte()))))
		}
	}

	return nodes
}

// collectInlineChildren processes the children of an inline-level node,
// extracting gap text between children and recursing into structural nodes.
// tree-sitter markdown_inline does not create child nodes for plain text;
// text that falls between (or around) named children must be recovered
// from the source using byte offsets.
func collectInlineChildren(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte, baseOffset int) []*Node {
	nodeText := bt.NodeText(n)
	src := []byte(nodeText)
	nodeStart := n.StartByte()

	if n.ChildCount() == 0 {
		// Leaf inline — all text
		if len(src) > 0 {
			return []*Node{textNodeRange(string(src), inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte())))}
		}
		return nil
	}

	var nodes []*Node
	cursor := uint32(0) // relative to nodeStart

	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		childStart := child.StartByte() - nodeStart
		childEnd := child.EndByte() - nodeStart

		// Gap text before this child
		if childStart > cursor {
			gap := string(src[cursor:childStart])
			if gap != "" {
				appendTextRange(&nodes, gap, inlineSpanRange(source, baseOffset, int(nodeStart+cursor), int(nodeStart+childStart)))
			}
		}

		ct := bt.NodeType(child)
		if isInlineStructural(ct) {
			nodes = append(nodes, convertInlineChildren(bt, child, source, baseOffset)...)
		} else {
			// Non-structural child (punctuation, etc.) — include its text
			text := bt.NodeText(child)
			if text != "" {
				appendTextRange(&nodes, text, inlineSpanRange(source, baseOffset, int(child.StartByte()), int(child.EndByte())))
			}
		}
		cursor = childEnd
	}

	// Trailing gap text
	if cursor < uint32(len(src)) {
		gap := string(src[cursor:])
		if gap != "" {
			appendTextRange(&nodes, gap, inlineSpanRange(source, baseOffset, int(nodeStart+cursor), int(nodeStart)+len(src)))
		}
	}

	return nodes
}

// collectInlineTextOnly extracts text from an inline node, skipping
// delimiter tokens (emphasis_delimiter, etc.) and recursing into nested
// inline structures. Uses gap-based extraction for text between children.
func collectInlineTextOnly(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte, baseOffset int) []*Node {
	nodeText := bt.NodeText(n)
	src := []byte(nodeText)
	nodeStart := n.StartByte()

	if n.ChildCount() == 0 {
		if len(src) > 0 {
			return []*Node{textNodeRange(string(src), inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte())))}
		}
		return nil
	}

	var nodes []*Node
	cursor := uint32(0)

	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		ct := bt.NodeType(child)
		childStart := child.StartByte() - nodeStart
		childEnd := child.EndByte() - nodeStart

		// Gap text before this child (content text between delimiters/children)
		if childStart > cursor {
			gap := string(src[cursor:childStart])
			if gap != "" {
				appendTextRange(&nodes, gap, inlineSpanRange(source, baseOffset, int(nodeStart+cursor), int(nodeStart+childStart)))
			}
		}

		if isDelimiter(ct) {
			// Skip delimiter token itself (don't emit its text)
			cursor = childEnd
			continue
		}

		if isInlineStructural(ct) {
			nodes = append(nodes, convertInlineChildren(bt, child, source, baseOffset)...)
		} else {
			text := bt.NodeText(child)
			if text != "" {
				appendTextRange(&nodes, text, inlineSpanRange(source, baseOffset, int(child.StartByte()), int(child.EndByte())))
			}
		}
		cursor = childEnd
	}

	// Trailing gap text
	if cursor < uint32(len(src)) {
		gap := string(src[cursor:])
		if gap != "" {
			appendTextRange(&nodes, gap, inlineSpanRange(source, baseOffset, int(nodeStart+cursor), int(nodeStart)+len(src)))
		}
	}

	return nodes
}

// appendText merges text into the last node if it's a text node,
// or appends a new text node.
func appendText(nodes *[]*Node, text string) {
	appendTextRange(nodes, text, Range{})
}

func appendTextRange(nodes *[]*Node, text string, r Range) {
	if len(*nodes) > 0 && (*nodes)[len(*nodes)-1].Type == NodeText {
		last := (*nodes)[len(*nodes)-1]
		last.Literal += text
		if last.Range.StartLine != 0 && r.StartLine != 0 {
			last.Range.EndByte = r.EndByte
			last.Range.EndLine = r.EndLine
			last.Range.EndCol = r.EndCol
		}
	} else {
		*nodes = append(*nodes, textNodeRange(text, r))
	}
}

// synthesiseSectionContent checks whether a section node contains
// unwrapped children that belong in a wrapper node and, if so,
// synthesises the wrapper.  Returns nil if no special handling applies.
func synthesiseSectionContent(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) *Node {
	if n.ChildCount() == 0 {
		return nil
	}

	// Collect child types for pattern matching.
	childTypes := make([]string, n.ChildCount())
	for i := 0; i < n.ChildCount(); i++ {
		childTypes[i] = bt.NodeType(n.Child(i))
	}

	// Pattern: block_quote_marker + paragraph/... = blockquote
	if childTypes[0] == "block_quote_marker" {
		bq := newNodeFromTree(NodeBlockquote, n)
		for i := 1; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := childTypes[i]
			if ct == "block_continuation" {
				continue
			}
			if converted := convertBlock(bt, child, source); converted != nil {
				bq.Children = append(bq.Children, converted)
			}
		}
		return bq
	}

	// Pattern: list_item children = list
	hasListItem := false
	for _, ct := range childTypes {
		if ct == "list_item" {
			hasListItem = true
			break
		}
	}
	if hasListItem {
		list := newNodeFromTree(NodeList, n)
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			if bt.NodeType(child) == "list_item" {
				applyListMarkerAttrs(bt, list, child)
				if converted := convertListItem(bt, child, source); converted != nil {
					list.Children = append(list.Children, converted)
				}
			}
		}
		return list
	}

	// Pattern: fenced_code_block_delimiter + info_string + code_fence_content + ... = code block
	hasFenceDelim := false
	for _, ct := range childTypes {
		if ct == "fenced_code_block_delimiter" {
			hasFenceDelim = true
			break
		}
	}
	if hasFenceDelim {
		return convertCodeBlock(bt, n, "fenced_code_block")
	}

	// Pattern: pipe_table_header + pipe_table_delimiter_row + pipe_table_row = table
	hasPipeTableHeader := false
	for _, ct := range childTypes {
		if ct == "pipe_table_header" {
			hasPipeTableHeader = true
			break
		}
	}
	if hasPipeTableHeader {
		table := newNodeFromTree(NodeTable, n)
		var aligns []string
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := childTypes[i]
			switch ct {
			case "pipe_table_header", "pipe_table_row":
				row := newNodeFromTree(NodeTableRow, child)
				for j := 0; j < child.ChildCount(); j++ {
					cell := child.Child(j)
					if bt.NodeType(cell) == "pipe_table_cell" {
						c := newNodeFromTree(NodeTableCell, cell)
						raw := bt.NodeText(cell)
						start, end := trimSpaceSpan(raw)
						text := raw[start:end]
						if text != "" {
							c.Children = append(c.Children, parseInlineAt(text, source, int(cell.StartByte())+start)...)
						}
						row.Children = append(row.Children, c)
					}
				}
				table.Children = append(table.Children, row)
			case "pipe_table_delimiter_row":
				aligns = readDelimiterRowAligns(bt, child)
			}
		}
		if table.Attrs == nil {
			table.Attrs = map[string]string{}
		}
		if len(aligns) > 0 {
			table.Attrs["align"] = strings.Join(aligns, ",")
		}
		return table
	}

	// Pattern: section with only "inline" child(ren) and no structural wrappers = paragraph.
	// In single-element documents, the block parser's "inline" children may only
	// cover punctuation. Use the full section text instead.
	allInlineOrSkip := true
	hasInline := false
	for _, ct := range childTypes {
		if ct == "inline" {
			hasInline = true
		} else if ct != "block_continuation" && ct != "_whitespace" {
			allInlineOrSkip = false
			break
		}
	}
	if allInlineOrSkip && hasInline {
		para := newNodeFromTree(NodeParagraph, n)
		sectionText := strings.TrimRight(bt.NodeText(n), "\n")
		para.Children = append(para.Children, parseInlineAt(sectionText, source, int(n.StartByte()))...)
		return para
	}

	return nil
}

// isInlineStructural returns true for node types that are meaningful inline
// structures (not raw text/punctuation).
func isInlineStructural(nodeType string) bool {
	switch nodeType {
	case "strong_emphasis", "emphasis", "strikethrough",
		"inline_link", "full_reference_link", "collapsed_reference_link", "shortcut_link",
		"uri_autolink", "email_autolink",
		"image", "code_span", "hard_line_break", "html_tag", "backslash_escape":
		return true
	default:
		return false
	}
}

// collectStrikethroughText extracts the text content from a strikethrough node,
// stripping tilde delimiters. Unlike collectInlineTextOnly, this treats nested
// strikethrough nodes as text content rather than structural elements, avoiding
// incorrect conversion of inner nodes in ~~double-tilde~~ constructs.
func collectStrikethroughText(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte, baseOffset int) []*Node {
	nodeText := bt.NodeText(n)
	src := []byte(nodeText)
	nodeStart := n.StartByte()

	if n.ChildCount() == 0 {
		if len(src) > 0 {
			return []*Node{textNodeRange(string(src), inlineSpanRange(source, baseOffset, int(n.StartByte()), int(n.EndByte())))}
		}
		return nil
	}

	var nodes []*Node
	cursor := uint32(0)

	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		ct := bt.NodeType(child)
		childStart := child.StartByte() - nodeStart
		childEnd := child.EndByte() - nodeStart

		if childStart > cursor {
			gap := string(src[cursor:childStart])
			if gap != "" {
				appendTextRange(&nodes, gap, inlineSpanRange(source, baseOffset, int(nodeStart+cursor), int(nodeStart+childStart)))
			}
		}

		if isDelimiter(ct) {
			cursor = childEnd
			continue
		}

		if ct == "strikethrough" {
			// Nested strikethrough: extract as text, skipping its delimiters
			inner := collectStrikethroughText(bt, child, source, baseOffset)
			for _, in := range inner {
				if in.Type == NodeText {
					appendTextRange(&nodes, in.Literal, in.Range)
				} else {
					nodes = append(nodes, in)
				}
			}
		} else if isInlineStructural(ct) {
			nodes = append(nodes, convertInlineChildren(bt, child, source, baseOffset)...)
		} else {
			text := bt.NodeText(child)
			if text != "" {
				appendTextRange(&nodes, text, inlineSpanRange(source, baseOffset, int(child.StartByte()), int(child.EndByte())))
			}
		}
		cursor = childEnd
	}

	if cursor < uint32(len(src)) {
		gap := string(src[cursor:])
		if gap != "" {
			appendTextRange(&nodes, gap, inlineSpanRange(source, baseOffset, int(nodeStart+cursor), int(nodeStart)+len(src)))
		}
	}

	return nodes
}

// isDelimiter returns true for delimiter node types that should be stripped.
func isDelimiter(nodeType string) bool {
	switch nodeType {
	case "emphasis_delimiter", "code_span_delimiter":
		return true
	default:
		return false
	}
}

// extractCodeSpanText gets the text inside a code_span, stripping delimiters.
// Uses gap-based extraction since tree-sitter may not represent all text as children.
func extractCodeSpanText(bt *gotreesitter.BoundTree, n *gotreesitter.Node) string {
	nodeText := bt.NodeText(n)
	src := []byte(nodeText)
	nodeStart := n.StartByte()

	if n.ChildCount() == 0 {
		return nodeText
	}

	var sb strings.Builder
	cursor := uint32(0)

	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		childStart := child.StartByte() - nodeStart
		childEnd := child.EndByte() - nodeStart

		// Gap text before this child
		if childStart > cursor {
			sb.Write(src[cursor:childStart])
		}

		if bt.NodeType(child) != "code_span_delimiter" {
			sb.WriteString(bt.NodeText(child))
		}
		cursor = childEnd
	}

	// Trailing gap text
	if cursor < uint32(len(src)) {
		sb.Write(src[cursor:])
	}

	return sb.String()
}

// headingLevel extracts the heading level (1-6) from an atx_heading or setext_heading node.
func headingLevel(bt *gotreesitter.BoundTree, n *gotreesitter.Node) int {
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		ct := bt.NodeType(child)
		switch ct {
		case "atx_h1_marker":
			return 1
		case "atx_h2_marker":
			return 2
		case "atx_h3_marker":
			return 3
		case "atx_h4_marker":
			return 4
		case "atx_h5_marker":
			return 5
		case "atx_h6_marker":
			return 6
		case "setext_h1_underline":
			return 1
		case "setext_h2_underline":
			return 2
		}
	}
	return 1
}

func extractHeadingTextSpan(bt *gotreesitter.BoundTree, n *gotreesitter.Node) (string, int, bool) {
	raw := strings.TrimRight(bt.NodeText(n), "\n")
	nodeStart := int(n.StartByte())
	switch bt.NodeType(n) {
	case "atx_heading":
		start, end, ok := atxHeadingTextRange([]byte(raw))
		if !ok {
			return "", 0, false
		}
		return raw[start:end], nodeStart + start, true
	case "setext_heading":
		if idx := strings.IndexByte(raw, '\n'); idx >= 0 {
			raw = raw[:idx]
		}
		start, end := trimSpaceSpan(raw)
		if start >= end {
			return "", 0, false
		}
		return raw[start:end], nodeStart + start, true
	}
	start, end := trimSpaceSpan(raw)
	if start >= end {
		return "", 0, false
	}
	return raw[start:end], nodeStart + start, true
}

func trimSpaceSpan(s string) (int, int) {
	start := 0
	for start < len(s) {
		switch s[start] {
		case ' ', '\t', '\n', '\r':
			start++
		default:
			goto foundStart
		}
	}
foundStart:
	end := len(s)
	for end > start {
		switch s[end-1] {
		case ' ', '\t', '\n', '\r':
			end--
		default:
			return start, end
		}
	}
	return start, end
}

// levelStr converts a heading level int to its string representation.
func levelStr(level int) string {
	switch level {
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	case 4:
		return "4"
	case 5:
		return "5"
	case 6:
		return "6"
	default:
		return "1"
	}
}

// stripQuotes removes surrounding quote characters from a string.
// tree-sitter link_title nodes include their surrounding quotes.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') || (first == '(' && last == ')') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// findChild finds the first child of n with the given node type.
func findChild(bt *gotreesitter.BoundTree, n *gotreesitter.Node, nodeType string) *gotreesitter.Node {
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		if bt.NodeType(child) == nodeType {
			return child
		}
	}
	return nil
}

// splitTextNewlines splits text nodes containing newlines into
// text + NodeSoftBreak sequences so the renderer can apply hard wraps.
func splitTextNewlines(nodes []*Node) []*Node {
	var out []*Node
	for _, n := range nodes {
		if n.Type != NodeText || !strings.Contains(n.Literal, "\n") {
			out = append(out, n)
			continue
		}
		lines := strings.Split(n.Literal, "\n")
		cursor := 0
		for i, line := range lines {
			if line != "" {
				out = append(out, textNodeRange(line, textSliceRange(n.Range, n.Literal, cursor, cursor+len(line))))
			}
			cursor += len(line)
			if i < len(lines)-1 {
				out = append(out, &Node{Type: NodeSoftBreak, Range: textSliceRange(n.Range, n.Literal, cursor, cursor+1)})
				cursor++
			}
		}
	}
	return out
}

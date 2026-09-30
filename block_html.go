package mdpp

import (
	"regexp"
	"strings"
)

// parseCommonMarkHTMLBlocks handles HTML block boundaries before the block
// grammar can consume their contents as inline Markdown. Each line is visited
// once; ordinary spans are handed back to the regular parser.
func parseCommonMarkHTMLBlocks(source []byte, ctx *parseCtx) *Document {
	i := 0
	for i < len(source) && source[i] == ' ' {
		i++
	}
	if i > 3 || i == len(source) || source[i] != '<' {
		return nil
	}
	lines := sourceLines(source)
	if len(lines) == 0 || htmlBlockStart(lines[0].text, true) == "" {
		return nil
	}
	var children []*Node
	var diagnostics []Diagnostic
	refs := make(map[string]linkRefDef)
	chunkStart := 0
	chunkLine := 1
	atBlockStart := true
	inFence := false
	for i := 0; i < len(lines); {
		line := lines[i].text
		if isMarkdownFenceLine(strings.TrimSpace(line)) {
			inFence = !inFence
		}
		kind := ""
		if !inFence {
			kind = htmlBlockStart(line, atBlockStart)
		}
		if kind == "" {
			atBlockStart = strings.TrimSpace(line) == ""
			i++
			continue
		}
		if lines[i].start > chunkStart {
			children = appendParsedSegmentCtx(children, &diagnostics, refs, source,
				blockChunk{start: chunkStart, end: lines[i].start, startLine: chunkLine, startCol: 1}, ctx)
		}
		start := lines[i].start
		endLine := i
		if strings.HasPrefix(kind, "close:") {
			closing := strings.TrimPrefix(kind, "close:")
			for endLine < len(lines) {
				if strings.Contains(strings.ToLower(lines[endLine].text), closing) {
					endLine++
					break
				}
				endLine++
			}
		} else {
			for endLine < len(lines) && strings.TrimSpace(lines[endLine].text) != "" {
				endLine++
			}
		}
		if endLine == i { // A blank cannot start an HTML block.
			i++
			continue
		}
		end := lines[endLine-1].next
		children = append(children, &Node{Type: NodeHTMLBlock, Literal: string(source[start:end]), Range: Range{
			StartByte: start, EndByte: end, StartLine: i + 1, StartCol: 1, EndLine: endLine + 1, EndCol: 1,
		}})
		i = endLine
		chunkStart = end
		chunkLine = i + 1
		atBlockStart = true
		for i < len(lines) && strings.TrimSpace(lines[i].text) == "" {
			chunkStart = lines[i].next
			chunkLine = i + 2
			i++
		}
	}
	if chunkStart < len(source) {
		children = appendParsedSegmentCtx(children, &diagnostics, refs, source,
			blockChunk{start: chunkStart, end: len(source), startLine: chunkLine, startCol: 1}, ctx)
	}
	doc := &Document{Root: &Node{Type: NodeDocument, Children: children, Range: sourceRange(source, 0, len(source))}, Source: source, diagnostics: diagnostics, linkRefDefs: refs}
	doc.extractFrontmatter()
	postProcess(doc)
	return doc
}

func htmlBlockStart(line string, atBlockStart bool) string {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	if i > 3 || i >= len(line) || line[i] != '<' {
		return ""
	}
	line = line[i:]
	lower := strings.ToLower(line)
	for _, tag := range []string{"script", "pre", "style", "textarea"} {
		prefix := "<" + tag
		if strings.HasPrefix(lower, prefix) && htmlTagBoundary(lower, len(prefix)) {
			return "close:</" + tag + ">"
		}
	}
	for _, pair := range [][2]string{{"<!--", "-->"}, {"<?", "?>"}, {"<![cdata[", "]]>"}} {
		if strings.HasPrefix(lower, pair[0]) {
			return "close:" + pair[1]
		}
	}
	if strings.HasPrefix(lower, "<!") && len(lower) > 2 && lower[2] >= 'a' && lower[2] <= 'z' {
		return "close:>"
	}
	for _, tag := range commonMarkBlockTags {
		for _, prefix := range []string{"<" + tag, "</" + tag} {
			if strings.HasPrefix(lower, prefix) && htmlTagBoundary(lower, len(prefix)) {
				return "blank"
			}
		}
	}
	trimmed := strings.TrimSpace(line)
	if strings.HasSuffix(trimmed, "/>") {
		return "" // MDX style self-closing components stay inline.
	}
	if atBlockStart && (standaloneOpenHTMLTagRe.MatchString(trimmed) || standaloneCloseHTMLTagRe.MatchString(trimmed)) {
		return "blank"
	}
	return ""
}

func htmlTagBoundary(line string, i int) bool {
	return i == len(line) || line[i] == ' ' || line[i] == '\t' || line[i] == '>' || line[i] == '/' || line[i] == '\r'
}

var standaloneCloseHTMLTagRe = regexp.MustCompile(`^</[A-Za-z][A-Za-z0-9-]*[ \t]*>$`)

var commonMarkBlockTags = []string{
	"address", "article", "aside", "base", "basefont", "blockquote", "body", "caption", "center", "col", "colgroup", "dd", "details", "dialog", "dir", "div", "dl", "dt", "fieldset", "figcaption", "figure", "footer", "form", "frame", "frameset", "h1", "h2", "h3", "h4", "h5", "h6", "head", "header", "hr", "html", "iframe", "legend", "li", "link", "main", "menu", "menuitem", "nav", "noframes", "ol", "optgroup", "option", "output", "p", "param", "search", "section", "source", "summary", "table", "tbody", "td", "tfoot", "th", "thead", "title", "tr", "track", "ul",
}

package mdpp

import (
	"bytes"
	"strings"
)

func isThematicBreakLine(line string) bool {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	if i > 3 || i == len(line) {
		return false
	}
	marker := line[i]
	if marker != '*' && marker != '-' && marker != '_' {
		return false
	}
	count := 0
	for ; i < len(line); i++ {
		switch line[i] {
		case marker:
			count++
		case ' ', '\t', '\r':
		default:
			return false
		}
	}
	return count >= 3
}

// A thematic break at the start of a document cannot be a setext underline.
// The grammar sometimes groups it with the following paragraph, so consume
// consecutive leading breaks before handing the rest to the block parser.
func parseLeadingThematicBreaks(source []byte, ctx *parseCtx) *Document {
	end := bytes.IndexByte(source, '\n')
	if end < 0 {
		end = len(source)
	}
	if !isThematicBreakLine(string(source[:end])) {
		return nil
	}
	lines := sourceLines(source)
	children := make([]*Node, 0, 2)
	i := 0
	for i < len(lines) && isThematicBreakLine(lines[i].text) {
		children = append(children, &Node{Type: NodeThematicBreak, Range: sourceRange(source, lines[i].start, lines[i].next)})
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i].text) == "" {
		i++
	}
	var diagnostics []Diagnostic
	refs := make(map[string]linkRefDef)
	if i < len(lines) {
		children = appendParsedSegmentCtx(children, &diagnostics, refs, source,
			blockChunk{start: lines[i].start, end: len(source), startLine: i + 1, startCol: 1}, ctx)
	}
	doc := &Document{Root: &Node{Type: NodeDocument, Children: children, Range: sourceRange(source, 0, len(source))}, Source: source, diagnostics: diagnostics, linkRefDefs: refs}
	doc.extractFrontmatter()
	postProcess(doc)
	return doc
}

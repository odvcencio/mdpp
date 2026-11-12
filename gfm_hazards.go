package mdpp

import "strings"

// In the GFM profile the grammar is the only reader of blocks. The grammar does
// not read every HTML block as CommonMark 4.6 defines it: it can read the lines
// of a "<pre>", "<script>" or "<div>" block as a paragraph, or end the block
// early, so that a "# h" line inside it is read as a heading. mdpp does not
// repair that reading in this profile. It finds the HTML blocks that the
// specification defines, with a line scan that builds no nodes, and reports
// each one for which the grammar's tree has no HTML block node over its lines.
//
// DiagHTMLBlockUnread is that report. Its range is the lines of the HTML block
// as the specification defines them. The tree at that range is not the
// structural reading of the source: a heading, task list item or comment that
// the tree shows there may be text of the HTML block, and a comment that the
// block holds is not an inline comment node.
//
// The scan reads top-level lines only. An HTML block that starts inside a block
// quote ("> <div>") is not found, and nothing is reported for it.
const DiagHTMLBlockUnread = "MDPP-PARSE-008"

// gfmHTMLBlockHazards returns the diagnostics for the HTML blocks of source that
// root has no HTML block node over.
func gfmHTMLBlockHazards(source []byte, root *Node) []Diagnostic {
	if root == nil || !containsHTMLBlockStart(source) {
		return nil
	}
	var spans [][2]int
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if n.Type == NodeHTMLBlock && n.Range.StartLine != 0 {
			spans = append(spans, [2]int{n.Range.StartByte, n.Range.EndByte})
		}
		return true
	})
	covered := func(start, end int) bool {
		for _, s := range spans {
			if s[0] <= start && end <= s[1] {
				return true
			}
		}
		return false
	}
	var out []Diagnostic
	lines := sourceLines(source)
	inFence := false
	atBlockStart := true
	for i := 0; i < len(lines); {
		line := lines[i].text
		if isMarkdownFenceLine(trimMarkdownSpace(line)) {
			inFence = !inFence
		}
		kind := ""
		if !inFence {
			kind = htmlBlockStart(line, atBlockStart, true)
		}
		if kind == "" {
			atBlockStart = trimMarkdownSpace(line) == ""
			i++
			continue
		}
		endLine := i
		if len(kind) > 6 && kind[:6] == "close:" {
			closing := kind[6:]
			for endLine < len(lines) {
				if containsFold(lines[endLine].text, closing) {
					endLine++
					break
				}
				endLine++
			}
		} else {
			for endLine < len(lines) && trimMarkdownSpace(lines[endLine].text) != "" {
				endLine++
			}
		}
		if endLine == i {
			i++
			continue
		}
		start, end := lines[i].start, lines[endLine-1].next
		if !covered(start, end) {
			out = append(out, Diagnostic{
				Code:     DiagHTMLBlockUnread,
				Severity: SeverityWarning,
				Message:  "HTML block not read as one block by the grammar; the tree here is not its structural reading",
				Range:    sourceRange(source, start, end),
			})
		}
		i = endLine
		atBlockStart = true
		for i < len(lines) && trimMarkdownSpace(lines[i].text) == "" {
			i++
		}
	}
	return out
}

// containsHTMLBlockStart is a cheap test before the scan: an HTML block starts
// with "<" after at most three spaces.
func containsHTMLBlockStart(source []byte) bool {
	for i := 0; i < len(source); i++ {
		if source[i] == '<' {
			start := i
			for start > 0 && source[start-1] == ' ' {
				start--
			}
			if i-start <= 3 && (start == 0 || source[start-1] == '\n') {
				return true
			}
		}
	}
	return false
}

func containsFold(line, lowerNeedle string) bool {
	return len(lowerNeedle) > 0 && strings.Contains(strings.ToLower(line), lowerNeedle)
}

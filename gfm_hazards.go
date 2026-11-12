package mdpp

import (
	"bytes"
	"strings"
)

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

// DiagBlockMisread reports a block that the grammar is known to read wrongly
// and that mdpp does not repair in the GFM profile. The tree at its range is not
// the structural reading of the source. Two shapes are found, by a line scan
// that builds no nodes:
//
//   - a fenced code block with a line indented four or more spaces that holds
//     only the fence characters. CommonMark 4.5 does not close the block there;
//     the grammar does, and reads the lines after it as blocks (a "# h" line
//     becomes a heading). The range runs from the opening fence to the real
//     closing fence, or to the end of the source.
//   - a link reference definition on one line directly followed by a setext
//     underline ("===" or "---"), where the grammar reads one heading over both
//     lines. CommonMark takes the definition out first, so the underline is a
//     paragraph or a thematic break. The range is the two lines.
const DiagBlockMisread = "MDPP-PARSE-009"

// gfmHTMLBlock is an HTML block as CommonMark 4.6 defines it, found by a line
// scan: the lines [first, last] of the source lines, the byte range of those
// lines (with the line end of the last), and whether it ends at a closing
// marker (start conditions 1 to 5) and not at a blank line (6 and 7).
type gfmHTMLBlock struct {
	first, last int
	start, end  int
	closed      bool
}

// gfmHTMLBlocks scans the top-level lines of source for HTML blocks. It builds
// no nodes.
func gfmHTMLBlocks(lines []sourceLine) []gfmHTMLBlock {
	var out []gfmHTMLBlock
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
		closed := len(kind) > 6 && kind[:6] == "close:"
		if closed {
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
		out = append(out, gfmHTMLBlock{first: i, last: endLine - 1, start: lines[i].start, end: lines[endLine-1].next, closed: closed})
		i = endLine
		atBlockStart = true
		for i < len(lines) && trimMarkdownSpace(lines[i].text) == "" {
			i++
		}
	}
	return out
}

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
	for _, block := range gfmHTMLBlocks(lines) {
		if !covered(block.start, block.end) {
			out = append(out, Diagnostic{
				Code:     DiagHTMLBlockUnread,
				Severity: SeverityWarning,
				Message:  "HTML block not read as one block by the grammar; the tree here is not its structural reading",
				Range:    sourceRange(source, block.start, block.end),
			})
		}
	}
	return out
}

// gfmBlockMisreads returns the DiagBlockMisread diagnostics for source. root is
// the grammar's tree; the setext shape is reported only where the tree has a
// heading over the definition line.
func gfmBlockMisreads(source []byte, root *Node) []Diagnostic {
	var out []Diagnostic
	lines := sourceLines(source)
	report := func(start, end int, message string) {
		out = append(out, Diagnostic{Code: DiagBlockMisread, Severity: SeverityWarning, Message: message, Range: sourceRange(source, start, end)})
	}
	// Fences.
	var fenceChar byte
	fenceLen, fenceStart, indentedClose := 0, 0, false
	for _, line := range lines {
		indent, rest := leadingColumns(line.text)
		if fenceLen == 0 {
			if indent <= 3 {
				if c, n := fenceRun(rest); n >= 3 && !(c == '`' && strings.IndexByte(rest[n:], '`') >= 0) {
					fenceChar, fenceLen, fenceStart, indentedClose = c, n, line.start, false
				}
			}
			continue
		}
		c, n := fenceRun(rest)
		if c != fenceChar || n < fenceLen || strings.TrimLeft(rest[n:], " \t") != "" {
			continue
		}
		if indent <= 3 {
			if indentedClose {
				report(fenceStart, line.next, "fenced code block holds an indented closing fence that the grammar reads as the end; the tree after it is not trusted")
			}
			fenceLen = 0
		} else {
			indentedClose = true
		}
	}
	if fenceLen != 0 && indentedClose {
		report(fenceStart, len(source), "fenced code block holds an indented closing fence that the grammar reads as the end; the tree after it is not trusted")
	}
	// A link reference definition directly followed by a setext underline.
	var headings [][2]int
	if root != nil {
		walkNodes(root, func(n *Node, parent *Node, index int) bool {
			if n.Type == NodeHeading && n.Range.StartLine != 0 {
				headings = append(headings, [2]int{n.Range.StartByte, n.Range.EndByte})
			}
			return true
		})
	}
	inFence := false
	for i := 0; i+1 < len(lines); i++ {
		indent, rest := leadingColumns(lines[i].text)
		if indent <= 3 && isMarkdownFenceLine(rest) {
			inFence = !inFence
		}
		if inFence || indent > 3 || !linkReferenceDefinitionLine(rest) || !isSetextUnderlineLine([]byte(lines[i+1].text)) {
			continue
		}
		for _, h := range headings {
			if h[0] <= lines[i].start && lines[i].start < h[1] {
				report(lines[i].start, lines[i+1].next, "link reference definition followed by a setext underline is read as a heading by the grammar; the tree here is not trusted")
				break
			}
		}
	}
	return out
}

// leadingColumns returns the columns of indentation of a line (a tab counts to
// the next multiple of four) and the rest of the line.
func leadingColumns(line string) (int, string) {
	columns := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			columns++
		case '\t':
			columns += 4 - columns%4
		default:
			return columns, line[i:]
		}
	}
	return columns, ""
}

// fenceRun returns the fence character and the length of the run of it at the
// start of s, or 0, 0.
func fenceRun(s string) (byte, int) {
	if s == "" || s[0] != '`' && s[0] != '~' {
		return 0, 0
	}
	n := 1
	for n < len(s) && s[n] == s[0] {
		n++
	}
	return s[0], n
}

// linkReferenceDefinitionLine reports whether s (a line without its
// indentation) is a one-line link reference definition: "[label]: destination".
func linkReferenceDefinitionLine(s string) bool {
	if !strings.HasPrefix(s, "[") {
		return false
	}
	end := strings.Index(s, "]:")
	if end < 2 || strings.ContainsAny(s[1:end], "[]") {
		return false
	}
	return strings.TrimLeft(s[end+2:], " \t") != ""
}

// gfmLowerSource returns the source that the grammar reads in the GFM profile,
// and the offsets in it of the two-byte insertions made (see
// remapLoweredRanges). The insertions are blank lines; a blank line changes no
// reading in CommonMark, and the grammar reads the document better with them:
//
//   - at the start of the document, when it begins with a "---" line, a block
//     and another "---" line. The grammar reads that as front matter, at the
//     first byte only, and hides the block (including its headings). GFM has
//     no front matter.
//   - after an HTML block that ends at a closing marker (a comment, "<?",
//     "<!x", CDATA, or "<pre>", "<script>", "<style>", "<textarea>") when the
//     next line is not blank, for a block that starts at the first column. The
//     grammar reads such a block at the start of a document as inline HTML
//     when a heading follows it directly.
//
// The grammar remains the only reader: nothing is built from the insertions.
func gfmLowerSource(source []byte) ([]byte, []int) {
	var at []int // offsets in source before which two bytes are inserted
	if bytes.HasPrefix(source, []byte("---\n")) && (bytes.Contains(source[3:], []byte("\n---\n")) || bytes.HasSuffix(source, []byte("\n---"))) {
		at = append(at, 0)
	}
	if containsHTMLBlockStart(source) {
		lines := sourceLines(source)
		for _, block := range gfmHTMLBlocks(lines) {
			if block.closed && lines[block.first].text != "" && lines[block.first].text[0] == '<' &&
				block.last+1 < len(lines) && trimMarkdownSpace(lines[block.last+1].text) != "" {
				at = append(at, lines[block.last].next)
			}
		}
	}
	if len(at) == 0 {
		return source, nil
	}
	out := make([]byte, 0, len(source)+2*len(at))
	inserts := make([]int, 0, len(at))
	prev := 0
	for _, pos := range at {
		out = append(out, source[prev:pos]...)
		inserts = append(inserts, len(out))
		out = append(out, '\n', '\n')
		prev = pos
	}
	out = append(out, source[prev:]...)
	return out, inserts
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

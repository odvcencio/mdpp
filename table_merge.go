package mdpp

// mergeAdjacentTables repairs a split that the syntax tree makes: a header
// row and delimiter row that follow a table directly, with no blank line or
// other block between, are two more body rows of that table (GFM 4.10: the
// table ends at the first blank line or at the start of another block). The
// grammar starts a second table instead. The merged table keeps the first
// table's alignments, takes the second header as a body row, reads the
// second delimiter line as a body row of its cells' text, fits every row it
// took to the first header's width, and ends where the second table ended.
func mergeAdjacentTables(doc *Document) {
	if doc == nil || doc.Root == nil {
		return
	}
	walkNodes(doc.Root, func(n *Node, parent *Node, index int) bool {
		if len(n.Children) < 2 {
			return true
		}
		merged := n.Children[:0:0]
		changed := false
		for _, child := range n.Children {
			if len(merged) > 0 && child.Type == NodeTable && merged[len(merged)-1].Type == NodeTable &&
				tablesAreAdjacent(doc.Source, merged[len(merged)-1], child) {
				if mergeTableInto(doc.Source, merged[len(merged)-1], child) {
					changed = true
					continue
				}
			}
			merged = append(merged, child)
		}
		if changed {
			n.Children = merged
		}
		return true
	})
}

// tablesAreAdjacent reports whether the second table starts on the physical
// line after the last line of the first table, with nothing in between. A line
// between them is a blank line (Markdown's definition: nothing but spaces and
// tabs once the container prefixes are removed) or content, and in both cases
// the tables stay apart.
//
// The check looks at the source lines, not at the table ranges: a range may or
// may not include the indentation of the line that follows it. The text from
// the last character of the first table that is not a space, tab or line end
// up to the start of the second table's header row must hold exactly one line
// end, and otherwise only what a container puts in front of a line: spaces,
// tabs and quote markers.
func tablesAreAdjacent(source []byte, first, second *Node) bool {
	if len(second.Children) == 0 || len(first.Children) == 0 {
		return false
	}
	end, start := first.Range.EndByte, second.Children[0].Range.StartByte
	if end > len(source) || start > len(source) || start < 0 {
		return false
	}
	for end > 0 && isTableGapSpace(source[end-1]) {
		end--
	}
	if end > start {
		return false
	}
	// start may lie on indentation that its row range includes; the header
	// row begins at its first character that is not a space or tab.
	newlines := 0
	for _, b := range source[end:start] {
		switch b {
		case '\n':
			newlines++
		case ' ', '\t', '\r', '>':
		default:
			return false
		}
	}
	return newlines == 1
}

func isTableGapSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// fitRowToWidth makes a row exactly width cells wide (GFM 4.10: excess cells
// are ignored, missing cells are empty). The padding cells are synthetic: they
// claim no source range.
func fitRowToWidth(row *Node, width int) {
	if len(row.Children) > width {
		row.Children = row.Children[:width:width]
		return
	}
	for len(row.Children) < width {
		row.Children = append(row.Children, &Node{Type: NodeTableCell})
	}
}

func mergeTableInto(source []byte, first, second *Node) bool {
	header := second.Children[0]
	// The delimiter row is the line after the second table's header line.
	nl := bytesIndexFrom(source, '\n', header.Range.StartByte)
	if nl < 0 || nl+1 >= len(source) {
		return false
	}
	lineStart := nl + 1
	lineEnd := bytesIndexFrom(source, '\n', lineStart)
	if lineEnd < 0 {
		lineEnd = len(source)
	}
	// Skip indentation and quote markers that belong to the container.
	for lineStart < lineEnd && (source[lineStart] == ' ' || source[lineStart] == '\t' || source[lineStart] == '>') {
		lineStart++
	}
	if lineStart >= lineEnd {
		return false
	}
	delimiter := tableRowFromLine(source, lineStart, string(source[lineStart:lineEnd]), nil)
	// Every row that moves into the first table takes the first header's width.
	width := len(first.Children[0].Children)
	promoted := append([]*Node{header, delimiter}, second.Children[1:]...)
	for _, row := range promoted {
		fitRowToWidth(row, width)
	}
	first.Children = append(first.Children, promoted...)
	first.Range.EndByte = second.Range.EndByte
	first.Range.EndLine = second.Range.EndLine
	first.Range.EndCol = second.Range.EndCol
	return true
}

func bytesIndexFrom(source []byte, b byte, from int) int {
	for i := from; i >= 0 && i < len(source); i++ {
		if source[i] == b {
			return i
		}
	}
	return -1
}

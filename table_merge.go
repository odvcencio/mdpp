package mdpp

// mergeAdjacentTables repairs a split that the syntax tree makes: a header
// row and delimiter row that follow a table directly, with no blank line or
// other block between, are two more body rows of that table (GFM 4.10: the
// table ends at the first blank line or at the start of another block). The
// grammar starts a second table instead. The merged table keeps the first
// table's alignments, takes the second header as a body row, reads the
// second delimiter line as a body row of its cells' text, and ends where the
// second table ended.
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

// tablesAreAdjacent reports whether the source between two tables holds no
// blank line and no content: at most one line end, with only indentation and
// quote markers around it.
func tablesAreAdjacent(source []byte, first, second *Node) bool {
	start, end := first.Range.EndByte, second.Range.StartByte
	if start > end || end > len(source) || len(second.Children) == 0 {
		return false
	}
	newlines := 0
	for _, b := range source[start:end] {
		switch b {
		case '\n':
			newlines++
		case ' ', '\t', '>':
		default:
			return false
		}
	}
	// A table range that ends after its line end leaves no room for another
	// line end: that one would be a blank line. A range that ends before the
	// line end allows exactly the one that separates the two lines.
	allowed := 1
	if start > 0 && source[start-1] == '\n' {
		allowed = 0
	}
	if newlines > allowed {
		return false
	}
	return true
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
	first.Children = append(first.Children, header, delimiter)
	first.Children = append(first.Children, second.Children[1:]...)
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

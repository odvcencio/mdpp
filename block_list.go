package mdpp

import (
	"bytes"
	"strings"
)

// CommonMark omits paragraph tags in list items unless a blank line makes
// the list loose. Keep that decision on the list items for the renderer.
func annotateListTightness(doc *Document) {
	if doc == nil || doc.Root == nil {
		return
	}
	walkNodes(doc.Root, func(n *Node, parent *Node, index int) bool {
		if n.Type != NodeList || n.Range.StartLine == 0 || n.Range.EndByte > len(doc.Source) {
			return true
		}
		raw := bytes.TrimRight(doc.Source[n.Range.StartByte:n.Range.EndByte], " \t\r\n")
		if hasBlankSourceLine(raw) {
			return true
		}
		for _, item := range n.Children {
			if item.Type != NodeListItem {
				continue
			}
			if item.Attrs == nil {
				item.Attrs = make(map[string]string)
			}
			item.Attrs["tight"] = "true"
		}
		return true
	})
}

func hasBlankSourceLine(source []byte) bool {
	for i := 0; i < len(source); i++ {
		if source[i] != '\n' {
			continue
		}
		j := i + 1
		for j < len(source) && (source[j] == ' ' || source[j] == '\t' || source[j] == '\r') {
			j++
		}
		if j < len(source) && source[j] == '\n' {
			return true
		}
	}
	return false
}

// The block grammar consumes a tab as one byte inside list items. Restore
// columns that remain after the list's content indent and the code indent.
func repairListCodeTabs(doc *Document) {
	if doc == nil || doc.Root == nil || !bytes.ContainsRune(doc.Source, '\t') {
		return
	}
	lines := sourceLines(doc.Source)
	walkNodes(doc.Root, func(n *Node, parent *Node, index int) bool {
		if n.Type != NodeListItem || n.Range.StartLine < 1 || n.Range.StartLine > len(lines) {
			return true
		}
		base := listItemContentColumn(lines[n.Range.StartLine-1].text)
		if base == 0 {
			return true
		}
		for _, child := range n.Children {
			if child.Type != NodeCodeBlock || child.Range.StartLine < 1 || child.Range.StartLine > len(lines) {
				continue
			}
			line := lines[child.Range.StartLine-1].text
			if !strings.ContainsRune(line, '\t') {
				continue
			}
			column := 0
			for i := 0; i < len(line); i++ {
				if line[i] == '\t' {
					column += 4 - column%4
				} else if line[i] == ' ' {
					column++
				} else if i == 0 && (line[i] == '-' || line[i] == '+' || line[i] == '*') {
					column++
				} else {
					break
				}
			}
			extra := column - base - 4
			if extra <= 0 {
				continue
			}
			child.Literal = strings.Repeat(" ", extra) + child.Literal
		}
		return true
	})
}

func listItemContentColumn(line string) int {
	column := 0
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		if line[i] == '\t' {
			column += 4 - column%4
		} else {
			column++
		}
		i++
	}
	if i >= len(line) {
		return 0
	}
	if line[i] == '-' || line[i] == '+' || line[i] == '*' {
		return column + 2
	}
	start := i
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i > start && i < len(line) && (line[i] == '.' || line[i] == ')') {
		return column + i - start + 2
	}
	return 0
}

package mdpp

import (
	"strings"
)

// parseBracketLinksAt handles bracket pairs before the inline grammar can
// reinterpret a nested pair as the outer link. Large spans continue through
// the bounded inline parser; the bracket scan here is linear in its input.
func parseBracketLinksAt(text string, source []byte, baseOffset int, ctx *parseCtx) ([]*Node, bool) {
	if len(text) > maxInlineParseChunk || !strings.Contains(text, "]") || strings.Contains(text, "`") || strings.Contains(text, "<![") {
		return nil, false
	}
	if first := strings.IndexByte(text, '['); first > 0 && strings.ContainsAny(text[:first], "*_") {
		return nil, false
	}
	closes := make([]int, len(text))
	for i := range closes {
		closes[i] = -1
	}
	var stack []int
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' && i+1 < len(text) {
			i++
			continue
		}
		switch text[i] {
		case '[':
			stack = append(stack, i)
		case ']':
			if len(stack) != 0 {
				open := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				closes[open] = i
			}
		}
	}
	var nodes []*Node
	cursor := 0
	found := false
	for i := 0; i < len(text); i++ {
		image := text[i] == '!' && i+1 < len(text) && text[i+1] == '['
		if image && isEscapedByte(text, i) {
			continue
		}
		open := i
		if image {
			open++
		}
		if text[open] != '[' || closes[open] < 0 || isEscapedByte(text, open) {
			continue
		}
		close := closes[open]
		label := text[open+1 : close]
		// URI autolinks and inline HTML can contain bracket characters. Let
		// the grammar consume those constructs before considering brackets.
		if strings.Contains(label, "<") && strings.Contains(text[close+1:], ">") {
			continue
		}
		end := close + 1
		attrs := map[string]string{}
		inline := false
		if end < len(text) && text[end] == '(' {
			if href, title, next, ok := parseBracketDestination(text, end); ok {
				attrs["href"] = href
				if image {
					attrs["src"] = href
				}
				if title != "" {
					attrs["title"] = title
				}
				end = next
				inline = true
			} else {
				continue
			}
		}
		if !inline {
			if end < len(text) && text[end] == '[' {
				if refClose := closes[end]; refClose > end {
					ref := text[end+1 : refClose]
					if ref != "" {
						attrs["ref"] = ref
					}
					end = refClose + 1
				}
			}
			attrs["raw"] = text[i:end]
			attrs["label"] = label
		}
		// An inline link inside link text deactivates the outer link.
		// Images may contain links in their alt text.
		if !image && hasNestedInlineLink(text, open+1, close, closes) {
			if strings.ContainsAny(label, "*_") {
				return nil, false
			}
			continue
		}
		if !inline && !image && label == "" {
			continue
		}
		if i > cursor {
			nodes = append(nodes, parseInlineBaseAt(text[cursor:i], source, addBaseOffset(baseOffset, cursor), ctx)...)
		}
		var n *Node
		if image {
			n = newNode(NodeImage)
			altNodes := parseInlineAt(label, source, addBaseOffset(baseOffset, open+1), ctx)
			attrs["alt"] = flattenImageAlt(altNodes)
		} else {
			n = newNode(NodeLink, parseInlineAt(label, source, addBaseOffset(baseOffset, open+1), ctx)...)
		}
		n.Attrs = attrs
		n.Range = inlineSpanRange(source, baseOffset, i, end)
		nodes = append(nodes, n)
		cursor = end
		i = end - 1
		found = true
	}
	if !found {
		return nil, false
	}
	if cursor < len(text) {
		nodes = append(nodes, parseInlineBaseAt(text[cursor:], source, addBaseOffset(baseOffset, cursor), ctx)...)
	}
	return splitTextNewlines(nodes), true
}

func hasNestedInlineLink(text string, start, end int, closes []int) bool {
	for i := start; i < end; i++ {
		if text[i] != '[' || closes[i] <= i || closes[i] >= end {
			continue
		}
		if i > 0 && text[i-1] == '!' && !isEscapedByte(text, i-1) {
			continue
		}
		next := closes[i] + 1
		if next < end && text[next] == '(' {
			if _, _, after, ok := parseBracketDestination(text[:end], next); ok && after <= end {
				return true
			}
		} else if next < end && text[next] == '[' && closes[next] > next && closes[next] < end {
			return true
		}
	}
	return false
}

func parseBracketDestination(text string, open int) (href, title string, end int, ok bool) {
	if open >= len(text) || text[open] != '(' {
		return "", "", open, false
	}
	i := open + 1
	for i < len(text) && isLinkSpace(text[i]) {
		i++
	}
	start := i
	if i < len(text) && text[i] == '<' {
		i++
		start = i
		for i < len(text) && text[i] != '>' {
			if text[i] == '\\' && i+1 < len(text) {
				i += 2
				continue
			}
			if text[i] == '<' || text[i] == '\n' || text[i] == '\r' {
				return "", "", open, false
			}
			i++
		}
		if i >= len(text) {
			return "", "", open, false
		}
		href = text[start:i]
		i++
	} else {
		depth := 0
		for i < len(text) {
			c := text[i]
			if c == '\\' && i+1 < len(text) {
				i += 2
				continue
			}
			if c == '(' {
				depth++
			} else if c == ')' {
				if depth == 0 {
					break
				}
				depth--
			} else if c == '<' || c == '>' || isLinkSpace(c) {
				break
			}
			i++
		}
		if depth != 0 {
			return "", "", open, false
		}
		href = text[start:i]
	}
	spaceStart := i
	for i < len(text) && isLinkSpace(text[i]) {
		i++
	}
	if i < len(text) && text[i] != ')' {
		if i == spaceStart || (text[i] != '"' && text[i] != '\'' && text[i] != '(') {
			return "", "", open, false
		}
		quote := text[i]
		close := quote
		if quote == '(' {
			close = ')'
		}
		i++
		start = i
		for i < len(text) && text[i] != close {
			if text[i] == '\\' && i+1 < len(text) {
				i += 2
				continue
			}
			i++
		}
		if i >= len(text) {
			return "", "", open, false
		}
		title = decodeMarkdownText(text[start:i])
		i++
		for i < len(text) && isLinkSpace(text[i]) {
			i++
		}
	}
	if i >= len(text) || text[i] != ')' {
		return "", "", open, false
	}
	return unescapeLinkDestination(href), title, i + 1, true
}

func isLinkSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n'
}

func flattenImageAlt(nodes []*Node) string {
	var out strings.Builder
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.Type == NodeImage {
			out.WriteString(n.Attrs["alt"])
			return
		}
		if n.Type == NodeText || n.Type == NodeCodeSpan {
			out.WriteString(decodeMarkdownText(n.Literal))
			return
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return out.String()
}

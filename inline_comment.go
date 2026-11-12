package mdpp

import (
	"regexp"
	"strconv"
	"strings"
)

// Inline HTML comments (CommonMark 6.6, GFM 6.6, example 645).
//
// A comment is "<!-->", "<!--->", or "<!--", any text that does not contain
// "-->", and "-->". The inline grammar reads only some of these as one raw
// HTML tag: "<!-->" and "<!--->" come back as plain text, and so does a
// comment whose text holds "--". The check below runs before the grammar: it
// finds the comments, swaps each for a raw HTML tag of the same length that
// the grammar reads as one node, parses the text, and puts the comment back.
// Because the length does not change, every offset in the result stays exact.

var (
	inlineOpenTagRe  = regexp.MustCompile("^<[A-Za-z][A-Za-z0-9-]*(?:[ \t\n]+[A-Za-z_:][A-Za-z0-9_.:-]*(?:[ \t\n]*=[ \t\n]*(?:[^ \t\n\"'=<>`]+|'[^']*'|\"[^\"]*\"))?)*[ \t\n]*/?>")
	inlineCloseTagRe = regexp.MustCompile("^</[A-Za-z][A-Za-z0-9-]*[ \t\n]*>")
	inlineAutolinkRe = regexp.MustCompile("^<[A-Za-z][A-Za-z0-9+.-]{1,31}:[^ \t\n<>]*>")
)

// maxInlineComments is the number of distinct stand-in tags (base 36, two
// digits). A text with more comments keeps the grammar's reading.
const maxInlineComments = 36 * 36

// inlineCommentEnd returns the end of the HTML comment that starts at text[i],
// or -1 when none starts there.
func inlineCommentEnd(text string, i int) int {
	rest := text[i:]
	switch {
	case strings.HasPrefix(rest, "<!-->"):
		return i + 5
	case strings.HasPrefix(rest, "<!--->"):
		return i + 6
	case strings.HasPrefix(rest, "<!--"):
		if end := strings.Index(rest[4:], "-->"); end >= 0 {
			return i + 4 + end + 3
		}
	}
	return -1
}

// findInlineComments returns the byte spans of the comments in text. Code
// spans, backslash-escaped "<", raw HTML tags and autolinks hide a comment
// that lies inside them.
func findInlineComments(text string) [][2]int {
	var spans [][2]int
	for i := 0; i < len(text); {
		switch text[i] {
		case '\\':
			i += 2
		case '`':
			runEnd := i + 1
			for runEnd < len(text) && text[runEnd] == '`' {
				runEnd++
			}
			if close := matchingBacktickRun(text, runEnd, runEnd-i); close >= 0 {
				i = close + (runEnd - i)
			} else {
				i = runEnd
			}
		case '<':
			if end := inlineCommentEnd(text, i); end >= 0 {
				spans = append(spans, [2]int{i, end})
				i = end
				continue
			}
			rest := text[i:]
			if m := inlineOpenTagRe.FindString(rest); m != "" {
				i += len(m)
			} else if m := inlineCloseTagRe.FindString(rest); m != "" {
				i += len(m)
			} else if m := inlineAutolinkRe.FindString(rest); m != "" {
				i += len(m)
			} else {
				i++
			}
		default:
			i++
		}
	}
	return spans
}

// inlineCommentMask returns the stand-in tag for comment number index that is
// n bytes long: "<q" and two base-36 digits, then spaces (and line ends where
// the comment has them), then ">". The stand-in is a valid raw HTML tag.
func inlineCommentMask(text string, span [2]int, index int) string {
	n := span[1] - span[0]
	digits := strconv.FormatInt(int64(index), 36)
	for len(digits) < 2 {
		digits = "0" + digits
	}
	var b strings.Builder
	b.Grow(n)
	b.WriteString("<q" + digits)
	for k := 4; k < n-1; k++ {
		if text[span[0]+k] == '\n' {
			b.WriteByte('\n')
		} else {
			b.WriteByte(' ')
		}
	}
	b.WriteByte('>')
	return b.String()
}

// parseInlineAt parses inline text. Comments that the inline grammar does not
// read as one node are swapped for stand-in tags for the parse (see above).
func parseInlineAt(text string, source []byte, baseOffset int, ctx *parseCtx) []*Node {
	if !strings.Contains(text, "<!--") {
		return parseInlineUnmaskedAt(text, source, baseOffset, ctx)
	}
	spans := findInlineComments(text)
	if len(spans) == 0 || len(spans) > maxInlineComments {
		return parseInlineUnmaskedAt(text, source, baseOffset, ctx)
	}
	masked := []byte(text)
	originals := make(map[string]string, len(spans))
	for index, span := range spans {
		mask := inlineCommentMask(text, span, index)
		copy(masked[span[0]:span[1]], mask)
		originals[mask] = text[span[0]:span[1]]
	}
	nodes := parseInlineUnmaskedAt(string(masked), source, baseOffset, ctx)
	for _, n := range nodes {
		restoreInlineComments(n, originals)
	}
	return nodes
}

func restoreInlineComments(n *Node, originals map[string]string) {
	if n == nil {
		return
	}
	if strings.Contains(n.Literal, "<q") {
		if original, ok := originals[n.Literal]; ok {
			n.Literal = original
		} else {
			for mask, original := range originals {
				n.Literal = strings.ReplaceAll(n.Literal, mask, original)
			}
		}
	}
	for key, value := range n.Attrs {
		if strings.Contains(value, "<q") {
			for mask, original := range originals {
				value = strings.ReplaceAll(value, mask, original)
			}
			n.Attrs[key] = value
		}
	}
	for _, child := range n.Children {
		restoreInlineComments(child, originals)
	}
}

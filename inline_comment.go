package mdpp

import (
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Inline HTML comments (CommonMark 0.31.2 section 6.6; GFM example 645).
//
// A comment is "<!-->", "<!--->", or "<!--", a string that does not contain
// "-->", and "-->". The inline grammar reads only some of these as one raw HTML
// tag, so mdpp finds comments itself, by source span, before the grammar runs:
//
//   - findInlineComments scans the inline text once, left to right, and returns
//     the byte span of every comment.
//   - parseInlineAt builds one HTMLInline node from each span and parses the
//     text between the comments with the ordinary inline reader.
//
// Nothing in the text is rewritten, so there is nothing to put back, no limit
// on the number of comments, and no chunk boundary can fall inside a comment.
//
// Precedence. The scanner takes constructs in the order in which they start,
// and a construct that starts earlier hides everything inside it:
//
//  1. a backslash escape hides the next character, so "\<!-->" is text;
//  2. a code span (a backtick string and the next backtick string of the same
//     length) hides its content; a backslash does not escape inside it;
//  3. an autolink, a raw HTML tag (with its attribute values), a processing
//     instruction, a declaration or a CDATA section that starts first hides a
//     comment opener inside it ("<a title='<!-- c -->'>" holds no comment);
//  4. the destination and title of an inline link or image hide a comment
//     opener inside them ("[a](<!-- c -->)" holds no comment); the link text is
//     scanned as ordinary inline text, so a comment in it is a comment.
//
// A comment that starts first hides every construct inside it.

var (
	inlineOpenTagRe  = regexp.MustCompile("^<[A-Za-z][A-Za-z0-9-]*(?:[ \t\n]+[A-Za-z_:][A-Za-z0-9_.:-]*(?:[ \t\n]*=[ \t\n]*(?:[^ \t\n\"'=<>`]+|'[^']*'|\"[^\"]*\"))?)*[ \t\n]*/?>")
	inlineCloseTagRe = regexp.MustCompile("^</[A-Za-z][A-Za-z0-9-]*[ \t\n]*>")
	inlineAutolinkRe = regexp.MustCompile("^<[A-Za-z][A-Za-z0-9+.-]{1,31}:[^\\x00-\\x20<>]*>")
	inlineEmailRe    = regexp.MustCompile("^<[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*>")
)

// commentScan holds the state of one scan. Each "find the end" search that
// fails once is remembered: a search that found no terminator from one
// position finds none from a later position, so the scan stays linear.
type commentScan struct {
	text      string
	ctx       *parseCtx
	checks    int
	cancelled bool
	steps     int
	// backtick runs by length, as ascending start offsets
	runs map[int][]int
	// searches that reached the end of the text without a terminator
	noCommentEnd, noPIEnd, noDeclEnd, noCDATAEnd bool
	noTitleEnd                                   [3]bool // ", ', (
}

type commentBracket struct {
	start int
	image bool
	epoch uint64
}

// findInlineComments returns the byte spans of the comments in text, in
// order, and the byte spans of the inline links and images in text (the part
// from "[" or "![" to the closing parenthesis), in order of their end.
func findInlineComments(text string) (comments, links [][2]int) {
	return findInlineCommentsScan(&commentScan{text: text})
}

func findInlineCommentsScan(s *commentScan) (comments, links [][2]int) {
	text := s.text
	s.indexBackticks()
	var stack []commentBracket
	var epoch uint64
	for i := 0; i < len(text); {
		if s.poll() {
			return nil, nil
		}
		s.steps++
		switch text[i] {
		case '\\':
			i += 2
		case '`':
			runEnd := i + 1
			for runEnd < len(text) && text[runEnd] == '`' {
				if s.poll() {
					return nil, nil
				}
				runEnd++
			}
			if closeAt := s.nextRun(runEnd-i, runEnd); closeAt >= 0 {
				i = closeAt + (runEnd - i)
			} else {
				i = runEnd
			}
		case '<':
			if end := s.commentEnd(i); end >= 0 {
				comments = append(comments, [2]int{i, end})
				i = end
			} else if end := s.otherHTMLEnd(i); end >= 0 {
				i = end
			} else {
				i++
			}
		case '!':
			if i+1 < len(text) && text[i+1] == '[' {
				stack = append(stack, commentBracket{start: i, image: true, epoch: epoch})
				i += 2
			} else {
				i++
			}
		case '[':
			stack = append(stack, commentBracket{start: i, epoch: epoch})
			i++
		case ']':
			i++
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if (!top.image && top.epoch != epoch) || i >= len(text) || text[i] != '(' {
				continue
			}
			if end := s.linkTailEnd(i + 1); end >= 0 {
				links = append(links, [2]int{top.start, end})
				i = end
				if !top.image {
					// Invalidate earlier link openers in constant time; image openers stay active.
					epoch++
				}
			}
		default:
			i++
		}
	}
	return comments, links
}

// poll bounds cancellation and deadline latency during scanner loops.
func (s *commentScan) poll() bool {
	if s.cancelled {
		return true
	}
	s.checks++
	if s.checks&4095 != 1 || s.ctx == nil || s.ctx.budget == nil {
		return false
	}
	if s.ctx.budget.check() {
		return false
	}
	s.ctx.inlineTimeoutOccurred = true
	s.cancelled = true
	return true
}

// index scans bounded chunks, preserving matches crossing a chunk boundary.
func (s *commentScan) index(start int, needle string) int {
	for start < len(s.text) {
		s.checks = 0 // check each bounded substring scan
		if s.poll() {
			return -1
		}
		end := min(len(s.text), start+4096+len(needle)-1)
		if at := strings.Index(s.text[start:end], needle); at >= 0 {
			return start + at
		}
		start += 4096
	}
	return -1
}

func (s *commentScan) indexBackticks() {
	text := s.text
	for i := 0; i < len(text); {
		if s.poll() {
			return
		}
		if text[i] != '`' {
			i++
			continue
		}
		j := i + 1
		for j < len(text) && text[j] == '`' {
			if s.poll() {
				return
			}
			j++
		}
		if s.runs == nil {
			s.runs = map[int][]int{}
		}
		s.runs[j-i] = append(s.runs[j-i], i)
		i = j
	}
}

// nextRun returns the start of the first backtick string of the given length
// that starts at or after from, or -1.
func (s *commentScan) nextRun(length, from int) int {
	starts := s.runs[length]
	k := sort.SearchInts(starts, from)
	if k < len(starts) {
		return starts[k]
	}
	return -1
}

// commentEnd returns the end of the comment that starts at text[i], or -1.
func (s *commentScan) commentEnd(i int) int {
	rest := s.text[i:]
	switch {
	case strings.HasPrefix(rest, "<!-->"):
		return i + 5
	case strings.HasPrefix(rest, "<!--->"):
		return i + 6
	case strings.HasPrefix(rest, "<!--"):
		if s.noCommentEnd {
			return -1
		}
		if end := s.index(i+4, "-->"); end >= 0 {
			return end + 3
		}
		s.noCommentEnd = true
	}
	return -1
}

// otherHTMLEnd returns the end of the autolink, raw HTML tag, processing
// instruction, declaration or CDATA section that starts at text[i], or -1.
func (s *commentScan) otherHTMLEnd(i int) int {
	text := s.text
	rest := text[i:]
	switch {
	case strings.HasPrefix(rest, "<?"):
		if s.noPIEnd {
			return -1
		}
		if end := s.index(i+2, "?>"); end >= 0 {
			return end + 2
		}
		s.noPIEnd = true
		return -1
	case strings.HasPrefix(rest, "<![CDATA["):
		if s.noCDATAEnd {
			return -1
		}
		if end := s.index(i+9, "]]>"); end >= 0 {
			return end + 3
		}
		s.noCDATAEnd = true
		return -1
	case strings.HasPrefix(rest, "<!") && len(rest) > 2 && (rest[2] >= 'A' && rest[2] <= 'Z' || rest[2] >= 'a' && rest[2] <= 'z'):
		if s.noDeclEnd {
			return -1
		}
		if end := s.index(i+2, ">"); end >= 0 {
			return end + 1
		}
		s.noDeclEnd = true
		return -1
	}
	for _, re := range []*regexp.Regexp{inlineAutolinkRe, inlineEmailRe, inlineOpenTagRe, inlineCloseTagRe} {
		if len(rest) <= 4096 || s.ctx == nil || s.ctx.budget == nil {
			if m := re.FindString(rest); m != "" {
				return i + len(m)
			}
			continue
		}
		m := re.FindReaderIndex(&commentRuneReader{scan: s, text: rest})
		if s.cancelled {
			return -1
		}
		if len(m) > 0 {
			return i + m[1]
		}
	}
	return -1
}

type commentRuneReader struct {
	scan   *commentScan
	text   string
	offset int
}

func (r *commentRuneReader) ReadRune() (rune, int, error) {
	if r.offset >= len(r.text) || r.scan.poll() {
		return 0, 0, io.EOF
	}
	ch, width := utf8.DecodeRuneInString(r.text[r.offset:])
	r.offset += width
	return ch, width, nil
}

func isASCIIPunct(b byte) bool {
	return b >= '!' && b <= '/' || b >= ':' && b <= '@' || b >= '[' && b <= '`' || b >= '{' && b <= '~'
}

// skipLinkSpace skips spaces, tabs and at most one line ending.
func (s *commentScan) skipLinkSpace(i int) int {
	text := s.text
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		if s.poll() {
			return len(text)
		}
		i++
	}
	if i < len(text) && text[i] == '\n' {
		i++
		for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
			if s.poll() {
				return len(text)
			}
			i++
		}
	}
	return i
}

// linkTailEnd parses the part of an inline link after "(" (destination,
// optional title, ")" per CommonMark 6.3) and returns the offset after ")",
// or -1.
func (s *commentScan) linkTailEnd(i int) int {
	text := s.text
	i = s.skipLinkSpace(i)
	if i >= len(text) {
		return -1
	}
	if text[i] == '<' {
		i++
		for {
			if s.poll() {
				return -1
			}
			if i >= len(text) || text[i] == '\n' || text[i] == '<' {
				return -1
			}
			if text[i] == '\\' && i+1 < len(text) && isASCIIPunct(text[i+1]) {
				i += 2
				continue
			}
			if text[i] == '>' {
				i++
				break
			}
			i++
		}
	} else {
		start, depth := i, 0
	dest:
		for i < len(text) {
			if s.poll() {
				return -1
			}
			c := text[i]
			switch {
			case c == '\\' && i+1 < len(text) && isASCIIPunct(text[i+1]):
				i += 2
			case c <= ' ' || c == 0x7f:
				break dest
			case c == '(':
				depth++
				if depth > 32 {
					return -1
				}
				i++
			case c == ')':
				if depth == 0 {
					break dest
				}
				depth--
				i++
			default:
				i++
			}
		}
		if depth != 0 || i == start && (i >= len(text) || text[i] != ')') {
			return -1
		}
	}
	afterDest := i
	i = s.skipLinkSpace(i)
	if i != afterDest && i < len(text) {
		if end := s.titleEnd(i); end >= 0 {
			i = s.skipLinkSpace(end)
		}
	}
	if i < len(text) && text[i] == ')' {
		return i + 1
	}
	return -1
}

// titleEnd returns the offset after the link title that starts at text[i], or
// -1 when none starts there.
func (s *commentScan) titleEnd(i int) int {
	text := s.text
	var kind int
	var closer byte
	switch text[i] {
	case '"':
		kind, closer = 0, '"'
	case '\'':
		kind, closer = 1, '\''
	case '(':
		kind, closer = 2, ')'
	default:
		return -1
	}
	if s.noTitleEnd[kind] {
		return -1
	}
	for j := i + 1; j < len(text); j++ {
		if s.poll() {
			return -1
		}
		switch c := text[j]; {
		case c == '\\' && j+1 < len(text) && isASCIIPunct(text[j+1]):
			j++
		case c == closer:
			return j + 1
		case kind == 2 && c == '(':
			return -1
		}
	}
	if kind != 2 {
		s.noTitleEnd[kind] = true
	}
	return -1
}

// parseInlineAt parses inline text. Each inline HTML comment becomes one
// HTMLInline node built from its source span; the text between comments goes
// to the inline reader. A comment inside the text of an inline link or image
// stays with that link, so the link keeps its shape; the reader meets the
// comment again when it parses the link text.
func parseInlineAt(text string, source []byte, baseOffset int, ctx *parseCtx) []*Node {
	scanner := &commentScan{text: text, ctx: ctx}
	found := false
	if ctx == nil || ctx.budget == nil {
		found = strings.Contains(text, "<!--")
	} else {
		found = scanner.index(0, "<!--") >= 0
	}
	if scanner.cancelled {
		return []*Node{textNodeRange(text, inlineSpanRange(source, baseOffset, 0, len(text)))}
	}
	if !found {
		return parseInlineSegmentAt(text, source, baseOffset, ctx)
	}
	comments, links := findInlineCommentsScan(scanner)
	if scanner.cancelled {
		return []*Node{textNodeRange(text, inlineSpanRange(source, baseOffset, 0, len(text)))}
	}
	if len(comments) == 0 {
		return parseInlineSegmentAt(text, source, baseOffset, ctx)
	}
	if nodes, ok := parseCommentDelimitersAt(text, source, baseOffset, comments, links, ctx); ok {
		return splitTextNewlines(nodes)
	}
	sort.Slice(links, func(a, b int) bool { return links[a][0] < links[b][0] })
	var nodes []*Node
	cursor, link := 0, 0
	for _, span := range comments {
		for link < len(links) && links[link][1] <= span[0] {
			link++
		}
		if link < len(links) && links[link][0] < span[0] {
			continue // inside a link or image: the link reader handles it
		}
		if span[0] > cursor {
			nodes = append(nodes, parseInlineSegmentAt(text[cursor:span[0]], source, addBaseOffset(baseOffset, cursor), ctx)...)
		}
		html := newNode(NodeHTMLInline)
		html.Range = inlineSpanRange(source, baseOffset, span[0], span[1])
		html.Literal = text[span[0]:span[1]]
		nodes = append(nodes, html)
		cursor = span[1]
	}
	if cursor == 0 {
		return parseInlineSegmentAt(text, source, baseOffset, ctx)
	}
	if cursor < len(text) {
		nodes = append(nodes, parseInlineSegmentAt(text[cursor:], source, addBaseOffset(baseOffset, cursor), ctx)...)
	}
	return nodes
}

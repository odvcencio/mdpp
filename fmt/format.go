// Package fmt provides canonical formatting for Markdown++ source.
package fmt

import (
	"bufio"
	"bytes"
	"errors"
	stdfmt "fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/mdpp"
)

var (
	// ErrMeaningChanged reports that formatting would violate the meaning-preservation contract.
	ErrMeaningChanged     = errors.New("formatter changed document meaning")
	whitespaceBetweenTags = regexp.MustCompile(`>\s+<`)
	setextH1Re            = regexp.MustCompile(`^\s*=+\s*$`)
	setextH2Re            = regexp.MustCompile(`^\s*-+\s*$`)
	orderedListLineRe     = regexp.MustCompile(`^([ \t]*)([0-9]+)([.)])([ \t]+)(.*)$`)
)

type formattedLine struct {
	text       string
	protected  bool
	sourceLine int
}

// Format reformats src into canonical Markdown++ form.
func Format(src []byte) ([]byte, error) {
	current := src
	// AST-aware rewrites can expose syntax to a later pass (for example,
	// unwrapping a paragraph can create a list-marker line). Iterate to the
	// formatter's fixed point so one public call always returns canonical,
	// idempotent output.
	for i := 0; i < 32; i++ {
		next, err := formatOnce(current)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(next, current) {
			if difference := meaningDifference(src, next); difference != "" {
				return append([]byte(nil), src...), stdfmt.Errorf("%w: %s", ErrMeaningChanged, difference)
			}
			return next, nil
		}
		current = next
	}
	if difference := meaningDifference(src, current); difference != "" {
		return append([]byte(nil), src...), stdfmt.Errorf("%w: %s", ErrMeaningChanged, difference)
	}
	return current, nil
}

func meaningDifference(before, after []byte) string {
	if bytes.Equal(before, after) {
		return ""
	}
	beforeDoc, err := mdpp.Parse(before)
	if err != nil {
		return "could not parse input"
	}
	afterDoc, err := mdpp.Parse(after)
	if err != nil {
		return "could not parse formatted output"
	}
	if !reflect.DeepEqual(beforeDoc.Frontmatter(), afterDoc.Frontmatter()) {
		return "frontmatter metadata changed"
	}
	beforeFrontmatter := frontmatterBytes(beforeDoc, before)
	afterFrontmatter := frontmatterBytes(afterDoc, after)
	if !bytes.Equal(beforeFrontmatter, afterFrontmatter) {
		return "frontmatter bytes changed"
	}
	beforeCode, err := protectedCodeSignature(beforeDoc, before)
	if err != nil {
		return "could not inspect protected code in input"
	}
	afterCode, err := protectedCodeSignature(afterDoc, after)
	if err != nil {
		return "could not inspect protected code in formatted output"
	}
	if !reflect.DeepEqual(beforeCode, afterCode) {
		return "code span text or code block info/body changed"
	}
	for _, opts := range []mdpp.RenderOptions{{}, {UnsafeHTML: true, HeadingIDs: true}} {
		beforeHTML, err := mdpp.Render(beforeDoc, opts)
		if err != nil {
			return "could not render input"
		}
		afterHTML, err := mdpp.Render(afterDoc, opts)
		if err != nil {
			return "could not render formatted output"
		}
		if normalizeHTML(string(beforeHTML)) != normalizeHTML(string(afterHTML)) {
			return "rendered HTML changed"
		}
	}
	return ""
}

// PreservesMeaning reports whether two Markdown sources have equivalent
// frontmatter, protected code, and rendered HTML under the formatter's
// meaning-preservation contract.
func PreservesMeaning(before, after []byte) bool {
	return meaningDifference(before, after) == ""
}

type codeSignature struct {
	kind string
	info string
	body string
}

func frontmatterBytes(doc *mdpp.Document, source []byte) []byte {
	if doc == nil || doc.Root == nil {
		return nil
	}
	var result []byte
	doc.Root.Walk(func(n *mdpp.Node) bool {
		if n.Type != mdpp.NodeFrontmatter {
			return true
		}
		start, end, ok := rawFrontmatterRange(source, n.Range.StartByte, n.Range.EndByte)
		if ok {
			result = append([]byte(nil), source[start:end]...)
		}
		return false
	})
	return result
}

func protectedCodeSignature(doc *mdpp.Document, source []byte) ([]codeSignature, error) {
	if doc == nil || doc.Root == nil {
		return nil, nil
	}
	var result []codeSignature
	var rangeErr error
	doc.Root.Walk(func(n *mdpp.Node) bool {
		switch n.Type {
		case mdpp.NodeCodeSpan:
			result = append(result, codeSignature{kind: "span", body: n.Literal})
		case mdpp.NodeCodeBlock, mdpp.NodeDiagram:
			info, body, err := exactCodeBlock(n, source)
			if err != nil {
				rangeErr = err
				return false
			}
			result = append(result, codeSignature{kind: "block", info: info, body: body})
		}
		return true
	})
	return result, rangeErr
}

func rawFrontmatterRange(source []byte, start, end int) (int, int, bool) {
	if start < 0 || end < start {
		return 0, 0, false
	}
	rawStart := rawOffsetForNormalizedSource(source, start)
	rawEnd := rawOffsetForNormalizedSource(source, end)
	if rawStart < 0 || rawEnd < rawStart || rawEnd > len(source) {
		return 0, 0, false
	}
	return rawStart, rawEnd, true
}

func rawOffsetForNormalizedSource(source []byte, target int) int {
	if target <= 0 {
		return 0
	}
	normalized := 0
	for raw := 0; raw < len(source); raw++ {
		if source[raw] == '\r' {
			if raw+1 < len(source) && source[raw+1] == '\n' {
				raw++
			}
			normalized++
		} else {
			normalized++
		}
		if normalized == target {
			return raw + 1
		}
	}
	if normalized < target {
		return len(source)
	}
	return -1
}

func exactCodeBlock(n *mdpp.Node, source []byte) (string, string, error) {
	lines := bytes.SplitAfter(source, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	start, end := n.Range.StartLine-1, n.Range.EndLine-1
	if start < 0 || end < start || start >= len(lines) {
		return "", "", stdfmt.Errorf("invalid code block line range %d-%d", n.Range.StartLine, n.Range.EndLine)
	}
	open := stripBlockquotePrefix(bytes.TrimLeft(lines[start], " \t"))
	if len(open) > 0 && (open[0] == '`' || open[0] == '~') {
		marker := open[0]
		run := 0
		for run < len(open) && open[run] == marker {
			run++
		}
		info := bytes.TrimSuffix(bytes.TrimSuffix(open[run:], []byte("\n")), []byte("\r"))
		last := min(end+1, len(lines)-1)
		for closeLine := start + 1; closeLine <= last; closeLine++ {
			if fenceLineCloses(lines[closeLine], marker, run) {
				return string(info), string(bytes.Join(lines[start+1:closeLine], nil)), nil
			}
		}
		return "", "", stdfmt.Errorf("missing closing fence for code block at lines %d-%d", n.Range.StartLine, n.Range.EndLine)
	}
	last := min(end, len(lines)-1)
	if n.Range.EndCol == 1 && last > start {
		last--
	}
	if last < start {
		last = start
	}
	return "", string(bytes.Join(lines[start:last+1], nil)), nil
}

func stripBlockquotePrefix(line []byte) []byte {
	for {
		line = bytes.TrimLeft(line, " \t")
		if len(line) == 0 || line[0] != '>' {
			return line
		}
		line = bytes.TrimPrefix(line[1:], []byte(" "))
	}
}

func fenceLineCloses(line []byte, marker byte, minRun int) bool {
	line = stripBlockquotePrefix(line)
	i := 0
	for i < len(line) && line[i] == marker {
		i++
	}
	return i >= minRun && strings.TrimSpace(string(line[i:])) == ""
}

func normalizeHTML(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = whitespaceBetweenTags.ReplaceAllString(s, "><")
	return strings.TrimSpace(s)
}

func formatOnce(src []byte) ([]byte, error) {
	// Normalize before parsing: the AST's byte and line ranges must be
	// computed against the same bytes the line rewrites read, or CRLF and
	// BOM inputs shift every range.
	src = bytes.TrimPrefix(normalizeLineEndings(src), []byte{0xEF, 0xBB, 0xBF})
	doc, err := mdpp.Parse(src)
	if err != nil {
		return nil, err
	}
	lines := scanLines(src)
	protectedLines := protectedSourceLines(doc.Root, src, lines)
	setextHeadings := parsedTopLevelSetextHeadings(doc.Root, lines)
	out := make([]formattedLine, 0, len(lines))

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if protectedLines[i] {
			out = append(out, formattedLine{text: line, protected: true, sourceLine: i + 1})
			continue
		}
		if level := setextHeadings[i+1]; level > 0 {
			out = append(out, formattedLine{text: strings.Repeat("#", level) + " " + line, sourceLine: i + 1})
			i++
			continue
		}

		// Trailing whitespace can be a hard break in Markdown. Preserve its
		// full source line when the AST does not give us a reliable range.
		if strings.TrimRight(line, " \t") != line {
			out = append(out, formattedLine{text: line, protected: true, sourceLine: i + 1})
			continue
		}
		out = append(out, formattedLine{text: line, sourceLine: i + 1})
	}

	// Ordered markers are rewritten from their parsed list start value. Other
	// canonical rewrites stay disabled until they can preserve AST boundaries.
	out = rewriteOrderedListNumbers(doc.Root, out)
	return []byte(joinFormattedLines(out)), nil
}

func parsedTopLevelSetextHeadings(root *mdpp.Node, lines []string) map[int]int {
	starts := make(map[int]int)
	if root == nil {
		return starts
	}
	for _, node := range root.Children {
		if node == nil || node.Type != mdpp.NodeHeading || node.Range.StartCol != 1 ||
			node.Range.StartLine == 0 || node.Range.EndLine != node.Range.StartLine+2 {
			continue
		}
		level, err := strconv.Atoi(node.Attr("level"))
		if err != nil || (level != 1 && level != 2) {
			continue
		}
		start := node.Range.StartLine - 1
		if start < 0 || start+1 >= len(lines) || lines[start] == "" || lines[start] != strings.TrimSpace(lines[start]) {
			continue
		}
		underline := strings.TrimSpace(lines[start+1])
		if (level == 1 && setextH1Re.MatchString(underline)) || (level == 2 && setextH2Re.MatchString(underline)) {
			starts[node.Range.StartLine] = level
		}
	}
	return starts
}

func protectedSourceLines(root *mdpp.Node, source []byte, lines []string) []bool {
	protected := make([]bool, len(lines))
	if root == nil || len(lines) == 0 {
		return protected
	}
	type lineRange struct{ start, end int }
	lineRanges := make([]lineRange, len(lines))
	offset := 0
	for i, line := range lines {
		start := offset
		end := start + len(line)
		if end > len(source) {
			end = len(source)
		}
		if end < len(source) && source[end] == '\n' {
			end++
		}
		lineRanges[i] = lineRange{start: start, end: end}
		offset = end
	}
	var markRange func(start, end int)
	markRange = func(start, end int) {
		for i, lr := range lineRanges {
			if start < lr.end && end > lr.start {
				protected[i] = true
			}
		}
	}
	var markAll func()
	markAll = func() {
		for i := range protected {
			protected[i] = true
		}
	}
	var walk func(n *mdpp.Node, fallback *mdpp.Range)
	walk = func(n *mdpp.Node, fallback *mdpp.Range) {
		if n == nil {
			return
		}
		isProtected := false
		switch n.Type {
		case mdpp.NodeFrontmatter, mdpp.NodeCodeSpan, mdpp.NodeCodeBlock, mdpp.NodeDiagram,
			mdpp.NodeHardBreak, mdpp.NodeMathInline, mdpp.NodeMathBlock, mdpp.NodeHTMLBlock, mdpp.NodeHTMLInline,
			mdpp.NodeContainerDirective:
			isProtected = true
		default:
		}
		r := n.Range
		rangeValid := r.StartByte >= 0 && r.EndByte > r.StartByte && r.EndByte <= len(source)
		if isProtected {
			if rangeValid {
				markRange(r.StartByte, r.EndByte)
			} else if fallback != nil && fallback.StartByte >= 0 && fallback.EndByte > fallback.StartByte && fallback.EndByte <= len(source) {
				markRange(fallback.StartByte, fallback.EndByte)
			} else {
				markAll()
			}
		}
		nextFallback := fallback
		if rangeValid {
			nextFallback = &r
		}
		for _, child := range n.Children {
			walk(child, nextFallback)
		}
	}
	walk(root, nil)
	return protected
}

// canonicalizeRewrittenLines closes the formatter's own transformation
// boundary. AST-driven rewrites such as paragraph unwrapping can bring a
// heading or list marker together with following text; canonicalize that new
// line in the same pass so the next Format call cannot reinterpret it and
// change the output again.
func canonicalizeRewrittenLines(lines []formattedLine) []formattedLine {
	for i := range lines {
		if lines[i].protected {
			continue
		}
		line := canonicalHeadingLine(lines[i].text)
		lines[i].text = line
	}
	return lines
}

func normalizeLineEndings(src []byte) []byte {
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	src = bytes.ReplaceAll(src, []byte("\r"), []byte("\n"))
	return src
}

func scanLines(src []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(src))
	scanner.Buffer(make([]byte, 0, 64*1024), len(src)+1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

func isFenceLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func isFenceCloseLine(trimmed string, marker byte, markerLen int) bool {
	if marker == 0 || markerLen == 0 || len(trimmed) < markerLen {
		return false
	}
	if trimmed[0] != marker {
		return false
	}
	i := 0
	for i < len(trimmed) && trimmed[i] == marker {
		i++
	}
	if i < markerLen {
		return false
	}
	return strings.TrimSpace(trimmed[i:]) == ""
}

func fenceRunLength(trimmed string) int {
	if trimmed == "" {
		return 0
	}
	marker := trimmed[0]
	n := 0
	for n < len(trimmed) && trimmed[n] == marker {
		n++
	}
	return n
}

func isDisplayMathDelimiter(trimmed string) bool {
	return strings.HasPrefix(trimmed, "$$") && strings.Count(trimmed, "$$") == 1
}

func isHTMLBlockLine(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">")
}

func canonicalFenceLine(line string) string {
	return canonicalFenceLineWithMarker(line, "")
}

func canonicalFenceLineWithMarker(line string, chosenMarker string) string {
	trimmed := strings.TrimRight(line, " \t")
	indentLen := len(trimmed) - len(strings.TrimLeft(trimmed, " "))
	indent := trimmed[:indentLen]
	rest := strings.TrimLeft(trimmed, " ")
	if !strings.HasPrefix(rest, "```") && !strings.HasPrefix(rest, "~~~") {
		return trimmed
	}
	markerByte := rest[0]
	markerLen := 0
	for markerLen < len(rest) && rest[markerLen] == markerByte {
		markerLen++
	}
	marker := rest[:markerLen]
	if chosenMarker != "" {
		marker = strings.Repeat(chosenMarker[:1], markerLen)
	}
	info := strings.TrimSpace(rest[markerLen:])
	if info == "" {
		return indent + marker
	}
	parts := strings.Fields(info)
	if len(parts) > 0 {
		parts[0] = canonicalFenceInfoToken(parts[0])
		for i := 1; i < len(parts); i++ {
			parts[i] = canonicalFenceAttrToken(parts[i])
		}
	}
	return indent + marker + " " + strings.Join(parts, " ")
}

func canonicalFenceInfoToken(token string) string {
	if token == "" {
		return token
	}
	if strings.Contains(token, "=") {
		return canonicalFenceAttrToken(token)
	}
	return strings.ToLower(token)
}

func canonicalFenceAttrToken(token string) string {
	if token == "" || !strings.Contains(token, "=") {
		return token
	}
	start := 0
	for start < len(token) && strings.ContainsRune("{[(", rune(token[start])) {
		start++
	}
	end := len(token)
	for end > start && strings.ContainsRune("}]),", rune(token[end-1])) {
		end--
	}
	core := token[start:end]
	key, value, ok := strings.Cut(core, "=")
	if !ok || !isSimpleFenceAttrKey(key) {
		return token
	}
	return token[:start] + strings.ToLower(key) + "=" + value + token[end:]
}

func isSimpleFenceAttrKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		case c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

func canonicalTildeFenceBlock(lines []string, start int) ([]formattedLine, int) {
	openLine := lines[start]
	trimmed := strings.TrimSpace(openLine)
	markerLen := fenceRunLength(trimmed)
	if markerLen == 0 || trimmed[0] != '~' {
		return []formattedLine{{text: openLine, sourceLine: start + 1}}, start
	}
	closeIdx := start + 1
	for closeIdx < len(lines) {
		if isFenceCloseLine(strings.TrimSpace(lines[closeIdx]), '~', markerLen) {
			break
		}
		closeIdx++
	}
	if closeIdx >= len(lines) {
		return []formattedLine{{text: canonicalFenceLine(openLine), protected: true, sourceLine: start + 1}}, start
	}
	body := strings.Join(lines[start+1:closeIdx], "\n")
	chosenMarker := "~~~"
	if !strings.Contains(body, "```") {
		chosenMarker = "```"
	}
	out := make([]formattedLine, 0, closeIdx-start+1)
	out = append(out, formattedLine{text: canonicalFenceLineWithMarker(openLine, chosenMarker), protected: true, sourceLine: start + 1})
	for i := start + 1; i < closeIdx; i++ {
		out = append(out, formattedLine{text: lines[i], protected: true, sourceLine: i + 1})
	}
	out = append(out, formattedLine{text: canonicalFenceLineWithMarker(lines[closeIdx], chosenMarker), protected: true, sourceLine: closeIdx + 1})
	return out, closeIdx
}

func canonicalHeadingLine(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(trimmed, "#") {
		return line
	}
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}
	if i == 0 || i > 6 {
		return line
	}
	if i < len(trimmed) && trimmed[i] != ' ' && trimmed[i] != '\t' {
		return line
	}
	text := strings.TrimSpace(trimmed[i:])
	contentEnd := len(text)
	for contentEnd > 0 && text[contentEnd-1] == '#' {
		contentEnd--
	}
	if contentEnd > 0 && contentEnd < len(text) && (text[contentEnd-1] == ' ' || text[contentEnd-1] == '\t') {
		text = strings.TrimSpace(text[:contentEnd])
	}
	if text == "" {
		return strings.Repeat("#", i)
	}
	return strings.Repeat("#", i) + " " + text
}

func canonicalOrderedListMarker(line string) string {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	j := i
	for j < len(line) && line[j] >= '0' && line[j] <= '9' {
		j++
	}
	if j == i || j >= len(line) || line[j] != ')' {
		return line
	}
	if j+1 < len(line) && line[j+1] == ' ' {
		return line[:j] + "." + line[j+1:]
	}
	return line
}

func canonicalUnorderedListMarker(line string) string {
	i := 0
	for i < len(line) && line[i] == ' ' {
		i++
	}
	if i+1 < len(line) && (line[i] == '*' || line[i] == '+') && line[i+1] == ' ' {
		return line[:i] + "-" + line[i+1:]
	}
	return line
}

func canonicalTaskMarker(line string) string {
	line = strings.Replace(line, "[X]", "[x]", 1)
	line = strings.Replace(line, "[✓]", "[x]", 1)
	return line
}

func rewriteCanonicalBlocks(root *mdpp.Node, lines []formattedLine, src []byte) []formattedLine {
	lines = rewriteSimplePipeTables(root, lines, src)
	lines = rewriteContainerFences(lines, root)
	return lines
}

func rewriteSimplePipeTables(root *mdpp.Node, lines []formattedLine, source []byte) []formattedLine {
	if root == nil {
		return lines
	}
	var tables []*mdpp.Node
	root.Walk(func(n *mdpp.Node) bool {
		if n.Type == mdpp.NodeTable {
			tables = append(tables, n)
		}
		return true
	})
	sort.SliceStable(tables, func(i, j int) bool {
		return tables[i].Range.StartLine < tables[j].Range.StartLine
	})
	for _, table := range tables {
		lines = rewriteSimplePipeTable(lines, table, source)
	}
	return lines
}

func rewriteSimplePipeTable(lines []formattedLine, table *mdpp.Node, source []byte) []formattedLine {
	if table == nil || len(table.Children) == 0 {
		return lines
	}
	for _, row := range table.Children {
		if row == nil || row.Type != mdpp.NodeTableRow {
			return lines
		}
		if row.Range.StartLine == 0 || row.Range.StartLine != row.Range.EndLine {
			return lines
		}
		if len(row.Children) == 0 {
			return lines
		}
	}
	headerLine := table.Children[0].Range.StartLine
	if headerLine == 0 {
		return lines
	}
	headerIdx := sourceLineIndex(lines, headerLine)
	if headerIdx < 0 || lines[headerIdx].protected {
		return lines
	}
	headerText, headerPrefix, ok := canonicalSimplePipeTableRow(table.Children[0], source, lines[headerIdx].text)
	if !ok {
		return lines
	}
	indent := headerPrefix
	delimiterLine := headerLine + 1
	if len(table.Children) > 1 && table.Children[1].Range.StartLine != delimiterLine+1 {
		return lines
	}
	for _, row := range table.Children {
		rowLine := row.Range.StartLine
		rowIdx := sourceLineIndex(lines, rowLine)
		if rowIdx < 0 || lines[rowIdx].protected {
			return lines
		}
		rowText, rowPrefix, ok := canonicalSimplePipeTableRow(row, source, lines[rowIdx].text)
		if !ok || rowPrefix != indent {
			return lines
		}
		lines[rowIdx].text = rowText
	}
	delimIdx := sourceLineIndex(lines, delimiterLine)
	if delimIdx < 0 || lines[delimIdx].protected {
		return lines
	}
	delimiterText, ok := canonicalSimplePipeTableDelimiter(table, indent)
	if !ok {
		return lines
	}
	lines[headerIdx].text = headerText
	lines[delimIdx].text = delimiterText
	return lines
}

func canonicalSimplePipeTableRow(row *mdpp.Node, source []byte, lineText string) (string, string, bool) {
	indent, ok := simplePipeTableIndent(lineText)
	if !ok {
		return "", "", false
	}
	cells := make([]string, 0, len(row.Children))
	prevStart, prevEnd := -1, -1
	for _, cell := range row.Children {
		if cell == nil || cell.Type != mdpp.NodeTableCell || cell.Range.StartLine == 0 || cell.Range.StartLine != cell.Range.EndLine {
			return "", "", false
		}
		// Cells that share one byte range signal a corrupt AST (each
		// "cell" would render as the whole row and multiply the table on
		// every pass). Leave such tables untouched.
		if cell.Range.StartByte == prevStart && cell.Range.EndByte == prevEnd {
			return "", "", false
		}
		prevStart, prevEnd = cell.Range.StartByte, cell.Range.EndByte
		raw := sourceNodeText(source, cell.Range.StartByte, cell.Range.EndByte)
		cells = append(cells, strings.TrimSpace(raw))
	}
	if len(cells) == 0 {
		return "", "", false
	}
	return indent + "| " + strings.Join(cells, " | ") + " |", indent, true
}

func canonicalSimplePipeTableDelimiter(table *mdpp.Node, indent string) (string, bool) {
	if table == nil || len(table.Children) == 0 {
		return "", false
	}
	cols := len(table.Children[0].Children)
	if cols == 0 {
		return "", false
	}
	aligns := strings.Split(table.Attr("align"), ",")
	parts := make([]string, cols)
	for i := 0; i < cols; i++ {
		align := ""
		if i < len(aligns) {
			align = aligns[i]
		}
		switch align {
		case "left":
			parts[i] = ":---"
		case "center":
			parts[i] = ":---:"
		case "right":
			parts[i] = "---:"
		default:
			parts[i] = "---"
		}
	}
	return indent + "|" + strings.Join(parts, "|") + "|", true
}

func simplePipeTableIndent(line string) (string, bool) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) || line[i] != '|' {
		return "", false
	}
	return line[:i], true
}

func sourceNodeText(source []byte, start, end int) string {
	if start < 0 || end < start || start >= len(source) {
		return ""
	}
	if end > len(source) {
		end = len(source)
	}
	return string(source[start:end])
}

func rewriteContainerFences(lines []formattedLine, root *mdpp.Node) []formattedLine {
	if root == nil {
		return lines
	}
	root.Walk(func(n *mdpp.Node) bool {
		if n.Type != mdpp.NodeContainerDirective || n.Range.StartLine == 0 || n.Range.EndLine == 0 {
			return true
		}
		openIdx := sourceLineIndex(lines, n.Range.StartLine)
		closeIdx := sourceLineIndex(lines, n.Range.EndLine)
		if openIdx < 0 || closeIdx < 0 || lines[openIdx].protected || lines[closeIdx].protected {
			return true
		}
		open, ok := canonicalContainerOpenLine(lines[openIdx].text, n)
		if ok {
			lines[openIdx].text = open
		}
		if close, ok := canonicalContainerCloseLine(lines[closeIdx].text, n); ok {
			lines[closeIdx].text = close
		}
		return true
	})
	return lines
}

func canonicalContainerOpenLine(line string, n *mdpp.Node) (string, bool) {
	if n == nil {
		return "", false
	}
	i := 0
	for i < len(line) && line[i] == ':' {
		i++
	}
	if i < 3 {
		return "", false
	}
	prefix := strings.Repeat(":", i)
	name := strings.ToLower(strings.TrimSpace(n.Attr("name")))
	if name == "" {
		return "", false
	}
	if extra := strings.TrimSpace(n.Attr("attrs")); extra != "" {
		return "", false
	}
	var parts []string
	if title := strings.TrimSpace(n.Attr("title")); title != "" {
		parts = append(parts, strconv.Quote(title))
	}
	var attrs []string
	if id := strings.TrimSpace(n.Attr("id")); id != "" {
		attrs = append(attrs, "#"+id)
	}
	if class := strings.TrimSpace(n.Attr("class")); class != "" {
		for _, c := range strings.Fields(class) {
			attrs = append(attrs, "."+c)
		}
	}
	if len(attrs) > 0 {
		parts = append(parts, "{"+strings.Join(attrs, " ")+"}")
	}
	out := prefix + name
	if len(parts) > 0 {
		out += " " + strings.Join(parts, " ")
	}
	return out, true
}

func canonicalContainerCloseLine(line string, n *mdpp.Node) (string, bool) {
	if n == nil {
		return "", false
	}
	i := 0
	for i < len(line) && line[i] == ':' {
		i++
	}
	if i < 3 {
		return "", false
	}
	return strings.Repeat(":", i), true
}

func canonicalContainerCloseLineText(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", false
	}
	i := 0
	for i < len(trimmed) && trimmed[i] == ':' {
		i++
	}
	if i < 3 || i != len(trimmed) {
		return "", false
	}
	return strings.Repeat(":", i), true
}

func rewriteNestedListIndentation(root *mdpp.Node, lines []formattedLine, source []string) []formattedLine {
	if root == nil {
		return lines
	}
	var walk func(n *mdpp.Node, depth int, inItem bool)
	walk = func(n *mdpp.Node, depth int, inItem bool) {
		if n == nil {
			return
		}
		if n.Type == mdpp.NodeList {
			for _, child := range n.Children {
				if child == nil || (child.Type != mdpp.NodeListItem && child.Type != mdpp.NodeTaskListItem) {
					continue
				}
				rewriteListItemIndentation(lines, source, child, depth)
				for _, grand := range child.Children {
					if grand == nil {
						continue
					}
					if grand.Type == mdpp.NodeList {
						walk(grand, depth+1, true)
					} else {
						walk(grand, depth, true)
					}
				}
			}
			return
		}
		for _, child := range n.Children {
			if child == nil {
				continue
			}
			if child.Type == mdpp.NodeList {
				if inItem {
					walk(child, depth+1, true)
				} else {
					walk(child, depth, false)
				}
				continue
			}
			walk(child, depth, inItem)
		}
	}
	walk(root, 0, false)
	return lines
}

func rewriteListItemIndentation(lines []formattedLine, source []string, item *mdpp.Node, depth int) {
	if item == nil || item.Range.StartLine == 0 {
		return
	}
	idx := sourceLineIndex(lines, item.Range.StartLine)
	if idx < 0 || lines[idx].protected {
		return
	}
	line := lines[idx].text
	if !linePrefixIsPlainWhitespace(line) {
		return
	}
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return
	}
	lines[idx].text = strings.Repeat("  ", depth) + trimmed
}

func linePrefixIsPlainWhitespace(line string) bool {
	for i := 0; i < len(line); i++ {
		if line[i] == ' ' || line[i] == '\t' {
			continue
		}
		return line[i] != '>'
	}
	return true
}

func normalizeBlankLineEntries(lines []formattedLine) []formattedLine {
	out := make([]formattedLine, 0, len(lines))
	blankRun := 0
	for _, line := range lines {
		if line.protected {
			blankRun = 0
			out = append(out, line)
			continue
		}
		if strings.TrimSpace(line.text) == "" {
			blankRun++
			if len(out) == 0 || blankRun > 1 {
				continue
			}
			out = append(out, formattedLine{})
			continue
		}
		blankRun = 0
		out = append(out, line)
	}
	for len(out) > 0 && !out[len(out)-1].protected && strings.TrimSpace(out[len(out)-1].text) == "" {
		out = out[:len(out)-1]
	}
	return out
}

func rewriteOrderedListNumbers(root *mdpp.Node, lines []formattedLine) []formattedLine {
	if root == nil {
		return lines
	}
	root.Walk(func(n *mdpp.Node) bool {
		if n.Type != mdpp.NodeList || n.Attrs == nil || n.Attrs["ordered"] != "true" {
			return true
		}
		start := 1
		if raw := n.Attrs["start"]; raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				start = parsed
			}
		}
		next := start
		for _, child := range n.Children {
			if child == nil || (child.Type != mdpp.NodeListItem && child.Type != mdpp.NodeTaskListItem) {
				continue
			}
			lineIdx := sourceLineIndex(lines, child.Range.StartLine)
			if lineIdx >= 0 && !lines[lineIdx].protected {
				lines[lineIdx].text = rewriteOrderedListLine(lines[lineIdx].text, next)
			}
			next++
		}
		return true
	})
	return lines
}

func rewriteOrderedListLine(line string, number int) string {
	match := orderedListLineRe.FindStringSubmatch(line)
	if match == nil {
		return line
	}
	return match[1] + strconv.Itoa(number) + match[3] + match[4] + match[5]
}

func sourceLineIndex(lines []formattedLine, sourceLine int) int {
	if sourceLine <= 0 {
		return -1
	}
	for i := range lines {
		if lines[i].sourceLine == sourceLine {
			return i
		}
	}
	return -1
}

func unwrapSimpleParagraphs(root *mdpp.Node, lines []formattedLine, source []string, src []byte, protected []bool) []formattedLine {
	if root == nil {
		return lines
	}
	listParagraphs := make(map[*mdpp.Node]struct{})
	var collectListParagraphs func(*mdpp.Node, bool)
	collectListParagraphs = func(n *mdpp.Node, inListItem bool) {
		if n == nil {
			return
		}
		inListItem = inListItem || n.Type == mdpp.NodeListItem || n.Type == mdpp.NodeTaskListItem
		if inListItem && n.Type == mdpp.NodeParagraph {
			listParagraphs[n] = struct{}{}
		}
		for _, child := range n.Children {
			collectListParagraphs(child, inListItem)
		}
	}
	collectListParagraphs(root, false)
	type span struct {
		startLine int
		endLine   int
		text      string
	}
	var spans []span
	root.Walk(func(n *mdpp.Node) bool {
		if n.Type != mdpp.NodeParagraph || n.Range.StartLine == 0 || n.Range.EndLine <= n.Range.StartLine {
			return true
		}
		for line := n.Range.StartLine; line <= n.Range.EndLine && line <= len(protected); line++ {
			if protected[line-1] {
				return true
			}
		}
		if !canUnwrapParagraph(n) {
			return true
		}
		text := unwrapParagraphText(n, src)
		if text == "" {
			return true
		}
		prefix := ""
		if n.Range.StartCol > 1 && n.Range.StartLine-1 < len(source) {
			line := source[n.Range.StartLine-1]
			if n.Range.StartCol-1 <= len(line) {
				prefix = line[:n.Range.StartCol-1]
			}
		}
		// A segmented fast parse can attach a lazy list continuation as a
		// separate paragraph whose first source line is indented. Unwrapping
		// that paragraph currently trims the indentation, which leaves the
		// preceding list paragraph and this one adjacent in rendered text
		// (for example, "Metal" + "variants"). Keep one separator when the
		// source proves this is a single physical soft-break boundary. Do not
		// infer a separator from AST adjacency alone: same-line fragments and
		// hard-break syntax must retain their existing semantics.
		_, isListParagraph := listParagraphs[n]
		if isListParagraph {
			return true
		}
		if !isListParagraph && n.Range.StartLine > 0 && n.Range.StartLine <= len(source) {
			firstLine := source[n.Range.StartLine-1]
			if strings.HasPrefix(firstLine, " ") || strings.HasPrefix(firstLine, "\t") {
				return true
			}
		}
		if prefix == "" && isListParagraph && paragraphStartsWithImplicitSoftBreak(n, src) {
			prefix = " "
		}
		// Neither Range.EndLine nor Range.EndByte is reliable across
		// paragraph shapes: blank-line-terminated paragraphs report an
		// exclusive EndLine while lazy continuations report an inclusive one
		// (trusting EndLine-1 then leaves the true last line behind, and
		// every format pass appends another copy of it), and EndByte can
		// overshoot into nested list children. For the paragraphs this pass
		// unwraps — text and soft-break children only, guaranteed by
		// canUnwrapParagraph — each soft break followed by more text starts
		// one more physical line. A trailing soft break (e.g. before a
		// nested list) does not.
		endLine := n.Range.StartLine
		lastText := -1
		for i, child := range n.Children {
			if child.Type == mdpp.NodeText && strings.TrimSpace(child.Literal) != "" {
				lastText = i
			}
		}
		for i, child := range n.Children {
			if i < lastText && child.Type == mdpp.NodeSoftBreak {
				endLine++
			}
		}
		spans = append(spans, span{
			startLine: n.Range.StartLine,
			endLine:   endLine,
			text:      prefix + text,
		})
		return true
	})
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].startLine > spans[j].startLine })
	for _, sp := range spans {
		lines = replaceSourceLineRange(lines, sp.startLine, sp.endLine, sp.text)
	}
	return lines
}

func replaceSourceLineRange(lines []formattedLine, startLine int, endLine int, replacement string) []formattedLine {
	if startLine <= 0 || endLine < startLine {
		return lines
	}
	startIdx, endIdx := -1, -1
	for i, line := range lines {
		if line.sourceLine < startLine || line.sourceLine > endLine {
			continue
		}
		if startIdx < 0 {
			startIdx = i
		}
		endIdx = i
	}
	if startIdx < 0 {
		return lines
	}
	repl := []formattedLine{{text: replacement, sourceLine: startLine}}
	lines = append(lines[:startIdx], append(repl, lines[endIdx+1:]...)...)
	return lines
}

func canUnwrapParagraph(n *mdpp.Node) bool {
	if n == nil || len(n.Children) == 0 {
		return false
	}
	hasSoftBreak := false
	for _, child := range n.Children {
		switch child.Type {
		case mdpp.NodeText:
			continue
		case mdpp.NodeSoftBreak:
			hasSoftBreak = true
		default:
			return false
		}
	}
	return hasSoftBreak
}

func unwrapParagraphText(n *mdpp.Node, source []byte) string {
	var parts []string
	var previousText *mdpp.Node
	for _, child := range n.Children {
		switch child.Type {
		case mdpp.NodeText:
			if previousText != nil && textChildrenCrossSoftBreak(previousText, child, source) {
				parts = append(parts, " ")
			}
			parts = append(parts, child.Literal)
			previousText = child
		case mdpp.NodeSoftBreak:
			parts = append(parts, " ")
			previousText = nil
		default:
			previousText = nil
		}
	}
	text := strings.Join(parts, "")
	text = strings.TrimSpace(text)
	text = strings.Join(strings.Fields(text), " ")
	return text
}

// paragraphStartsWithImplicitSoftBreak reports the segmented-parser shape in
// which an indented continuation paragraph starts on the physical line after
// non-blank prose. The source check deliberately rejects blank-line gaps and
// Markdown hard-break markers; only a single ordinary line ending is treated
// as a word-separating soft break.
func paragraphStartsWithImplicitSoftBreak(n *mdpp.Node, source []byte) bool {
	if n == nil || n.Range.StartLine <= 1 || n.Range.StartByte <= 0 || n.Range.StartByte > len(source) {
		return false
	}
	lineStart := n.Range.StartByte
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}
	if lineStart >= len(source) || (source[lineStart] != ' ' && source[lineStart] != '\t') {
		return false
	}
	prevEnd := lineStart - 1
	prevStart := prevEnd
	for prevStart > 0 && source[prevStart-1] != '\n' {
		prevStart--
	}
	prevLine := string(source[prevStart:prevEnd])
	if strings.TrimSpace(prevLine) == "" {
		return false
	}
	trimmedPrev := strings.TrimRight(prevLine, " \t")
	if strings.HasSuffix(trimmedPrev, "\\") || len(prevLine)-len(trimmedPrev) >= 2 {
		return false
	}
	return true
}

// textChildrenCrossSoftBreak detects a missing explicit NodeSoftBreak from
// source ranges. It only joins text children separated by exactly one ordinary
// physical newline; same-line fragments, blank-line paragraph boundaries, and
// hard-break syntax are intentionally excluded.
func textChildrenCrossSoftBreak(previous, current *mdpp.Node, source []byte) bool {
	if previous == nil || current == nil || previous.Range.StartLine == 0 || current.Range.StartLine == 0 ||
		previous.Range.StartByte < 0 || previous.Range.EndByte < 0 || current.Range.StartByte < 0 || current.Range.EndByte < 0 ||
		previous.Range.StartByte > len(source) || previous.Range.EndByte > len(source) ||
		current.Range.StartByte > len(source) || current.Range.EndByte > len(source) ||
		previous.Range.EndByte < previous.Range.StartByte || current.Range.EndByte < current.Range.StartByte ||
		current.Range.StartByte < previous.Range.EndByte {
		return false
	}
	gap := source[previous.Range.EndByte:current.Range.StartByte]
	if bytes.Count(gap, []byte{'\n'}) != 1 || bytes.Contains(gap, []byte("  \n")) || bytes.Contains(gap, []byte("\\\n")) {
		return false
	}
	return true
}

func joinFormattedLines(lines []formattedLine) string {
	parts := make([]string, len(lines))
	for i, line := range lines {
		parts[i] = line.text
	}
	return strings.Join(parts, "\n") + "\n"
}

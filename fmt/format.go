// Package fmt provides canonical formatting for Markdown++ source.
package fmt

import (
	"bufio"
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/odvcencio/mdpp"
)

var (
	tocDirectiveLineRe   = regexp.MustCompile(`^\s*\[\[\s*([Tt][Oo][Cc])\s*\]\]\s*$`)
	embedDirectiveLineRe = regexp.MustCompile(`^\s*\[\[\s*([Ee][Mm][Bb][Ee][Dd])\s*:\s*(.*?)\s*\]\]\s*$`)
	setextH1Re           = regexp.MustCompile(`^\s*=+\s*$`)
	setextH2Re           = regexp.MustCompile(`^\s*-+\s*$`)
	footnoteDefLineRe    = regexp.MustCompile(`^ {0,3}\[\^([A-Za-z0-9_-]+)\]:[ \t]*(.+)$`)
	refDefLineRe         = regexp.MustCompile(`^ {0,3}\[([^\]\^][^\]]*)\]:[ \t]*(\S.*)$`)
	orderedListLineRe    = regexp.MustCompile(`^([ \t]*)([0-9]+)([.)])([ \t]+)(.*)$`)
	strongUnderscoreRe   = regexp.MustCompile(`(^|[^[:alnum:]_])__([^_\n][^_\n]*?)__([^[:alnum:]_]|$)`)
	emUnderscoreRe       = regexp.MustCompile(`(^|[^[:alnum:]_])_([^_\n][^_\n]*?)_([^[:alnum:]_]|$)`)
)

type formattedLine struct {
	text       string
	protected  bool
	sourceLine int
}

type collectedDef struct {
	label string
	line  string
}

// Format reformats src into canonical Markdown++ form.
func Format(src []byte) ([]byte, error) {
	doc, err := mdpp.Parse(src)
	if err != nil {
		return nil, err
	}
	src = bytes.TrimPrefix(normalizeLineEndings(src), []byte{0xEF, 0xBB, 0xBF})
	lines := scanLines(src)
	out := make([]formattedLine, 0, len(lines))
	var refs, footnotes []collectedDef
	inFence := false
	fenceMarker := byte(0)
	fenceMarkerLen := 0
	inFrontmatter := false
	inMathBlock := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if i == 0 && trimmed == "---" {
			inFrontmatter = true
			out = append(out, formattedLine{text: "---", protected: true, sourceLine: i + 1})
			continue
		}
		if inFrontmatter {
			out = append(out, formattedLine{text: line, protected: true, sourceLine: i + 1})
			if trimmed == "---" {
				inFrontmatter = false
				if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
					out = append(out, formattedLine{})
				}
			}
			continue
		}

		if !inFence && strings.HasPrefix(trimmed, "~~~") {
			block, nextIndex := canonicalTildeFenceBlock(lines, i)
			out = append(out, block...)
			i = nextIndex
			continue
		}

		if !inFence && isFenceLine(trimmed) {
			inFence = true
			fenceMarker = trimmed[0]
			fenceMarkerLen = fenceRunLength(trimmed)
			out = append(out, formattedLine{text: canonicalFenceLine(line), protected: true, sourceLine: i + 1})
			continue
		}
		if inFence {
			out = append(out, formattedLine{text: line, protected: true, sourceLine: i + 1})
			if isFenceCloseLine(trimmed, fenceMarker, fenceMarkerLen) {
				inFence = false
				fenceMarker = 0
				fenceMarkerLen = 0
			}
			continue
		}

		if isDisplayMathDelimiter(trimmed) {
			out = append(out, formattedLine{text: strings.TrimRight(line, " \t"), protected: true, sourceLine: i + 1})
			inMathBlock = !inMathBlock
			continue
		}
		if inMathBlock || isHTMLBlockLine(trimmed) {
			out = append(out, formattedLine{text: line, protected: true, sourceLine: i + 1})
			continue
		}

		if i+1 < len(lines) && strings.TrimSpace(line) != "" {
			next := strings.TrimSpace(lines[i+1])
			switch {
			case setextH1Re.MatchString(next):
				out = append(out, formattedLine{text: "# " + strings.TrimSpace(line)})
				i++
				continue
			case setextH2Re.MatchString(next):
				out = append(out, formattedLine{text: "## " + strings.TrimSpace(line)})
				i++
				continue
			}
		}

		hadTrailingWhitespace := len(line) != len(strings.TrimRight(line, " \t"))
		line = strings.TrimRight(line, " \t")
		if match := tocDirectiveLineRe.FindStringSubmatch(line); match != nil {
			line = "[[toc]]"
		} else if match := embedDirectiveLineRe.FindStringSubmatch(line); match != nil {
			line = "[[embed:" + match[2] + "]]"
		} else if match := refDefLineRe.FindStringSubmatch(line); match != nil {
			refs = append(refs, collectedDef{label: normalizeSortLabel(match[1]), line: "[" + strings.TrimSpace(match[1]) + "]: " + strings.TrimSpace(match[2])})
			continue
		} else if match := footnoteDefLineRe.FindStringSubmatch(line); match != nil {
			footnotes = append(footnotes, collectedDef{label: strings.ToLower(match[1]), line: "[^" + match[1] + "]: " + strings.TrimSpace(match[2])})
			continue
		} else {
			line = canonicalHeadingLine(line)
			line = canonicalUnorderedListMarker(line)
			line = canonicalOrderedListMarker(line)
			line = canonicalTaskMarker(line)
			line = canonicalEmphasis(line)
			line = canonicalHardBreakLine(line, hadTrailingWhitespace, i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "")
		}
		out = append(out, formattedLine{text: line, sourceLine: i + 1})
	}

	out = rewriteOrderedListNumbers(doc.Root, out)
	out = unwrapSimpleParagraphs(doc.Root, out, lines)
	out = normalizeBlankLineEntries(out)
	out = appendDefinitions(out, refs, footnotes)
	return []byte(joinFormattedLines(out)), nil
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
		parts[0] = strings.ToLower(parts[0])
	}
	return indent + marker + " " + strings.Join(parts, " ")
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
	text := strings.TrimSpace(trimmed[i:])
	text = strings.TrimRight(text, "#")
	text = strings.TrimSpace(text)
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

func canonicalHardBreakLine(line string, hadTrailingWhitespace bool, nextNonBlank bool) string {
	if !hadTrailingWhitespace || !nextNonBlank {
		return line
	}
	if strings.TrimSpace(line) == "" {
		return line
	}
	if strings.HasSuffix(line, "\\") {
		return line
	}
	return line + "\\"
}

func canonicalEmphasis(line string) string {
	line = strongUnderscoreRe.ReplaceAllString(line, "$1**$2**$3")
	line = emUnderscoreRe.ReplaceAllString(line, "$1*$2*$3")
	return line
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
			if lineIdx >= 0 {
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
	return match[1] + strconv.Itoa(number) + "." + match[4] + match[5]
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

func unwrapSimpleParagraphs(root *mdpp.Node, lines []formattedLine, source []string) []formattedLine {
	if root == nil {
		return lines
	}
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
		if !canUnwrapParagraph(n) {
			return true
		}
		text := unwrapParagraphText(n)
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
		spans = append(spans, span{
			startLine: n.Range.StartLine,
			endLine:   n.Range.EndLine - 1,
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

func unwrapParagraphText(n *mdpp.Node) string {
	var parts []string
	for _, child := range n.Children {
		switch child.Type {
		case mdpp.NodeText:
			parts = append(parts, child.Literal)
		case mdpp.NodeSoftBreak:
			parts = append(parts, " ")
		}
	}
	text := strings.Join(parts, "")
	text = strings.TrimSpace(text)
	text = strings.Join(strings.Fields(text), " ")
	return text
}

func appendDefinitions(lines []formattedLine, refs []collectedDef, footnotes []collectedDef) []formattedLine {
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].label < refs[j].label })
	sort.SliceStable(footnotes, func(i, j int) bool { return footnotes[i].label < footnotes[j].label })
	if len(refs) > 0 {
		lines = appendDefinitionBlock(lines, refs)
	}
	if len(footnotes) > 0 {
		lines = appendDefinitionBlock(lines, footnotes)
	}
	return lines
}

func appendDefinitionBlock(lines []formattedLine, defs []collectedDef) []formattedLine {
	for len(lines) > 0 && !lines[len(lines)-1].protected && strings.TrimSpace(lines[len(lines)-1].text) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 0 {
		lines = append(lines, formattedLine{})
	}
	for _, def := range defs {
		lines = append(lines, formattedLine{text: def.line})
	}
	return lines
}

func normalizeSortLabel(label string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.Trim(label, "[]"))), " ")
}

func joinFormattedLines(lines []formattedLine) string {
	parts := make([]string, len(lines))
	for i, line := range lines {
		parts[i] = line.text
	}
	return strings.Join(parts, "\n") + "\n"
}

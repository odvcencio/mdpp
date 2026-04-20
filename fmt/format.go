// Package fmt provides canonical formatting for Markdown++ source.
package fmt

import (
	"bufio"
	"bytes"
	"regexp"
	"sort"
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
	strongUnderscoreRe   = regexp.MustCompile(`(^|[^[:alnum:]_])__([^_\n][^_\n]*?)__([^[:alnum:]_]|$)`)
	emUnderscoreRe       = regexp.MustCompile(`(^|[^[:alnum:]_])_([^_\n][^_\n]*?)_([^[:alnum:]_]|$)`)
)

type formattedLine struct {
	text      string
	protected bool
}

type collectedDef struct {
	label string
	line  string
}

// Format reformats src into canonical Markdown++ form.
func Format(src []byte) ([]byte, error) {
	_, err := mdpp.Parse(src)
	if err != nil {
		return nil, err
	}
	src = bytes.TrimPrefix(normalizeLineEndings(src), []byte{0xEF, 0xBB, 0xBF})
	lines := scanLines(src)
	out := make([]formattedLine, 0, len(lines))
	var refs, footnotes []collectedDef
	inFence := false
	inFrontmatter := false
	inMathBlock := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if i == 0 && trimmed == "---" {
			inFrontmatter = true
			out = append(out, formattedLine{text: "---", protected: true})
			continue
		}
		if inFrontmatter {
			out = append(out, formattedLine{text: line, protected: true})
			if trimmed == "---" {
				inFrontmatter = false
				if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
					out = append(out, formattedLine{})
				}
			}
			continue
		}

		if isFenceLine(trimmed) {
			out = append(out, formattedLine{text: canonicalFenceLine(line), protected: true})
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, formattedLine{text: line, protected: true})
			continue
		}

		if isDisplayMathDelimiter(trimmed) {
			out = append(out, formattedLine{text: strings.TrimRight(line, " \t"), protected: true})
			inMathBlock = !inMathBlock
			continue
		}
		if inMathBlock || isHTMLBlockLine(trimmed) {
			out = append(out, formattedLine{text: line, protected: true})
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
		}
		out = append(out, formattedLine{text: line})
	}

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

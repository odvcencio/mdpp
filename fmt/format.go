// Package fmt provides canonical formatting for Markdown++ source.
package fmt

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"github.com/odvcencio/mdpp"
)

var (
	tocDirectiveLineRe   = regexp.MustCompile(`^\s*\[\[\s*([Tt][Oo][Cc])\s*\]\]\s*$`)
	embedDirectiveLineRe = regexp.MustCompile(`^\s*\[\[\s*([Ee][Mm][Bb][Ee][Dd])\s*:\s*(.*?)\s*\]\]\s*$`)
	setextH1Re           = regexp.MustCompile(`^\s*=+\s*$`)
	setextH2Re           = regexp.MustCompile(`^\s*-+\s*$`)
)

// Format reformats src into canonical Markdown++ form.
func Format(src []byte) ([]byte, error) {
	_, err := mdpp.Parse(src)
	if err != nil {
		return nil, err
	}
	src = bytes.TrimPrefix(normalizeLineEndings(src), []byte{0xEF, 0xBB, 0xBF})
	lines := scanLines(src)
	out := make([]string, 0, len(lines))
	inFence := false
	inFrontmatter := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if i == 0 && trimmed == "---" {
			inFrontmatter = true
			out = append(out, "---")
			continue
		}
		if inFrontmatter {
			out = append(out, strings.TrimRight(line, " \t"))
			if trimmed == "---" {
				inFrontmatter = false
				if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
					out = append(out, "")
				}
			}
			continue
		}

		if isFenceLine(trimmed) {
			out = append(out, canonicalFenceLine(line))
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}

		if i+1 < len(lines) && strings.TrimSpace(line) != "" {
			next := strings.TrimSpace(lines[i+1])
			switch {
			case setextH1Re.MatchString(next):
				out = append(out, "# "+strings.TrimSpace(line))
				i++
				continue
			case setextH2Re.MatchString(next):
				out = append(out, "## "+strings.TrimSpace(line))
				i++
				continue
			}
		}

		line = strings.TrimRight(line, " \t")
		if match := tocDirectiveLineRe.FindStringSubmatch(line); match != nil {
			line = "[[toc]]"
		} else if match := embedDirectiveLineRe.FindStringSubmatch(line); match != nil {
			line = "[[embed:" + match[2] + "]]"
		} else {
			line = canonicalHeadingLine(line)
			line = canonicalOrderedListMarker(line)
		}
		out = append(out, line)
	}

	out = normalizeBlankLines(out)
	return []byte(strings.Join(out, "\n") + "\n"), nil
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

func normalizeBlankLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	blankRun := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blankRun++
			if len(out) == 0 || blankRun > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blankRun = 0
		out = append(out, line)
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

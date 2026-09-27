package mdpp

import (
	"bytes"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Heading represents a heading found in the document.
type Heading struct {
	Level int
	Text  string
	ID    string
}

// WordCount returns the number of words in the document's prose content,
// excluding code and diagram blocks.
func (d *Document) WordCount() int {
	if d.Root == nil {
		return 0
	}
	return countWords(d.Root)
}

// countWords recursively counts words in text nodes, skipping code and diagram blocks.
func countWords(n *Node) int {
	if n == nil {
		return 0
	}
	// Skip code and diagram blocks entirely.
	if n.Type == NodeCodeBlock || n.Type == NodeDiagram {
		return 0
	}
	// Count words in text node literals.
	if n.Type == NodeText {
		return len(strings.Fields(n.Literal))
	}
	count := 0
	for _, child := range n.Children {
		count += countWords(child)
	}
	return count
}

// ReadingTime estimates how long it takes to read the document at 200 words
// per minute. Returns a minimum of 1 minute if the document has any words.
func (d *Document) ReadingTime() time.Duration {
	wc := d.WordCount()
	if wc == 0 {
		return 0
	}
	minutes := wc / 200
	if minutes < 1 {
		minutes = 1
	}
	return time.Duration(minutes) * time.Minute
}

// Headings returns all headings in document order with their level, text,
// and a slugified ID.
func (d *Document) Headings() []Heading {
	if d.Root == nil {
		return nil
	}
	var headings []Heading
	collectHeadings(d.Root, &headings)
	return headings
}

// collectHeadings walks the AST and appends headings to the slice.
func collectHeadings(n *Node, out *[]Heading) {
	if n == nil {
		return
	}
	if n.Type == NodeHeading {
		level := 1
		if s, ok := n.Attrs["level"]; ok {
			if v, err := strconv.Atoi(s); err == nil {
				level = v
			}
		}
		text := collectNodeText(n)
		*out = append(*out, Heading{
			Level: level,
			Text:  text,
			ID:    slugify(text),
		})
	}
	for _, child := range n.Children {
		collectHeadings(child, out)
	}
}

// TableOfContents returns a table of contents derived from all headings
// in the document.
func (d *Document) TableOfContents() []TOCEntry {
	headings := d.Headings()
	if len(headings) == 0 {
		return nil
	}
	entries := make([]TOCEntry, len(headings))
	for i, h := range headings {
		entries[i] = TOCEntry{
			Level: h.Level,
			ID:    h.ID,
			Text:  h.Text,
		}
	}
	return entries
}

// extractFrontmatter parses YAML frontmatter from the document source.
// Frontmatter must be delimited by --- on its own line at the start
// of the document.
func (d *Document) extractFrontmatter() {
	if d.Source == nil {
		return
	}
	var data map[string]any
	yamlBlock, end, ok := parseFrontmatter(d.Source, &data)
	if !ok {
		return
	}
	d.frontmatterData = data
	d.attachFrontmatterNode(yamlBlock, end)
}

// parseFrontmatter returns the YAML bytes and complete source range for a
// valid leading frontmatter block. The parser and source transforms share
// this detector so an indented YAML scalar line cannot become a delimiter.
func parseFrontmatter(source []byte, data *map[string]any) ([]byte, int, bool) {
	if !bytes.HasPrefix(source, []byte("---\n")) {
		return nil, 0, false
	}
	rest := source[4:]
	idx := bytes.Index(rest, []byte("\n---\n"))
	closingLen := len("\n---\n")
	if idx < 0 {
		if bytes.HasSuffix(rest, []byte("\n---")) {
			idx = len(rest) - len("\n---")
			closingLen = len("\n---")
		} else {
			return nil, 0, false
		}
	}
	yamlBlock := rest[:idx]
	var parsed map[string]any
	if err := yaml.Unmarshal(yamlBlock, &parsed); err != nil {
		return nil, 0, false
	}
	if data != nil {
		*data = parsed
	}
	return yamlBlock, 4 + idx + closingLen, true
}

func (d *Document) attachFrontmatterNode(yamlBlock []byte, end int) {
	if d.Root == nil {
		return
	}
	fm := &Node{
		Type:    NodeFrontmatter,
		Literal: string(yamlBlock),
		Range:   sourceRange(d.Source, 0, end),
	}
	children := make([]*Node, 0, len(d.Root.Children)+1)
	children = append(children, fm)
	for _, child := range d.Root.Children {
		if child == nil || isFrontmatterParseArtifact(child, end) {
			continue
		}
		children = append(children, child)
	}
	d.Root.Children = children
}

func isFrontmatterParseArtifact(n *Node, end int) bool {
	if n.Range.EndByte > 0 && n.Range.EndByte <= end {
		return true
	}
	if n.Range.StartByte < end && strings.HasPrefix(strings.TrimSpace(collectNodeText(n)), "---") {
		return true
	}
	return false
}

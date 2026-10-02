package mdpp

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// StorySymbol identifies a declaration or reference by exact UTF-8 byte range.
// Cue scopes are slides; actor scopes are individual Sirena fences. Names in
// unrelated diagrams never participate in each other's rename operations.
type StorySymbol struct {
	Kind        string
	Name        string
	Scope       string
	Declaration bool
	Range       Range
}
type StoryIndex struct {
	Symbols     []StorySymbol
	Diagnostics []Diagnostic
}

var storyName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// IndexStory indexes parsed slide metadata, motion dependencies, local story
// links and Sirena actor relationships without changing the document's AST.
// It deliberately uses parsed fences/directives, not searches through prose.
// Split documents are indexed from a temporary parse of their source, retaining
// the ranges of metadata fences that slide splitting removes from the AST.
func IndexStory(doc *Document) StoryIndex {
	var result StoryIndex
	if doc == nil || doc.Root == nil {
		return result
	}
	if doc.SourceHadCarriageReturns() {
		return result
	} // normalized offsets cannot edit original CRLF
	if documentHasSlides(doc.Root) {
		// SplitSlides discards metadata nodes and separator ranges. Use the
		// parser's normal bounded path to recover both without altering doc.
		parsed, err := Parse(doc.Source)
		if err != nil || parsed == nil || parsed.Root == nil || parsed.SourceHadCarriageReturns() {
			return result
		}
		doc = parsed
	}
	add := func(kind, name, scope string, decl bool, start, end int) {
		result.Symbols = append(result.Symbols, StorySymbol{kind, name, scope, decl, sourceRange(doc.Source, start, end)})
	}
	scope := "slide:0"
	slide := 0
	var links []*Node
	var visit func(*Node)
	visit = func(n *Node) {
		if n == nil {
			return
		}
		if n.Type == NodeFrontmatter {
			indexStoryMetadata(doc, n, scope, &result)
			return
		}
		if n.Type == NodeCodeBlock && (n.Attr("language") == "yaml" || n.Attr("language") == "yml") {
			indexStoryMetadata(doc, n, scope, &result)
			return
		}
		if n.Type == NodeDiagram && n.Attr("syntax") == "sirena" {
			indexStoryActors(doc, n, &result)
			return
		}
		if n.Type == NodeContainerDirective && n.Attr("name") == "motion" {
			attrs, _ := directiveSourceAttributes(doc, n.Range.StartByte)
			for _, a := range attrs {
				if a.key == "cue" || a.key == "after" {
					if storyName.MatchString(a.value) {
						decl := a.key == "cue"
						if decl {
							for _, s := range result.Symbols {
								if s.Kind == "cue" && s.Scope == scope && s.Name == a.value && s.Declaration {
									decl = false
									break
								}
							}
						}
						add("cue", a.value, scope, decl, a.start, a.end)
					}
				}
			}
		}
		if n.Type == NodeLink {
			links = append(links, n)
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	for _, n := range doc.Root.Children {
		if n.Type == NodeThematicBreak {
			slide++
			scope = fmt.Sprintf("slide:%d", slide)
			continue
		}
		visit(n)
	}
	// Resolve links only when an authored slide ID exists, leaving ordinary
	// Markdown heading anchors to the normal navigation implementation.
	slides := map[string]string{}
	for _, s := range result.Symbols {
		if s.Kind == "slide" && s.Declaration {
			slides[s.Name] = s.Scope
		}
	}
	for _, n := range links {
		href := n.Attr("href")
		if !strings.HasPrefix(href, "#") {
			continue
		}
		id, cue, _ := strings.Cut(strings.TrimPrefix(href, "#"), "/")
		target, ok := slides[id]
		if !ok {
			continue
		}
		start, end := n.Range.StartByte, n.Range.EndByte
		if start < 0 || end > len(doc.Source) {
			continue
		}
		rel := bytes.LastIndex(doc.Source[start:end], []byte(href))
		if rel < 0 {
			continue
		}
		a := start + rel + 1
		add("slide", id, target, false, a, a+len(id))
		if cue != "" {
			add("cue", cue, target, false, a+len(id)+1, a+len(id)+1+len(cue))
		}
	}
	declarations := map[string][]StorySymbol{}
	for _, s := range result.Symbols {
		if s.Declaration {
			key := s.Kind + ":" + s.Scope + ":" + s.Name
			declarations[key] = append(declarations[key], s)
		}
	}
	for _, s := range result.Symbols {
		key := s.Kind + ":" + s.Scope + ":" + s.Name
		if !s.Declaration && len(declarations[key]) == 0 {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "STORY-UNRESOLVED", Severity: SeverityWarning, Message: fmt.Sprintf("unknown %s %q", s.Kind, s.Name), Range: s.Range})
		}
		if s.Declaration && len(declarations[key]) > 1 {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "STORY-DUPLICATE", Severity: SeverityWarning, Message: fmt.Sprintf("ambiguous %s %q", s.Kind, s.Name), Range: s.Range})
		}
	}
	// Slide IDs address the whole deck, rather than just their local slide.
	ids := map[string]int{}
	for _, s := range result.Symbols {
		if s.Kind == "slide" && s.Declaration {
			ids[s.Name]++
		}
	}
	for _, s := range result.Symbols {
		if s.Kind == "slide" && s.Declaration && ids[s.Name] > 1 {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "STORY-DUPLICATE-ID", Severity: SeverityWarning, Message: "duplicate slide ID " + s.Name, Range: s.Range})
		}
	}
	sort.Slice(result.Symbols, func(i, j int) bool { return result.Symbols[i].Range.StartByte < result.Symbols[j].Range.StartByte })
	return result
}

// YAML nodes distinguish comments, strings and collections. Only complete
// names are indexed; their edit ranges retain the original scalar spelling.
func indexStoryMetadata(doc *Document, n *Node, scope string, result *StoryIndex) {
	start, end := n.Range.StartByte, n.Range.EndByte
	if start < 0 || end < start || end > len(doc.Source) || n.Literal == "" {
		return
	}
	body := []byte(n.Literal)
	rel := bytes.Index(doc.Source[start:end], body)
	if rel < 0 {
		return // transformed literals have no reliable source edit ranges
	}
	start += rel
	var root yaml.Node
	if yaml.Unmarshal(body, &root) != nil || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return
	}
	var values map[string]any
	if root.Decode(&values) != nil {
		return // invalid mappings, including duplicate keys, have no declarations
	}
	add := func(kind, name string, a, b int) {
		result.Symbols = append(result.Symbols, StorySymbol{kind, name, scope, true, sourceRange(doc.Source, start+a, start+b)})
	}
	var cues func(*yaml.Node)
	cues = func(value *yaml.Node) {
		if value.Kind == yaml.SequenceNode {
			for _, item := range value.Content {
				if item.Kind == yaml.ScalarNode {
					cues(item)
				}
			}
			return
		}
		a, b, quote, ok := storyScalarSource(body, value)
		if !ok {
			return
		}
		parts, rawParts := strings.Split(value.Value, ","), bytes.Split(body[a:b], []byte(","))
		if len(parts) != len(rawParts) {
			return // escaped separators do not provide contiguous cue ranges
		}
		for i, raw := range rawParts {
			name, trimmed := strings.TrimSpace(parts[i]), bytes.TrimSpace(raw)
			if storyName.MatchString(name) && storyScalarText(trimmed, quote) == name {
				offset := a + bytes.Index(raw, trimmed)
				add("cue", name, offset, offset+len(trimmed))
			}
			a += len(raw) + 1
		}
	}
	mapping := root.Content[0].Content
	for i := 0; i+1 < len(mapping); i += 2 {
		key, value := mapping[i], mapping[i+1]
		switch key.Value {
		case "id":
			if a, b, _, ok := storyScalarSource(body, value); ok && storyName.MatchString(value.Value) {
				add("slide", value.Value, a, b)
			}
		case "cues":
			cues(value)
		}
	}
}

// yaml.Node columns count runes. Convert them to UTF-8 byte offsets before
// finding the scalar token; quotes and inline comments stay outside edits.
func storyScalarSource(body []byte, n *yaml.Node) (int, int, byte, bool) {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return 0, 0, 0, false
	}
	a := 0
	for line := 1; line < n.Line; line++ {
		next := bytes.IndexByte(body[a:], '\n')
		if next < 0 {
			return 0, 0, 0, false
		}
		a += next + 1
	}
	for column := 1; column < n.Column && a < len(body); column++ {
		_, size := utf8.DecodeRune(body[a:])
		a += size
	}
	if a >= len(body) {
		return 0, 0, 0, false
	}
	quote := body[a]
	if quote != '\'' && quote != '"' {
		return a, a + len(n.Value), 0, bytes.HasPrefix(body[a:], []byte(n.Value))
	}
	a++
	for b := a; b < len(body) && body[b] != '\n'; b++ {
		if quote == '"' && body[b] == '\\' {
			b++
			continue
		}
		if body[b] != quote {
			continue
		}
		if quote == '\'' && b+1 < len(body) && body[b+1] == quote {
			b++
			continue
		}
		return a, b, quote, storyScalarText(body[a:b], quote) == n.Value
	}
	return 0, 0, 0, false
}

func storyScalarText(raw []byte, quote byte) string {
	if quote == 0 {
		return string(raw)
	}
	var value string
	encoded := append([]byte{quote}, raw...)
	encoded = append(encoded, quote)
	if yaml.Unmarshal(encoded, &value) != nil {
		return ""
	}
	return value
}

func (idx StoryIndex) At(offset int) (StorySymbol, bool) {
	for _, s := range idx.Symbols {
		if offset >= s.Range.StartByte && offset < s.Range.EndByte {
			return s, true
		}
	}
	return StorySymbol{}, false
}
func (idx StoryIndex) References(target StorySymbol, includeDeclaration bool) []StorySymbol {
	var out []StorySymbol
	for _, s := range idx.Symbols {
		if s.Kind == target.Kind && s.Scope == target.Scope && s.Name == target.Name && (includeDeclaration || !s.Declaration) {
			out = append(out, s)
		}
	}
	return out
}

// Rename returns disjoint edits, refusing ambiguous declarations and collisions.
func (idx StoryIndex) Rename(target StorySymbol, name string) ([]SourceEdit, error) {
	if !storyName.MatchString(name) {
		return nil, fmt.Errorf("story name must contain 1–64 letters, digits, underscores or hyphens and start with a letter")
	}
	declarations := 0
	for _, s := range idx.Symbols {
		if s.Kind == target.Kind && (target.Kind == "slide" || s.Scope == target.Scope) && s.Declaration {
			if s.Name == target.Name {
				declarations++
			} else if s.Name == name {
				return nil, fmt.Errorf("story name %q already exists", name)
			}
		}
	}
	if declarations != 1 {
		return nil, fmt.Errorf("story reference has no unique declaration")
	}
	var edits []SourceEdit
	for _, s := range idx.References(target, true) {
		edits = append(edits, SourceEdit{s.Range, name})
	}
	return edits, nil
}

// Actor identifiers are lexed only inside a parsed Sirena fence. Quoted labels
// and comments are skipped; declaration kinds and edge endpoints are indexed.
// Layout/semantic validation remains the supplied Sirena renderer's job.
func indexStoryActors(doc *Document, n *Node, result *StoryIndex) {
	start, end := n.Range.StartByte, n.Range.EndByte
	if start < 0 || end > len(doc.Source) {
		return
	}
	body := bytes.IndexByte(doc.Source[start:end], '\n')
	if body < 0 {
		return
	}
	start += body + 1
	source := doc.Source[start:end]
	type token struct {
		text       string
		start, end int
	}
	var tokens []token
	for i := 0; i < len(source); {
		c := source[i]
		if c == '"' || c == '\'' {
			quote := c
			i++
			for i < len(source) {
				if source[i] == '\\' {
					i += 2
					continue
				}
				if source[i] == quote {
					i++
					break
				}
				i++
			}
			continue
		}
		if c == '#' || c == '/' && i+1 < len(source) && source[i+1] == '/' {
			for i < len(source) && source[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(source) && source[i+1] == '*' {
			i += 2
			for i+1 < len(source) && !(source[i] == '*' && source[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_' {
			a := i
			i++
			for i < len(source) && (source[i] >= 'A' && source[i] <= 'Z' || source[i] >= 'a' && source[i] <= 'z' || source[i] >= '0' && source[i] <= '9' || source[i] == '_' || source[i] == '-' && !(i+1 < len(source) && source[i+1] == '>')) {
				i++
			}
			tokens = append(tokens, token{string(source[a:i]), start + a, start + i})
			continue
		}
		if i+1 < len(source) && (string(source[i:i+2]) == "->" || string(source[i:i+2]) == "<-") {
			a := i
			i += 2
			if i < len(source) && source[i] == '>' {
				i++
			}
			tokens = append(tokens, token{string(source[a:i]), start + a, start + i})
			continue
		}
		tokens = append(tokens, token{string(c), start + i, start + i + 1})
		i++
	}
	scope := fmt.Sprintf("diagram:%d", n.Range.StartByte)
	kinds := map[string]bool{"service": true, "client": true, "database": true, "queue": true, "cache": true, "job": true, "external": true, "gateway": true, "node": true}
	add := func(t token, decl bool) {
		if storyName.MatchString(t.text) {
			result.Symbols = append(result.Symbols, StorySymbol{"actor", t.text, scope, decl, sourceRange(doc.Source, t.start, t.end)})
		}
	}
	for i, t := range tokens {
		if kinds[t.text] {
			j := i + 1
			for j < len(tokens) && (tokens[j].text == " " || tokens[j].text == "\t") {
				j++
			}
			if j < len(tokens) {
				add(tokens[j], true)
			}
		}
		if t.text == "->" || t.text == "<-" || t.text == "<->" {
			a, b := i-1, i+1
			for a >= 0 && (tokens[a].text == " " || tokens[a].text == "\t") {
				a--
			}
			for b < len(tokens) && (tokens[b].text == " " || tokens[b].text == "\t") {
				b++
			}
			if a >= 0 {
				add(tokens[a], false)
			}
			if b < len(tokens) {
				add(tokens[b], false)
			}
		}
	}
}

// SourceEdit replaces exactly one source range, preserving unrelated bytes.
type SourceEdit struct {
	Range   Range
	NewText string
}
type sourceAttribute struct {
	key, value string
	start, end int
}

func directiveSourceAttributes(doc *Document, start int) ([]sourceAttribute, error) {
	if start < 0 || start >= len(doc.Source) {
		return nil, fmt.Errorf("invalid directive offset")
	}
	end := start
	for end < len(doc.Source) && doc.Source[end] != '\n' {
		end++
	}
	source := doc.Source
	var out []sourceAttribute
	brace := bytes.IndexByte(source[start:end], '{')
	if brace < 0 {
		return out, nil
	}
	for i := start + brace + 1; i < end; {
		for i < end && (source[i] == ' ' || source[i] == '\t') {
			i++
		}
		a := i
		for i < end && (source[i] >= 'a' && source[i] <= 'z' || source[i] >= 'A' && source[i] <= 'Z' || source[i] >= '0' && source[i] <= '9' || source[i] == '_' || source[i] == '-') {
			i++
		}
		if a == i {
			break
		}
		key := string(source[a:i])
		for i < end && (source[i] == ' ' || source[i] == '\t') {
			i++
		}
		if i >= end || source[i] != '=' {
			break
		}
		i++
		for i < end && (source[i] == ' ' || source[i] == '\t') {
			i++
		}
		a = i
		quote := byte(0)
		if i < end && (source[i] == '"' || source[i] == '\'') {
			quote = source[i]
			i++
			a = i
			for i < end && source[i] != quote {
				if source[i] == '\\' {
					i++
				}
				i++
			}
		} else {
			for i < end && source[i] != ' ' && source[i] != '\t' && source[i] != '}' {
				i++
			}
		}
		b := i
		value := string(source[a:b])
		if quote == '"' {
			if decoded, err := strconv.Unquote("\"" + value + "\""); err == nil {
				value = decoded
			}
		}
		out = append(out, sourceAttribute{key, value, a, b})
		if quote != 0 {
			i++
		}
	}
	return out, nil
}

// EditDirectiveAttributes patches an existing parsed directive opening. It
// preserves spacing, quote style, unknown attributes and its complete body.
func EditDirectiveAttributes(doc *Document, start int, changes map[string]string) (SourceEdit, error) {
	if doc == nil || doc.Root == nil || doc.SourceHadCarriageReturns() {
		return SourceEdit{}, fmt.Errorf("directive edits require parsed LF source")
	}
	found := false
	doc.Root.Walk(func(n *Node) bool {
		if n.Type == NodeContainerDirective && n.Range.StartByte == start {
			found = true
		}
		return true
	})
	if !found {
		return SourceEdit{}, fmt.Errorf("no parsed directive at offset %d", start)
	}
	attrs, err := directiveSourceAttributes(doc, start)
	if err != nil {
		return SourceEdit{}, err
	}
	end := start
	for end < len(doc.Source) && doc.Source[end] != '\n' {
		end++
	}
	edits := []SourceEdit{}
	seen := map[string]bool{}
	for _, a := range attrs {
		if seen[a.key] {
			return SourceEdit{}, fmt.Errorf("duplicate directive attribute %q", a.key)
		}
		seen[a.key] = true
		if value, ok := changes[a.key]; ok {
			if strings.ContainsAny(value, "\r\n") {
				return SourceEdit{}, fmt.Errorf("attribute cannot contain a newline")
			}
			text := value
			if a.start > start && doc.Source[a.start-1] == '"' {
				quoted := strconv.Quote(value)
				text = quoted[1 : len(quoted)-1]
			} else if a.start > start && doc.Source[a.start-1] == '\'' {
				if strings.ContainsAny(value, "'\\") {
					return SourceEdit{}, fmt.Errorf("cannot preserve single quotes for attribute %q", a.key)
				}
			} else if strings.ContainsAny(value, " \t{}\"'") {
				text = strconv.Quote(value)
			}
			edits = append(edits, SourceEdit{sourceRange(doc.Source, a.start, a.end), text})
		}
	}
	var missing []string
	for key, value := range changes {
		if !storyName.MatchString(key) || strings.ContainsAny(value, "\r\n") {
			return SourceEdit{}, fmt.Errorf("invalid directive attribute")
		}
		if !seen[key] {
			missing = append(missing, key+"="+strconv.Quote(value))
		}
	}
	sort.Strings(missing)
	opening := string(doc.Source[start:end])
	sort.Slice(edits, func(i, j int) bool { return edits[i].Range.StartByte > edits[j].Range.StartByte })
	for _, edit := range edits {
		a, b := edit.Range.StartByte-start, edit.Range.EndByte-start
		opening = opening[:a] + edit.NewText + opening[b:]
	}
	if len(missing) > 0 {
		brace := strings.LastIndex(opening, "}")
		if brace >= 0 {
			opening = opening[:brace] + " " + strings.Join(missing, " ") + opening[brace:]
		} else {
			opening += " {" + strings.Join(missing, " ") + "}"
		}
	}
	return SourceEdit{sourceRange(doc.Source, start, end), opening}, nil
}

// Package lint provides diagnostics over Markdown++ documents.
package lint

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/odvcencio/mdpp"
)

// Severity classifies a diagnostic.
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
	SeverityInfo
	SeverityHint
)

// Diagnostic is a single lint finding.
type Diagnostic struct {
	Range    mdpp.Range
	Severity Severity
	Code     string
	Message  string
	Fix      *TextEdit
	Related  []RelatedInfo
}

// TextEdit is a single replacement.
type TextEdit struct {
	Range   mdpp.Range
	NewText string
}

// RelatedInfo points at another relevant location.
type RelatedInfo struct {
	Range   mdpp.Range
	Message string
}

// Rule is a single lint rule.
type Rule interface {
	Code() string
	DefaultSeverity() Severity
	Title() string
	Description() string
	Check(d *mdpp.Document, emit func(Diagnostic))
}

type builtinRule struct {
	code, title, description string
	severity                 Severity
}

func (r builtinRule) Code() string                           { return r.code }
func (r builtinRule) DefaultSeverity() Severity              { return r.severity }
func (r builtinRule) Title() string                          { return r.title }
func (r builtinRule) Description() string                    { return r.description }
func (r builtinRule) Check(*mdpp.Document, func(Diagnostic)) {}

var rules = []Rule{
	builtinRule{"MD004", "Inconsistent unordered list marker", "", SeverityInfo},
	builtinRule{"MD009", "Trailing whitespace", "", SeverityInfo},
	builtinRule{"MD012", "Multiple consecutive blank lines", "", SeverityInfo},
	builtinRule{"MD034", "Bare URL not in autolink form", "", SeverityInfo},
	builtinRule{"MD045", "Missing image alt text", "", SeverityWarning},
	builtinRule{"MD049", "Inconsistent emphasis style", "", SeverityInfo},
	builtinRule{"MDPP100", "Undefined footnote reference", "", SeverityError},
	builtinRule{"MDPP101", "Footnote definition with no reference", "", SeverityWarning},
	builtinRule{"MDPP102", "Broken intra-doc link", "", SeverityError},
	builtinRule{"MDPP103", "Duplicate heading ID", "", SeverityWarning},
	builtinRule{"MDPP104", "Undefined container type", "", SeverityWarning},
	builtinRule{"MDPP105", "Unused link reference definition", "", SeverityWarning},
	builtinRule{"MDPP106", "Reference link to undefined ref", "", SeverityError},
	builtinRule{"MDPP107", "Frontmatter mdpp version mismatch", "", SeverityInfo},
	builtinRule{"MDPP108", "Multiple TOC directives", "", SeverityWarning},
	builtinRule{"MDPP109", "TOC without headings", "", SeverityInfo},
	builtinRule{"MDPP110", "Auto-embed unrecognized provider", "", SeverityInfo},
	builtinRule{"MDPP111", "Auto-embed malformed URL", "", SeverityError},
	builtinRule{"MDPP200", "Heading-level skip", "", SeverityWarning},
	builtinRule{"MDPP201", "Bare URL autolink should use descriptive text", "", SeverityInfo},
	builtinRule{"MDPP202", "Empty link text", "", SeverityError},
	builtinRule{"MDPP203", "Table without header row", "", SeverityWarning},
	builtinRule{"MDPP300", "Inconsistent fence info-string style", "", SeverityInfo},
}

// Rules returns every built-in rule, in code order.
func Rules() []Rule {
	out := make([]Rule, len(rules))
	copy(out, rules)
	return out
}

// RuleByCode returns the built-in rule with the given code, or nil.
func RuleByCode(code string) Rule {
	for _, r := range rules {
		if r.Code() == code {
			return r
		}
	}
	return nil
}

// Lint runs all default-enabled rules over d and returns diagnostics in source order.
func Lint(d *mdpp.Document) []Diagnostic {
	if d == nil || d.Root == nil {
		return nil
	}
	ctx := collectContext(d)
	var out []Diagnostic
	emit := func(diag Diagnostic) {
		if diag.Range.StartLine == 0 {
			diag.Range = mdpp.Range{StartByte: 0, EndByte: 1, StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 2}
		}
		if !ctx.suppressed(diag.Code, diag.Range.StartLine) {
			out = append(out, diag)
		}
	}
	lintAST(d, ctx, emit)
	lintSource(d, ctx, emit)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Range.StartByte == out[j].Range.StartByte {
			return out[i].Code < out[j].Code
		}
		return out[i].Range.StartByte < out[j].Range.StartByte
	})
	return out
}

type lintContext struct {
	source            []byte
	headings          map[string]*mdpp.Node
	headingCounts     map[string]int
	footnoteDefs      map[string]*mdpp.Node
	footnoteRefs      map[string][]*mdpp.Node
	linkRefDefs       map[string]mdpp.Range
	linkRefUses       map[string][]mdpp.Range
	ignoredRanges     []mdpp.Range
	fileSuppressions  map[string]bool
	blockSuppressions map[int]map[string]bool
	nextSuppressions  map[int]map[string]bool
}

func collectContext(d *mdpp.Document) *lintContext {
	ctx := &lintContext{
		source:            d.Source,
		headings:          map[string]*mdpp.Node{},
		headingCounts:     map[string]int{},
		footnoteDefs:      map[string]*mdpp.Node{},
		footnoteRefs:      map[string][]*mdpp.Node{},
		linkRefDefs:       collectReferenceDefinitions(d.Source),
		fileSuppressions:  map[string]bool{},
		blockSuppressions: map[int]map[string]bool{},
		nextSuppressions:  map[int]map[string]bool{},
	}
	ctx.linkRefUses = collectReferenceUses(d.Source, ctx.linkRefDefs)
	for _, r := range ctx.linkRefDefs {
		ctx.ignoredRanges = append(ctx.ignoredRanges, r)
	}
	ctx.collectSuppressions(d)
	d.Root.Walk(func(n *mdpp.Node) bool {
		switch n.Type {
		case mdpp.NodeHeading:
			id := mdpp.Slugify(n.Text())
			ctx.headingCounts[id]++
			if ctx.headings[id] == nil {
				ctx.headings[id] = n
			}
		case mdpp.NodeFootnoteDef:
			ctx.footnoteDefs[n.Attr("id")] = n
		case mdpp.NodeFootnoteRef:
			ctx.footnoteRefs[n.Attr("id")] = append(ctx.footnoteRefs[n.Attr("id")], n)
		case mdpp.NodeCodeBlock, mdpp.NodeDiagram, mdpp.NodeCodeSpan, mdpp.NodeHTMLBlock, mdpp.NodeHTMLInline, mdpp.NodeMathInline, mdpp.NodeMathBlock, mdpp.NodeFrontmatter, mdpp.NodeAutoEmbed:
			if n.Range.StartLine != 0 {
				ctx.ignoredRanges = append(ctx.ignoredRanges, n.Range)
			}
		case mdpp.NodeLink:
			if ref := normalizeLabel(n.Attr("ref")); ref != "" {
				ctx.linkRefUses[ref] = append(ctx.linkRefUses[ref], n.Range)
			}
			if raw := n.Attr("raw"); raw != "" && strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
				label := normalizeLabel(strings.Trim(raw, "[]"))
				if _, ok := ctx.linkRefDefs[label]; ok {
					ctx.linkRefUses[label] = append(ctx.linkRefUses[label], n.Range)
				}
			}
		}
		return true
	})
	return ctx
}

func lintAST(d *mdpp.Document, ctx *lintContext, emit func(Diagnostic)) {
	var tocCount int
	var previousHeading int
	d.Root.Walk(func(n *mdpp.Node) bool {
		switch n.Type {
		case mdpp.NodeFootnoteRef:
			id := n.Attr("id")
			if ctx.footnoteDefs[id] == nil {
				emitDiag(emit, n.Range, SeverityError, "MDPP100", "undefined footnote reference [^"+id+"]")
			}
		case mdpp.NodeFootnoteDef:
			id := n.Attr("id")
			if len(ctx.footnoteRefs[id]) == 0 {
				emitDiag(emit, n.Range, SeverityWarning, "MDPP101", "footnote definition [^"+id+"] is never referenced")
			}
		case mdpp.NodeLink:
			href := n.Attr("href")
			if strings.HasPrefix(href, "#") {
				anchor := strings.TrimPrefix(href, "#")
				if ctx.headings[anchor] == nil {
					emitDiag(emit, n.Range, SeverityError, "MDPP102", "broken intra-doc link #"+anchor)
				}
			}
			if n.Attr("ref") != "" && href == "" {
				emitDiag(emit, n.Range, SeverityError, "MDPP106", "reference link ["+n.Attr("ref")+"] has no definition")
			}
			if strings.TrimSpace(n.Text()) == "" && href != "" {
				emitDiag(emit, n.Range, SeverityError, "MDPP202", "link text is empty")
			}
		case mdpp.NodeHeading:
			id := mdpp.Slugify(n.Text())
			if ctx.headingCounts[id] > 1 && ctx.headings[id] != n {
				emit(Diagnostic{Range: n.Range, Severity: SeverityWarning, Code: "MDPP103", Message: "duplicate heading id #" + id, Related: []RelatedInfo{{Range: ctx.headings[id].Range, Message: "first heading with this id"}}})
			}
			level := n.Level()
			if previousHeading > 0 && level > previousHeading+1 {
				emitDiag(emit, n.Range, SeverityWarning, "MDPP200", "heading level skips from h"+strconv.Itoa(previousHeading)+" to h"+strconv.Itoa(level))
			}
			if level > 0 {
				previousHeading = level
			}
		case mdpp.NodeContainerDirective:
			name := n.Attr("name")
			if !allowedContainer(name) {
				emitDiag(emit, n.Range, SeverityWarning, "MDPP104", "unknown container type :::"+name)
			}
		case mdpp.NodeImage:
			if strings.TrimSpace(n.Attr("alt")) == "" {
				emitDiag(emit, n.Range, SeverityWarning, "MD045", "image alt text is empty")
			}
		case mdpp.NodeTableOfContents:
			tocCount++
			if tocCount > 1 {
				emitDiag(emit, n.Range, SeverityWarning, "MDPP108", "multiple [[toc]] directives")
			}
		case mdpp.NodeAutoEmbed:
			src := n.Attr("src")
			u, err := url.Parse(src)
			if err != nil || u.Scheme == "" || u.Host == "" {
				emitDiag(emit, n.Range, SeverityError, "MDPP111", "auto-embed URL is malformed")
			} else if n.Attr("provider") == "generic" && !isDirectMediaEmbed(src) {
				emitDiag(emit, n.Range, SeverityInfo, "MDPP110", "auto-embed provider is generic")
			}
		}
		return true
	})
	if tocCount > 0 && len(d.Headings()) == 0 {
		for _, n := range d.Root.Find(mdpp.NodeTableOfContents) {
			emitDiag(emit, n.Range, SeverityInfo, "MDPP109", "[[toc]] has no headings to populate")
		}
	}
	for label, r := range ctx.linkRefDefs {
		if len(ctx.linkRefUses[label]) == 0 {
			emit(Diagnostic{
				Range:    r,
				Severity: SeverityWarning,
				Code:     "MDPP105",
				Message:  "link reference [" + label + "] is never used",
				Fix:      &TextEdit{Range: r, NewText: ""},
			})
		}
	}
	if version := d.FormatVersion(); version != "" && version > mdpp.SpecVersion {
		emitDiag(emit, mdpp.Range{StartByte: 0, EndByte: 1, StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 2}, SeverityInfo, "MDPP107", "frontmatter mdpp version "+version+" is newer than parser "+mdpp.SpecVersion)
	}
}

func lintSource(d *mdpp.Document, ctx *lintContext, emit func(Diagnostic)) {
	lines := strings.Split(string(d.Source), "\n")
	blankRun := 0
	listMarkers := map[string]bool{}
	emStar := false
	emUnderscore := false
	emStarRe := regexp.MustCompile(`(^|[^*])\*[^*\s][^*]*\*`)
	emUnderscoreRe := regexp.MustCompile(`(^|[^_])_[^_\s][^_]*_`)
	bareURLRe := regexp.MustCompile(`https?://[^\s<>()]+`)
	fenceLangs := map[string]string{}
	lineStart := 0
	for i, line := range lines {
		lineNo := i + 1
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			r := lineRange(d.Source, lineNo, len(strings.TrimRight(line, " \t")), len(line))
			emit(Diagnostic{Range: r, Severity: SeverityInfo, Code: "MD009", Message: "trailing whitespace", Fix: &TextEdit{Range: r, NewText: ""}})
		}
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			trimmedFence := strings.TrimSpace(line)
			lang := ""
			langStart := -1
			if strings.HasPrefix(trimmedFence, "```") {
				rest := strings.TrimSpace(strings.TrimPrefix(trimmedFence, "```"))
				if rest != "" {
					lang = strings.Fields(rest)[0]
					langStart = strings.Index(line, lang)
				}
			}
			if lang != "" {
				lower := strings.ToLower(lang)
				if lang != lower {
					r := lineRange(d.Source, lineNo, langStart, langStart+len(lang))
					emit(Diagnostic{Range: r, Severity: SeverityInfo, Code: "MDPP300", Message: "fence info-string should be lowercase", Fix: &TextEdit{Range: r, NewText: lower}})
				}
				fenceLangs[lower] = lang
			}
		}
		lineWholeRange := byteRange(d.Source, lineStart, lineStart+len(line))
		if ctx.ignored(lineWholeRange) {
			blankRun = 0
			lineStart += len(line)
			if i < len(lines)-1 {
				lineStart++
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun >= 3 {
				emitDiag(emit, lineRange(d.Source, lineNo, 0, len(line)), SeverityInfo, "MD012", "multiple consecutive blank lines")
			}
			lineStart += len(line)
			if i < len(lines)-1 {
				lineStart++
			}
			continue
		}
		blankRun = 0
		if emStarRe.MatchString(line) {
			emStar = true
		}
		if emUnderscoreRe.MatchString(line) {
			emUnderscore = true
		}
		trimmed := strings.TrimLeft(line, " ")
		if len(trimmed) > 1 && strings.Contains("-*+", trimmed[:1]) && trimmed[1] == ' ' {
			listMarkers[trimmed[:1]] = true
		}
		for _, match := range bareURLRe.FindAllStringIndex(line, -1) {
			before := ""
			if match[0] > 0 {
				before = line[match[0]-1 : match[0]]
			}
			after := ""
			if match[1] < len(line) {
				after = line[match[1] : match[1]+1]
			}
			matchRange := byteRange(d.Source, lineStart+match[0], lineStart+match[1])
			if before == "<" && after == ">" && !ctx.ignored(matchRange) {
				emitDiag(emit, matchRange, SeverityInfo, "MDPP201", "autolink URL should use descriptive link text")
			}
			if before != "<" && before != "(" && !ctx.ignored(matchRange) {
				emitDiag(emit, matchRange, SeverityInfo, "MD034", "bare URL should use explicit link syntax")
			}
		}
		lineStart += len(line)
		if i < len(lines)-1 {
			lineStart++
		}
	}
	if len(listMarkers) > 1 {
		emitDiag(emit, mdpp.Range{StartByte: 0, EndByte: 1, StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 2}, SeverityInfo, "MD004", "unordered list markers are inconsistent")
	}
	if emStar && emUnderscore {
		emitDiag(emit, mdpp.Range{StartByte: 0, EndByte: 1, StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 2}, SeverityInfo, "MD049", "emphasis styles are inconsistent")
	}
	for range fenceLangs {
		break
	}
}

func (ctx *lintContext) ignored(r mdpp.Range) bool {
	if ctx == nil || len(ctx.ignoredRanges) == 0 || r.StartLine == 0 {
		return false
	}
	for _, ignored := range ctx.ignoredRanges {
		if r.StartByte >= ignored.StartByte && r.EndByte <= ignored.EndByte {
			return true
		}
	}
	return false
}

func emitDiag(emit func(Diagnostic), r mdpp.Range, sev Severity, code string, msg string) {
	emit(Diagnostic{Range: r, Severity: sev, Code: code, Message: msg})
}

func allowedContainer(name string) bool {
	switch strings.ToLower(name) {
	case "note", "tip", "warning", "caution", "important", "info", "details", "aside", "columns", "column", "col":
		return true
	default:
		return false
	}
}

func isDirectMediaEmbed(src string) bool {
	u, err := url.Parse(src)
	if err != nil {
		return false
	}
	path := strings.ToLower(u.Path)
	for _, ext := range []string{".mp4", ".webm", ".mov", ".m4v", ".mp3", ".wav", ".ogg", ".oga", ".flac"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

var refDefRe = regexp.MustCompile(`(?m)^ {0,3}\[([^\]\^][^\]]*)\]:[ \t]*(\S+)`)

func collectReferenceDefinitions(src []byte) map[string]mdpp.Range {
	out := map[string]mdpp.Range{}
	for _, loc := range refDefRe.FindAllSubmatchIndex(src, -1) {
		label := normalizeLabel(string(src[loc[2]:loc[3]]))
		end := loc[1]
		if end < len(src) && src[end] == '\n' {
			end++
		}
		out[label] = byteRange(src, loc[0], end)
	}
	return out
}

var bracketTokenRe = regexp.MustCompile(`!?\[([^\]\n]+)\]`)

func collectReferenceUses(src []byte, defs map[string]mdpp.Range) map[string][]mdpp.Range {
	out := map[string][]mdpp.Range{}
	if len(defs) == 0 {
		return out
	}
	for _, loc := range bracketTokenRe.FindAllSubmatchIndex(src, -1) {
		label := normalizeLabel(string(src[loc[2]:loc[3]]))
		if _, ok := defs[label]; !ok {
			continue
		}
		if loc[1] < len(src) {
			switch src[loc[1]] {
			case ':', '(', '[':
				continue
			}
		}
		out[label] = append(out[label], byteRange(src, loc[2], loc[3]))
	}
	return out
}

func normalizeLabel(label string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.Trim(label, "[]"))), " ")
}

func byteRange(src []byte, start, end int) mdpp.Range {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > len(src) {
		end = len(src)
	}
	sl, sc := lineCol(src, start)
	el, ec := lineCol(src, end)
	return mdpp.Range{StartByte: start, EndByte: end, StartLine: sl, StartCol: sc, EndLine: el, EndCol: ec}
}

func lineRange(src []byte, lineNo int, startCol0 int, endCol0 int) mdpp.Range {
	start := 0
	line := 1
	for start < len(src) && line < lineNo {
		if src[start] == '\n' {
			line++
		}
		start++
	}
	return byteRange(src, start+startCol0, start+endCol0)
}

func lineCol(src []byte, offset int) (int, int) {
	line, col := 1, 1
	for i := 0; i < offset && i < len(src); i++ {
		if src[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

var suppressCommentRe = regexp.MustCompile(`<!--\s*(mdpp-disable-next-line|mdpp-disable|mdpp-enable)\s*([^>]*)-->`)

func (ctx *lintContext) collectSuppressions(d *mdpp.Document) {
	if fm := d.Frontmatter(); fm != nil {
		if raw, ok := fm["mdpp-disable"]; ok {
			for _, code := range suppressionCodes(raw) {
				ctx.fileSuppressions[code] = true
			}
		}
	}
	active := map[string]bool{}
	lines := strings.Split(string(d.Source), "\n")
	for i, line := range lines {
		lineNo := i + 1
		if len(active) > 0 {
			ctx.blockSuppressions[lineNo] = copyCodes(active)
		}
		m := suppressCommentRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		codes := parseSuppressionList(m[2])
		switch m[1] {
		case "mdpp-disable-next-line":
			target := nextNonBlankLine(lines, i+1)
			if target > 0 {
				ctx.nextSuppressions[target] = codes
			}
		case "mdpp-disable":
			for code, all := range codes {
				active[code] = all
			}
		case "mdpp-enable":
			for code := range codes {
				delete(active, code)
			}
		}
	}
}

func (ctx *lintContext) suppressed(code string, line int) bool {
	return codeSuppressed(ctx.nextSuppressions[line], code) ||
		codeSuppressed(ctx.blockSuppressions[line], code) ||
		codeSuppressed(ctx.fileSuppressions, code)
}

func codeSuppressed(codes map[string]bool, code string) bool {
	if len(codes) == 0 {
		return false
	}
	return codes["*"] || codes[code]
}

func parseSuppressionList(raw string) map[string]bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]bool{"*": true}
	}
	out := map[string]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func suppressionCodes(raw any) []string {
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	case string:
		return strings.Split(v, ",")
	default:
		return nil
	}
}

func copyCodes(in map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func nextNonBlankLine(lines []string, start int) int {
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return i + 1
		}
	}
	return 0
}

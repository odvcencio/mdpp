// Package lint provides diagnostics over Markdown++ documents.
package lint

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"m31labs.dev/mdpp"
	mdppfmt "m31labs.dev/mdpp/fmt"
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
	if len(d.Diagnostics()) > 0 || d.SourceHadCarriageReturns() || sourceHasUnsupportedControl(d.Source) {
		// Parser recovery marks source whose syntax is ambiguous or malformed.
		// Invalid UTF-8 and control characters beyond Markdown whitespace also
		// make source edits unsafe. Normalized line endings change byte offsets
		// from the input file. Keep findings, but withhold all edits.
		for i := range out {
			out[i].Fix = nil
		}
	} else if !fixesPreserveRenderedMeaning(d, out) {
		for i := range out {
			out[i].Fix = nil
		}
	}
	return out
}

func fixesPreserveRenderedMeaning(before *mdpp.Document, diagnostics []Diagnostic) bool {
	var edits []TextEdit
	for _, diag := range diagnostics {
		if diag.Fix != nil {
			edits = append(edits, *diag.Fix)
		}
	}
	if len(edits) == 0 {
		return true
	}
	fixed, ok := applyLintTextEdits(before.Source, edits)
	if !ok {
		return false
	}
	after, err := mdpp.Parse(fixed)
	if err != nil || after == nil || after.Root == nil {
		return false
	}
	return mdppfmt.PreservesMeaning(before.Source, fixed)
}

func applyLintTextEdits(source []byte, edits []TextEdit) ([]byte, bool) {
	sort.SliceStable(edits, func(i, j int) bool {
		return edits[i].Range.StartByte < edits[j].Range.StartByte
	})
	out := make([]byte, 0, len(source))
	previousEnd := 0
	for _, edit := range edits {
		start, end := edit.Range.StartByte, edit.Range.EndByte
		if start < previousEnd || start < 0 || end < start || end > len(source) {
			return nil, false
		}
		out = append(out, source[previousEnd:start]...)
		out = append(out, edit.NewText...)
		previousEnd = end
	}
	out = append(out, source[previousEnd:]...)
	return out, true
}

func sourceHasUnsupportedControl(source []byte) bool {
	for len(source) > 0 {
		r, size := utf8.DecodeRune(source)
		if r == utf8.RuneError && size == 1 {
			return true
		}
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return true
		}
		source = source[size:]
	}
	return false
}

type lintContext struct {
	source             []byte
	headings           map[string]*mdpp.Node
	headingCounts      map[string]int
	tableRanges        []mdpp.Range
	footnoteDefs       map[string]*mdpp.Node
	footnoteRefs       map[string][]*mdpp.Node
	linkRefDefs        map[string]mdpp.Range
	linkRefUses        map[string][]mdpp.Range
	linkDestRanges     []mdpp.Range
	listItemRanges     []mdpp.Range
	trailingTextRanges []mdpp.Range
	softBreakRanges    []mdpp.Range
	ignoredRanges      []mdpp.Range
	hardBreakLines     map[int]bool
	fileSuppressions   map[string]bool
	blockSuppressions  map[int]map[string]bool
	nextSuppressions   map[int]map[string]bool
}

func collectContext(d *mdpp.Document) *lintContext {
	ctx := &lintContext{
		source:            d.Source,
		headings:          map[string]*mdpp.Node{},
		headingCounts:     map[string]int{},
		tableRanges:       []mdpp.Range{},
		footnoteDefs:      map[string]*mdpp.Node{},
		footnoteRefs:      map[string][]*mdpp.Node{},
		linkRefDefs:       map[string]mdpp.Range{},
		linkRefUses:       map[string][]mdpp.Range{},
		hardBreakLines:    map[int]bool{},
		fileSuppressions:  map[string]bool{},
		blockSuppressions: map[int]map[string]bool{},
		nextSuppressions:  map[int]map[string]bool{},
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
		case mdpp.NodeTable:
			ctx.tableRanges = append(ctx.tableRanges, n.Range)
		case mdpp.NodeListItem, mdpp.NodeTaskListItem:
			ctx.listItemRanges = append(ctx.listItemRanges, n.Range)
		case mdpp.NodeText:
			if strings.TrimRight(n.Literal, " \t") != n.Literal {
				ctx.trailingTextRanges = append(ctx.trailingTextRanges, n.Range)
			}
		case mdpp.NodeSoftBreak:
			ctx.softBreakRanges = append(ctx.softBreakRanges, n.Range)
		case mdpp.NodeFootnoteDef:
			ctx.footnoteDefs[n.Attr("id")] = n
		case mdpp.NodeFootnoteRef:
			ctx.footnoteRefs[n.Attr("id")] = append(ctx.footnoteRefs[n.Attr("id")], n)
		case mdpp.NodeLinkReferenceDefinition:
			label := normalizeLabel(n.Attr("label"))
			if label != "" {
				r := n.Range
				sourceEnd := len(d.Source)
				if sourceEnd > 0 && d.Source[sourceEnd-1] == '\n' {
					sourceEnd--
				}
				if r.EndByte > sourceEnd {
					r = byteRange(d.Source, r.StartByte, sourceEnd)
				} else if r.EndByte < sourceEnd && d.Source[r.EndByte] == '\n' {
					r = byteRange(d.Source, r.StartByte, r.EndByte+1)
				}
				ctx.linkRefDefs[label] = r
				ctx.ignoredRanges = append(ctx.ignoredRanges, r)
			}
		case mdpp.NodeHardBreak:
			if n.Range.StartLine > 0 {
				if n.Range.StartLine > 1 && n.Range.StartByte > 0 && n.Range.StartByte <= len(d.Source) && d.Source[n.Range.StartByte-1] == '\n' {
					ctx.hardBreakLines[n.Range.StartLine-1] = true
				} else {
					ctx.hardBreakLines[n.Range.StartLine] = true
				}
			}
		case mdpp.NodeCodeBlock, mdpp.NodeDiagram, mdpp.NodeCodeSpan, mdpp.NodeHTMLBlock, mdpp.NodeHTMLInline, mdpp.NodeMathInline, mdpp.NodeMathBlock, mdpp.NodeFrontmatter, mdpp.NodeAutoEmbed:
			if n.Range.StartLine != 0 {
				ctx.ignoredRanges = append(ctx.ignoredRanges, n.Range)
			}
		case mdpp.NodeLink, mdpp.NodeImage:
			ref := n.Attr("ref")
			if ref == "" {
				ref = n.Attr("resolved-ref")
			}
			if ref = normalizeLabel(ref); ref != "" {
				ctx.linkRefUses[ref] = append(ctx.linkRefUses[ref], n.Range)
			}
			if r, ok := linkDestinationRange(d.Source, n.Range); ok {
				ctx.linkDestRanges = append(ctx.linkDestRanges, r)
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
			if ref := normalizeLabel(n.Attr("ref")); ref != "" {
				if _, ok := ctx.linkRefDefs[ref]; !ok {
					emitDiag(emit, n.Range, SeverityError, "MDPP106", "reference link ["+n.Attr("ref")+"] has no definition")
				}
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
			var fix *TextEdit
			if !ctx.inListItem(r) && r.EndByte < len(ctx.source)-1 {
				fix = &TextEdit{Range: r, NewText: ""}
			}
			emit(Diagnostic{
				Range:    r,
				Severity: SeverityWarning,
				Code:     "MDPP105",
				Message:  "link reference [" + label + "] is never used",
				Fix:      fix,
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
	lineStart := 0
	for i, line := range lines {
		lineNo := i + 1
		lineWholeRange := byteRange(d.Source, lineStart, lineStart+len(line))
		lineInProtected := ctx.ignored(lineWholeRange)
		advanceLine := func() {
			lineStart += len(line)
			if i < len(lines)-1 {
				lineStart++
			}
		}

		trailingStart := len(strings.TrimRight(line, " \t"))
		if trailingStart < len(line) && !lineInProtected && !ctx.hardBreakLines[lineNo] {
			r := byteRange(d.Source, lineStart+trailingStart, lineStart+len(line))
			if !ctx.overlapsIgnored(r) {
				var fix *TextEdit
				if r.EndByte < len(d.Source)-1 && !ctx.inListItem(r) && !ctx.trailingWhitespaceIsRendered(r) {
					fix = &TextEdit{Range: r, NewText: ""}
				}
				emit(Diagnostic{Range: r, Severity: SeverityInfo, Code: "MD009", Message: "trailing whitespace", Fix: fix})
			}
		}
		if !lineInProtected && strings.HasPrefix(strings.TrimSpace(line), "```") {
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
					r := byteRange(d.Source, lineStart+langStart, lineStart+langStart+len(lang))
					if !ctx.overlapsIgnored(r) {
						emit(Diagnostic{Range: r, Severity: SeverityInfo, Code: "MDPP300", Message: "fence info-string should be lowercase", Fix: &TextEdit{Range: r, NewText: lower}})
					}
				}
			}
		}
		if lineInProtected {
			blankRun = 0
			advanceLine()
			continue
		}
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun >= 3 {
				r := byteRange(d.Source, lineStart, lineStart+len(line))
				if !ctx.overlapsIgnored(r) {
					emitDiag(emit, r, SeverityInfo, "MD012", "multiple consecutive blank lines")
				}
			}
			advanceLine()
			continue
		}
		blankRun = 0
		if isTableDelimiterRow(line) && !ctx.inTable(lineWholeRange) {
			if next := nextNonBlankLine(lines, i+1); next > 0 && looksLikeTableRow(lines[next-1]) {
				emitDiag(emit, lineWholeRange, SeverityWarning, "MDPP203", "table without header row")
			}
		}
		for _, match := range emStarRe.FindAllStringIndex(line, -1) {
			r := byteRange(d.Source, lineStart+match[0], lineStart+match[1])
			if !ctx.overlapsIgnored(r) {
				emStar = true
				break
			}
		}
		for _, match := range emUnderscoreRe.FindAllStringIndex(line, -1) {
			r := byteRange(d.Source, lineStart+match[0], lineStart+match[1])
			if !ctx.overlapsIgnored(r) {
				emUnderscore = true
				break
			}
		}
		trimmed := strings.TrimLeft(line, " ")
		if len(trimmed) > 1 && strings.Contains("-*+", trimmed[:1]) && trimmed[1] == ' ' {
			markerStart := lineStart + len(line) - len(trimmed)
			if !ctx.overlapsIgnored(byteRange(d.Source, markerStart, markerStart+1)) {
				listMarkers[trimmed[:1]] = true
			}
		}
		if !strings.Contains(line, "http://") && !strings.Contains(line, "https://") {
			advanceLine()
			continue
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
			if ctx.overlapsIgnored(matchRange) {
				continue
			}
			inDestination := ctx.inLinkDestination(matchRange)
			if !inDestination && before == "<" && after == ">" {
				emitDiag(emit, matchRange, SeverityInfo, "MDPP201", "autolink URL should use descriptive link text")
			}
			if before != "<" && before != "(" && !inDestination {
				emitDiag(emit, matchRange, SeverityInfo, "MD034", "bare URL should use explicit link syntax")
			}
		}
		advanceLine()
	}
	if len(listMarkers) > 1 {
		emitDiag(emit, mdpp.Range{StartByte: 0, EndByte: 1, StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 2}, SeverityInfo, "MD004", "unordered list markers are inconsistent")
	}
	if emStar && emUnderscore {
		emitDiag(emit, mdpp.Range{StartByte: 0, EndByte: 1, StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 2}, SeverityInfo, "MD049", "emphasis styles are inconsistent")
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

func (ctx *lintContext) overlapsIgnored(r mdpp.Range) bool {
	if ctx == nil || len(ctx.ignoredRanges) == 0 || r.StartLine == 0 {
		return false
	}
	for _, ignored := range ctx.ignoredRanges {
		if r.StartByte < ignored.EndByte && r.EndByte > ignored.StartByte {
			return true
		}
		if r.StartByte == r.EndByte && r.StartByte >= ignored.StartByte && r.StartByte < ignored.EndByte {
			return true
		}
	}
	return false
}

func (ctx *lintContext) inLinkDestination(r mdpp.Range) bool {
	if ctx == nil {
		return false
	}
	for _, dest := range ctx.linkDestRanges {
		if r.StartByte < dest.EndByte && r.EndByte > dest.StartByte {
			return true
		}
	}
	return false
}

func (ctx *lintContext) inListItem(r mdpp.Range) bool {
	for _, item := range ctx.listItemRanges {
		if r.StartByte >= item.StartByte && r.EndByte <= item.EndByte {
			return true
		}
	}
	return false
}

func (ctx *lintContext) trailingWhitespaceIsRendered(r mdpp.Range) bool {
	for _, text := range ctx.trailingTextRanges {
		if ((r.StartByte < text.EndByte && r.EndByte > text.StartByte) || r.EndByte == text.StartByte) && !ctx.softBreakFollows(text) {
			return true
		}
	}
	if r.StartCol == 1 {
		for _, br := range ctx.softBreakRanges {
			if r.StartByte < br.EndByte && r.EndByte > br.StartByte {
				return true
			}
		}
	}
	return false
}

func (ctx *lintContext) softBreakFollows(text mdpp.Range) bool {
	for _, br := range ctx.softBreakRanges {
		if br.StartByte == text.EndByte {
			return true
		}
	}
	return false
}

func (ctx *lintContext) inTable(r mdpp.Range) bool {
	if ctx == nil || len(ctx.tableRanges) == 0 || r.StartLine == 0 {
		return false
	}
	for _, table := range ctx.tableRanges {
		if r.StartByte >= table.StartByte && r.EndByte <= table.EndByte {
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

func isTableDelimiterRow(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || trimmed == "" || !strings.Contains(trimmed, "|") {
		return false
	}
	cells := 0
	for _, raw := range strings.Split(trimmed, "|") {
		cell := strings.TrimSpace(raw)
		if cell == "" {
			continue
		}
		if len(cell) < 3 {
			return false
		}
		start := 0
		end := len(cell)
		if cell[start] == ':' {
			start++
		}
		if end > start && cell[end-1] == ':' {
			end--
		}
		if end-start < 3 {
			return false
		}
		for i := start; i < end; i++ {
			if cell[i] != '-' {
				return false
			}
		}
		cells++
	}
	return cells > 0
}

func looksLikeTableRow(line string) bool {
	return strings.Contains(line, "|") && !isTableDelimiterRow(line)
}

var (
	emStarRe       = regexp.MustCompile(`(^|[^*])\*[^*\s][^*]*\*`)
	emUnderscoreRe = regexp.MustCompile(`(^|[^_])_[^_\s][^_]*_`)
	bareURLRe      = regexp.MustCompile(`https?://[^\s<>()]+`)
)

func normalizeLabel(label string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.Trim(label, "[]"))), " ")
}

func linkDestinationRange(src []byte, nodeRange mdpp.Range) (mdpp.Range, bool) {
	if nodeRange.StartByte < 0 || nodeRange.EndByte > len(src) || nodeRange.EndByte <= nodeRange.StartByte {
		return mdpp.Range{}, false
	}
	raw := src[nodeRange.StartByte:nodeRange.EndByte]
	closeLabel := strings.Index(string(raw), "](")
	if closeLabel < 0 {
		return mdpp.Range{}, false
	}
	start := closeLabel + 2
	for start < len(raw) && (raw[start] == ' ' || raw[start] == '\t' || raw[start] == '\n' || raw[start] == '\r') {
		start++
	}
	if start >= len(raw) {
		return mdpp.Range{}, false
	}
	if raw[start] == '<' {
		start++
		end := start
		for end < len(raw) && raw[end] != '>' && raw[end] != '\n' {
			if raw[end] == '\\' && end+1 < len(raw) {
				end += 2
				continue
			}
			end++
		}
		if end > start {
			return byteRange(src, nodeRange.StartByte+start, nodeRange.StartByte+end), true
		}
		return mdpp.Range{}, false
	}
	depth := 0
	end := start
	for end < len(raw) {
		ch := raw[end]
		if ch == '\\' && end+1 < len(raw) {
			end += 2
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				if end > start {
					return byteRange(src, nodeRange.StartByte+start, nodeRange.StartByte+end), true
				}
				return mdpp.Range{}, false
			}
			depth--
		case ' ', '\t', '\n', '\r':
			if depth == 0 {
				if end > start {
					return byteRange(src, nodeRange.StartByte+start, nodeRange.StartByte+end), true
				}
				return mdpp.Range{}, false
			}
		}
		end++
	}
	if end > start {
		return byteRange(src, nodeRange.StartByte+start, nodeRange.StartByte+end), true
	}
	return mdpp.Range{}, false
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

package lsp

import (
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/odvcencio/mdpp"
)

var (
	lspFootnoteDefRe = regexp.MustCompile(`(?m)^ {0,3}\[\^([A-Za-z0-9_-]+)\]:`)
	lspFootnoteRefRe = regexp.MustCompile(`\[\^([A-Za-z0-9_-]+)\]`)
	lspLinkDefRe     = regexp.MustCompile(`(?m)^ {0,3}\[([^\]\^][^\]]*)\]:`)
)

func (s *Server) definition(params DefinitionParams) ([]Location, error) {
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	doc, source, index, _, offset, path, err := documentPositionContext(open, params.Position)
	if err != nil {
		return nil, err
	}
	for i := len(path) - 1; i >= 0; i-- {
		n := path[i]
		switch n.Type {
		case mdpp.NodeFootnoteRef:
			if def := findFootnoteDef(doc.Root, n.Attr("id")); def != nil {
				return []Location{nodeLocation(open.URI, index, def)}, nil
			}
		case mdpp.NodeLink:
			if loc, ok := linkDefinitionLocation(open.URI, doc, source, index, n); ok {
				return []Location{loc}, nil
			}
		}
	}
	if defRange, ok := definitionAtSourceOffset(source, offset); ok {
		return []Location{{URI: open.URI, Range: index.RangeToLSP(defRange)}}, nil
	}
	return nil, nil
}

func (s *Server) references(params ReferenceParams) ([]Location, error) {
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	doc, source, index, _, offset, path, err := documentPositionContext(open, params.Position)
	if err != nil {
		return nil, err
	}
	var locs []Location
	for i := len(path) - 1; i >= 0; i-- {
		n := path[i]
		switch n.Type {
		case mdpp.NodeHeading:
			id := mdpp.Slugify(n.Text())
			if params.Context.IncludeDeclaration {
				locs = append(locs, nodeLocation(open.URI, index, n))
			}
			locs = append(locs, anchorReferenceLocations(open.URI, source, index, id)...)
			return dedupeLocations(locs), nil
		case mdpp.NodeLink:
			if href := n.Attr("href"); strings.HasPrefix(href, "#") {
				id := strings.TrimPrefix(href, "#")
				if params.Context.IncludeDeclaration {
					if h := findHeadingByID(doc.Root, id); h != nil {
						locs = append(locs, nodeLocation(open.URI, index, h))
					}
				}
				locs = append(locs, anchorReferenceLocations(open.URI, source, index, id)...)
				return dedupeLocations(locs), nil
			}
			if label := referenceLabelForNode(source, n); label != "" {
				return linkReferenceLocations(open.URI, source, index, label, params.Context.IncludeDeclaration), nil
			}
		case mdpp.NodeFootnoteRef, mdpp.NodeFootnoteDef:
			id := n.Attr("id")
			return footnoteReferenceLocations(open.URI, source, index, id, params.Context.IncludeDeclaration), nil
		}
	}
	if label, ok := linkDefinitionLabelAtOffset(source, offset); ok {
		return linkReferenceLocations(open.URI, source, index, label, params.Context.IncludeDeclaration), nil
	}
	if id, ok := footnoteIDAtOffset(source, offset); ok {
		return footnoteReferenceLocations(open.URI, source, index, id, params.Context.IncludeDeclaration), nil
	}
	return nil, nil
}

func (s *Server) foldingRanges(params FoldingRangeParams) []FoldingRange {
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return nil
	}
	doc, _, index, _ := open.Snapshot()
	var out []FoldingRange
	addNodeFold := func(n *mdpp.Node, kind string) {
		if fr, ok := foldingRangeForNode(index, n, kind); ok {
			out = append(out, fr)
		}
	}
	doc.Root.Walk(func(n *mdpp.Node) bool {
		switch n.Type {
		case mdpp.NodeCodeBlock:
			addNodeFold(n, "region")
			return false
		case mdpp.NodeFrontmatter:
			addNodeFold(n, "comment")
			return false
		case mdpp.NodeContainerDirective, mdpp.NodeAdmonition, mdpp.NodeBlockquote, mdpp.NodeList, mdpp.NodeTable, mdpp.NodeFootnoteDef:
			addNodeFold(n, "region")
		}
		return true
	})
	out = append(out, headingFoldingRanges(doc, index)...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartLine == out[j].StartLine {
			return out[i].EndLine < out[j].EndLine
		}
		return out[i].StartLine < out[j].StartLine
	})
	return dedupeFoldingRanges(out)
}

func (s *Server) documentSymbols(params DocumentSymbolParams) []DocumentSymbol {
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return nil
	}
	doc, _, index, _ := open.Snapshot()
	if doc == nil || doc.Root == nil {
		return nil
	}
	var out []DocumentSymbol
	if fm := firstNodeOfType(doc.Root, mdpp.NodeFrontmatter); fm != nil {
		out = append(out, DocumentSymbol{
			Name:           "frontmatter",
			Kind:           symbolKindFile,
			Range:          index.RangeToLSP(fm.Range),
			SelectionRange: index.RangeToLSP(fm.Range),
		})
	}

	var headingStack []*DocumentSymbol
	for _, child := range doc.Root.Children {
		appendStructuralSymbols(child, index, &out, &headingStack)
	}
	return out
}

func (s *Server) prepareRename(params TextDocumentPositionParams) (*Range, error) {
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	_, source, index, _, offset, path, err := documentPositionContext(open, params.Position)
	if err != nil {
		return nil, err
	}
	if target, ok := renameTarget(source, index, offset, path); ok {
		r := index.RangeToLSP(target.selection)
		return &r, nil
	}
	return nil, nil
}

func (s *Server) rename(params RenameParams) (*WorkspaceEdit, error) {
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return &WorkspaceEdit{}, nil
	}
	doc, source, index, _, offset, path, err := documentPositionContext(open, params.Position)
	if err != nil {
		return nil, err
	}
	target, ok := renameTarget(source, index, offset, path)
	if !ok {
		return &WorkspaceEdit{}, nil
	}
	var edits []TextEdit
	switch target.kind {
	case "heading":
		oldSlug := mdpp.Slugify(target.oldText)
		newSlug := mdpp.Slugify(params.NewName)
		edits = append(edits, TextEdit{Range: index.RangeToLSP(target.selection), NewText: params.NewName})
		for _, loc := range anchorReferenceLocations(open.URI, source, index, oldSlug) {
			edits = append(edits, TextEdit{Range: loc.Range, NewText: "#" + newSlug})
		}
	case "heading-link":
		oldSlug := strings.TrimPrefix(target.oldText, "#")
		newSlug := mdpp.Slugify(params.NewName)
		if h := findHeadingByID(doc.Root, oldSlug); h != nil {
			if start, end, ok := headingTextRange(source, h.Range); ok {
				edits = append(edits, TextEdit{Range: index.RangeToLSP(lspSourceRange(source, start, end)), NewText: params.NewName})
			}
		}
		for _, loc := range anchorReferenceLocations(open.URI, source, index, oldSlug) {
			edits = append(edits, TextEdit{Range: loc.Range, NewText: "#" + newSlug})
		}
	case "footnote":
		for _, r := range footnoteIDRanges(source, target.oldText) {
			edits = append(edits, TextEdit{Range: index.RangeToLSP(r), NewText: params.NewName})
		}
	case "link-ref":
		for _, r := range linkReferenceLabelRanges(source, target.oldText, true) {
			edits = append(edits, TextEdit{Range: index.RangeToLSP(r), NewText: params.NewName})
		}
	}
	return &WorkspaceEdit{Changes: map[DocumentURI][]TextEdit{open.URI: dedupeTextEdits(edits)}}, nil
}

type renameTargetInfo struct {
	kind      string
	oldText   string
	selection mdpp.Range
}

func renameTarget(source []byte, index *LineIndex, offset int, path []*mdpp.Node) (renameTargetInfo, bool) {
	for i := len(path) - 1; i >= 0; i-- {
		n := path[i]
		switch n.Type {
		case mdpp.NodeHeading:
			start, end, ok := headingTextRange(source, n.Range)
			if !ok {
				return renameTargetInfo{}, false
			}
			return renameTargetInfo{kind: "heading", oldText: n.Text(), selection: lspSourceRange(source, start, end)}, true
		case mdpp.NodeLink:
			if href := n.Attr("href"); strings.HasPrefix(href, "#") {
				return renameTargetInfo{kind: "heading-link", oldText: href, selection: n.Range}, true
			}
			if label := referenceLabelForNode(source, n); label != "" {
				return renameTargetInfo{kind: "link-ref", oldText: label, selection: n.Range}, true
			}
		case mdpp.NodeFootnoteRef, mdpp.NodeFootnoteDef:
			id := n.Attr("id")
			return renameTargetInfo{kind: "footnote", oldText: id, selection: footnoteIDRangeAtOffset(source, offset, id)}, true
		}
	}
	if label, ok := linkDefinitionLabelAtOffset(source, offset); ok {
		return renameTargetInfo{kind: "link-ref", oldText: label, selection: linkDefinitionLabelRangeAtOffset(source, offset)}, true
	}
	if id, ok := footnoteIDAtOffset(source, offset); ok {
		return renameTargetInfo{kind: "footnote", oldText: id, selection: footnoteIDRangeAtOffset(source, offset, id)}, true
	}
	_ = index
	return renameTargetInfo{}, false
}

func appendStructuralSymbols(n *mdpp.Node, index *LineIndex, out *[]DocumentSymbol, headingStack *[]*DocumentSymbol) {
	if n == nil || n.Range.StartLine == 0 {
		return
	}
	switch n.Type {
	case mdpp.NodeHeading:
		sym := DocumentSymbol{
			Name:           n.Text(),
			Detail:         "h" + strconvItoa(n.Level()),
			Kind:           symbolKindNamespace,
			Range:          index.RangeToLSP(n.Range),
			SelectionRange: index.RangeToLSP(n.Range),
		}
		level := n.Level()
		for len(*headingStack) >= level && len(*headingStack) > 0 {
			*headingStack = (*headingStack)[:len(*headingStack)-1]
		}
		if len(*headingStack) == 0 {
			*out = append(*out, sym)
			*headingStack = append(*headingStack, &(*out)[len(*out)-1])
			return
		}
		parent := (*headingStack)[len(*headingStack)-1]
		parent.Children = append(parent.Children, sym)
		*headingStack = append(*headingStack, &parent.Children[len(parent.Children)-1])
		return
	case mdpp.NodeContainerDirective:
		addFlatSymbol(out, index, n, ":::"+n.Attr("name"), n.Attr("title"), symbolKindClass)
	case mdpp.NodeFootnoteDef:
		addFlatSymbol(out, index, n, "[^"+n.Attr("id")+"]", "footnote", symbolKindField)
	}
	for _, child := range n.Children {
		appendStructuralSymbols(child, index, out, headingStack)
	}
}

func addFlatSymbol(out *[]DocumentSymbol, index *LineIndex, n *mdpp.Node, name string, detail string, kind int) {
	*out = append(*out, DocumentSymbol{
		Name:           name,
		Detail:         detail,
		Kind:           kind,
		Range:          index.RangeToLSP(n.Range),
		SelectionRange: index.RangeToLSP(n.Range),
	})
}

func documentPositionContext(open *OpenDocument, pos Position) (*mdpp.Document, []byte, *LineIndex, int32, int, []*mdpp.Node, error) {
	doc, source, index, version := open.Snapshot()
	offset, ok := index.PositionToOffset(pos)
	if !ok {
		return nil, nil, nil, 0, 0, nil, errors.New("position is outside the document")
	}
	path := nodePathAt(doc.Root, offset, len(source))
	return doc, source, index, version, offset, path, nil
}

func nodeLocation(uri DocumentURI, index *LineIndex, n *mdpp.Node) Location {
	return Location{URI: uri, Range: index.RangeToLSP(n.Range)}
}

func linkDefinitionLocation(uri DocumentURI, doc *mdpp.Document, source []byte, index *LineIndex, n *mdpp.Node) (Location, bool) {
	if href := n.Attr("href"); strings.HasPrefix(href, "#") {
		if h := findHeadingByID(doc.Root, strings.TrimPrefix(href, "#")); h != nil {
			return nodeLocation(uri, index, h), true
		}
	}
	label := referenceLabelForNode(source, n)
	if label == "" {
		return Location{}, false
	}
	defs := linkDefinitionRanges(source)
	r, ok := defs[normalizeLSPLabel(label)]
	if !ok {
		return Location{}, false
	}
	return Location{URI: uri, Range: index.RangeToLSP(r)}, true
}

func referenceLabelForNode(source []byte, n *mdpp.Node) string {
	if n == nil || n.Type != mdpp.NodeLink {
		return ""
	}
	if ref := n.Attr("ref"); ref != "" {
		return ref
	}
	start, end, ok := rangeBounds(source, n.Range)
	if !ok {
		return ""
	}
	raw := string(source[start:end])
	if strings.Contains(raw, "](") || strings.HasPrefix(raw, "<") {
		return ""
	}
	if loc := regexp.MustCompile(`^\[[^\]\n]+\]\[([^\]\n]*)\]`).FindStringSubmatch(raw); loc != nil {
		if loc[1] != "" {
			return loc[1]
		}
		return n.Text()
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		return strings.Trim(raw, "[]")
	}
	return ""
}

func linkDefinitionRanges(source []byte) map[string]mdpp.Range {
	out := map[string]mdpp.Range{}
	for _, loc := range lspLinkDefRe.FindAllSubmatchIndex(source, -1) {
		if len(loc) < 4 || loc[2] < 0 {
			continue
		}
		label := normalizeLSPLabel(string(source[loc[2]:loc[3]]))
		out[label] = lspSourceRange(source, loc[0], loc[1])
	}
	return out
}

func anchorReferenceLocations(uri DocumentURI, source []byte, index *LineIndex, id string) []Location {
	if id == "" {
		return nil
	}
	re := regexp.MustCompile(`\(#` + regexp.QuoteMeta(id) + `\)`)
	var out []Location
	for _, loc := range re.FindAllIndex(source, -1) {
		out = append(out, Location{URI: uri, Range: index.RangeToLSP(lspSourceRange(source, loc[0]+1, loc[1]-1))})
	}
	return out
}

func linkReferenceLocations(uri DocumentURI, source []byte, index *LineIndex, label string, includeDeclaration bool) []Location {
	key := normalizeLSPLabel(label)
	var out []Location
	if includeDeclaration {
		if r, ok := linkDefinitionRanges(source)[key]; ok {
			out = append(out, Location{URI: uri, Range: index.RangeToLSP(r)})
		}
	}
	re := regexp.MustCompile(`\[[^\]\n]+\]\[([^\]\n]*)\]|\[([^\]\n]+)\]`)
	for _, loc := range re.FindAllSubmatchIndex(source, -1) {
		if len(loc) < 6 {
			continue
		}
		raw := string(source[loc[0]:loc[1]])
		if strings.Contains(raw, "](") || strings.HasPrefix(raw, "![") {
			continue
		}
		useLabel := ""
		start, end := loc[0], loc[1]
		if loc[2] >= 0 {
			useLabel = string(source[loc[2]:loc[3]])
			if useLabel == "" {
				useLabel = strings.Trim(raw[:strings.Index(raw, "]")+1], "[]")
			}
			start, end = loc[2], loc[3]
			if start == end {
				start, end = loc[0]+1, loc[0]+1+len(useLabel)
			}
		} else if loc[4] >= 0 {
			useLabel = string(source[loc[4]:loc[5]])
			start, end = loc[4], loc[5]
		}
		if normalizeLSPLabel(useLabel) == key {
			out = append(out, Location{URI: uri, Range: index.RangeToLSP(lspSourceRange(source, start, end))})
		}
	}
	return dedupeLocations(out)
}

func footnoteReferenceLocations(uri DocumentURI, source []byte, index *LineIndex, id string, includeDeclaration bool) []Location {
	var out []Location
	for _, loc := range lspFootnoteRefRe.FindAllSubmatchIndex(source, -1) {
		if len(loc) < 4 || loc[2] < 0 || string(source[loc[2]:loc[3]]) != id {
			continue
		}
		isDef := loc[1] < len(source) && source[loc[1]] == ':'
		if isDef && !includeDeclaration {
			continue
		}
		out = append(out, Location{URI: uri, Range: index.RangeToLSP(lspSourceRange(source, loc[0], loc[1]))})
	}
	return out
}

func definitionAtSourceOffset(source []byte, offset int) (mdpp.Range, bool) {
	for _, loc := range lspLinkDefRe.FindAllSubmatchIndex(source, -1) {
		if offset >= loc[0] && offset <= loc[1] {
			return lspSourceRange(source, loc[0], loc[1]), true
		}
	}
	for _, loc := range lspFootnoteDefRe.FindAllSubmatchIndex(source, -1) {
		if offset >= loc[0] && offset <= loc[1] {
			return lspSourceRange(source, loc[0], loc[1]), true
		}
	}
	return mdpp.Range{}, false
}

func linkDefinitionLabelAtOffset(source []byte, offset int) (string, bool) {
	for _, loc := range lspLinkDefRe.FindAllSubmatchIndex(source, -1) {
		if len(loc) >= 4 && offset >= loc[0] && offset <= loc[1] {
			return string(source[loc[2]:loc[3]]), true
		}
	}
	return "", false
}

func footnoteIDAtOffset(source []byte, offset int) (string, bool) {
	for _, loc := range lspFootnoteRefRe.FindAllSubmatchIndex(source, -1) {
		if len(loc) >= 4 && offset >= loc[0] && offset <= loc[1] {
			return string(source[loc[2]:loc[3]]), true
		}
	}
	return "", false
}

func footnoteIDRanges(source []byte, id string) []mdpp.Range {
	var out []mdpp.Range
	for _, loc := range lspFootnoteRefRe.FindAllSubmatchIndex(source, -1) {
		if len(loc) >= 4 && loc[2] >= 0 && string(source[loc[2]:loc[3]]) == id {
			out = append(out, lspSourceRange(source, loc[2], loc[3]))
		}
	}
	return out
}

func footnoteIDRangeAtOffset(source []byte, offset int, id string) mdpp.Range {
	for _, r := range footnoteIDRanges(source, id) {
		if offset >= r.StartByte && offset <= r.EndByte+2 {
			return r
		}
	}
	return mdpp.Range{}
}

func linkReferenceLabelRanges(source []byte, label string, includeDeclaration bool) []mdpp.Range {
	key := normalizeLSPLabel(label)
	var out []mdpp.Range
	if includeDeclaration {
		for _, loc := range lspLinkDefRe.FindAllSubmatchIndex(source, -1) {
			if len(loc) >= 4 && normalizeLSPLabel(string(source[loc[2]:loc[3]])) == key {
				out = append(out, lspSourceRange(source, loc[2], loc[3]))
			}
		}
	}
	re := regexp.MustCompile(`\[[^\]\n]+\]\[([^\]\n]*)\]|\[([^\]\n]+)\]`)
	for _, loc := range re.FindAllSubmatchIndex(source, -1) {
		if len(loc) < 6 {
			continue
		}
		raw := string(source[loc[0]:loc[1]])
		if strings.Contains(raw, "](") || strings.HasPrefix(raw, "![") {
			continue
		}
		useLabel := ""
		start, end := loc[0], loc[1]
		if loc[2] >= 0 {
			useLabel = string(source[loc[2]:loc[3]])
			if useLabel == "" {
				useLabel = strings.Trim(raw[:strings.Index(raw, "]")+1], "[]")
				start, end = loc[0]+1, loc[0]+1+len(useLabel)
			} else {
				start, end = loc[2], loc[3]
			}
		} else if loc[4] >= 0 {
			useLabel = string(source[loc[4]:loc[5]])
			start, end = loc[4], loc[5]
		}
		if normalizeLSPLabel(useLabel) == key {
			out = append(out, lspSourceRange(source, start, end))
		}
	}
	return out
}

func linkDefinitionLabelRangeAtOffset(source []byte, offset int) mdpp.Range {
	for _, loc := range lspLinkDefRe.FindAllSubmatchIndex(source, -1) {
		if len(loc) >= 4 && offset >= loc[0] && offset <= loc[1] {
			return lspSourceRange(source, loc[2], loc[3])
		}
	}
	return mdpp.Range{}
}

func dedupeTextEdits(in []TextEdit) []TextEdit {
	out := make([]TextEdit, 0, len(in))
	seen := map[string]bool{}
	for _, edit := range in {
		key := strconvItoa(int(edit.Range.Start.Line)) + ":" + strconvItoa(int(edit.Range.Start.Character)) + ":" + strconvItoa(int(edit.Range.End.Line)) + ":" + strconvItoa(int(edit.Range.End.Character)) + ":" + edit.NewText
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, edit)
	}
	return out
}

func normalizeLSPLabel(label string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.Trim(label, "[]"))), " ")
}

func lspSourceRange(source []byte, start int, end int) mdpp.Range {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > len(source) {
		end = len(source)
	}
	lineCol := func(offset int) (int, int) {
		line, col := 1, 1
		for i := 0; i < offset && i < len(source); i++ {
			if source[i] == '\n' {
				line++
				col = 1
				continue
			}
			col++
		}
		return line, col
	}
	startLine, startCol := lineCol(start)
	endLine, endCol := lineCol(end)
	return mdpp.Range{StartByte: start, EndByte: end, StartLine: startLine, StartCol: startCol, EndLine: endLine, EndCol: endCol}
}

func foldingRangeForNode(index *LineIndex, n *mdpp.Node, kind string) (FoldingRange, bool) {
	r := index.RangeToLSP(n.Range)
	endLine := r.End.Line
	if endLine > 0 && r.End.Character == 0 {
		endLine--
	}
	if endLine <= r.Start.Line {
		return FoldingRange{}, false
	}
	return FoldingRange{StartLine: r.Start.Line, EndLine: endLine, Kind: kind}, true
}

func headingFoldingRanges(doc *mdpp.Document, index *LineIndex) []FoldingRange {
	if doc == nil || doc.Root == nil {
		return nil
	}
	headings := doc.Root.Find(mdpp.NodeHeading)
	var out []FoldingRange
	for i, h := range headings {
		start := index.RangeToLSP(h.Range).Start.Line
		endLine := uint32(len(index.lineStarts) - 1)
		for j := i + 1; j < len(headings); j++ {
			if headings[j].Level() <= h.Level() {
				next := index.RangeToLSP(headings[j].Range).Start.Line
				if next > 0 {
					endLine = next - 1
				}
				break
			}
		}
		if endLine > start {
			out = append(out, FoldingRange{StartLine: start, EndLine: endLine, Kind: "region"})
		}
	}
	return out
}

func dedupeFoldingRanges(in []FoldingRange) []FoldingRange {
	out := make([]FoldingRange, 0, len(in))
	seen := map[[2]uint32]bool{}
	for _, fr := range in {
		key := [2]uint32{fr.StartLine, fr.EndLine}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, fr)
	}
	return out
}

func dedupeLocations(in []Location) []Location {
	out := make([]Location, 0, len(in))
	seen := map[string]bool{}
	for _, loc := range in {
		key := string(loc.URI) + ":" + strconvItoa(int(loc.Range.Start.Line)) + ":" + strconvItoa(int(loc.Range.Start.Character)) + ":" + strconvItoa(int(loc.Range.End.Line)) + ":" + strconvItoa(int(loc.Range.End.Character))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, loc)
	}
	return out
}

func firstNodeOfType(root *mdpp.Node, typ mdpp.NodeType) *mdpp.Node {
	var out *mdpp.Node
	root.Walk(func(n *mdpp.Node) bool {
		if n.Type == typ {
			out = n
			return false
		}
		return true
	})
	return out
}

func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

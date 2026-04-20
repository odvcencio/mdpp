package lsp

import (
	"regexp"
	"sort"
	"strings"

	"github.com/odvcencio/mdpp"
)

type semanticToken struct {
	start     int
	end       int
	tokenType int
	modifiers uint32
}

var (
	containerOpenRe = regexp.MustCompile(`(?m)^([ \t]*:{3,}[ \t]*)([A-Za-z][A-Za-z0-9_-]*)`)
	admonitionRe    = regexp.MustCompile(`\[!([A-Za-z][A-Za-z0-9_-]*)\]`)
	frontmatterRe   = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_-]*)(\s*:)`)
)

func (s *Server) semanticTokensFull(params SemanticTokensParams) (*SemanticTokens, error) {
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return &SemanticTokens{}, nil
	}
	doc, source, index, _ := open.Snapshot()
	return &SemanticTokens{Data: encodeSemanticTokens(collectSemanticTokens(doc, source, index, nil), index)}, nil
}

func (s *Server) semanticTokensRange(params SemanticTokensRangeParams) (*SemanticTokens, error) {
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return &SemanticTokens{}, nil
	}
	doc, source, index, _ := open.Snapshot()
	start, ok := index.PositionToOffset(params.Range.Start)
	if !ok {
		start = 0
	}
	end, ok := index.PositionToOffset(params.Range.End)
	if !ok {
		end = len(source)
	}
	filter := &byteRange{start: start, end: end}
	return &SemanticTokens{Data: encodeSemanticTokens(collectSemanticTokens(doc, source, index, filter), index)}, nil
}

type byteRange struct {
	start int
	end   int
}

func collectSemanticTokens(doc *mdpp.Document, source []byte, index *LineIndex, filter *byteRange) []semanticToken {
	if doc == nil || doc.Root == nil || index == nil {
		return nil
	}
	var tokens []semanticToken
	add := func(start, end int, typ int, mods uint32) {
		if start < 0 {
			start = 0
		}
		if end > len(source) {
			end = len(source)
		}
		if end <= start {
			return
		}
		if filter != nil && (end <= filter.start || start >= filter.end) {
			return
		}
		if filter != nil {
			if start < filter.start {
				start = filter.start
			}
			if end > filter.end {
				end = filter.end
			}
		}
		for start < end {
			lineEnd := index.LineContentEndForOffset(start)
			if lineEnd <= start {
				next := index.NextLineStartAfterOffset(start)
				if next <= start {
					return
				}
				start = next
				continue
			}
			pieceEnd := end
			if pieceEnd > lineEnd {
				pieceEnd = lineEnd
			}
			if pieceEnd > start {
				tokens = append(tokens, semanticToken{start: start, end: pieceEnd, tokenType: typ, modifiers: mods})
			}
			if pieceEnd >= end {
				break
			}
			next := index.NextLineStartAfterOffset(pieceEnd)
			if next <= pieceEnd {
				break
			}
			start = next
		}
	}

	var walk func(*mdpp.Node)
	walk = func(n *mdpp.Node) {
		if n == nil {
			return
		}
		switch n.Type {
		case mdpp.NodeHeading:
			if start, end, ok := headingTextRange(source, n.Range); ok {
				add(start, end, tokenHeading, headingLevelModifier(n.Level()))
			}
			return
		case mdpp.NodeTableOfContents:
			add(n.Range.StartByte, n.Range.EndByte, tokenDirective, modifierBit(tokenModTOC))
			return
		case mdpp.NodeAutoEmbed:
			emitEmbedDirective(source, n.Range, add)
			return
		case mdpp.NodeContainerDirective:
			if start, end, ok := containerNameRange(source, n.Range); ok {
				add(start, end, tokenContainerType, 0)
			}
		case mdpp.NodeAdmonition:
			if start, end, ok := admonitionTypeRange(source, n.Range); ok {
				add(start, end, tokenAdmonitionType, 0)
			}
		case mdpp.NodeFootnoteRef:
			add(n.Range.StartByte, n.Range.EndByte, tokenFootnote, modifierBit(tokenModReference))
			return
		case mdpp.NodeFootnoteDef:
			add(n.Range.StartByte, minNonNegative(n.Range.StartByte+len("[^"+n.Attr("id")+"]"), n.Range.EndByte), tokenFootnote, modifierBit(tokenModDefinition))
		case mdpp.NodeLink:
			mods := uint32(0)
			if n.Attr("ref") != "" {
				mods |= modifierBit(tokenModReference)
			} else {
				mods |= modifierBit(tokenModInline)
			}
			if n.Attr("href") != "" {
				mods |= modifierBit(tokenModResolved)
			} else if n.Attr("ref") != "" {
				mods |= modifierBit(tokenModBroken)
			}
			add(n.Range.StartByte, n.Range.EndByte, tokenLink, mods)
			return
		case mdpp.NodeImage:
			add(n.Range.StartByte, n.Range.EndByte, tokenImageAlt, 0)
			return
		case mdpp.NodeMathInline:
			add(n.Range.StartByte, n.Range.EndByte, tokenMath, modifierBit(tokenModInline))
			return
		case mdpp.NodeMathBlock:
			add(n.Range.StartByte, n.Range.EndByte, tokenMath, modifierBit(tokenModDisplay))
			return
		case mdpp.NodeEmoji:
			add(n.Range.StartByte, n.Range.EndByte, tokenEmojiShortcode, 0)
			return
		case mdpp.NodeStrikethrough:
			add(n.Range.StartByte, n.Range.EndByte, tokenStrikethrough, 0)
			return
		case mdpp.NodeEmphasis:
			add(n.Range.StartByte, n.Range.EndByte, tokenEmphasis, modifierBit(tokenModItalic))
			return
		case mdpp.NodeStrong:
			add(n.Range.StartByte, n.Range.EndByte, tokenEmphasis, modifierBit(tokenModBold))
			return
		case mdpp.NodeTaskListItem:
			if start, end, ok := taskMarkerRange(source, n.Range); ok {
				mod := tokenModUnchecked
				raw := strings.ToLower(string(source[start:end]))
				if strings.Contains(raw, "x") {
					mod = tokenModChecked
				}
				add(start, end, tokenTaskMarker, modifierBit(mod))
			}
		case mdpp.NodeDefinitionTerm:
			add(n.Range.StartByte, n.Range.EndByte, tokenDefinitionTerm, 0)
			return
		case mdpp.NodeFrontmatter:
			emitFrontmatter(source, n.Range, add)
			return
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(doc.Root)
	sort.SliceStable(tokens, func(i, j int) bool {
		if tokens[i].start == tokens[j].start {
			return tokens[i].end < tokens[j].end
		}
		return tokens[i].start < tokens[j].start
	})
	return removeOverlappingSemanticTokens(tokens)
}

func encodeSemanticTokens(tokens []semanticToken, index *LineIndex) []uint32 {
	data := make([]uint32, 0, len(tokens)*5)
	var prevLine uint32
	var prevChar uint32
	for _, tok := range tokens {
		start := index.OffsetToPosition(tok.start)
		length := index.UTF16Length(tok.start, tok.end)
		if length == 0 {
			continue
		}
		deltaLine := start.Line - prevLine
		deltaChar := start.Character
		if deltaLine == 0 {
			deltaChar = start.Character - prevChar
		}
		data = append(data, deltaLine, deltaChar, length, uint32(tok.tokenType), tok.modifiers)
		prevLine = start.Line
		prevChar = start.Character
	}
	return data
}

func removeOverlappingSemanticTokens(in []semanticToken) []semanticToken {
	out := make([]semanticToken, 0, len(in))
	lastEnd := -1
	for _, tok := range in {
		if tok.start < lastEnd {
			continue
		}
		out = append(out, tok)
		if tok.end > lastEnd {
			lastEnd = tok.end
		}
	}
	return out
}

func headingTextRange(source []byte, r mdpp.Range) (int, int, bool) {
	start, end, ok := rangeBounds(source, r)
	if !ok {
		return 0, 0, false
	}
	lineEnd := start
	for lineEnd < end && source[lineEnd] != '\n' {
		lineEnd++
	}
	i := start
	for i < lineEnd && (source[i] == ' ' || source[i] == '\t') {
		i++
	}
	for i < lineEnd && source[i] == '#' {
		i++
	}
	for i < lineEnd && (source[i] == ' ' || source[i] == '\t') {
		i++
	}
	j := lineEnd
	for j > i && (source[j-1] == ' ' || source[j-1] == '\t' || source[j-1] == '#') {
		j--
	}
	return i, j, j > i
}

func containerNameRange(source []byte, r mdpp.Range) (int, int, bool) {
	start, end, ok := rangeBounds(source, r)
	if !ok {
		return 0, 0, false
	}
	lineEnd := start
	for lineEnd < end && source[lineEnd] != '\n' {
		lineEnd++
	}
	loc := containerOpenRe.FindSubmatchIndex(source[start:lineEnd])
	if loc == nil || len(loc) < 6 || loc[4] < 0 {
		return 0, 0, false
	}
	return start + loc[4], start + loc[5], true
}

func admonitionTypeRange(source []byte, r mdpp.Range) (int, int, bool) {
	start, end, ok := rangeBounds(source, r)
	if !ok {
		return 0, 0, false
	}
	loc := admonitionRe.FindSubmatchIndex(source[start:end])
	if loc == nil || len(loc) < 4 || loc[2] < 0 {
		return 0, 0, false
	}
	return start + loc[2], start + loc[3], true
}

func taskMarkerRange(source []byte, r mdpp.Range) (int, int, bool) {
	start, end, ok := rangeBounds(source, r)
	if !ok {
		return 0, 0, false
	}
	lineEnd := start
	for lineEnd < end && source[lineEnd] != '\n' {
		lineEnd++
	}
	line := source[start:lineEnd]
	for _, marker := range [][]byte{[]byte("[ ]"), []byte("[x]"), []byte("[X]")} {
		if loc := indexBytes(line, marker); loc >= 0 {
			return start + loc, start + loc + len(marker), true
		}
	}
	return 0, 0, false
}

func emitEmbedDirective(source []byte, r mdpp.Range, add func(int, int, int, uint32)) {
	start, end, ok := rangeBounds(source, r)
	if !ok {
		return
	}
	raw := source[start:end]
	lower := strings.ToLower(string(raw))
	head := strings.Index(lower, "[[embed:")
	if head < 0 {
		return
	}
	headStart := start + head
	argStart := headStart + len("[[embed:")
	closeRel := strings.LastIndex(string(raw), "]]")
	argEnd := end
	if closeRel >= 0 {
		argEnd = start + closeRel
	}
	add(headStart, argStart, tokenDirective, modifierBit(tokenModEmbed))
	add(argStart, argEnd, tokenDirectiveArgument, modifierBit(tokenModEmbed))
}

func emitFrontmatter(source []byte, r mdpp.Range, add func(int, int, int, uint32)) {
	start, end, ok := rangeBounds(source, r)
	if !ok {
		return
	}
	for _, loc := range frontmatterRe.FindAllSubmatchIndex(source[start:end], -1) {
		if len(loc) < 4 || loc[2] < 0 {
			continue
		}
		add(start+loc[2], start+loc[3], tokenFrontmatterKey, 0)
	}
}

func rangeBounds(source []byte, r mdpp.Range) (int, int, bool) {
	start := r.StartByte
	end := r.EndByte
	if start < 0 {
		start = 0
	}
	if end > len(source) {
		end = len(source)
	}
	return start, end, end > start && start <= len(source)
}

func headingLevelModifier(level int) uint32 {
	switch level {
	case 1:
		return modifierBit(tokenModLevel1)
	case 2:
		return modifierBit(tokenModLevel2)
	case 3:
		return modifierBit(tokenModLevel3)
	case 4:
		return modifierBit(tokenModLevel4)
	case 5:
		return modifierBit(tokenModLevel5)
	case 6:
		return modifierBit(tokenModLevel6)
	default:
		return 0
	}
}

func modifierBit(mod int) uint32 {
	return 1 << uint(mod)
}

func indexBytes(haystack []byte, needle []byte) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return i
		}
	}
	return -1
}

func minNonNegative(a, b int) int {
	if a < 0 {
		return b
	}
	if b < 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

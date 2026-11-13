package mdpp

import (
	"strings"
	"unicode/utf8"
)

type commentDelimiterPair struct {
	openStart, openEnd, closeStart, closeEnd int
	strong                                   bool
	marker                                   byte
}

func parseCommentDelimitersAt(text string, source []byte, baseOffset int, comments, links [][2]int, ctx *parseCtx) ([]*Node, bool) {
	if len(text) == 0 || !strings.ContainsAny(text, "*_~") {
		return nil, false
	}
	checks := 0
	cancelled := func(_ int) bool {
		checks++
		if checks&4095 != 1 || ctx == nil || ctx.budget == nil {
			return false
		}
		if ctx.budget.check() {
			return false
		}
		ctx.inlineTimeoutOccurred = true
		return true
	}
	abort := func() ([]*Node, bool) {
		return []*Node{textNodeRange(text, inlineSpanRange(source, baseOffset, 0, len(text)))}, true
	}
	// Inline atoms must not contribute delimiter runs. Parse each atom with
	// the normal reader when reconstructing the result.
	atomEnd := make(map[int]int)
	commentAt := make(map[int]int)
	literalAt := make(map[int]bool)
	referenceSuffix := make(map[int]string)
	for _, span := range comments {
		commentAt[span[0]] = span[1]
	}
	for _, span := range links {
		if span[1] > atomEnd[span[0]] {
			atomEnd[span[0]] = span[1]
		}
	}
	scanner := &commentScan{text: text, ctx: ctx}
	scanner.indexBackticks()
	if scanner.cancelled {
		return abort()
	}
	// Pair brackets once, skipping already recognized inline atoms. The normal
	// bracket reader still constructs reference links and their attributes.
	closes := make(map[int]int)
	var brackets []int
	for i := 0; i < len(text); {
		if cancelled(i) {
			return abort()
		}
		if end := atomEnd[i]; end > i {
			i = end
			continue
		}
		if end := commentAt[i]; end > i {
			i = end
			continue
		}
		if text[i] == '\\' && i+1 < len(text) {
			i += 2
			continue
		}
		if text[i] == '`' {
			end := i + 1
			for end < len(text) && text[end] == '`' {
				end++
			}
			close := scanner.nextRun(end-i, end)
			if close >= 0 {
				i = close + end - i
				continue
			}
			i = end
			continue
		}
		if text[i] == '<' {
			if end := scanner.otherHTMLEnd(i); end > i {
				i = end
				continue
			}
		}
		if text[i] == '[' {
			brackets = append(brackets, i)
		} else if text[i] == ']' && len(brackets) > 0 {
			open := brackets[len(brackets)-1]
			brackets = brackets[:len(brackets)-1]
			closes[open] = i
		}
		i++
	}
	for i := 0; i < len(text); {
		if cancelled(i) {
			return abort()
		}
		if end := atomEnd[i]; end > i {
			i = end
			continue
		}
		if end := commentAt[i]; end > i {
			atomEnd[i] = end
			i = end
			continue
		}
		switch text[i] {
		case '`':
			end := i + 1
			for end < len(text) && text[end] == '`' {
				end++
			}
			close := scanner.nextRun(end-i, end)
			if close < 0 {
				atomEnd[i] = end
				literalAt[i] = true
				i = end
				continue
			}
			atomEnd[i] = close + end - i
			i = atomEnd[i]
		case '<':
			end := scanner.otherHTMLEnd(i)
			if end < 0 {
				// Leave a non-tag opener in the ordinary text span so its
				// existing inline reader preserves diagnostics.
				i++
				continue
			}
			atomEnd[i] = end
			i = end
		case '\\':
			if i+1 >= len(text) {
				return nil, false
			}
			atomEnd[i] = i + 2
			i += 2
		case '!':
			if i+1 < len(text) && text[i+1] == '[' {
				return nil, false
			}
			i++
		case '[':
			close, ok := closes[i]
			if !ok || close == i+1 {
				atomEnd[i] = i + 1
				literalAt[i] = true
				i++
				continue
			}
			end := close + 1
			if end < len(text) && text[end] == '(' {
				return nil, false
			}
			if end < len(text) && text[end] == '[' {
				if c, ok := closes[end]; ok {
					end = c + 1
				}
			}
			referenceSuffix[i] = text[close+1 : end]
			atomEnd[i] = end
			i = end
		case ']':
			atomEnd[i] = i + 1
			literalAt[i] = true
			i++
		default:
			i++
		}
	}

	runs := make([]emphasisDelimiterRun, 0, min(len(text)/2, 128))
	for i := 0; i < len(text); {
		if cancelled(i) {
			return abort()
		}
		if end, ok := atomEnd[i]; ok {
			i = end
			continue
		}
		if text[i] != '*' && text[i] != '_' && text[i] != '~' {
			_, width := utf8.DecodeRuneInString(text[i:])
			i += width
			continue
		}
		start := i
		marker := text[i]
		for i < len(text) && text[i] == marker {
			if cancelled(i) {
				return abort()
			}
			i++
		}
		if marker == '~' && i-start == 1 {
			continue
		}
		previous, hasPrevious := rune(0), start > 0
		if hasPrevious {
			previous, _ = utf8.DecodeLastRuneInString(text[:start])
		}
		next, hasNext := rune(0), i < len(text)
		if hasNext {
			next, _ = utf8.DecodeRuneInString(text[i:])
		}
		previousWhitespace := !hasPrevious || isCommonMarkWhitespace(previous)
		nextWhitespace := !hasNext || isCommonMarkWhitespace(next)
		previousPunctuation := hasPrevious && isCommonMarkPunctuation(previous)
		nextPunctuation := hasNext && isCommonMarkPunctuation(next)
		leftFlanking := !nextWhitespace && (!nextPunctuation || previousWhitespace || previousPunctuation)
		rightFlanking := !previousWhitespace && (!previousPunctuation || nextWhitespace || nextPunctuation)
		canOpen, canClose := leftFlanking, rightFlanking
		if marker == '_' {
			canOpen = leftFlanking && (!rightFlanking || previousPunctuation)
			canClose = rightFlanking && (!leftFlanking || nextPunctuation)
		}
		runs = append(runs, emphasisDelimiterRun{
			start: start, end: i, marker: marker, remaining: i - start,
			canOpen: canOpen, canClose: canClose,
		})
	}

	// Opener stacks are separated by marker, remaining-length residue, and
	// whether the opener can also close. Lazy stale-entry removal keeps each
	// candidate insertion/removal amortized constant time.
	var openers [3][3][2][]int
	// order holds every live opener in source order. A match kills every opener
	// between the matched opener and its closer, as the CommonMark algorithm
	// does; alive is the lookup used by the class stacks below.
	order := make([]int, 0, len(runs))
	alive := make([]bool, len(runs))
	markerSlot := func(marker byte) int {
		if marker == '*' {
			return 0
		}
		if marker == '_' {
			return 1
		}
		return 2
	}
	pushOpener := func(index int, first bool) {
		run := &runs[index]
		if run.canOpen && run.remaining > 0 {
			if first {
				order = append(order, index)
				alive[index] = true
			}
			closeClass := 0
			if run.canClose {
				closeClass = 1
			}
			slot := markerSlot(run.marker)
			openers[slot][run.remaining%3][closeClass] = append(openers[slot][run.remaining%3][closeClass], index)
		}
	}
	topOpener := func(slot, residue, closeClass int) int {
		stack := openers[slot][residue][closeClass]
		for len(stack) > 0 {
			candidate := stack[len(stack)-1]
			run := &runs[candidate]
			if alive[candidate] && run.remaining > 0 && run.canOpen && run.remaining%3 == residue {
				return candidate
			}
			stack = stack[:len(stack)-1]
		}
		openers[slot][residue][closeClass] = stack
		return -1
	}

	pairs := make([]commentDelimiterPair, 0, len(runs))
	for closerIndex := range runs {
		if cancelled(closerIndex) {
			return abort()
		}
		closer := &runs[closerIndex]
		if closer.canClose {
			slot := markerSlot(closer.marker)
			for closer.remaining > 0 {
				best := -1
				for residue := 0; residue < 3; residue++ {
					for closeClass := 0; closeClass < 2; closeClass++ {
						// Rule of three: a closer that can open, or an opener
						// that can close, cannot match a multiple-of-three sum
						// unless both delimiter lengths are multiples of three.
						if (closer.canOpen || closeClass == 1) && (residue+closer.remaining%3)%3 == 0 && (residue != 0 || closer.remaining%3 != 0) {
							continue
						}
						candidate := topOpener(slot, residue, closeClass)
						if candidate > best {
							best = candidate
						}
					}
				}
				if best < 0 {
					break
				}
				opener := &runs[best]
				use := 1
				if opener.marker == '~' && (opener.remaining < 2 || closer.remaining < 2) {
					break
				}
				if opener.remaining >= 2 && closer.remaining >= 2 {
					use = 2
				}
				openEnd, closeStart := opener.end, closer.start
				pairs = append(pairs, commentDelimiterPair{
					openStart: openEnd - use, openEnd: openEnd,
					closeStart: closeStart, closeEnd: closeStart + use,
					strong: use == 2,
					marker: opener.marker,
				})
				opener.end -= use
				opener.remaining -= use
				closer.start += use
				closer.remaining -= use
				for len(order) > 0 && order[len(order)-1] > best {
					alive[order[len(order)-1]] = false
					order = order[:len(order)-1]
				}
				if opener.remaining == 0 && len(order) > 0 && order[len(order)-1] == best {
					alive[best] = false
					order = order[:len(order)-1]
				} else if opener.remaining > 0 {
					pushOpener(best, false)
				}
			}
		}
		pushOpener(closerIndex, true)
	}

	openingAt := make(map[int]*commentDelimiterPair, len(pairs))
	for i := range pairs {
		pair := &pairs[i]
		openingAt[pair.openStart] = pair
	}
	if len(pairs) == 0 {
		return nil, false
	}
	// Every frame corresponds to a matched pair, so stack size is bounded
	// by the input. WorkBudget remains reserved for parser iteration work.
	// The explicit frames are bounded by the number of pairs, avoiding a
	// Go call stack proportional to delimiter nesting.
	type frame struct {
		start, end, cursor int
		pair               *commentDelimiterPair
		nodes              []*Node
	}
	frames := []frame{{start: 0, end: len(text)}}
	for len(frames) > 0 {
		if cancelled(frames[len(frames)-1].cursor) {
			return abort()
		}
		f := &frames[len(frames)-1]
		if f.cursor < f.end {
			pair := openingAt[f.cursor]
			if pair == nil || pair.closeEnd > f.end {
				f.cursor++
				continue
			}
			if f.cursor > f.start {
				appendCommentAtoms(&f.nodes, text, source, baseOffset, f.start, f.cursor, atomEnd, commentAt, literalAt, referenceSuffix, ctx)
			}

			f.start = pair.closeEnd
			f.cursor = f.start
			frames = append(frames, frame{start: pair.openEnd, end: pair.closeStart, cursor: pair.openEnd, pair: pair})
			continue
		}
		if f.start < f.end {
			appendCommentAtoms(&f.nodes, text, source, baseOffset, f.start, f.end, atomEnd, commentAt, literalAt, referenceSuffix, ctx)
		}
		nodes := f.nodes
		pair := f.pair
		frames = frames[:len(frames)-1]
		if pair == nil {
			return nodes, true
		}
		kind := NodeEmphasis
		if pair.marker == '~' {
			kind = NodeStrikethrough
		} else if pair.strong {
			kind = NodeStrong
		}
		n := newNode(kind, splitTextNewlines(nodes)...)
		n.Range = inlineSpanRange(source, baseOffset, pair.openStart, pair.closeEnd)
		parent := &frames[len(frames)-1]
		parent.nodes = append(parent.nodes, n)
	}
	return nil, false

}

func appendCommentAtoms(nodes *[]*Node, text string, source []byte, base, start, end int, atoms, comments map[int]int, literals map[int]bool, referenceSuffix map[int]string, ctx *parseCtx) {
	plain := start
	for i := start; i < end; i++ {
		atomEnd, ok := atoms[i]
		if !ok || atomEnd > end {
			continue
		}
		if plain < i {
			*nodes = append(*nodes, parseInlineSegmentAt(text[plain:i], source, addBaseOffset(base, plain), ctx)...)
		}
		if literals[i] {
			appendTextRange(nodes, text[i:atomEnd], inlineSpanRange(source, base, i, atomEnd))
		} else if comments[i] == atomEnd {
			n := newNode(NodeHTMLInline)
			n.Range = inlineSpanRange(source, base, i, atomEnd)
			n.Literal = text[i:atomEnd]
			*nodes = append(*nodes, n)
		} else {
			parsed := parseInlineSegmentAt(text[i:atomEnd], source, addBaseOffset(base, i), ctx)
			if suffix, ok := referenceSuffix[i]; ok && len(parsed) == 1 && parsed[0].Type == NodeLink {
				parsed[0].referenceSuffix = &suffix
			}
			*nodes = append(*nodes, parsed...)
		}
		i = atomEnd - 1
		plain = atomEnd
	}
	if plain < end {
		*nodes = append(*nodes, parseInlineSegmentAt(text[plain:end], source, addBaseOffset(base, plain), ctx)...)
	}
}

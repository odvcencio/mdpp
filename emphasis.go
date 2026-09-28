package mdpp

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type emphasisDelimiterRun struct {
	start, end int
	marker     byte
	remaining  int
	canOpen    bool
	canClose   bool
}

type emphasisDelimiterPair struct {
	openStart, openEnd   int
	closeStart, closeEnd int
	strong               bool
}

// parseEmphasisDelimiterRunsAt applies the CommonMark delimiter-stack rules
// to plain inline spans. Other inline constructs retain their existing parser
// precedence. The fixed chunk limit bounds stack memory and nesting depth.
func parseEmphasisDelimiterRunsAt(text string, source []byte, baseOffset int) ([]*Node, bool) {
	if len(text) == 0 || len(text) > maxInlineParseChunk || !strings.ContainsAny(text, "*_") || strings.ContainsAny(text, "[]<>\\!~`") {
		return nil, false
	}

	runs := make([]emphasisDelimiterRun, 0, len(text)/2)
	for i := 0; i < len(text); {
		if text[i] != '*' && text[i] != '_' {
			_, width := utf8.DecodeRuneInString(text[i:])
			i += width
			continue
		}
		start := i
		marker := text[i]
		for i < len(text) && text[i] == marker {
			i++
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
	var openers [2][3][2][]int
	markerSlot := func(marker byte) int {
		if marker == '*' {
			return 0
		}
		return 1
	}
	pushOpener := func(index int) {
		run := &runs[index]
		if run.canOpen && run.remaining > 0 {
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
			if run.remaining > 0 && run.canOpen && run.remaining%3 == residue {
				return candidate
			}
			stack = stack[:len(stack)-1]
		}
		openers[slot][residue][closeClass] = stack
		return -1
	}

	pairs := make([]emphasisDelimiterPair, 0, len(runs))
	for closerIndex := range runs {
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
				if opener.remaining >= 2 && closer.remaining >= 2 {
					use = 2
				}
				openEnd, closeStart := opener.end, closer.start
				pairs = append(pairs, emphasisDelimiterPair{
					openStart: openEnd - use, openEnd: openEnd,
					closeStart: closeStart, closeEnd: closeStart + use,
					strong: use == 2,
				})
				opener.end -= use
				opener.remaining -= use
				closer.start += use
				closer.remaining -= use
				pushOpener(best)
			}
		}
		pushOpener(closerIndex)
	}

	openingAt := make([]*emphasisDelimiterPair, len(text)+1)
	for i := range pairs {
		pair := &pairs[i]
		openingAt[pair.openStart] = pair
	}
	var build func(start, end int) []*Node
	build = func(start, end int) []*Node {
		var nodes []*Node
		for cursor := start; cursor < end; {
			pair := openingAt[cursor]
			if pair == nil || pair.closeEnd > end {
				cursor++
				continue
			}
			if cursor > start {
				appendTextRange(&nodes, text[start:cursor], inlineSpanRange(source, baseOffset, start, cursor))
			}
			kind := NodeEmphasis
			if pair.strong {
				kind = NodeStrong
			}
			formatted := newNode(kind, build(pair.openEnd, pair.closeStart)...)
			formatted.Range = inlineSpanRange(source, baseOffset, pair.openStart, pair.closeEnd)
			nodes = append(nodes, formatted)
			start = pair.closeEnd
			cursor = start
		}
		if start < end {
			appendTextRange(&nodes, text[start:end], inlineSpanRange(source, baseOffset, start, end))
		}
		return nodes
	}
	return build(0, len(text)), true
}

func isCommonMarkWhitespace(r rune) bool {
	return r == '\t' || r == '\n' || r == '\f' || r == '\r' || unicode.Is(unicode.Zs, r)
}

func isCommonMarkPunctuation(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

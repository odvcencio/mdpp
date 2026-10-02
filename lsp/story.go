package lsp

import (
	"bytes"
	"fmt"
	"sort"

	"m31labs.dev/mdpp"
)

type storySourceIndex struct {
	mdpp.StoryIndex
	sourceHadCR bool
}

func (story storySourceIndex) Rename(target mdpp.StorySymbol, name string) ([]mdpp.SourceEdit, error) {
	if story.sourceHadCR {
		return nil, fmt.Errorf("story source edits require LF input; the editor source contained carriage returns")
	}
	return story.StoryIndex.Rename(target, name)
}

// storyIndexForSource maps the core index's normalized ranges to the original
// editor bytes, so At and RangeToLSP share one coordinate system. It retains
// the core index's edit-safety flag. Only range endpoints are allocated; one
// forward scan maps CRLF/lone-CR normalization without a per-byte offset table.
func storyIndexForSource(doc *mdpp.Document, source []byte) storySourceIndex {
	story := storySourceIndex{mdpp.IndexStory(doc), bytes.IndexByte(source, '\r') >= 0}
	if !story.sourceHadCR {
		return story
	}
	type endpoint struct {
		offset          int
		byte, line, col *int
	}
	points := make([]endpoint, 0, 2*(len(story.Symbols)+len(story.Diagnostics)))
	add := func(r *mdpp.Range) {
		points = append(points,
			endpoint{r.StartByte, &r.StartByte, &r.StartLine, &r.StartCol},
			endpoint{r.EndByte, &r.EndByte, &r.EndLine, &r.EndCol})
	}
	for i := range story.Symbols {
		add(&story.Symbols[i].Range)
	}
	for i := range story.Diagnostics {
		add(&story.Diagnostics[i].Range)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].offset < points[j].offset })
	original, normalized, line, col := 0, 0, 1, 1
	for _, point := range points {
		for normalized < point.offset && original < len(source) {
			if width := sourceLineBreakWidth(source, original); width > 0 {
				original += width
				line++
				col = 1
			} else {
				original++
				col++
			}
			normalized++
		}
		*point.byte, *point.line, *point.col = original, line, col
	}
	return story
}

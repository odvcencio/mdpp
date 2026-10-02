package mdpp

import "testing"

func TestStoryFastChunksPreserveSlideBreaksAndCodeHighlights(t *testing.T) {
	source := "---\ntitle: Story\n---\n\n# First\n\n---\n\n# Code\n\n```go {2-3|6}\na := 1\nb := 2\n```\n"
	doc, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	SplitSlides(doc)
	if slides := doc.Slides(); len(slides) != 2 {
		t.Fatalf("lost slide separator: %s", DumpTreeForSnapshot(doc.AST()))
	}
	blocks := doc.AST().Find(NodeCodeBlock)
	if len(blocks) != 1 || blocks[0].Attr("language") != "go" || blocks[0].Attr("highlights") != "2-3|6" {
		t.Fatalf("lost fence metadata: %+v", blocks)
	}
}

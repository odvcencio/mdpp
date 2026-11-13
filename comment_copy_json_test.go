package mdpp

import (
	"bytes"
	"encoding/json"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"strings"
	"testing"
)

func TestCommentReferenceCopyJSONAndCache(t *testing.T) {
	source := []byte("*a <!--> [x *y*] b*\n\nOther paragraph\n")
	d := MustParse(source)
	expected := NewRenderer().Render(d)
	if !strings.Contains(expected, "[x <em>y</em>]") {
		t.Fatal(expected)
	}
	encoded, err := json.Marshal(d.Root)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("referenceSuffix")) || bytes.Contains(encoded, []byte("__mdpp")) {
		t.Fatalf("internal metadata leaked: %s", encoded)
	}
	var n Node
	if err = json.Unmarshal(encoded, &n); err != nil {
		t.Fatal(err)
	}
	if got := NewRenderer().Render(&Document{Root: &n}); got != expected {
		t.Errorf("JSON round trip got %q want %q", got, expected)
	}
	for _, link := range d.Root.Find(NodeLink) {
		if link.referenceSuffix != nil {
			t.Error("temporary reference state escaped public AST")
		}
	}
	suffix := "[id]"
	marked := &Node{Type: NodeLink, referenceSuffix: &suffix}
	for name, copy := range map[string]*Node{"copy": cloneAndShift(marked, 0, nil), "anchored": cloneAndShiftAnchored(marked, 0, 1, 1, 1, 1), "cache": cloneForCache(marked, 0)} {
		if copy.referenceSuffix == nil || *copy.referenceSuffix != suffix {
			t.Errorf("%s lost temporary reference state", name)
		}
	}
	for name, copy := range map[string]*Node{"copy": cloneAndShift(d.Root, 0, nil), "anchored": cloneAndShiftAnchored(d.Root, 0, 1, 1, 1, 1), "cache": cloneForCache(d.Root, 0)} {
		if got := NewRenderer().Render(&Document{Root: copy}); got != expected {
			t.Errorf("%s got %q want %q", name, got, expected)
		}
	}

	p := NewParser()
	defer p.Close()
	if _, err = p.Parse(source); err != nil {
		t.Fatal(err)
	}
	pos := strings.Index(string(source), "Other") + len("Other")
	modified := append(append(append([]byte{}, source[:pos]...), 'x'), source[pos:]...)
	edit := gotreesitter.InputEdit{StartByte: uint32(pos), OldEndByte: uint32(pos), NewEndByte: uint32(pos + 1), StartPoint: gotreesitter.Point{Row: 2, Column: 5}, OldEndPoint: gotreesitter.Point{Row: 2, Column: 5}, NewEndPoint: gotreesitter.Point{Row: 2, Column: 6}}
	incremental, err := p.ParseIncremental(modified, edit)
	if err != nil {
		t.Fatal(err)
	}
	fresh := MustParse(modified)
	hits, _ := p.LastStats()
	if hits == 0 {
		t.Error("expected cache hit")
	}
	if got, want := NewRenderer().Render(incremental), NewRenderer().Render(fresh); got != want {
		t.Errorf("cached=%q fresh=%q", got, want)
	}
}

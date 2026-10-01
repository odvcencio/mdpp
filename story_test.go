package mdpp

import (
	"strings"
	"testing"
)

const storyFixture = "```yaml\nid: opening\ncues: overview, request, recovery\n```\n\n# Café\n\n:::motion {cue=request duration=900 custom=keep}\nHello\n:::\n\n:::motion {cue=recovery after=request}\nRecovered\n:::\n\n[Jump](#opening/recovery)\n\n```sirena\nclient browser { label: \"api\" }\nservice api\nbrowser -> api: calls \"Request\"\n// service fake\n```\n\n```text\n:::motion {cue=fake}\nservice fake\n```\n"

func TestStoryNavigationRenameAndScope(t *testing.T) {
	doc := MustParse([]byte(storyFixture))
	before := DumpTreeForSnapshot(doc.Root)
	idx := IndexStory(doc)
	if len(idx.Diagnostics) != 0 {
		t.Fatalf("unexpected: %+v", idx.Diagnostics)
	}
	for _, name := range []string{"request", "recovery", "api"} {
		offset := strings.Index(storyFixture, "after="+name) + len("after=")
		if name == "recovery" {
			offset = strings.Index(storyFixture, "/recovery") + 1
		}
		if name == "api" {
			offset = strings.Index(storyFixture, "-> api") + 3
		}
		target, ok := idx.At(offset)
		if !ok {
			t.Fatalf("no symbol %s", name)
		}
		edits, err := idx.Rename(target, "updated")
		if err != nil {
			t.Fatal(err)
		}
		if len(edits) < 2 {
			t.Fatalf("missing declaration/references: %+v", edits)
		}
		for _, edit := range edits {
			if string(doc.Source[edit.Range.StartByte:edit.Range.EndByte]) != name {
				t.Fatalf("imprecise rename %+v", edit)
			}
		}
	}
	for _, s := range idx.Symbols {
		if s.Name == "fake" {
			t.Fatal("indexed prose/comment or ordinary fence")
		}
	}
	if DumpTreeForSnapshot(doc.Root) != before {
		t.Fatal("index changed AST")
	}
	other := MustParse([]byte("```sirena\nservice api\n```\n\n```sirena\nservice api\n```\n"))
	idx = IndexStory(other)
	target, _ := idx.At(strings.Index(string(other.Source), "api"))
	edits, err := idx.Rename(target, "renamed")
	if err != nil || len(edits) != 1 {
		t.Fatal("diagram scopes leaked", edits, err)
	}
}

func TestStoryDiagnosticsAndCollision(t *testing.T) {
	doc := MustParse([]byte("```yaml\nid: opening\ncues: overview, request\n```\n\n:::motion {cue=done after=missing}\nx\n:::\n\n[jump](#opening/missing)\n"))
	idx := IndexStory(doc)
	if len(idx.Diagnostics) != 2 {
		t.Fatalf("missing unresolved diagnostics %+v", idx.Diagnostics)
	}
	target, _ := idx.At(strings.Index(string(doc.Source), "request"))
	if _, err := idx.Rename(target, "overview"); err == nil {
		t.Fatal("rename collision accepted")
	}
}

func TestEditParsedDirectivePreservesFormatting(t *testing.T) {
	source := "# Café\n\n:::motion {duration = '900' easing=\"cubic-bezier(.2, .5, .8, 1)\" custom = keep}\n**Hello**\n:::\n\n```text\n:::motion {duration=900}\n```\n"
	doc := MustParse([]byte(source))
	start := strings.Index(source, ":::motion")
	edit, err := EditDirectiveAttributes(doc, start, map[string]string{"duration": "1200", "replay": "step"})
	if err != nil {
		t.Fatal(err)
	}
	updated := source[:edit.Range.StartByte] + edit.NewText + source[edit.Range.EndByte:]
	want := strings.Replace(source, "duration = '900'", "duration = '1200'", 1)
	want = strings.Replace(want, "custom = keep}", "custom = keep replay=\"step\"}", 1)
	if updated != want {
		t.Fatalf("format drift\ngot %s\nwant %s", updated, want)
	}
	fake := strings.LastIndex(source, ":::motion")
	if _, err := EditDirectiveAttributes(doc, fake, map[string]string{"duration": "1"}); err == nil {
		t.Fatal("edited ordinary fence")
	}
	if _, err := EditDirectiveAttributes(MustParse([]byte(strings.ReplaceAll(source, "\n", "\r\n"))), start, map[string]string{}); err == nil {
		t.Fatal("unsafe normalized offsets accepted")
	}
}

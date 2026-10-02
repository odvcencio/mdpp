package mdpp

import (
	"reflect"
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

func TestStorySlideRenameRejectsDeckWideCollisions(t *testing.T) {
	for _, other := range []string{"closing", "opening"} {
		doc := MustParse([]byte("```yaml\nid: opening\n```\n\n# First\n\n---\n\n```yaml\nid: " + other + "\n```\n\n# Second\n"))
		idx := IndexStory(doc)
		target, ok := idx.At(strings.Index(string(doc.Source), "opening"))
		if !ok {
			t.Fatal("missing slide declaration")
		}
		if edits, err := idx.Rename(target, "closing"); err == nil || len(edits) != 0 {
			t.Fatalf("accepted collision/ambiguity: %v, %v", edits, err)
		}
	}
}

func TestStoryAllActorKindsAndUnspacedEdges(t *testing.T) {
	doc := MustParse([]byte("```sirena\njob worker\ngateway ingress\nexternal outside\nworker->ingress: calls \"Request\"\ningress->outside: calls \"Dispatch\"\n```\n"))
	idx := IndexStory(doc)
	if len(idx.Diagnostics) != 0 {
		t.Fatalf("valid actors unresolved: %+v", idx.Diagnostics)
	}
	target, ok := idx.At(strings.Index(string(doc.Source), "worker->"))
	if !ok {
		t.Fatal("missing edge endpoint")
	}
	edits, err := idx.Rename(target, "executor")
	if err != nil || len(edits) != 2 {
		t.Fatal(edits, err)
	}
}

func TestStoryYAMLMetadataExactRanges(t *testing.T) {
	type symbol struct{ kind, name, spelling string }
	tests := []struct {
		name, metadata string
		want           []symbol
	}{
		{"comments", "id: opening # closing\ncues: overview, request # note request\n", []symbol{{"slide", "opening", "opening"}, {"cue", "overview", "overview"}, {"cue", "request", "request"}}},
		{"quoted strings", "id: 'opening' # note\ncues: \" overview , request \" # note\n", []symbol{{"slide", "opening", "opening"}, {"cue", "overview", "overview"}, {"cue", "request", "request"}}},
		{"quoted hash", "id: \"opening#note\"\ncues: \"overview # note, request\"\n", []symbol{{"cue", "request", "request"}}},
		{"plain hash", "id: opening#note\ncues: overview#note, request\n", []symbol{{"cue", "request", "request"}}},
		{"flow list", "id: opening\ncues: [overview, 'request', \"recovery\"] # note\n", []symbol{{"slide", "opening", "opening"}, {"cue", "overview", "overview"}, {"cue", "request", "request"}, {"cue", "recovery", "recovery"}}},
		{"block list", "id: opening\ncues:\n  - overview # note\n  - 'request'\n  - \"recovery\"\n", []symbol{{"slide", "opening", "opening"}, {"cue", "overview", "overview"}, {"cue", "request", "request"}, {"cue", "recovery", "recovery"}}},
		{"unicode columns", "title: Café\ncues: [\"café\", \"request\"] # Café\n", []symbol{{"cue", "request", "request"}}},
		{"escaped names", "id: \"op\\u0065ning\"\ncues: \"over\\u0076iew, request\"\n", []symbol{{"slide", "opening", "op\\u0065ning"}, {"cue", "overview", "over\\u0076iew"}, {"cue", "request", "request"}}},
		{"invalid scalar types", "id: true\ncues: 42\n", nil},
		{"invalid names", "id: bad name\ncues: bad name, 9bad, good, [wrong]\n", []symbol{{"cue", "good", "good"}}},
		{"mixed list", "id: [opening]\ncues: [true, null, 17, \"bad name\", \"bad#name\", allowed]\n", []symbol{{"cue", "allowed", "allowed"}}},
		{"maps", "id: {name: opening}\ncues: {name: request}\n", nil},
		{"nested metadata", "other:\n  id: opening\n  cues: request\n", nil},
		{"duplicate keys", "id: opening\nid: closing\ncues: request\n", nil},
		{"invalid YAML", "id: opening\ncues: [request\n", nil},
	}
	for _, tt := range tests {
		for _, frontmatter := range []bool{false, true} {
			t.Run(tt.name+"/frontmatter="+map[bool]string{false: "false", true: "true"}[frontmatter], func(t *testing.T) {
				source := "```yaml\n" + tt.metadata + "```\n\n# Café\n"
				if frontmatter {
					source = "---\n" + tt.metadata + "---\n\n# Café\n"
				}
				doc := MustParse([]byte(source))
				idx := IndexStory(doc)
				if len(idx.Symbols) != len(tt.want) {
					t.Fatalf("symbols=%+v, want %+v", idx.Symbols, tt.want)
				}
				for i, want := range tt.want {
					s := idx.Symbols[i]
					start := strings.Index(source, want.spelling)
					if s.Kind != want.kind || s.Name != want.name || !s.Declaration || s.Range != sourceRange(doc.Source, start, start+len(want.spelling)) {
						t.Fatalf("symbol=%+v, want %+v at byte %d", s, want, start)
					}
				}
			})
		}
	}
}

func TestStoryYAMLMetadataRenamePreservesCommentsAndQuotes(t *testing.T) {
	for _, tt := range []struct{ metadata, request, updated string }{
		{"id: 'opening' # opening\ncues: overview, request # request\n", "request", "updated"},
		{"id: \"opening\" # opening\ncues: \"overview, request\" # request\n", "request", "updated"},
		{"id: opening # opening\ncues: [\"café\", 'request'] # request\n", "request", "updated"},
		{"id: opening # opening\ncues:\n  - overview # request\n  - \"request\" # request\n", "\"request\"", "\"updated\""},
		{"id: \"op\\u0065ning\" # opening\ncues: \"overview, re\\u0071uest\" # request\n", "re\\u0071uest", "updated"},
	} {
		source := "```yml\n" + tt.metadata + "```\n\n:::motion {after=request}\nHello\n:::\n\n[Jump](#opening/request)\n"
		doc := MustParse([]byte(source))
		idx := IndexStory(doc)
		for _, name := range []string{"opening", "request"} {
			offset := strings.LastIndex(source, "/request") + 1
			if name == "opening" {
				offset = strings.LastIndex(source, "#opening") + 1
			}
			target, ok := idx.At(offset)
			if !ok {
				t.Fatalf("missing target %s in %s", name, source)
			}
			edits, err := idx.Rename(target, "updated")
			wantCount := 3
			if name == "opening" {
				wantCount = 2
			}
			if err != nil || len(edits) != wantCount {
				t.Fatalf("rename %s: edits=%+v error=%v", name, edits, err)
			}
			updated := source
			for i := len(edits) - 1; i >= 0; i-- {
				e := edits[i]
				updated = updated[:e.Range.StartByte] + e.NewText + updated[e.Range.EndByte:]
			}
			spelling := name
			if strings.Contains(tt.metadata, "\\u") {
				spelling = map[string]string{"opening": "op\\u0065ning", "request": "re\\u0071uest"}[name]
			}
			want := strings.Replace(source, spelling, "updated", 1)
			if name == "request" {
				want = strings.Replace(source, tt.request, tt.updated, 1)
				want = strings.ReplaceAll(want, "after=request", "after=updated")
				want = strings.ReplaceAll(want, "/request)", "/updated)")
			} else {
				want = strings.ReplaceAll(want, "#opening/", "#updated/")
			}
			if updated != want {
				t.Fatalf("rename changed formatting/comment\ngot: %s\nwant: %s", updated, want)
			}
		}
		comment := strings.LastIndex(source, "# request") + 2
		if _, ok := idx.At(comment); ok {
			t.Fatal("comment is a rename target")
		}
	}
}

func TestStoryIndexSurvivesSlideSplitting(t *testing.T) {
	source := "---\ntitle: Café\n---\n\n" +
		"```yaml\nid: opening\ntitle: Café\ncues: [\"café\", overview, 'request'] # request\n```\n\n# First\n\n" +
		":::motion {cue=request after=overview}\nFirst\n:::\n\n:::motion {after=missing}\nUnresolved\n:::\n\n[Next](#closing/request)\n\n" +
		"```sirena\nservice api\nclient web\nweb -> api: calls \"Café\"\n```\n\n---\n\n" +
		"```yml\nid: closing # closing\ncues:\n  - overview\n  - \"request\" # request\n```\n\n# Second\n\n" +
		":::motion {cue=request after=overview}\nSecond\n:::\n\n[Back](#opening/request)\n\n" +
		"```sirena\nservice api\nclient web\nweb -> api: calls \"Café\"\n```\n\n---\n\n" +
		"```yaml\nid: final\ncues: overview, recovery # recovery\n```\n\n:::motion {cue=recovery after=missing}\nFinal\n:::\n\n[Start](#opening/overview)\n"
	want := IndexStory(MustParse([]byte(source)))
	ids, requestScopes, actors := map[string]string{}, []string{}, 0
	for _, symbol := range want.Symbols {
		if symbol.Declaration {
			switch symbol.Kind {
			case "slide":
				ids[symbol.Name] = symbol.Scope
			case "cue":
				if symbol.Name == "request" {
					requestScopes = append(requestScopes, symbol.Scope)
				}
			case "actor":
				actors++
			}
		}
	}
	if !reflect.DeepEqual(ids, map[string]string{"opening": "slide:0", "closing": "slide:1", "final": "slide:2"}) ||
		!reflect.DeepEqual(requestScopes, []string{"slide:0", "slide:1"}) || actors != 4 || len(want.Diagnostics) != 2 {
		t.Fatalf("incomplete baseline: ids=%v requests=%v actors=%d diagnostics=%+v", ids, requestScopes, actors, want.Diagnostics)
	}
	for _, transform := range []struct {
		name string
		run  func(*Document)
	}{
		{"SplitSlides", func(doc *Document) { SplitSlides(doc) }},
		{"Document.Slides", func(doc *Document) { doc.Slides() }},
	} {
		t.Run(transform.name, func(t *testing.T) {
			doc := MustParse([]byte(source))
			transform.run(doc)
			transform.run(doc) // both public split operations are idempotent
			before := DumpTreeForSnapshot(doc.Root)
			got := IndexStory(doc)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("split story index differs\ngot: %+v\nwant: %+v", got, want)
			}
			for _, target := range want.Symbols {
				if found, ok := got.At(target.Range.StartByte); !ok || found != target {
					t.Fatalf("lost exact source range for %+v", target)
				}
				for _, declarations := range []bool{false, true} {
					if !reflect.DeepEqual(got.References(target, declarations), want.References(target, declarations)) {
						t.Fatalf("references changed for %+v", target)
					}
				}
				edits, err := got.Rename(target, "updated")
				wantEdits, wantErr := want.Rename(target, "updated")
				if !reflect.DeepEqual(edits, wantEdits) || (err == nil) != (wantErr == nil) || (err != nil && err.Error() != wantErr.Error()) {
					t.Fatalf("rename changed for %+v: edits=%+v err=%v, want edits=%+v err=%v", target, edits, err, wantEdits, wantErr)
				}
			}
			if DumpTreeForSnapshot(doc.Root) != before || string(doc.Source) != source {
				t.Fatal("index changed the caller's split AST or source")
			}
		})
	}
}

package lsp

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestServerCodeActionsConversionsAndPlaceholders(t *testing.T) {
	src := "See [the site][docs], note[^a], and [missing][ref].\n\n[docs]: https://example.com \"Docs\"\n"
	uri := DocumentURI("file:///doc.md")
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: uri, Version: 1, Text: src})

	actions, err := s.codeActions(CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 2, Character: 0},
		},
		Context: CodeActionContext{Only: []string{"quickfix"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 3 {
		t.Fatalf("expected conversion plus two placeholder actions, got %#v", actions)
	}

	byTitle := map[string]CodeAction{}
	for _, action := range actions {
		byTitle[action.Title] = action
	}

	convert := byTitle["Convert reference link to inline link"]
	if convert.Edit == nil {
		t.Fatalf("expected inline conversion edit, got %#v", convert)
	}
	if got := convert.Edit.Changes[uri][0].NewText; !strings.Contains(got, "(<https://example.com> \"Docs\")") {
		t.Fatalf("unexpected conversion text: %q", got)
	}

	footnote := byTitle["Create missing footnote definition"]
	if footnote.Edit == nil {
		t.Fatalf("expected footnote placeholder edit, got %#v", footnote)
	}
	if got := footnote.Edit.Changes[uri][0].NewText; !strings.Contains(got, "[^a]: ") {
		t.Fatalf("unexpected footnote placeholder: %#v", footnote.Edit.Changes[uri][0])
	}

	reference := byTitle["Create missing reference definition"]
	if reference.Edit == nil {
		t.Fatalf("expected reference placeholder edit, got %#v", reference)
	}
	if got := reference.Edit.Changes[uri][0].NewText; !strings.Contains(got, "[ref]: ") {
		t.Fatalf("unexpected reference placeholder: %#v", reference.Edit.Changes[uri][0])
	}
}

func TestServerCompletionHeadingAnchorsAndEmojiRegistry(t *testing.T) {
	t.Run("heading anchors", func(t *testing.T) {
		src := "# Intro\n\n## Getting Started\n\nSee [guide](#\n"
		items := completionItemsAt(t, src, positionAfterSubstring(src, "(#"))
		item := completionItemByLabel(items, "#intro")
		if item == nil {
			t.Fatalf("expected heading anchor completion, got %#v", items)
		}
		if item.InsertText != "intro" {
			t.Fatalf("unexpected heading anchor insert text: %#v", item)
		}
	})

	t.Run("emoji registry", func(t *testing.T) {
		src := "Status :zany_\n"
		items := completionItemsAt(t, src, positionAfterSubstring(src, ":zany_"))
		item := completionItemByLabel(items, "zany_face")
		if item == nil {
			t.Fatalf("expected full emoji registry completion, got %#v", items)
		}
		if item.InsertText != "zany_face:" {
			t.Fatalf("unexpected emoji insert text: %#v", item)
		}
	})
}

func TestServerDidChangeRandomizedSync(t *testing.T) {
	const seed = 1
	rng := rand.New(rand.NewSource(seed))
	uri := DocumentURI("file:///doc.md")
	s := NewServer()
	expected := "Alpha🙂\nBeta\nGamma é\n"
	s.store.Open(TextDocumentItem{URI: uri, Version: 1, Text: expected})

	for version := int32(2); version < 250; version++ {
		change := randomDocumentChange(rng, expected)
		params := DidChangeTextDocumentParams{
			TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: version},
			ContentChanges: []TextDocumentContentChangeEvent{
				change,
			},
		}
		body, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		_, respErr, err := s.dispatch(&out, incomingMessage{Method: "textDocument/didChange", Params: body})
		if err != nil {
			t.Fatal(err)
		}
		if respErr != nil {
			t.Fatalf("unexpected response error: %+v", respErr)
		}
		expected = applyDocumentChange(expected, change)
		open, ok := s.store.Get(uri)
		if !ok {
			t.Fatal("expected open document")
		}
		_, got, _, gotVersion := open.Snapshot()
		if gotVersion != version {
			t.Fatalf("version mismatch: got %d want %d", gotVersion, version)
		}
		if string(got) != expected {
			t.Fatalf("document sync mismatch after version %d:\n got  %q\n want %q", version, got, expected)
		}
	}
}

func TestLineIndexUTF16Boundaries(t *testing.T) {
	src := "a🙂é\n"
	index := NewLineIndex([]byte(src))

	cases := []struct {
		name string
		pos  Position
		want int
	}{
		{name: "start", pos: Position{Line: 0, Character: 0}, want: 0},
		{name: "after ascii", pos: Position{Line: 0, Character: 1}, want: 1},
		{name: "inside surrogate", pos: Position{Line: 0, Character: 2}, want: 1},
		{name: "after surrogate", pos: Position{Line: 0, Character: 3}, want: len("a🙂")},
		{name: "after accent", pos: Position{Line: 0, Character: 4}, want: len("a🙂é")},
	}
	for _, tc := range cases {
		got, ok := index.PositionToOffset(tc.pos)
		if !ok {
			t.Fatalf("%s: expected valid position", tc.name)
		}
		if got != tc.want {
			t.Fatalf("%s: got offset %d, want %d", tc.name, got, tc.want)
		}
	}
	if got := index.OffsetToPosition(len("a🙂")); got.Character != 3 {
		t.Fatalf("unexpected utf-16 position after emoji: %#v", got)
	}
	if got := index.UTF16Length(len("a"), len("a🙂")); got != 2 {
		t.Fatalf("unexpected utf-16 length for emoji: %d", got)
	}
}

func randomDocumentChange(rng *rand.Rand, src string) TextDocumentContentChangeEvent {
	bounds := utf8Boundaries(src)
	if len(bounds) < 2 || rng.Intn(4) == 0 {
		return TextDocumentContentChangeEvent{Text: randomReplacementText(rng)}
	}
	start := bounds[rng.Intn(len(bounds))]
	end := bounds[rng.Intn(len(bounds))]
	if end < start {
		start, end = end, start
	}
	rngLineIndex := NewLineIndex([]byte(src))
	lo := rngLineIndex.OffsetToPosition(start)
	hi := rngLineIndex.OffsetToPosition(end)
	return TextDocumentContentChangeEvent{
		Range: &Range{
			Start: lo,
			End:   hi,
		},
		Text: randomReplacementText(rng),
	}
}

func applyDocumentChange(src string, change TextDocumentContentChangeEvent) string {
	if change.Range == nil {
		return change.Text
	}
	index := NewLineIndex([]byte(src))
	start, _ := index.PositionToOffset(change.Range.Start)
	end, _ := index.PositionToOffset(change.Range.End)
	if end < start {
		start, end = end, start
	}
	var out strings.Builder
	out.Grow(len(src) - (end - start) + len(change.Text))
	out.WriteString(src[:start])
	out.WriteString(change.Text)
	out.WriteString(src[end:])
	return out.String()
}

func utf8Boundaries(src string) []int {
	bounds := make([]int, 0, utf8.RuneCountInString(src)+1)
	for i := range src {
		bounds = append(bounds, i)
	}
	bounds = append(bounds, len(src))
	return bounds
}

func randomReplacementText(rng *rand.Rand) string {
	runes := []rune("abcXYZ123🙂éø \n")
	n := rng.Intn(8)
	if n == 0 {
		return ""
	}
	out := make([]rune, n)
	for i := range out {
		out[i] = runes[rng.Intn(len(runes))]
	}
	return string(out)
}

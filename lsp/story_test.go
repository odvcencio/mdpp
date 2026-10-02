package lsp

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestStoryDefinitionReferencesAndRename(t *testing.T) {
	source := "```yaml\nid: opening\ncues: overview, request\n```\n\n# Café\n\n:::motion {cue=done after=request}\nx\n:::\n\n[Jump](#opening/request)\n\n```sirena\nservice api\nclient web\nweb -> api: calls \"api\"\n```\n"
	uri := DocumentURI("file:///story.md")
	s := NewServer()
	open := s.store.Open(TextDocumentItem{URI: uri, Version: 1, Text: source})
	_, _, index, _ := open.SnapshotReady()
	for _, offset := range []int{strings.Index(source, "after=request") + 6, strings.Index(source, "-> api") + 3} {
		position := index.OffsetToPosition(offset)
		defs, err := s.definition(DefinitionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position})
		if err != nil || len(defs) != 1 {
			t.Fatalf("definition: %v %v", defs, err)
		}
		refs, err := s.references(ReferenceParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position, Context: ReferenceContext{IncludeDeclaration: true}})
		if err != nil || len(refs) < 2 {
			t.Fatalf("references: %v %v", refs, err)
		}
		r, err := s.prepareRename(TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position})
		if err != nil || r == nil {
			t.Fatal("prepare rename", err)
		}
		edit, err := s.rename(RenameParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position, NewName: "updated"})
		if err != nil || len(edit.Changes[uri]) != len(refs) {
			t.Fatal("rename", edit, err)
		}
	}
}

func TestStoryLineEndingsReadOnlyPositionsAndEditSafety(t *testing.T) {
	const lf = "```yaml\nid: opening\ntitle: Café\ncues: [\"😀\", overview, 'request'] # request\n```\n\n# Café\n\n" +
		":::motion {note=\"😀 Café\" cue=done after=request}\nx\n:::\n\n" +
		":::motion {note=\"😀 Café\" after=missing}\nx\n:::\n\n[😀 Café](#opening/request)\n\n" +
		"```sirena\nservice api\nclient web\nweb -> api: calls \"😀 Café\"\n```\n"
	targets := []struct {
		marker, name string
		skip, count  int
	}{
		{"after=request", "request", len("after="), 3},
		{"/request", "request", 1, 3},
		{"#opening", "opening", 1, 2},
		{"-> api", "api", 3, 2},
	}
	uri := DocumentURI("file:///story-crlf.md")
	type response struct {
		definitions, references [][]Location
		diagnostics             []Diagnostic
	}
	collect := func(t *testing.T, source string, carriageReturns bool) response {
		t.Helper()
		s := NewServer()
		open := s.store.Open(TextDocumentItem{URI: uri, Version: 1, Text: source})
		doc, original, index, _ := open.SnapshotReady()
		if string(doc.Source) != lf || string(original) != source || doc.SourceHadCarriageReturns() != carriageReturns {
			t.Fatal("snapshot source normalization changed")
		}
		var out response
		for _, target := range targets {
			offset := strings.Index(source, target.marker) + target.skip
			position := index.OffsetToPosition(offset)
			prefix := lf[:strings.Index(lf, target.marker)+target.skip]
			wantPosition := Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(len(utf16.Encode([]rune(prefix[strings.LastIndex(prefix, "\n")+1:]))))}
			if position != wantPosition {
				t.Fatalf("incorrect UTF-16 query position: got %+v, want %+v", position, wantPosition)
			}
			defs, err := s.definition(DefinitionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position})
			if err != nil || len(defs) != 1 {
				t.Fatalf("definition for %s: %v %v", target.name, defs, err)
			}
			refs, err := s.references(ReferenceParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position, Context: ReferenceContext{IncludeDeclaration: true}})
			if err != nil || len(refs) != target.count {
				t.Fatalf("references for %s: %v %v", target.name, refs, err)
			}
			for _, loc := range append(append([]Location{}, defs...), refs...) {
				a, okA := index.PositionToOffset(loc.Range.Start)
				b, okB := index.PositionToOffset(loc.Range.End)
				if !okA || !okB || string(original[a:b]) != target.name {
					t.Fatalf("range does not select original %s: %+v", target.name, loc)
				}
			}
			out.definitions = append(out.definitions, defs)
			out.references = append(out.references, refs)
			selection, err := s.prepareRename(TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position})
			if carriageReturns {
				if selection != nil || err == nil || !strings.Contains(err.Error(), "LF input") {
					t.Fatalf("prepareRename did not reject carriage returns: %v %v", selection, err)
				}
			} else if selection == nil || err != nil {
				t.Fatalf("LF prepareRename failed: %v %v", selection, err)
			}
			edit, err := s.rename(RenameParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position, NewName: "updated"})
			if carriageReturns {
				if edit != nil || err == nil || !strings.Contains(err.Error(), "LF input") {
					t.Fatalf("rename did not reject carriage returns: %v %v", edit, err)
				}
			} else if err != nil || len(edit.Changes[uri]) != target.count {
				t.Fatalf("LF rename failed: %v %v", edit, err)
			}
		}
		for _, diagnostic := range documentDiagnostics(uri, doc, index) {
			if strings.HasPrefix(diagnostic.Code, "STORY-") {
				out.diagnostics = append(out.diagnostics, diagnostic)
				a, _ := index.PositionToOffset(diagnostic.Range.Start)
				b, _ := index.PositionToOffset(diagnostic.Range.End)
				if string(original[a:b]) != "missing" {
					t.Fatalf("diagnostic range does not select original name: %+v", diagnostic)
				}
			}
		}
		if len(out.diagnostics) != 1 {
			t.Fatalf("missing story diagnostic: %+v", out.diagnostics)
		}
		for _, symbol := range storyIndexForSource(doc, original).Symbols {
			if symbol.Range != lspSourceRange(original, symbol.Range.StartByte, symbol.Range.EndByte) {
				t.Fatalf("inconsistent original byte/line range: %+v", symbol)
			}
		}
		if string(original) != source || string(doc.Source) != lf {
			t.Fatal("story requests changed source buffers")
		}
		return out
	}
	want := collect(t, lf, false)
	var mixed strings.Builder
	for i, line := range strings.SplitAfter(lf, "\n") {
		mixed.WriteString(strings.TrimSuffix(line, "\n"))
		if strings.HasSuffix(line, "\n") {
			ending := []string{"\r", "\n", "\r\n"}[i%3]
			if line == "\n" && ending == "\n" {
				ending = "\r\n" // do not merge the prior CR with an empty LF line
			}
			mixed.WriteString(ending)
		}
	}
	for _, tc := range []struct{ name, source string }{
		{"CRLF", strings.ReplaceAll(lf, "\n", "\r\n")},
		{"CR", strings.ReplaceAll(lf, "\n", "\r")},
		{"mixed", mixed.String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := collect(t, tc.source, true)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("read-only responses differ\ngot %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestLineIndexLineEndingBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		endings []string
	}{
		{"LF", []string{"\n", "\n", "\n"}},
		{"CRLF", []string{"\r\n", "\r\n", "\r\n"}},
		{"CR", []string{"\r", "\r", "\r"}},
		{"mixed", []string{"\r\n", "\r", "\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := []string{"😀", "é", "x"}
			source := lines[0] + tc.endings[0] + lines[1] + tc.endings[1] + lines[2] + tc.endings[2]
			index := NewLineIndex([]byte(source))
			start := 0
			for line, text := range lines {
				end := start + len(text)
				next := end + len(tc.endings[line])
				width := uint32(len(utf16.Encode([]rune(text))))
				positions := []struct {
					offset int
					pos    Position
				}{
					{start, Position{Line: uint32(line)}},
					{end, Position{Line: uint32(line), Character: width}},
					{next, Position{Line: uint32(line + 1)}},
				}
				for _, point := range positions {
					if got := index.OffsetToPosition(point.offset); got != point.pos {
						t.Fatalf("offset %d: got %+v, want %+v", point.offset, got, point.pos)
					}
					if got, ok := index.PositionToOffset(point.pos); !ok || got != point.offset {
						t.Fatalf("position %+v: got %d/%v, want %d", point.pos, got, ok, point.offset)
					}
				}
				for offset := end; offset < next; offset++ {
					pos := index.OffsetToPosition(offset)
					if pos != (Position{Line: uint32(line), Character: width}) {
						t.Fatalf("terminator byte %d returned invalid position %+v", offset, pos)
					}
					if got, ok := index.PositionToOffset(pos); !ok || got != end {
						t.Fatalf("terminator position did not map to canonical content end: %d/%v", got, ok)
					}
					r := lspSourceRange([]byte(source), offset, offset)
					if r.StartLine != line+1 || r.StartCol != len(text)+1 {
						t.Fatalf("terminator range metadata differs from position: %+v", r)
					}
				}
				if got, ok := index.PositionToOffset(Position{Line: uint32(line), Character: width + 1}); ok || got != end {
					t.Fatalf("position beyond content accepted: %d/%v", got, ok)
				}
				if got, ok := index.LinePrefix(Position{Line: uint32(line), Character: width}); !ok || got != text {
					t.Fatalf("line prefix includes terminator: %q/%v", got, ok)
				}
				if index.LineContentEndForOffset(start) != end || index.NextLineStartAfterOffset(start) != next {
					t.Fatal("incorrect content or next-line boundary")
				}
				start = next
			}
			if index.LineContentEndForOffset(len(source)) != len(source) {
				t.Fatal("trailing empty line consumed the preceding terminator")
			}
		})
	}
}

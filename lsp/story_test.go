package lsp

import (
	"strings"
	"testing"
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

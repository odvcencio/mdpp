package lsp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/odvcencio/mdpp"
	mdppfmt "github.com/odvcencio/mdpp/fmt"
	"github.com/odvcencio/mdpp/lint"
)

func documentDiagnostics(uri DocumentURI, doc *mdpp.Document, index *LineIndex) []Diagnostic {
	if doc == nil || index == nil {
		return nil
	}
	var out []Diagnostic
	for _, d := range doc.Diagnostics() {
		out = append(out, Diagnostic{
			Range:    index.RangeToLSP(d.Range),
			Severity: parserSeverity(d.Severity),
			Code:     d.Code,
			Source:   "mdpp",
			Message:  d.Message,
		})
	}
	for _, d := range lint.Lint(doc) {
		diag := Diagnostic{
			Range:    index.RangeToLSP(d.Range),
			Severity: lintSeverity(d.Severity),
			Code:     d.Code,
			Source:   "mdpp",
			Message:  d.Message,
		}
		for _, related := range d.Related {
			diag.RelatedInformation = append(diag.RelatedInformation, DiagnosticRelatedInformation{
				Location: Location{URI: uri, Range: index.RangeToLSP(related.Range)},
				Message:  related.Message,
			})
		}
		out = append(out, diag)
	}
	return out
}

func parserSeverity(sev mdpp.Severity) int {
	switch sev {
	case mdpp.SeverityError:
		return diagnosticSeverityError
	case mdpp.SeverityWarning:
		return diagnosticSeverityWarning
	default:
		return diagnosticSeverityInformation
	}
}

func lintSeverity(sev lint.Severity) int {
	switch sev {
	case lint.SeverityError:
		return diagnosticSeverityError
	case lint.SeverityWarning:
		return diagnosticSeverityWarning
	case lint.SeverityHint:
		return diagnosticSeverityHint
	default:
		return diagnosticSeverityInformation
	}
}

func (s *Server) hover(params HoverParams) (*Hover, error) {
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	doc, _, index, _ := open.Snapshot()
	if doc == nil || doc.Root == nil {
		return nil, nil
	}
	offset, ok := index.PositionToOffset(params.Position)
	if !ok {
		return nil, errors.New("hover position is outside the document")
	}
	path := nodePathAt(doc.Root, offset, len(index.source))
	for i := len(path) - 1; i >= 0; i-- {
		body, ok := hoverBody(doc, path[i])
		if !ok {
			continue
		}
		r := index.RangeToLSP(path[i].Range)
		return &Hover{
			Contents: MarkupContent{Kind: "markdown", Value: body},
			Range:    &r,
		}, nil
	}
	return nil, nil
}

func hoverBody(doc *mdpp.Document, n *mdpp.Node) (string, bool) {
	if n == nil {
		return "", false
	}
	switch n.Type {
	case mdpp.NodeTableOfContents:
		return fmt.Sprintf("**[[toc]]**\n\nTable of contents generated from headings in this document. Currently lists %d headings.", len(doc.Headings())), true
	case mdpp.NodeAutoEmbed:
		src := n.Attr("src")
		provider := n.Attr("provider")
		if provider == "" {
			provider = "generic"
		}
		rendering := "rich embed"
		if provider == "generic" {
			rendering = "generic link fallback"
		}
		return fmt.Sprintf("**[[embed:]]**\n\nAuto-embed provider: `%s`.\n\nURL: `%s`\n\nWill render as: %s.", provider, src, rendering), true
	case mdpp.NodeContainerDirective:
		name := n.Attr("name")
		return fmt.Sprintf("**:::%s**\n\n%s", name, containerDescription(name)), true
	case mdpp.NodeAdmonition:
		typ := strings.ToUpper(n.Attr("type"))
		if typ == "" {
			typ = "NOTE"
		}
		return fmt.Sprintf("**[!%s]**\n\n%s", typ, admonitionDescription(typ)), true
	case mdpp.NodeHeading:
		text := n.Text()
		return fmt.Sprintf("**Heading**\n\n`#%s` · level %d\n\n%s", mdpp.Slugify(text), n.Level(), text), true
	case mdpp.NodeFootnoteRef:
		id := n.Attr("id")
		if def := findFootnoteDef(doc.Root, id); def != nil {
			return fmt.Sprintf("**Footnote `[^%s]`**\n\n%s", id, strings.TrimSpace(def.Text())), true
		}
		return fmt.Sprintf("*Undefined footnote `[^%s]`.*", id), true
	case mdpp.NodeFootnoteDef:
		id := n.Attr("id")
		return fmt.Sprintf("**Footnote definition `[^%s]`**\n\n%s", id, strings.TrimSpace(n.Text())), true
	case mdpp.NodeLink:
		if ref := n.Attr("ref"); ref != "" {
			if href := n.Attr("href"); href != "" {
				return fmt.Sprintf("**Reference link `%s`**\n\n%s", ref, href), true
			}
			return fmt.Sprintf("*Undefined reference `%s`.*", ref), true
		}
		if href := n.Attr("href"); href != "" {
			if strings.HasPrefix(href, "#") {
				if h := findHeadingByID(doc.Root, strings.TrimPrefix(href, "#")); h != nil {
					return fmt.Sprintf("**Internal link**\n\nTarget: `%s`\n\n%s", href, h.Text()), true
				}
			}
			return fmt.Sprintf("**Link**\n\n`%s`", href), true
		}
	case mdpp.NodeImage:
		return fmt.Sprintf("**Image**\n\nAlt: `%s`\n\nSource: `%s`", n.Attr("alt"), n.Attr("src")), true
	case mdpp.NodeMathInline:
		return fmt.Sprintf("**Inline math**\n\n```tex\n%s\n```", strings.TrimSpace(n.Literal)), true
	case mdpp.NodeMathBlock:
		return fmt.Sprintf("**Display math**\n\n```tex\n%s\n```", strings.TrimSpace(n.Literal)), true
	case mdpp.NodeEmoji:
		code := n.Attr("code")
		return fmt.Sprintf("%s `:%s:`", n.Literal, code), true
	case mdpp.NodeCodeBlock:
		lang := n.Attr("lang")
		if lang == "" {
			lang = n.Attr("info")
		}
		if lang == "" {
			return "**Code block**", true
		}
		return fmt.Sprintf("**Code block**\n\nLanguage: `%s`", lang), true
	}
	return "", false
}

func (s *Server) completion(params CompletionParams) (*CompletionList, error) {
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return &CompletionList{Items: nil}, nil
	}
	doc, source, index, _, offset, _, err := documentPositionContext(open, params.Position)
	if err != nil {
		return nil, err
	}
	prefix, ok := index.LinePrefix(params.Position)
	if !ok {
		return nil, errors.New("completion position is outside the document")
	}
	trimmedPrefix := strings.TrimLeft(prefix, " \t")
	if offsetInFrontmatter(source, offset) {
		if items := frontmatterCompletionItems(doc, trimmedPrefix); len(items) > 0 {
			return &CompletionList{Items: items}, nil
		}
	}
	switch {
	case strings.Contains(trimmedPrefix, "[[embed:"):
		return &CompletionList{Items: nil}, nil
	case strings.HasPrefix(trimmedPrefix, ":::"):
		return &CompletionList{Items: containerCompletions()}, nil
	case strings.Contains(prefix, "[!"):
		return &CompletionList{Items: admonitionCompletions()}, nil
	default:
		if items := completionItems(doc, source, trimmedPrefix); len(items) > 0 {
			return &CompletionList{Items: items}, nil
		}
		return &CompletionList{Items: nil}, nil
	}
}

func (s *Server) codeActions(params CodeActionParams) ([]CodeAction, error) {
	quickfixAllowed := codeActionKindAllowed(params.Context.Only, "quickfix")
	fixAllAllowed := codeActionKindAllowed(params.Context.Only, "source.fixAll.mdpp")
	if !quickfixAllowed && !fixAllAllowed {
		return nil, nil
	}
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	doc, source, index, _ := open.Snapshot()
	var actions []CodeAction
	if quickfixAllowed {
		for _, d := range lint.Lint(doc) {
			if d.Fix == nil {
				continue
			}
			diagRange := index.RangeToLSP(d.Range)
			if !rangesOverlap(params.Range, diagRange) {
				continue
			}
			editRange := index.RangeToLSP(d.Fix.Range)
			diagnostic := Diagnostic{
				Range:    diagRange,
				Severity: lintSeverity(d.Severity),
				Code:     d.Code,
				Source:   "mdpp",
				Message:  d.Message,
			}
			actions = append(actions, CodeAction{
				Title:       fmt.Sprintf("Fix %s: %s", d.Code, d.Message),
				Kind:        "quickfix",
				Diagnostics: []Diagnostic{diagnostic},
				Edit: &WorkspaceEdit{Changes: map[DocumentURI][]TextEdit{
					params.TextDocument.URI: {{
						Range:   editRange,
						NewText: d.Fix.NewText,
					}},
				}},
			})
		}
	}
	if fixAllAllowed {
		formatted, err := mdppfmt.Format(source)
		if err != nil {
			return nil, err
		}
		if string(formatted) != string(source) {
			actions = append(actions, CodeAction{
				Title: "Format document with mdpp",
				Kind:  "source.fixAll.mdpp",
				Edit: &WorkspaceEdit{Changes: map[DocumentURI][]TextEdit{
					params.TextDocument.URI: {{
						Range: Range{
							Start: Position{Line: 0, Character: 0},
							End:   index.OffsetToPosition(len(source)),
						},
						NewText: string(formatted),
					}},
				}},
			})
		}
	}
	return actions, nil
}

func codeActionKindAllowed(only []string, kind string) bool {
	if len(only) == 0 {
		return true
	}
	for _, allowed := range only {
		if allowed == kind || strings.HasPrefix(kind, allowed+".") {
			return true
		}
	}
	return false
}

func rangesOverlap(a Range, b Range) bool {
	if comparePosition(a.Start, a.End) == 0 {
		return comparePosition(b.Start, a.Start) <= 0 && comparePosition(a.Start, b.End) <= 0
	}
	if comparePosition(b.Start, b.End) == 0 {
		return comparePosition(a.Start, b.Start) <= 0 && comparePosition(b.Start, a.End) <= 0
	}
	return comparePosition(a.Start, b.End) < 0 && comparePosition(b.Start, a.End) < 0
}

func comparePosition(a Position, b Position) int {
	if a.Line < b.Line {
		return -1
	}
	if a.Line > b.Line {
		return 1
	}
	if a.Character < b.Character {
		return -1
	}
	if a.Character > b.Character {
		return 1
	}
	return 0
}

func (s *Server) formatting(params DocumentFormattingParams) ([]TextEdit, error) {
	open, ok := s.store.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	_, source, index, _ := open.Snapshot()
	formatted, err := mdppfmt.Format(source)
	if err != nil {
		return nil, err
	}
	if string(formatted) == string(source) {
		return []TextEdit{}, nil
	}
	return []TextEdit{{
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   index.OffsetToPosition(len(source)),
		},
		NewText: string(formatted),
	}}, nil
}

func (s *Server) renderPreview(params RenderPreviewParams) (*RenderPreviewResult, error) {
	uri := params.URI
	if uri == "" {
		uri = params.TextDocument.URI
	}
	if uri == "" {
		return nil, errors.New("renderPreview requires uri or textDocument.uri")
	}
	open, ok := s.store.Get(uri)
	if !ok {
		return nil, errors.New("document is not open: " + string(uri))
	}
	doc, _, _, version := open.Snapshot()
	html, err := mdpp.Render(doc, mdpp.RenderOptions{
		HeadingIDs:      true,
		SourcePositions: true,
	})
	if err != nil {
		return nil, err
	}
	return &RenderPreviewResult{
		URI:         uri,
		HTML:        string(html),
		Frontmatter: doc.Frontmatter(),
		TOCEntries:  tocEntries(doc.TableOfContents()),
		Version:     version,
	}, nil
}

func tocEntries(in []mdpp.TOCEntry) []mdppTOCEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]mdppTOCEntry, len(in))
	for i, entry := range in {
		out[i] = mdppTOCEntry{Level: entry.Level, ID: entry.ID, Text: entry.Text}
	}
	return out
}

func directiveCompletions() []CompletionItem {
	return []CompletionItem{
		{
			Label:      "[[toc]]",
			Kind:       completionItemKindKeyword,
			Detail:     "Insert generated table of contents directive",
			InsertText: "[[toc]]",
			Documentation: &MarkupContent{
				Kind:  "markdown",
				Value: "Generates a table of contents from document headings.",
			},
		},
		{
			Label:            "[[embed:]]",
			Kind:             completionItemKindSnippet,
			Detail:           "Insert auto-embed directive",
			InsertText:       "[[embed:${1:https://example.com}]]",
			InsertTextFormat: insertTextFormatSnippet,
			Documentation: &MarkupContent{
				Kind:  "markdown",
				Value: "Embeds a supported URL, with a generic link fallback for unknown providers.",
			},
		},
	}
}

func admonitionCompletions() []CompletionItem {
	types := []string{"NOTE", "TIP", "WARNING", "CAUTION", "IMPORTANT"}
	items := make([]CompletionItem, 0, len(types))
	for _, typ := range types {
		items = append(items, CompletionItem{
			Label:      typ,
			Kind:       completionItemKindValue,
			Detail:     admonitionDescription(typ),
			InsertText: typ + "]",
		})
	}
	return items
}

func containerCompletions() []CompletionItem {
	names := []string{"note", "tip", "warning", "caution", "important", "info", "details", "aside", "columns", "column"}
	items := make([]CompletionItem, 0, len(names))
	for _, name := range names {
		items = append(items, CompletionItem{
			Label:            name,
			Kind:             completionItemKindSnippet,
			Detail:           containerDescription(name),
			InsertText:       name + "\n${0}\n:::",
			InsertTextFormat: insertTextFormatSnippet,
		})
	}
	return items
}

func admonitionDescription(typ string) string {
	switch strings.ToUpper(typ) {
	case "NOTE":
		return "Context or supporting detail."
	case "TIP":
		return "Helpful suggestion or shortcut."
	case "WARNING":
		return "Risk or important caveat."
	case "CAUTION":
		return "Potentially destructive or surprising behavior."
	case "IMPORTANT":
		return "Information the reader should not miss."
	default:
		return "Custom admonition type."
	}
}

func containerDescription(name string) string {
	switch strings.ToLower(name) {
	case "note":
		return "Admonition-style note container."
	case "tip":
		return "Admonition-style tip container."
	case "warning":
		return "Admonition-style warning container."
	case "caution":
		return "Admonition-style caution container."
	case "important":
		return "Admonition-style important container."
	case "info":
		return "Informational block container."
	case "details":
		return "Disclosure block container."
	case "aside":
		return "Secondary aside content."
	case "columns", "column":
		return "Column layout container."
	default:
		return "Custom block container."
	}
}

func nodePathAt(root *mdpp.Node, offset int, sourceLen int) []*mdpp.Node {
	var path []*mdpp.Node
	var walk func(*mdpp.Node) bool
	walk = func(n *mdpp.Node) bool {
		if n == nil || !nodeContainsOffset(n, offset, sourceLen) {
			return false
		}
		path = append(path, n)
		for _, child := range n.Children {
			if walk(child) {
				return true
			}
		}
		return true
	}
	walk(root)
	return path
}

func nodeContainsOffset(n *mdpp.Node, offset int, sourceLen int) bool {
	if n == nil {
		return false
	}
	start := n.Range.StartByte
	end := n.Range.EndByte
	if n.Type == mdpp.NodeDocument && start == 0 && end == 0 {
		end = sourceLen
	}
	if end < start {
		end = start
	}
	if offset == sourceLen {
		return offset >= start && offset <= end
	}
	return offset >= start && offset < end
}

func findFootnoteDef(root *mdpp.Node, id string) *mdpp.Node {
	var found *mdpp.Node
	root.Walk(func(n *mdpp.Node) bool {
		if n.Type == mdpp.NodeFootnoteDef && n.Attr("id") == id {
			found = n
			return false
		}
		return true
	})
	return found
}

func findHeadingByID(root *mdpp.Node, id string) *mdpp.Node {
	var found *mdpp.Node
	root.Walk(func(n *mdpp.Node) bool {
		if n.Type == mdpp.NodeHeading && mdpp.Slugify(n.Text()) == id {
			found = n
			return false
		}
		return true
	})
	return found
}

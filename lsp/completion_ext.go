package lsp

import (
	"bytes"
	"sort"
	"strings"

	"github.com/odvcencio/mdpp"
)

var commonFrontmatterKeys = []string{
	"aliases",
	"author",
	"categories",
	"date",
	"description",
	"draft",
	"lang",
	"layout",
	"mdpp",
	"published",
	"slug",
	"summary",
	"tags",
	"title",
	"updated",
}

func completionItems(doc *mdpp.Document, source []byte, prefix string) []CompletionItem {
	trimmed := strings.TrimLeft(prefix, " \t")
	token := completionToken(prefix)

	switch {
	case strings.HasPrefix(trimmed, "[[embed:"):
		return nil
	case strings.HasPrefix(trimmed, "[[") || strings.HasSuffix(trimmed, "[["):
		return directiveCompletions()
	case isFootnoteCompletionContext(token):
		if items := footnoteCompletionItems(source, footnoteCompletionPrefix(token)); len(items) > 0 {
			return items
		}
	case isReferenceCompletionContext(token):
		if items := referenceCompletionItems(source, referenceCompletionPrefix(token)); len(items) > 0 {
			return items
		}
	case isHeadingAnchorCompletionContext(token):
		if items := headingAnchorCompletionItems(doc, headingAnchorCompletionPrefix(token)); len(items) > 0 {
			return items
		}
	case isEmojiCompletionContext(token):
		if items := emojiCompletionItems(emojiCompletionPrefix(token)); len(items) > 0 {
			return items
		}
	}

	return nil
}

func completionToken(prefix string) string {
	return prefix[strings.LastIndexAny(prefix, " \t")+1:]
}

func offsetInFrontmatter(source []byte, offset int) bool {
	if offset < 0 || len(source) < 4 || !bytes.HasPrefix(source, []byte("---\n")) {
		return false
	}
	if offset <= len("---\n") {
		return true
	}
	rest := source[len("---\n"):]
	closeIdx := bytes.Index(rest, []byte("\n---\n"))
	closeLen := len("\n---\n")
	if closeIdx < 0 {
		if bytes.HasSuffix(rest, []byte("\n---")) {
			closeIdx = len(rest) - 4
			closeLen = len("\n---")
		} else {
			return false
		}
	}
	return offset < len("---\n")+closeIdx+closeLen
}

func frontmatterCompletionItems(doc *mdpp.Document, prefix string) []CompletionItem {
	if strings.HasPrefix(prefix, "---") {
		return nil
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	seen := map[string]struct{}{}
	for _, key := range commonFrontmatterKeys {
		seen[key] = struct{}{}
	}
	if fm := doc.Frontmatter(); fm != nil {
		for key := range fm {
			seen[key] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(key), prefix) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return completionItemsFromStrings(keys, completionItemKindKeyword, ": ", "Frontmatter key")
}

func footnoteCompletionItems(source []byte, prefix string) []CompletionItem {
	ids := map[string]struct{}{}
	for _, loc := range lspFootnoteDefRe.FindAllSubmatchIndex(source, -1) {
		if len(loc) < 4 || loc[2] < 0 {
			continue
		}
		ids[string(source[loc[2]:loc[3]])] = struct{}{}
	}
	values := make([]string, 0, len(ids))
	for id := range ids {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(id), strings.ToLower(prefix)) {
			continue
		}
		values = append(values, id)
	}
	sort.Strings(values)
	return completionItemsFromStrings(values, completionItemKindValue, "]", "Footnote ID")
}

func referenceCompletionItems(source []byte, prefix string) []CompletionItem {
	defs := linkDefinitionRanges(source)
	values := make([]string, 0, len(defs))
	for label := range defs {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(label), strings.ToLower(prefix)) {
			continue
		}
		values = append(values, label)
	}
	sort.Strings(values)
	return completionItemsFromStrings(values, completionItemKindValue, "]", "Reference label")
}

func headingAnchorCompletionItems(doc *mdpp.Document, prefix string) []CompletionItem {
	if doc == nil {
		return nil
	}
	seen := map[string]struct{}{}
	values := make([]string, 0, len(doc.Headings()))
	filter := strings.ToLower(prefix)
	for _, heading := range doc.Headings() {
		if heading.ID == "" {
			continue
		}
		if _, ok := seen[heading.ID]; ok {
			continue
		}
		if filter != "" && !strings.HasPrefix(strings.ToLower(heading.ID), filter) {
			continue
		}
		seen[heading.ID] = struct{}{}
		values = append(values, heading.ID)
	}
	sort.Strings(values)
	items := make([]CompletionItem, 0, len(values))
	for _, value := range values {
		items = append(items, CompletionItem{
			Label:      "#" + value,
			Kind:       completionItemKindValue,
			Detail:     "Heading anchor",
			InsertText: value,
		})
	}
	return items
}

func emojiCompletionItems(prefix string) []CompletionItem {
	prefix = strings.ToLower(prefix)
	codes := emojiShortcodes()
	values := make([]string, 0, len(codes))
	for _, code := range codes {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(code), prefix) {
			continue
		}
		values = append(values, code)
	}
	sort.Strings(values)
	return completionItemsFromStrings(values, completionItemKindValue, ":", "Emoji shortcode")
}

func completionItemsFromStrings(values []string, kind int, insertSuffix string, detail string) []CompletionItem {
	items := make([]CompletionItem, 0, len(values))
	for _, value := range values {
		items = append(items, CompletionItem{
			Label:      value,
			Kind:       kind,
			Detail:     detail,
			InsertText: value + insertSuffix,
		})
	}
	return items
}

func isFootnoteCompletionContext(prefix string) bool {
	return strings.HasPrefix(prefix, "[^")
}

func footnoteCompletionPrefix(prefix string) string {
	return strings.TrimPrefix(prefix, "[^")
}

func isReferenceCompletionContext(prefix string) bool {
	switch {
	case strings.HasPrefix(prefix, "[["):
		return false
	case strings.HasPrefix(prefix, "[^"):
		return false
	case strings.Contains(prefix, "]("):
		return false
	case strings.Contains(prefix, "]["):
		return true
	case strings.HasPrefix(prefix, "["):
		return true
	default:
		return false
	}
}

func isHeadingAnchorCompletionContext(prefix string) bool {
	return strings.Contains(prefix, "(#")
}

func referenceCompletionPrefix(prefix string) string {
	if idx := strings.LastIndex(prefix, "]["); idx >= 0 {
		return prefix[idx+2:]
	}
	if strings.HasPrefix(prefix, "[") && !strings.HasPrefix(prefix, "[[") && !strings.HasPrefix(prefix, "[^") {
		return prefix[1:]
	}
	return ""
}

func headingAnchorCompletionPrefix(prefix string) string {
	if idx := strings.LastIndex(prefix, "(#"); idx >= 0 {
		return prefix[idx+2:]
	}
	return ""
}

func isEmojiCompletionContext(prefix string) bool {
	token := emojiCompletionPrefix(prefix)
	return token != ""
}

func emojiCompletionPrefix(prefix string) string {
	token := prefix[strings.LastIndexAny(prefix, " \t")+1:]
	if !strings.HasPrefix(token, ":") || strings.HasPrefix(token, "::") {
		return ""
	}
	value := token[1:]
	if value != "" && !isEmojiFragment(value) {
		return ""
	}
	return value
}

func isEmojiFragment(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_', r == '-', r == '+':
		default:
			return false
		}
	}
	return true
}

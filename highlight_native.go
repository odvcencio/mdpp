//go:build !js || !wasm

package mdpp

import (
	"html"
	"sort"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// highlightCodeImpl parses source with the gotreesitter grammar for language,
// walks the tree, and emits <span class="hl-{category}"> wrapped tokens.
//
// Results are memoised in a process-wide LRU keyed by (language, source).
// Preview workloads re-render the document on every keystroke, so the same
// fence content flows through here many times as the user edits elsewhere;
// the cache lets those redraws skip the parse and tree walk entirely.
func highlightCodeImpl(language, source string) (string, bool) {
	if source == "" || language == "" {
		return "", false
	}

	key := hlCacheKey(language, source)
	if cached, hit := highlightHTMLCache.get(key); hit {
		return cached.html, cached.ok
	}

	entry := grammars.DetectLanguageByName(language)
	if entry == nil {
		highlightHTMLCache.put(hlCacheEntry{key: key})
		return "", false
	}
	canonicalLanguage := entry.Name
	if canonicalLanguage == "" {
		canonicalLanguage = language
	}

	lang := entry.Language()
	if lang == nil {
		highlightHTMLCache.put(hlCacheEntry{key: key})
		return "", false
	}

	src := []byte(source)

	tree, err := parsePooled(lang, entry, src)
	if err != nil || tree == nil {
		highlightHTMLCache.put(hlCacheEntry{key: key})
		return "", false
	}
	defer tree.Release()

	bt := gotreesitter.Bind(tree)
	root := bt.RootNode()
	if root == nil {
		highlightHTMLCache.put(hlCacheEntry{key: key})
		return "", false
	}

	ctx := hlCtx{
		language:   canonicalLanguage,
		bt:         bt,
		classifier: languageClassifiers[canonicalLanguage],
	}

	var spans []hlSpan
	ctx.collect(root, &spans)

	if len(spans) == 0 {
		highlightHTMLCache.put(hlCacheEntry{key: key})
		return "", false
	}

	out := renderHighlightedHTML(source, spans)
	highlightHTMLCache.put(hlCacheEntry{key: key, html: out, ok: true})
	return out, true
}

// hlSpan marks a byte range in the source with a highlight class.
type hlSpan struct {
	start uint32
	end   uint32
	class string
}

// hlCtx carries language context through the recursive tree walk so that
// classify() can consult the per-language classifier table without globals.
type hlCtx struct {
	language   string
	bt         *gotreesitter.BoundTree
	classifier *languageClassifier
}

// collect walks the tree, recording a span for any leaf-ish node that
// classifies to a non-empty highlight class. We stop descending past a
// classified node to avoid double-decoration.
func (c *hlCtx) collect(n *gotreesitter.Node, spans *[]hlSpan) {
	if n == nil {
		return
	}
	if class := c.classify(n); class != "" && n.EndByte() > n.StartByte() {
		*spans = append(*spans, hlSpan{
			start: n.StartByte(),
			end:   n.EndByte(),
			class: class,
		})
		return
	}
	for i := 0; i < n.ChildCount(); i++ {
		c.collect(n.Child(i), spans)
	}
}

// classify maps a tree-sitter node to a highlight CSS class. The per-language
// classifier table wins; where it has no opinion, we fall back to structural
// call-site detection and generic heuristics that are safe across languages.
func (c *hlCtx) classify(n *gotreesitter.Node) string {
	nodeType := c.bt.NodeType(n)

	// 1. Per-language exact node-type match.
	if c.classifier != nil {
		if class, ok := c.classifier.exact[nodeType]; ok {
			return class
		}
	}

	// 2. Structural function-call detection. The per-language callParent
	//    overrides the default "call_expression" rule; an empty callParent
	//    opts out entirely (e.g. Java, whose method_invocation has different
	//    structure).
	if class := c.classifyCallSite(n, nodeType); class != "" {
		return class
	}

	text := c.bt.NodeText(n)

	// 3. Per-language keyword text.
	if c.classifier != nil {
		if c.classifier.keywords[text] {
			return "hl-keyword"
		}
	}

	// 4. Generic fallback: only runs for languages without a classifier
	//    table. We intentionally do not run the heuristic matches for
	//    classified languages — the table is the source of truth there.
	if c.classifier == nil {
		return classifyGeneric(nodeType, text)
	}

	// 5. For classified languages, still catch universal punctuation/operator
	//    tokens that would be tedious to enumerate per-language.
	if isHLOperator(text) {
		return "hl-operator"
	}
	if isHLPunctuation(text) {
		return "hl-punctuation"
	}
	return ""
}

// classifyCallSite returns "hl-function" when n names a function — either
// the declared name in a function/method definition, or the callee of a call
// expression. Returns "" when n is not in a function-naming position.
func (c *hlCtx) classifyCallSite(n *gotreesitter.Node, nodeType string) string {
	if nodeType != "identifier" && nodeType != "field_identifier" && nodeType != "property_identifier" {
		return ""
	}

	parent := n.Parent()
	if parent == nil {
		return ""
	}
	parentType := c.bt.NodeType(parent)

	// Function declarations across languages. The direct identifier child of
	// these node types is the declared function name. Nested parameter names
	// sit deeper (parameter_list → parameter_declaration → identifier) and
	// are not affected.
	switch parentType {
	case "function_declaration", "method_declaration",
		"function_definition", "function_item",
		"method_definition":
		return "hl-function"
	}

	// Function calls — the callee's first child under the language's
	// configured call node type.
	callParent := "call_expression"
	if c.classifier != nil {
		callParent = c.classifier.callParent
	}
	if callParent == "" {
		return ""
	}

	if parentType == callParent && parent.Child(0) == n {
		return "hl-function"
	}

	// Member access as the callee (e.g. fmt.Println, x.foo()). Only the
	// selected member — a field_identifier in Go, property_identifier in
	// JS/TS — classifies as a function. A plain identifier child of the
	// selector is the receiver, not the callee.
	if parentType == "selector_expression" || parentType == "member_expression" {
		if nodeType != "field_identifier" && nodeType != "property_identifier" {
			return ""
		}
		if grandparent := parent.Parent(); grandparent != nil &&
			c.bt.NodeType(grandparent) == callParent &&
			grandparent.Child(0) == parent {
			return "hl-function"
		}
	}
	return ""
}

// classifyGeneric is the legacy heuristic classifier. It runs only for
// languages that don't have a classifier table — shallow, string-matching,
// and occasionally wrong, but better than nothing for the long tail of
// grammars we haven't written a table for.
func classifyGeneric(nodeType, text string) string {
	if strings.Contains(nodeType, "comment") {
		return "hl-comment"
	}
	if strings.Contains(nodeType, "string") {
		return "hl-string"
	}

	switch nodeType {
	case "interpreted_string_literal", "raw_string_literal", "string_literal",
		"template_string", "string_content", "escape_sequence":
		return "hl-string"
	case "int_literal", "float_literal", "imaginary_literal", "rune_literal",
		"number", "integer", "float":
		return "hl-number"
	case "type_identifier":
		return "hl-type"
	}

	switch text {
	case "true", "false", "True", "False":
		return "hl-keyword"
	case "nil", "null", "undefined", "None":
		return "hl-keyword"
	}

	if hlKeywords[nodeType] || hlKeywords[text] {
		return "hl-keyword"
	}

	// Uppercase-initial identifier heuristic — Go-style convention.
	// Intentionally absent from the per-language path where tables decide.
	if nodeType == "identifier" && len(text) > 0 && text[0] >= 'A' && text[0] <= 'Z' {
		return "hl-type"
	}

	if isHLOperator(nodeType) || isHLOperator(text) {
		return "hl-operator"
	}
	if isHLPunctuation(nodeType) {
		return "hl-punctuation"
	}

	return ""
}

// renderHighlightedHTML renders source with highlight spans applied.
func renderHighlightedHTML(source string, spans []hlSpan) string {
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end < spans[j].end
		}
		return spans[i].start < spans[j].start
	})

	src := []byte(source)
	pos := uint32(0)
	end := uint32(len(src))

	var b strings.Builder
	b.Grow(len(source) * 2)

	for _, span := range spans {
		if span.start < pos || span.end > end || span.end <= pos {
			continue
		}
		if span.start > pos {
			b.WriteString(html.EscapeString(string(src[pos:span.start])))
		}
		b.WriteString(`<span class="`)
		b.WriteString(span.class)
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(string(src[span.start:span.end])))
		b.WriteString(`</span>`)
		pos = span.end
	}
	if pos < end {
		b.WriteString(html.EscapeString(string(src[pos:end])))
	}
	return b.String()
}

// hlKeywords is the fallback keyword set consulted by classifyGeneric for
// languages without a per-language table. Per-language keywords live in
// highlight_classify.go.
var hlKeywords = map[string]bool{
	"keyword": true,

	"async": true, "await": true, "break": true, "case": true, "chan": true,
	"class": true, "const": true, "continue": true, "default": true, "def": true,
	"defer": true, "del": true, "elif": true, "else": true, "except": true,
	"export": true, "extends": true, "fallthrough": true, "finally": true,
	"for": true, "from": true, "func": true, "function": true, "go": true,
	"goto": true, "if": true, "import": true, "in": true, "interface": true,
	"is": true, "lambda": true, "let": true, "map": true, "new": true,
	"not": true, "or": true, "and": true, "package": true, "pass": true,
	"raise": true, "range": true, "return": true, "select": true,
	"struct": true, "switch": true, "try": true, "type": true, "var": true,
	"while": true, "with": true, "yield": true,
}

func isHLOperator(token string) bool {
	switch token {
	case "+", "-", "*", "/", "%", "=", "==", "!=", "<", ">", "<=", ">=",
		":=", "&&", "||", "!", "...", "**", "//", "+=", "-=", "*=", "/=",
		"&", "|", "^", "~", "<<", ">>":
		return true
	default:
		return false
	}
}

func isHLPunctuation(token string) bool {
	switch token {
	case "(", ")", "{", "}", "[", "]", ",", ".", ":", ";":
		return true
	default:
		return false
	}
}

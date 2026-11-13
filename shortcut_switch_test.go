package mdpp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// shortcutEntries are the shortcut readers: functions that build block nodes
// without the grammar. Each one asks parseCtx.shortcut first, with its own name.
var shortcutEntries = []string{
	"parseCommonMarkHTMLBlocks",
	"parseContainerDocumentCtx",
	"parseDeepNestedListDocument",
	"parseAllIndentedDocument",
	"parseFastBlockChunk",
	"parseLeadingThematicBreaks",
	"parseOnlyReferenceDefinitions",
	"parseSegmentedDocumentCtx",
	"parseSimpleBlockquoteDocument",
}

// notShortcuts are the other functions that take source bytes and return a
// document. Each is on this list with the reason it does not read blocks
// without the grammar. A function that returns a *Document and is on neither
// list fails the test: a new reader must be classified.
var notShortcuts = map[string]string{
	"Parse":                      "public API",
	"ParseWithOptions":           "public API",
	"ParseContext":               "public API",
	"ParseContextWithOptions":    "public API",
	"MustParse":                  "public API",
	"ParseWithTree":              "public API",
	"ParseIncremental":           "public API",
	"parseDocument":              "the grammar path",
	"parseDocumentCtx":           "the grammar path",
	"parseDocumentRetainTree":    "the grammar path",
	"parseDocumentRetainTreeCtx": "the grammar path",
	"parseLoweredDocumentCtx":    "the grammar path",
	"parseBodyChunkCtx":          "re-enters the grammar path for a container body",
	"parseContainerDocument":     "default-profile wrapper of parseContainerDocumentCtx",
	"parseLongLineFallback":      "a limit, not a reader: a line over the complexity limit is kept as text with a diagnostic",
	"deepListUncertainDocument":  "a limit, not a reader: the source as text with a diagnostic",
	"budgetFallback":             "a limit, not a reader: the source as text with a diagnostic",
	"textFallbackDocument":       "a limit, not a reader: the source as text with a diagnostic",
	"finishBudgetedParse":        "post-processing of a finished parse",
	"SplitSlides":                "operates on a finished document",
}

// functionsReturningDocument lists the functions (not methods) of the package
// that have a *Document among their results, with their bodies.
func functionsReturningDocument(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := map[string]*ast.FuncDecl{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Type.Results == nil {
				continue
			}
			for _, result := range fn.Type.Results.List {
				if star, ok := result.Type.(*ast.StarExpr); ok {
					if id, ok := star.X.(*ast.Ident); ok && id.Name == "Document" {
						found[fn.Name.Name] = fn
					}
				}
			}
		}
	}
	return found
}

// consultsSwitch reports whether the body calls shortcut with the function's
// own name as its argument.
func consultsSwitch(fn *ast.FuncDecl) bool {
	consults := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "shortcut" || len(call.Args) != 1 {
			return true
		}
		if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if name, err := strconv.Unquote(lit.Value); err == nil && name == fn.Name.Name {
				consults = true
			}
		}
		return true
	})
	return consults
}

// Every function that returns a document is a listed shortcut entry (which
// consults the switch first) or is listed as not a shortcut, with its reason.
// A new reader that is on neither list fails here.
func TestEveryShortcutEntryConsultsTheSwitch(t *testing.T) {
	found := functionsReturningDocument(t)
	isEntry := map[string]bool{}
	for _, name := range shortcutEntries {
		isEntry[name] = true
	}
	var names []string
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fn := found[name]
		switch {
		case isEntry[name]:
			if !consultsSwitch(fn) {
				t.Errorf("shortcut entry %s does not call ctx.shortcut(%q)", name, name)
			}
		case notShortcuts[name] != "":
		default:
			t.Errorf("%s returns a *Document and is on neither list: if it builds block nodes without the grammar, "+
				"add it to shortcutEntries and call ctx.shortcut(%q) first; otherwise add it to notShortcuts with the reason", name, name)
		}
	}
	for _, name := range shortcutEntries {
		if found[name] == nil {
			// parseFastBlockChunk returns (*Document, bool): found above too.
			t.Errorf("shortcut entry %s is not a function of the package that returns a document", name)
		}
	}
	for name := range notShortcuts {
		if found[name] == nil {
			t.Errorf("notShortcuts lists %s, which is not a function that returns a document: remove it", name)
		}
	}
}

// In the GFM profile no shortcut entry is allowed, on any input of the
// specification examples and the differential corpus; in the default profile
// the same entries are allowed. Every entry is reached, so each one is seen to
// consult the switch at run time and not only in the source.
func TestGFMProfileTakesNoShortcut(t *testing.T) {
	type tally struct{ allowed, refused int }
	seen := map[string]*tally{}
	shortcutObserver = func(entry string, allowed bool) {
		c := seen[entry]
		if c == nil {
			c = &tally{}
			seen[entry] = c
		}
		if allowed {
			c.allowed++
		} else {
			c.refused++
		}
	}
	defer func() { shortcutObserver = nil }()

	inputs := specAndConformanceInputs(t)
	if len(inputs) > 3000 {
		inputs = inputs[:3000]
	}
	inputs = append(inputs, differentialInputs()[:3000]...)
	inputs = append(inputs,
		"[a]: /u\n", "    code\n", "> q\n> r\n", ":::note\nx\n:::\n", "<div>\nx\n</div>\n", "***\n# h\n", "# a\n\n# b\n",
		"- a\n  - a\n    - a\n      - a\n        - a\n          - a\n")

	for _, src := range inputs {
		doc, err := ParseWithOptions([]byte(src), ParseOptions{Profile: ProfileGFM})
		if err != nil || doc == nil {
			t.Fatalf("%q: %v", src, err)
		}
	}
	for name, c := range seen {
		if c.allowed != 0 {
			t.Errorf("GFM profile: shortcut %s was allowed %d times", name, c.allowed)
		}
	}
	for _, name := range shortcutEntries {
		if name == "parseFastBlockChunk" {
			continue // reached only through parseSegmentedDocumentCtx, which the GFM profile refuses
		}
		if seen[name] == nil {
			t.Errorf("GFM profile: shortcut %s was never consulted on %d inputs; it is not reached or it does not consult the switch", name, len(inputs))
		}
	}

	seen = map[string]*tally{}
	for _, src := range inputs {
		if _, err := Parse([]byte(src)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range shortcutEntries {
		if c := seen[name]; c == nil || c.allowed == 0 {
			t.Errorf("default profile: shortcut %s was never allowed", name)
		}
		if c := seen[name]; c != nil && c.refused != 0 {
			t.Errorf("default profile: shortcut %s was refused %d times", name, c.refused)
		}
	}
}

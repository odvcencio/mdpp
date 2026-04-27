//go:build !js || !wasm

package mdpp

// languageClassifier is a per-language mapping from tree-sitter node types
// (and selected text tokens) to highlight CSS classes. It is consulted before
// the generic heuristic classifier; entries here override anything the
// fallback would produce.
//
// callParent names the node type whose first child is the function name in a
// call expression. When set, an identifier whose parent is that type and is
// its first child is classified as "hl-function". Empty string disables the
// per-language call-site rule and lets the generic rule handle it.
type languageClassifier struct {
	exact      map[string]string
	keywords   map[string]bool
	callParent string
}

// languageClassifiers maps language names (as returned by
// gotreesitter.LangEntry.Name) to their classifier tables. Languages absent
// here fall through to the generic heuristic classifier.
var languageClassifiers = map[string]*languageClassifier{
	"go":         goClassifier,
	"rust":       rustClassifier,
	"python":     pythonClassifier,
	"javascript": jsClassifier,
	"typescript": tsClassifier,
	"tsx":        tsClassifier,
	"c":          cClassifier,
	"cpp":        cppClassifier,
	"java":       javaClassifier,
	"bash":       bashClassifier,
	"json":       jsonClassifier,
}

func kwSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

var goClassifier = &languageClassifier{
	exact: map[string]string{
		"comment":                    "hl-comment",
		"interpreted_string_literal": "hl-string",
		"raw_string_literal":         "hl-string",
		"rune_literal":               "hl-string",
		"escape_sequence":            "hl-string",
		"string_content":             "hl-string",
		"int_literal":                "hl-number",
		"float_literal":              "hl-number",
		"imaginary_literal":          "hl-number",
		"true":                       "hl-keyword",
		"false":                      "hl-keyword",
		"nil":                        "hl-keyword",
		"iota":                       "hl-keyword",
		"type_identifier":            "hl-type",
		"package_identifier":         "hl-type",
	},
	keywords: kwSet(
		"break", "case", "chan", "const", "continue", "default", "defer",
		"else", "fallthrough", "for", "func", "go", "goto", "if", "import",
		"interface", "map", "package", "range", "return", "select", "struct",
		"switch", "type", "var",
	),
	callParent: "call_expression",
}

var rustClassifier = &languageClassifier{
	exact: map[string]string{
		"line_comment":       "hl-comment",
		"block_comment":      "hl-comment",
		"string_literal":     "hl-string",
		"raw_string_literal": "hl-string",
		"char_literal":       "hl-string",
		"escape_sequence":    "hl-string",
		"integer_literal":    "hl-number",
		"float_literal":      "hl-number",
		"boolean_literal":    "hl-keyword",
		"type_identifier":    "hl-type",
		"primitive_type":     "hl-type",
	},
	keywords: kwSet(
		"let", "mut", "fn", "pub", "struct", "enum", "impl", "trait", "use",
		"mod", "match", "if", "else", "for", "while", "loop", "return",
		"break", "continue", "in", "as", "ref", "self", "Self", "super",
		"crate", "type", "where", "static", "const", "unsafe", "async",
		"await", "move", "dyn", "box",
	),
	callParent: "call_expression",
}

var pythonClassifier = &languageClassifier{
	exact: map[string]string{
		"comment":         "hl-comment",
		"string":          "hl-string",
		"string_start":    "hl-string",
		"string_content":  "hl-string",
		"string_end":      "hl-string",
		"escape_sequence": "hl-string",
		"integer":         "hl-number",
		"float":           "hl-number",
		"true":            "hl-keyword",
		"false":           "hl-keyword",
		"none":            "hl-keyword",
		"type":            "hl-type",
	},
	keywords: kwSet(
		"def", "class", "return", "if", "elif", "else", "for", "while",
		"break", "continue", "pass", "import", "from", "as", "with", "try",
		"except", "finally", "raise", "lambda", "global", "nonlocal", "yield",
		"async", "await", "and", "or", "not", "in", "is",
		"True", "False", "None",
	),
	callParent: "call",
}

var jsClassifier = &languageClassifier{
	exact: map[string]string{
		"comment":         "hl-comment",
		"string":          "hl-string",
		"template_string": "hl-string",
		"string_fragment": "hl-string",
		"regex":           "hl-string",
		"escape_sequence": "hl-string",
		"number":          "hl-number",
		"true":            "hl-keyword",
		"false":           "hl-keyword",
		"null":            "hl-keyword",
		"undefined":       "hl-keyword",
	},
	keywords: kwSet(
		"function", "const", "let", "var", "class", "extends", "import",
		"export", "from", "default", "if", "else", "for", "while", "do",
		"return", "break", "continue", "switch", "case", "try", "catch",
		"finally", "throw", "new", "this", "super", "typeof", "instanceof",
		"in", "of", "async", "await", "yield", "void", "delete",
	),
	callParent: "call_expression",
}

var tsClassifier = &languageClassifier{
	exact: map[string]string{
		"comment":         "hl-comment",
		"string":          "hl-string",
		"template_string": "hl-string",
		"string_fragment": "hl-string",
		"regex":           "hl-string",
		"escape_sequence": "hl-string",
		"number":          "hl-number",
		"true":            "hl-keyword",
		"false":           "hl-keyword",
		"null":            "hl-keyword",
		"undefined":       "hl-keyword",
		"type_identifier": "hl-type",
		"predefined_type": "hl-type",
	},
	keywords: kwSet(
		"function", "const", "let", "var", "class", "extends", "import",
		"export", "from", "default", "if", "else", "for", "while", "do",
		"return", "break", "continue", "switch", "case", "try", "catch",
		"finally", "throw", "new", "this", "super", "typeof", "instanceof",
		"in", "of", "async", "await", "yield", "void", "delete",
		"type", "interface", "enum", "namespace", "declare", "readonly",
		"private", "public", "protected", "abstract", "implements", "as",
		"satisfies", "keyof", "is",
	),
	callParent: "call_expression",
}

var cClassifier = &languageClassifier{
	exact: map[string]string{
		"comment":              "hl-comment",
		"string_literal":       "hl-string",
		"char_literal":         "hl-string",
		"system_lib_string":    "hl-string",
		"escape_sequence":      "hl-string",
		"preproc_arg":          "hl-string",
		"number_literal":       "hl-number",
		"true":                 "hl-keyword",
		"false":                "hl-keyword",
		"null":                 "hl-keyword",
		"primitive_type":       "hl-type",
		"type_identifier":      "hl-type",
		"sized_type_specifier": "hl-type",
	},
	keywords: kwSet(
		"int", "char", "short", "long", "void", "float", "double", "signed",
		"unsigned", "bool", "struct", "enum", "union", "typedef", "return",
		"if", "else", "for", "while", "do", "break", "continue", "switch",
		"case", "default", "static", "extern", "const", "volatile", "sizeof",
		"goto", "inline", "register", "auto", "restrict",
	),
	callParent: "call_expression",
}

var cppClassifier = &languageClassifier{
	exact: map[string]string{
		"comment":              "hl-comment",
		"string_literal":       "hl-string",
		"char_literal":         "hl-string",
		"raw_string_literal":   "hl-string",
		"system_lib_string":    "hl-string",
		"escape_sequence":      "hl-string",
		"preproc_arg":          "hl-string",
		"number_literal":       "hl-number",
		"true":                 "hl-keyword",
		"false":                "hl-keyword",
		"nullptr":              "hl-keyword",
		"this":                 "hl-keyword",
		"primitive_type":       "hl-type",
		"type_identifier":      "hl-type",
		"sized_type_specifier": "hl-type",
		"auto":                 "hl-type",
	},
	keywords: kwSet(
		"int", "char", "short", "long", "void", "float", "double", "signed",
		"unsigned", "bool", "struct", "enum", "union", "typedef", "return",
		"if", "else", "for", "while", "do", "break", "continue", "switch",
		"case", "default", "static", "extern", "const", "volatile", "sizeof",
		"goto", "inline", "class", "public", "private", "protected",
		"template", "typename", "namespace", "using", "new", "delete",
		"throw", "try", "catch", "virtual", "override", "final", "constexpr",
		"mutable", "friend", "decltype", "explicit", "operator", "static_cast",
		"dynamic_cast", "const_cast", "reinterpret_cast",
	),
	callParent: "call_expression",
}

var javaClassifier = &languageClassifier{
	exact: map[string]string{
		"line_comment":                   "hl-comment",
		"block_comment":                  "hl-comment",
		"string_literal":                 "hl-string",
		"character_literal":              "hl-string",
		"escape_sequence":                "hl-string",
		"decimal_integer_literal":        "hl-number",
		"decimal_floating_point_literal": "hl-number",
		"octal_integer_literal":          "hl-number",
		"hex_integer_literal":            "hl-number",
		"binary_integer_literal":         "hl-number",
		"true":                           "hl-keyword",
		"false":                          "hl-keyword",
		"null_literal":                   "hl-keyword",
		"type_identifier":                "hl-type",
		"void_type":                      "hl-type",
		"boolean_type":                   "hl-type",
		"integral_type":                  "hl-type",
		"floating_point_type":            "hl-type",
	},
	keywords: kwSet(
		"package", "import", "public", "private", "protected", "class",
		"interface", "enum", "extends", "implements", "static", "final",
		"abstract", "native", "synchronized", "transient", "volatile",
		"new", "this", "super", "return", "if", "else", "for", "while",
		"do", "break", "continue", "switch", "case", "default", "try",
		"catch", "finally", "throw", "throws", "instanceof",
	),
	// method_invocation has distinct structure (object.method()) that the
	// generic first-child rule misclassifies. Leave callParent empty.
	callParent: "",
}

var bashClassifier = &languageClassifier{
	exact: map[string]string{
		"comment":              "hl-comment",
		"string":               "hl-string",
		"raw_string":           "hl-string",
		"ansi_c_string":        "hl-string",
		"expansion":            "hl-string",
		"simple_expansion":     "hl-string",
		"command_substitution": "hl-string",
		"number":               "hl-number",
	},
	keywords: kwSet(
		"if", "then", "else", "elif", "fi", "for", "in", "do", "done",
		"while", "until", "case", "esac", "function", "return", "break",
		"continue", "local", "export", "readonly", "declare", "unset",
		"source",
	),
}

var jsonClassifier = &languageClassifier{
	exact: map[string]string{
		"comment":         "hl-comment",
		"string":          "hl-string",
		"string_content":  "hl-string",
		"escape_sequence": "hl-string",
		"number":          "hl-number",
		"true":            "hl-keyword",
		"false":           "hl-keyword",
		"null":            "hl-keyword",
	},
}

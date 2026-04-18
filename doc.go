// Package mdpp parses and renders Markdown++ documents.
//
// MDPP keeps Markdown-compatible prose as the base syntax and adds
// structured nodes for technical writing features such as frontmatter,
// admonitions, footnotes, math, emoji shortcodes, task lists, and diagram
// fences. The parser is backed by gotreesitter and returns an AST that can
// be used by renderers, editors, formatters, diagnostics, and language
// service features.
package mdpp

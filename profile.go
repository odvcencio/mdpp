package mdpp

// ParseProfile selects the dialect that a parse reads.
//
// The zero value, ProfileMarkdownPlus, is Markdown++: CommonMark with the GFM
// extensions and every Markdown++ extension. Other profiles read the same
// source as a narrower dialect. A parse reads one profile throughout; a
// Document reports it with Document.Profile.
type ParseProfile int

const (
	// ProfileMarkdownPlus reads Markdown++, the default.
	ProfileMarkdownPlus ParseProfile = iota
	// ProfileGFM reads the source as GitHub-flavoured Markdown (the GFM
	// specification 0.29-gfm: CommonMark 0.31.2 plus tables, task list items,
	// strikethrough, autolinks and, at render time, the tag filter). Every
	// Markdown++ extension is off, unless GFM has the construct:
	//
	//   - components and expressions: an uppercase or mixed-case tag is HTML
	//     and "{x}" is text;
	//   - container directives: ":::note" is text;
	//   - admonitions and alerts: "> [!NOTE]" is a block quote and its
	//     continuation lines follow the lazy continuation rule;
	//   - the merge of lists that a paragraph interrupts: they stay two lists;
	//   - front matter: a leading "---" is a thematic break or a setext
	//     underline, and no YAML is read;
	//   - table of contents ("[[toc]]"), embeds ("[[embed:url]]"), diagrams
	//     (a fence keeps its code block), slides, math ("$x$", "$$"),
	//     footnotes ("[^1]: x" is a link reference definition), definition
	//     lists, superscript ("^x^"), emoji shortcodes, and the "highlights="
	//     fence attribute;
	//   - a single tilde pair ("~x~") is strikethrough, as in GFM, not
	//     subscript.
	//
	// Task list items, tables, strikethrough, autolinks and the reading of
	// reference links stay. Ranges, diagnostics and the budgets behave as in
	// the default profile.
	ProfileGFM
)

// String returns the profile name used by the command-line tool.
func (p ParseProfile) String() string {
	switch p {
	case ProfileMarkdownPlus:
		return "markdown++"
	case ProfileGFM:
		return "gfm"
	}
	return "unknown"
}

// ParseProfileByName returns the profile that has name ("markdown++", "mdpp"
// or "gfm"), and false for any other name.
func ParseProfileByName(name string) (ParseProfile, bool) {
	switch name {
	case "markdown++", "mdpp", "default", "":
		return ProfileMarkdownPlus, true
	case "gfm":
		return ProfileGFM, true
	}
	return ProfileMarkdownPlus, false
}

// gfm reports whether the parse reads the GFM profile. It is safe on a nil
// context, which reads the default profile.
func (c *parseCtx) gfm() bool {
	return c != nil && c.profile == ProfileGFM
}

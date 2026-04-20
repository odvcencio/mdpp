package mdpp

import (
	"html"
	"strconv"
	"strings"
	"unicode"
)

// renderNode recursively renders an AST node to HTML. Returns a string
// for convenience at the top level, but internally delegates to
// renderNodeInto which writes directly into a shared strings.Builder —
// that avoids the per-node fmt.Sprintf + intermediate string allocation
// that the previous implementation paid for every AST node.
func renderNode(r *Renderer, n *Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	renderNodeInto(r, &b, n)
	return b.String()
}

// renderNodeInto writes the HTML for n into b. Internal recursive
// entry point — callers that already have a builder should use this
// directly to avoid an intermediate string alloc per subtree.
func renderNodeInto(r *Renderer, b *strings.Builder, n *Node) {
	if n == nil {
		return
	}

	switch n.Type {
	case NodeDocument:
		renderChildrenInto(r, b, n)

	case NodeHeading:
		level := n.Attrs["level"]
		if level == "" {
			level = "1"
		}
		b.WriteString("<h")
		b.WriteString(level)
		if r.headingIDs {
			id := slugify(collectNodeText(n))
			b.WriteString(` id="`)
			b.WriteString(id)
			b.WriteByte('"')
		}
		writeSourceAttrs(r, b, n)
		b.WriteByte('>')
		renderChildrenInto(r, b, n)
		b.WriteString("</h")
		b.WriteString(level)
		b.WriteString(">\n")

	case NodeParagraph:
		b.WriteString("<p")
		writeSourceAttrs(r, b, n)
		b.WriteByte('>')
		renderChildrenInto(r, b, n)
		b.WriteString("</p>\n")

	case NodeCodeBlock:
		lang := n.Attrs["language"]
		if r.highlightCode && lang != "" {
			if highlighted, ok := highlightCode(lang, n.Literal); ok {
				b.WriteString(`<pre`)
				writeSourceAttrs(r, b, n)
				b.WriteString(`><code class="language-`)
				b.WriteString(html.EscapeString(lang))
				b.WriteString(`">`)
				b.WriteString(highlighted)
				b.WriteString("</code></pre>\n")
				return
			}
		}
		code := html.EscapeString(n.Literal)
		if lang != "" {
			b.WriteString(`<pre`)
			writeSourceAttrs(r, b, n)
			b.WriteString(`><code class="language-`)
			b.WriteString(html.EscapeString(lang))
			b.WriteString(`">`)
			b.WriteString(code)
			b.WriteString("</code></pre>\n")
			return
		}
		b.WriteString("<pre")
		writeSourceAttrs(r, b, n)
		b.WriteString("><code>")
		b.WriteString(code)
		b.WriteString("</code></pre>\n")

	case NodeDiagram:
		renderDiagramInto(b, n)

	case NodeFrontmatter:
		return

	case NodeBlockquote:
		b.WriteString("<blockquote")
		writeSourceAttrs(r, b, n)
		b.WriteString(">\n")
		renderChildrenInto(r, b, n)
		b.WriteString("</blockquote>\n")

	case NodeList:
		tag := "ul"
		if n.Attrs != nil && n.Attrs["ordered"] == "true" {
			tag = "ol"
		}
		b.WriteByte('<')
		b.WriteString(tag)
		if tag == "ol" && n.Attrs != nil && n.Attrs["start"] != "" {
			b.WriteString(` start="`)
			b.WriteString(html.EscapeString(n.Attrs["start"]))
			b.WriteByte('"')
		}
		writeSourceAttrs(r, b, n)
		b.WriteString(">\n")
		renderChildrenInto(r, b, n)
		b.WriteString("</")
		b.WriteString(tag)
		b.WriteString(">\n")

	case NodeListItem:
		b.WriteString("<li")
		writeSourceAttrs(r, b, n)
		b.WriteByte('>')
		// Render children into a temporary builder so we can trim the
		// trailing newline cleanly. The child nodes are typically a
		// single paragraph so the temp cost is bounded.
		var inner strings.Builder
		renderChildrenInto(r, &inner, n)
		b.WriteString(strings.TrimRight(inner.String(), "\n"))
		b.WriteString("</li>\n")

	case NodeTable:
		renderTableInto(r, b, n)

	case NodeThematicBreak:
		b.WriteString("<hr")
		writeSourceAttrs(r, b, n)
		b.WriteString(" />\n")

	case NodeLink:
		href := html.EscapeString(n.Attrs["href"])
		if href == "" {
			if raw := n.Attrs["raw"]; raw != "" {
				b.WriteString(html.EscapeString(raw))
			} else {
				renderChildrenInto(r, b, n)
			}
			return
		}
		title := n.Attrs["title"]
		b.WriteString(`<a href="`)
		b.WriteString(href)
		b.WriteByte('"')
		if title != "" {
			b.WriteString(` title="`)
			b.WriteString(html.EscapeString(title))
			b.WriteByte('"')
		}
		b.WriteByte('>')
		renderChildrenInto(r, b, n)
		b.WriteString("</a>")

	case NodeImage:
		src := n.Attrs["src"]
		if r.imageResolver != nil {
			src = r.imageResolver(src)
		}
		alt := html.EscapeString(n.Attrs["alt"])
		src = html.EscapeString(src)
		title := n.Attrs["title"]
		if title != "" {
			b.WriteString(`<figure`)
			writeSourceAttrs(r, b, n)
			b.WriteString(`><img src="`)
			b.WriteString(src)
			b.WriteString(`" alt="`)
			b.WriteString(alt)
			b.WriteString(`" /><figcaption>`)
			b.WriteString(html.EscapeString(title))
			b.WriteString("</figcaption></figure>")
			return
		}
		b.WriteString(`<img`)
		writeSourceAttrs(r, b, n)
		b.WriteString(` src="`)
		b.WriteString(src)
		b.WriteString(`" alt="`)
		b.WriteString(alt)
		b.WriteString(`" />`)

	case NodeEmphasis:
		b.WriteString("<em>")
		renderChildrenInto(r, b, n)
		b.WriteString("</em>")

	case NodeStrong:
		b.WriteString("<strong>")
		renderChildrenInto(r, b, n)
		b.WriteString("</strong>")

	case NodeStrikethrough:
		b.WriteString("<del>")
		renderChildrenInto(r, b, n)
		b.WriteString("</del>")

	case NodeCodeSpan:
		b.WriteString("<code>")
		b.WriteString(html.EscapeString(n.Literal))
		b.WriteString("</code>")

	case NodeText:
		b.WriteString(html.EscapeString(n.Literal))

	case NodeHTMLBlock:
		if r.unsafeHTML {
			b.WriteString(n.Literal)
		} else {
			b.WriteString(html.EscapeString(n.Literal))
		}

	case NodeHTMLInline:
		if r.unsafeHTML {
			b.WriteString(n.Literal)
		} else {
			b.WriteString(html.EscapeString(n.Literal))
		}

	case NodeSoftBreak:
		if r.hardWraps {
			b.WriteString("<br />\n")
		} else {
			b.WriteByte('\n')
		}

	case NodeHardBreak:
		b.WriteString("<br />\n")

	case NodeAdmonition:
		adType := n.Attrs["type"]
		title := strings.ToUpper(adType)
		if n.Attrs["title"] != "" {
			title = n.Attrs["title"]
		}
		b.WriteString(`<div class="admonition admonition-`)
		b.WriteString(adType)
		b.WriteByte('"')
		writeSourceAttrs(r, b, n)
		b.WriteString(`><p class="admonition-title">`)
		renderAdmonitionTitleInto(r, b, title)
		b.WriteString("</p>")
		renderChildrenInto(r, b, n)
		b.WriteString("</div>\n")

	case NodeContainerDirective:
		renderContainerDirectiveInto(r, b, n)

	case NodeTaskListItem:
		renderTaskListItemInto(r, b, n)

	case NodeFootnoteRef:
		id := html.EscapeString(n.Attrs["id"])
		b.WriteString(`<sup><a class="footnote-ref" href="#fn-`)
		b.WriteString(id)
		b.WriteString(`" id="fnref-`)
		b.WriteString(id)
		b.WriteString(`">[`)
		b.WriteString(id)
		b.WriteString("]</a></sup>")

	case NodeFootnoteDef:
		id := html.EscapeString(n.Attrs["id"])
		b.WriteString(`<section class="footnotes"><ol><li id="fn-`)
		b.WriteString(id)
		b.WriteString(`">`)
		renderChildrenInto(r, b, n)
		b.WriteString(` <a href="#fnref-`)
		b.WriteString(id)
		b.WriteString(`">\u21a9</a></li></ol></section>`)
		b.WriteByte('\n')

	case NodeMathInline:
		if r.math == MathOmit {
			return
		}
		if r.math == MathRaw {
			b.WriteString(`\(`)
			b.WriteString(html.EscapeString(n.Literal))
			b.WriteString(`\)`)
			return
		}
		b.WriteString(`<span class="math-inline">`)
		b.WriteString(renderLatexMath(n.Literal))
		b.WriteString("</span>")

	case NodeMathBlock:
		if r.math == MathOmit {
			return
		}
		if r.math == MathRaw {
			b.WriteString(`\[`)
			b.WriteString(html.EscapeString(n.Literal))
			b.WriteString(`\]`)
			return
		}
		b.WriteString(`<div class="math-block">`)
		b.WriteString(renderLatexMath(n.Literal))
		b.WriteString("</div>\n")

	case NodeSuperscript:
		b.WriteString("<sup>")
		b.WriteString(html.EscapeString(n.Literal))
		b.WriteString("</sup>")

	case NodeSubscript:
		b.WriteString("<sub>")
		b.WriteString(html.EscapeString(n.Literal))
		b.WriteString("</sub>")

	case NodeEmoji:
		if r.wrapEmoji {
			code := n.Attrs["code"]
			b.WriteString(`<span class="emoji" role="img" aria-label="`)
			b.WriteString(html.EscapeString(code))
			b.WriteString(`">`)
			b.WriteString(n.Literal)
			b.WriteString("</span>")
		} else {
			b.WriteString(n.Literal)
		}

	case NodeTableOfContents:
		b.WriteString(`<nav class="mdpp-toc" aria-label="Table of contents"`)
		writeSourceAttrs(r, b, n)
		b.WriteString(">\n")
		renderChildrenInto(r, b, n)
		b.WriteString("</nav>\n")

	case NodeDefinitionList:
		b.WriteString("<dl")
		writeSourceAttrs(r, b, n)
		b.WriteString(">\n")
		renderChildrenInto(r, b, n)
		b.WriteString("</dl>\n")

	case NodeDefinitionTerm:
		b.WriteString("<dt")
		writeSourceAttrs(r, b, n)
		b.WriteByte('>')
		renderChildrenInto(r, b, n)
		b.WriteString("</dt>\n")

	case NodeDefinitionDesc:
		b.WriteString("<dd")
		writeSourceAttrs(r, b, n)
		b.WriteByte('>')
		renderChildrenInto(r, b, n)
		b.WriteString("</dd>\n")

	case NodeAutoEmbed:
		src := html.EscapeString(n.Attrs["src"])
		provider := html.EscapeString(n.Attrs["provider"])
		b.WriteString(`<div class="mdpp-embed`)
		if provider != "" {
			b.WriteString(" mdpp-embed-")
			b.WriteString(provider)
		}
		b.WriteString(`" data-src="`)
		b.WriteString(src)
		b.WriteByte('"')
		if provider != "" {
			b.WriteString(` data-provider="`)
			b.WriteString(provider)
			b.WriteByte('"')
		}
		writeSourceAttrs(r, b, n)
		b.WriteString(`><a href="`)
		b.WriteString(src)
		b.WriteString(`">`)
		b.WriteString(src)
		b.WriteString("</a></div>\n")

	default:
		renderChildrenInto(r, b, n)
	}
}

// renderChildrenInto writes the children of n into b without allocating
// an intermediate string per child.
func renderChildrenInto(r *Renderer, b *strings.Builder, n *Node) {
	for _, child := range n.Children {
		renderNodeInto(r, b, child)
	}
}

func writeSourceAttrs(r *Renderer, b *strings.Builder, n *Node) {
	if r == nil || !r.sourcePositions || n == nil || n.Range.StartLine == 0 {
		return
	}
	b.WriteString(` data-mdpp-source-start="`)
	b.WriteString(strconv.Itoa(n.Range.StartByte))
	b.WriteString(`" data-mdpp-source-end="`)
	b.WriteString(strconv.Itoa(n.Range.EndByte))
	b.WriteString(`" data-mdpp-source-line="`)
	b.WriteString(strconv.Itoa(n.Range.StartLine))
	b.WriteString(`" data-mdpp-source-col="`)
	b.WriteString(strconv.Itoa(n.Range.StartCol))
	b.WriteString(`" data-mdpp-source-end-line="`)
	b.WriteString(strconv.Itoa(n.Range.EndLine))
	b.WriteString(`" data-mdpp-source-end-col="`)
	b.WriteString(strconv.Itoa(n.Range.EndCol))
	b.WriteByte('"')
}

func renderTaskListItemInto(r *Renderer, b *strings.Builder, n *Node) {
	b.WriteString(`<li class="task-list-item"`)
	writeSourceAttrs(r, b, n)
	b.WriteString(`><input type="checkbox" disabled`)
	if n.Attrs["checked"] == "true" {
		b.WriteString(" checked")
	}
	b.WriteString(" />")
	if len(n.Children) == 1 && n.Children[0].Type == NodeParagraph {
		renderChildrenInto(r, b, n.Children[0])
		b.WriteString("</li>\n")
		return
	}
	var inner strings.Builder
	renderChildrenInto(r, &inner, n)
	b.WriteString(strings.TrimRight(inner.String(), "\n"))
	b.WriteString("</li>\n")
}

func renderContainerDirectiveInto(r *Renderer, b *strings.Builder, n *Node) {
	var body strings.Builder
	renderChildrenInto(r, &body, n)
	bodyHTML := body.String()
	if r.containerHTML != nil {
		b.WriteString(r.containerHTML(n, bodyHTML))
		return
	}

	name := strings.ToLower(n.Attrs["name"])
	if isAdmonitionContainer(name) {
		b.WriteString(`<div class="admonition admonition-`)
		b.WriteString(html.EscapeString(name))
		writeContainerCommonAttrs(b, n, name, false)
		writeSourceAttrs(r, b, n)
		b.WriteByte('>')
		title := n.Attrs["title"]
		if title == "" {
			title = strings.ToUpper(name)
		}
		b.WriteString(`<p class="admonition-title">`)
		renderAdmonitionTitleInto(r, b, title)
		b.WriteString("</p>")
		b.WriteString(bodyHTML)
		b.WriteString("</div>\n")
		return
	}

	if name == "details" {
		b.WriteString(`<details class="mdpp-container mdpp-container-details" data-mdpp-container="details"`)
		writeContainerIDAndExtraAttrs(b, n)
		writeSourceAttrs(r, b, n)
		b.WriteByte('>')
		if title := n.Attrs["title"]; title != "" {
			b.WriteString("<summary>")
			renderAdmonitionTitleInto(r, b, title)
			b.WriteString("</summary>")
		}
		b.WriteString(bodyHTML)
		b.WriteString("</details>\n")
		return
	}

	tagClass := "mdpp-container-" + classToken(name)
	if name == "column" || name == "col" {
		tagClass = "mdpp-col"
	}
	b.WriteString(`<div class="mdpp-container `)
	b.WriteString(tagClass)
	writeContainerCommonAttrs(b, n, name, true)
	writeSourceAttrs(r, b, n)
	b.WriteByte('>')
	if title := n.Attrs["title"]; title != "" {
		b.WriteString(`<p class="mdpp-container-title">`)
		renderAdmonitionTitleInto(r, b, title)
		b.WriteString("</p>")
	}
	b.WriteString(bodyHTML)
	b.WriteString("</div>\n")
}

func writeContainerCommonAttrs(b *strings.Builder, n *Node, name string, includeBaseData bool) {
	if class := strings.TrimSpace(n.Attrs["class"]); class != "" {
		b.WriteByte(' ')
		b.WriteString(html.EscapeString(class))
	}
	b.WriteByte('"')
	if includeBaseData {
		b.WriteString(` data-mdpp-container="`)
		b.WriteString(html.EscapeString(name))
		b.WriteByte('"')
	}
	writeContainerIDAndExtraAttrs(b, n)
}

func writeContainerIDAndExtraAttrs(b *strings.Builder, n *Node) {
	if id := n.Attrs["id"]; id != "" {
		b.WriteString(` id="`)
		b.WriteString(html.EscapeString(id))
		b.WriteByte('"')
	}
	// Free-form key=value attributes are intentionally not emitted yet; the
	// parsed JSON is preserved in Attrs["attrs"] for downstream tools.
}

func isAdmonitionContainer(name string) bool {
	switch strings.ToLower(name) {
	case "note", "tip", "warning", "caution", "important":
		return true
	default:
		return false
	}
}

func renderAdmonitionTitleInto(r *Renderer, b *strings.Builder, title string) {
	root := newNode(NodeDocument)
	root.Children = parseInline(title, nil)
	processInlineMath(root)
	processSuperscripts(root)
	processEmojiShortcodes(root)

	titleRenderer := *r
	titleRenderer.unsafeHTML = false
	titleRenderer.hardWraps = false
	renderChildrenInto(&titleRenderer, b, root)
}

func renderTableInto(r *Renderer, b *strings.Builder, n *Node) {
	aligns := parseTableAligns(n.Attrs["align"])
	hasAlign := false
	for _, a := range aligns {
		if a != "" {
			hasAlign = true
			break
		}
	}

	// Responsive wrapper: a <div> with overflow-x:auto via CSS class.
	b.WriteString(`<div class="mdpp-table"`)
	writeSourceAttrs(r, b, n)
	b.WriteString(">\n")
	b.WriteString("<table>\n")

	// Emit <colgroup> when any column has alignment so CSS can target
	// columns and screen readers see semantic structure.
	if hasAlign {
		b.WriteString("<colgroup>\n")
		for _, a := range aligns {
			if a == "" {
				b.WriteString("<col />\n")
				continue
			}
			b.WriteString(`<col style="text-align:`)
			b.WriteString(a)
			b.WriteString(`" />` + "\n")
		}
		b.WriteString("</colgroup>\n")
	}

	hasBody := false
	for i, row := range n.Children {
		if row.Type != NodeTableRow {
			continue
		}
		if i == 0 {
			b.WriteString("<thead>\n<tr>")
			for ci, cell := range row.Children {
				b.WriteString("<th scope=\"col\"")
				writeTableAlignAttr(b, aligns, ci)
				b.WriteByte('>')
				renderChildrenInto(r, b, cell)
				b.WriteString("</th>")
			}
			b.WriteString("</tr>\n</thead>\n")
			continue
		}
		if !hasBody {
			b.WriteString("<tbody>\n")
			hasBody = true
		}
		b.WriteString("<tr>")
		for ci, cell := range row.Children {
			b.WriteString("<td")
			writeTableAlignAttr(b, aligns, ci)
			b.WriteByte('>')
			renderChildrenInto(r, b, cell)
			b.WriteString("</td>")
		}
		b.WriteString("</tr>\n")
	}
	if hasBody {
		b.WriteString("</tbody>\n")
	}
	b.WriteString("</table>\n")
	b.WriteString("</div>\n")
}

// parseTableAligns splits the NodeTable `align` attribute ("left,center,,right")
// into its per-column slice. Unset columns render as empty strings.
func parseTableAligns(attr string) []string {
	if attr == "" {
		return nil
	}
	return strings.Split(attr, ",")
}

// writeTableAlignAttr emits ` style="text-align:<value>"` on a cell when
// the corresponding column declares an alignment. Noop otherwise.
func writeTableAlignAttr(b *strings.Builder, aligns []string, idx int) {
	if idx >= len(aligns) {
		return
	}
	a := aligns[idx]
	if a == "" {
		return
	}
	b.WriteString(` style="text-align:`)
	b.WriteString(a)
	b.WriteByte('"')
}

func renderDiagramInto(b *strings.Builder, n *Node) {
	syntax := n.Attrs["syntax"]
	if syntax == "" {
		syntax = "diagram"
	}
	kind := n.Attrs["kind"]
	if kind == "" {
		kind = syntax
	}
	language := n.Attrs["language"]
	if language == "" {
		language = syntax
	}
	codeLanguage := normalizedFenceLanguage(language)
	if codeLanguage == "" {
		codeLanguage = syntax
	}

	b.WriteString(`<figure class="mdpp-diagram mdpp-diagram-`)
	b.WriteString(classToken(syntax))
	b.WriteString(` mdpp-diagram-`)
	b.WriteString(classToken(kind))
	b.WriteString(`" data-diagram-syntax="`)
	b.WriteString(html.EscapeString(syntax))
	b.WriteString(`" data-diagram-kind="`)
	b.WriteString(html.EscapeString(kind))
	b.WriteString(`">`)
	b.WriteString(`<pre><code class="language-`)
	b.WriteString(html.EscapeString(codeLanguage))
	b.WriteString(`">`)
	b.WriteString(html.EscapeString(n.Literal))
	b.WriteString("</code></pre></figure>\n")
}

func classToken(value string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// collectNodeText recursively extracts plain text from a node tree.
func collectNodeText(n *Node) string {
	if n == nil {
		return ""
	}
	if n.Type == NodeText {
		return n.Literal
	}
	if n.Type == NodeCodeSpan {
		return n.Literal
	}
	var sb strings.Builder
	for _, c := range n.Children {
		sb.WriteString(collectNodeText(c))
	}
	return sb.String()
}

// slugify converts text into a URL-friendly ID string.
func slugify(s string) string {
	var sb strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
			prevDash = false
		} else if r == ' ' || r == '-' || r == '_' {
			if !prevDash && sb.Len() > 0 {
				sb.WriteByte('-')
				prevDash = true
			}
		}
	}
	result := sb.String()
	result = strings.TrimRight(result, "-")
	return result
}

// Slugify converts heading text into the renderer's auto-generated id.
func Slugify(s string) string {
	return slugify(s)
}

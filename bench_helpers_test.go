package mdpp

const benchShortMarkdown = `# Hello

This is a **short** markdown document with a [link](https://example.com)
and some inline ` + "`code`" + `. It covers the most common nodes a typical
docs page uses.

- First item
- Second item
- Third item
`

const benchLongMarkdown = `# Field Notes

MDPP is a small writing format for technical documents that need more
structure than baseline Markdown without turning prose into markup soup.
It keeps paragraphs readable in plain text and gives the renderer enough
shape to produce useful HTML, metadata, and editor affordances.

## Installation

Add the package to a Go project:

` + "```bash\ngo get github.com/odvcencio/mdpp\n```" + `

Then parse and render a document:

` + "```go\ndoc := mdpp.MustParse([]byte(source))\nhtml := mdpp.RenderString(source)\n_ = doc\n_ = html\n```" + `

## Core concepts

MDPP provides **five** document primitives:

1. **AST** — structured nodes for parsers, renderers, and editors
2. **Math** — inline and block math for science-flavored prose
3. **Footnotes** — numbered references that stay easy to read in source
4. **Admonitions** — semantic callouts for notes, warnings, and tips
5. **Diagrams** — fenced Mermaid and diagram blocks as first-class nodes

Each primitive still reads like text. A document can start as ordinary
Markdown and adopt richer syntax only where the extra structure helps.

### Example

Here's a short technical note:

` + "```md\n> [!NOTE]\n> Measurements were taken after warmup.\n\nThe signal converges in $O(n \\log n)$ time.[^1]\n\n[^1]: Use enough samples to smooth out scheduler noise.\n```" + `

The parser keeps the note, math, and citation available as structured
nodes instead of forcing every consumer to reverse-engineer rendered HTML.

## Features

MDPP supports a wide range of common paper and notes patterns:

- Frontmatter for metadata
- Tables and task lists
- Syntax-highlighted code
- Emoji shortcodes
- Diagram fences for Mermaid-style flowcharts

See the package documentation for the full feature matrix.

## Why not only Markdown?

Markdown is excellent for prose, but science-y writing often needs
math, callouts, citations, diagrams, and editor support. Those features
usually arrive as unrelated extensions with slightly different rules.

MDPP keeps the extension set in one grammar and one renderer so tools can
share behavior instead of each reimplementing edge cases.

> The best markup is still readable before it is rendered.

## What's next?

Try a document with frontmatter, a diagram fence, and a footnote, then
inspect the AST before rendering.
`

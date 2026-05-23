# Sub-project A — Engine + CLI — Design Spec

**Status.** Draft
**Date.** 2026-04-19
**Owner.** Oscar Villavicencio (m31labs)
**Parent.** [`2026-04-19-markdown-plus-plus-roadmap-design.md`](./2026-04-19-markdown-plus-plus-roadmap-design.md) §4.1
**Scope.** The Markdown++ engine (`github.com/odvcencio/mdpp`) and its companion CLI (`cmd/mdpp`). Defines the public Go API, the AST taxonomy, the new `:::` container directive, the PDF rendering pipeline, the parser-hardening corpus strategy, the CLI shape, and the conformance corpus layout. Cross-cutting decisions from §1–3 and §5–10 of the roadmap are taken as fixed and not re-litigated here.

---

## 0. Progress snapshot (as of 2026-04-19)

A burst of shipping happened the same day this sub-spec was drafted. The list below is the actual engine state; everything from §1 onward still describes the target.

**Done:**

- **Parser hardening corpus shipped.** `corpus_test.go` carries an integrated multi-construct "Hello World" corpus; `hardening_test.go` covers edge cases (list stitching, deep nesting, all-indented code blocks, bracket-quote heading text, numeric LaTeX argument forms, recovery from malformed input); `security_test.go` locks in XSS escaping at every render surface. Together these roughly satisfy the §6 "100+ real-world `.md` files" intent for v0.1, pending curated corpus expansion.
- **Concurrent parser pool.** `parser_pool.go` (new file, ~35 lines) wraps `gotreesitter.ParserPool` per language behind a `sync.Map`. Not in the original §10 plan — pleasant addition that pre-resolves the concurrent-access overhead concern.
- **`[[name]]` inline directives.** `[[toc]]` and `[[embed:url]]` are shipped (see new §4A below). Emit `NodeTableOfContents` and `NodeAutoEmbed` respectively. Case-insensitive name matching; provider detection for embeds (YouTube, Vimeo, etc.).
- **Wired previously-stub features.** Definition lists (`Term\n: Def`), link reference definitions, autolinks, embeds.
- **Table column alignment** with responsive wrapper and ARIA roles for accessibility.
- **Parser robustness fixes.** List stitching, deep-nested lists, all-indented code blocks, bracket-quote heading text, numeric LaTeX command arguments, Go-language highlight spans for type conversions, recovered-inline suffix rendering, parser fallback text preservation, ordered list rendering, custom and inline admonition titles.

**Still pending (priority order):**

1. **`Node.Range` (start+end byte fields on every AST node) — BLOCKING for sub-projects B, C, and D.** This sub-spec committed to it in §2.3; the work has not happened. Highest-priority engine task; must precede B/C/D since their hover, lint, formatter-preserve and code-action work all assume byte-accurate ranges.
2. `:::name` block container directive (parser path; AST node `NodeContainerDirective`) — see §3 / §4.
3. PDF rendering via `chromedp` — see §5.
4. `cmd/mdpp` CLI — see §7.
5. `examples/conformance/` directory — corpus material exists embedded in `corpus_test.go`; needs extraction into the per-case directory layout (`input.md` + `expected.html` + `README.md` per case) defined in §8.
6. `SPEC.md` document.

---

## 1. Scope

**In.** Public Go API (`Parse`, `Render`, `RenderPDF`, traversal, metadata); full v0.1 AST taxonomy; the `:::name` container directive (grammar, attributes, nesting, HTML mapping); PDF pipeline as a thin `chromedp` layer; parser hardening via a 100+-doc corpus and recoverable diagnostics; the `cmd/mdpp` CLI; `examples/conformance/` layout and runner.

**Out.** Formatter (B), linter (C), LSP (D), highlighter (E), VS Code extension (F) — only the `mdpp fmt` / `mdpp lint` forwarding stubs live in A. No net-new headless-browser infra; the chromedp pattern already shipping in m31labs.dev is reused. Charts, data fences, slide output deferred per roadmap.

---

## 2. Public Go API

The public surface is the contract every other sub-project depends on. It is small on purpose: a few entry-point functions, a `Document`, a `Node` tree, and value-typed option structs. Within the v0.x line nothing in §2 may break without a documented reason and a deprecation notice.

### 2.1 Top-level functions

```go
// Parse parses Markdown++ source into a Document. Never returns nil; on
// fatal lexer panic recovery the returned Document carries the original
// source and an error-marker root with diagnostics. The error return is
// reserved for I/O-style problems (currently always nil — present for
// forward compatibility and to match the roadmap signature).
func Parse(src []byte) (*Document, error)

// MustParse is the panic-on-error sibling of Parse, intended for tests
// and one-off scripts.
func MustParse(src []byte) *Document

// Render produces an HTML rendering of doc using opts. Always returns
// HTML even when opts is the zero value — defaults match the historical
// behavior of the package-level RenderString.
func Render(doc *Document, opts RenderOptions) ([]byte, error)

// RenderPDF produces a PDF rendering of doc using opts. Internally:
//   1. Calls Render to produce HTML.
//   2. Wraps it in a print-CSS shell.
//   3. Drives chromedp's page.PrintToPDF over the wrapped document.
// May return an error if the headless browser cannot be obtained; HTML
// rendering itself is infallible.
func RenderPDF(doc *Document, opts PDFOptions) ([]byte, error)
```

The package keeps the legacy `RenderString(source string) string` and `*Renderer` chain (§2.6) for binary-compat with current callers — see migration notes in §9.

### 2.2 `Document`

```go
type Document struct {
    // Source is the normalized source the document was parsed from
    // (line-endings normalized, trailing newline ensured). Byte ranges
    // in nodes index into Source.
    Source []byte

    // Root is always non-nil and always of NodeType NodeDocument.
    Root *Node

    // unexported: linkRefDefs, frontmatterData, diagnostics
}

func (d *Document) AST() *Node                  // returns d.Root
func (d *Document) Frontmatter() map[string]any // YAML frontmatter, nil if absent
func (d *Document) Diagnostics() []Diagnostic   // recoverable parse diagnostics
func (d *Document) Headings() []Heading
func (d *Document) TableOfContents() []TOCEntry
func (d *Document) WordCount() int
func (d *Document) ReadingTime() time.Duration
func (d *Document) FormatVersion() string       // value of frontmatter `mdpp:` key, or "" if absent
```

`AST()` exists as the spelled-out sibling of `.Root` because the roadmap names it explicitly and because it reads better in walker code. Both forms are stable.

`Diagnostics()` returns the slice of recoverable parse warnings (see §6). Hard parse failure is impossible; the worst-case Document is a single `NodeText` literal containing the whole source plus a diagnostic explaining what fell back.

### 2.3 `Node`

```go
type NodeType int

type Node struct {
    Type     NodeType
    Children []*Node
    Literal  string            // leaf payload (text, code, math TeX, etc.)
    Attrs    map[string]string // structured fields (see §3)
    Range    Range             // byte range in Document.Source (new for v0.1)
}

type Range struct {
    StartByte int
    EndByte   int
    StartLine int // 1-indexed
    StartCol  int // 1-indexed, byte column
    EndLine   int
    EndCol    int
}

// Convenience accessors (read-only — Attrs is the source of truth).
func (n *Node) Attr(key string) string         // "" if absent
func (n *Node) HasAttr(key string) bool
func (n *Node) Level() int                     // headings: 1..6, otherwise 0
func (n *Node) Text() string                   // collected text for prose nodes
func (n *Node) Walk(visit func(n *Node) bool)  // pre-order; return false to skip subtree
func (n *Node) Find(typ NodeType) []*Node      // pre-order collect
```

The `Range` field is new for v0.1 and is what makes diagnostics, LSP hover, and code actions trivial downstream. Today the AST drops byte ranges after the tree-sitter conversion; carrying them through is a one-time cost and unblocks all the tooling sub-projects.

`Attrs` stays a `map[string]string` rather than becoming a typed union. The map shape is well-known per node type, documented per type in §3, and string-typed so the JSON dump in `mdpp parse --json` is loss-free.

### 2.4 `RenderOptions`

```go
type RenderOptions struct {
    // HTML output knobs (a value-typed mirror of the existing Option chain).
    HighlightCode bool
    HeadingIDs    bool
    UnsafeHTML    bool
    HardWraps     bool
    WrapEmoji     bool

    // ImageResolver rewrites image src attributes (e.g. for a relative-path
    // root or a CDN). nil means pass-through.
    ImageResolver func(src string) string

    // ContainerRenderer optionally overrides the default HTML mapping
    // for `:::name` containers. nil means use the default in §4.4.
    ContainerRenderer func(c *Node, body string) string

    // Math controls how NodeMathInline / NodeMathBlock render.
    Math MathOption

    // Sanitize, when true, runs the rendered HTML through a strict allow-list
    // sanitizer before returning. Defaults to false (the historical behavior).
    Sanitize bool
}

type MathOption int
const (
    MathServer MathOption = iota // server-side render via mdpp/latex.go (default)
    MathRaw                       // emit raw \(...\) / \[...\] markers (for client-side MathJax)
    MathOmit                      // strip math; for plaintext-leaning targets
)
```

Zero-value `RenderOptions{}` reproduces the current package-level `RenderString` behavior so existing callers can switch over without flag-tuning.

### 2.5 `PDFOptions`

```go
type PDFOptions struct {
    // Page geometry. Defaults: PaperSize = PaperLetter, Margin = 0.5in all sides.
    PaperSize PaperSize       // PaperLetter (8.5x11) | PaperA4 | PaperLegal | PaperCustom
    PaperWidthInches  float64 // used only for PaperCustom
    PaperHeightInches float64 // used only for PaperCustom
    MarginInches      Margins // top/right/bottom/left, default 0.5

    // CSS controls. UserCSS is appended after the built-in print stylesheet.
    UserCSS string

    // Background controls whether print-background is honored. Default true.
    Background bool

    // HeaderFooter, when non-zero-value, populates running headers/footers
    // using chromedp's HTML-template syntax (date, title, pageNumber, totalPages).
    HeaderFooter HeaderFooterTemplate

    // Render pipeline knobs.
    RenderOptions RenderOptions // HTML rendering options (composes §2.4)

    // Browser controls.
    BrowserURL string        // ws:// URL of a remote chrome; "" launches local
    Timeout    time.Duration // default 60s

    // Wait controls. Default: WaitForLoadEvent + 250ms settle.
    SettleDelay time.Duration
}

type PaperSize int
const (
    PaperLetter PaperSize = iota
    PaperA4
    PaperLegal
    PaperCustom
)

type Margins struct{ Top, Right, Bottom, Left float64 }
type HeaderFooterTemplate struct{ HeaderHTML, FooterHTML string }
```

The defaults track exactly what `m31labs.dev/app/blog_pdf.go` uses today (Letter, 0.5in margins, print-background on, 60s timeout). The `BrowserURL` field exists so the LSP and CLI can share a long-lived browser when one is available (`CHROME_WS_URL` env, mirroring m31labs.dev's allocator).

### 2.6 Backward-compat surface

These existing exports stay supported unchanged in the v0.x line:

```go
func RenderString(src string) string
func NewRenderer(opts ...Option) *Renderer
func (r *Renderer) Render(doc *Document) string
func (r *Renderer) RenderString(source string) string
type Option = func(*Renderer)
func WithHighlightCode(bool) Option
func WithHeadingIDs(bool) Option
func WithUnsafeHTML(bool) Option
func WithHardWraps(bool) Option
func WithWrapEmoji(bool) Option
func WithImageResolver(func(string) string) Option
```

They become thin shims that build a `RenderOptions` and call `Render`. See §9.

### 2.7 Stability guarantees

§2.1–§2.6 is API-stable for the v0.x line. Adding fields to `RenderOptions` / `PDFOptions` is non-breaking (callers use named-field literals). New `NodeType` constants append to the `iota` block — never mid-block — so switch statements with `default:` keep compiling and the `mdpp parse --json` wire format stays stable. `Node.Attrs` keys documented in §3 are frozen-meaning; new keys may be added. `Parse` and `Render` return `(_, nil)` in v0.1; `RenderPDF` wraps chromedp errors as `pdf: <op>: %w`.

---

## 3. AST node taxonomy

The complete v0.1 node set, with field documentation. Nodes marked **(new)** are added in v0.1; everything else exists today and may pick up new attribute keys.

### 3.1 Block nodes

| `NodeType` | Children | Key `Attrs` | Notes |
|---|---|---|---|
| `NodeDocument` | any block | — | Always the root. Exactly one per `Document`. |
| `NodeFrontmatter` | none | `format` (`yaml`); `raw` (literal text) | Optional; first child of `NodeDocument` when present. |
| `NodeHeading` | inline | `level` (1–6) | Heading IDs are computed at render time from text via `slugify`. |
| `NodeParagraph` | inline | — | |
| `NodeBlockquote` | block | — | |
| `NodeList` | `NodeListItem` / `NodeTaskListItem` | `ordered` (`true`/`false`); `start` (string int when ordered); `tight` (`true`/`false`) | |
| `NodeListItem` | block | — | |
| `NodeTaskListItem` | block | `checked` (`true`/`false`) | First child is a paragraph by convention. |
| `NodeDefinitionList` | `NodeDefinitionTerm` + `NodeDefinitionDesc` | — | |
| `NodeDefinitionTerm` | inline | — | |
| `NodeDefinitionDesc` | block | — | |
| `NodeCodeBlock` | none | `language`; `info` (full info-string); `fence` (`backtick`/`tilde`/`indented`) | `Literal` holds the verbatim block text. |
| `NodeDiagram` | none | `language`; `syntax` (`mermaid` etc.); `kind` | `Literal` is the diagram source. Currently transformed from a fenced block in `diagram.go`. |
| `NodeTable` | `NodeTableRow` | `align` (comma list of `left`/`right`/`center`/`none`, one per column) | First row is the header. |
| `NodeTableRow` | `NodeTableCell` | — | |
| `NodeTableCell` | inline | `align` (per-cell override) | |
| `NodeThematicBreak` | none | — | |
| `NodeHTMLBlock` | none | — | `Literal` is the raw HTML. |
| `NodeMathBlock` | none | — | `Literal` is the TeX source. |
| `NodeFootnoteDef` | block | `id` | Definitions are appended to the document tail by post-processing. |
| `NodeAdmonition` | block | `type` (`NOTE`, `WARNING`, `TIP`, `CAUTION`, `IMPORTANT`, …); `title` (optional, parsed as inline) | Today these are recognized via post-processing on blockquotes. v0.1 keeps the same node shape; the pre-render lowering also accepts the `:::` form (see §4.4). |
| `NodeContainerDirective` **(new)** | block | `name` (e.g. `warning`, `columns`); `id` (optional, from `#id`); `class` (space-separated, from `.cls`); `attrs` (JSON-encoded map of key=value pairs); `raw` (the original info-string before the open fence) | The `:::name` block. See §4. |
| `NodeTableOfContents` **(shipped)** | inline list | `max-level` (digits, optional) | Inserted by the `[[toc]]` inline directive (see §4A) or by frontmatter `toc: true`. Materializes a nested `<ul>`/`<ol>` of heading anchors at render time. |
| `NodeAutoEmbed` **(shipped)** | none | `src`; `provider` (`youtube`, `vimeo`, …) | Inserted by the `[[embed:url]]` inline directive (see §4A). Renders to `<div class="mdpp-embed mdpp-embed-{provider}">` with `data-src`, `data-provider`, and a fallback `<a>` child. |

### 3.2 Inline nodes

| `NodeType` | Children | Key `Attrs` | Notes |
|---|---|---|---|
| `NodeText` | none | — | `Literal` is the text. |
| `NodeSoftBreak` | none | — | |
| `NodeHardBreak` | none | — | |
| `NodeEmphasis` | inline | `style` (`*`/`_`) | The `style` key is new for v0.1 — needed by the formatter to canonicalize. |
| `NodeStrong` | inline | `style` (`*`/`_`) | |
| `NodeStrikethrough` | inline | — | |
| `NodeCodeSpan` | none | — | `Literal` holds the code text. |
| `NodeLink` | inline | `href`; `title`; `ref` (when reference-style and unresolved); `raw` (the original `[…](…)` text); `kind` (`inline`/`reference`/`autolink`/`shortcut`) | `kind` is new for v0.1. |
| `NodeImage` | inline (alt only) | `src`; `alt`; `title` | |
| `NodeFootnoteRef` | none | `id`; `defined` (`true`/`false`) | `defined` is new — lets the linter flag broken refs without re-walking definitions. |
| `NodeMathInline` | none | — | `Literal` is the TeX source. |
| `NodeSuperscript` | none | — | `Literal` carries the wrapped text (no inline children today; expanding to inline children is a v0.2 question). |
| `NodeSubscript` | none | — | Same shape as superscript. |
| `NodeEmoji` | none | `code` (shortcode), `name` (display name) | `Literal` is the resolved Unicode glyph. |
| `NodeHTMLInline` | none | — | `Literal` is the raw HTML. |

### 3.3 Parent/child rules

`NodeDocument` accepts block children (at most one leading `NodeFrontmatter`). Inline-only parents: `NodeHeading`, `NodeParagraph`, `NodeTableCell`, `NodeDefinitionTerm`. Block-accepting wrappers: `NodeBlockquote`, `NodeListItem`, `NodeTaskListItem`, `NodeAdmonition`, `NodeContainerDirective`, `NodeFootnoteDef`, `NodeDefinitionDesc`. Leaves with payload in `Literal`: `NodeCodeBlock`, `NodeDiagram`, `NodeMathBlock`, `NodeMathInline`, `NodeCodeSpan`, `NodeText`, `NodeSuperscript`, `NodeSubscript`, `NodeEmoji`, `NodeHTMLBlock`, `NodeHTMLInline`, `NodeFrontmatter`.

### 3.4 What's *not* a node

Reference-link definitions consume into `Document.linkRefDefs` rather than materializing as nodes — the formatter needs to canonicalize them as a tail block, decision deferred to B. Emoji shortcodes resolve to `NodeEmoji` if known, else pass through as `NodeText`; the unknown-shortcode lint rule lives in C.

---

## 4. The `:::name` container directive

The single new spec feature in v0.1. Generic styled regions used for callouts, columns, asides, and any future "this is a class of content" markup. The roadmap defers the syntax decision to this sub-spec.

> **Distinct from the `[[name]]` form already shipped (TOC, embed).** `[[name]]` is a single-line, inline-positioned directive (see §4A); `:::name`...`:::` is a multi-line block container. Both coexist in v0.1 and serve non-overlapping syntactic niches: short stand-alone directives use `[[name]]`, regions wrapping arbitrary block content use `:::name`.

### 4.1 Syntax

A container directive is a block beginning with three or more colons followed by a name, optional attribute group, optional title text, then any number of body lines, then a closing line of three or more colons:

```
:::warning
This is the body. It can include any block markdown:

- lists
- math: $x = 1$
- nested containers (see §4.5)

:::
```

Parsing rules:

1. **Open fence.** Three or more colons (`:::` minimum), at column 0, followed by an optional space, then a *name* (`[A-Za-z][A-Za-z0-9_-]*`), optional attribute group (§4.2), optional title text. The colon count must match on close.
2. **Close fence.** A line of *exactly the same number of colons* as the open, at column 0, with no trailing text. Mismatched counts allow nesting (§4.5).
3. **Indentation.** No indentation allowed on the fence lines themselves. Body content is parsed with the same column-zero baseline as the surrounding document.
4. **Blank lines.** Allowed inside the body. The container ends only at a matching close fence or end-of-document. Unclosed containers produce a recoverable diagnostic and are auto-closed at EOF.
5. **Where allowed.** Top-level and inside other containers. Not allowed inside paragraphs (must be a block). Not parsed inside fenced code blocks (` ``` `, `~~~`).

### 4.2 Attribute syntax — decision: Pandoc-style

After the name, an optional `{...}` group carries attributes:

```
:::warning {#first .important .border-red key=value title="My Warning"}
body
:::
```

Within the braces: `#id` sets the HTML id (one allowed), `.class` adds a class (multiple allowed), `key=value` / `key="value"` sets an attribute. A bare title may appear between name and brace group: `:::note "Heads up" {#first}` is lighter than `{title="..."}` for the common callout case.

**Why Pandoc-style.** Familiarity (Pandoc, MyST, markdown-it-attrs all use it), composability (one grammar covers classes, ids, and free-form keys like `lang=fr` or `data-source=ref-1`), and graceful degradation (plain CommonMark renderers print the braces as body text — ugly but readable; a custom mini-DSL would garble). The bare `:::warning` form without any brace group is fully supported and is the right answer for the common case.

### 4.3 Tree-sitter grammar approach — decision: post-processing in v0.1, grammar in v0.2

Extending upstream tree-sitter-markdown via grammargen to recognize `:::` fences is feasible (we own the toolchain), but the markdown grammar's external scanner handles fences and indented blocks — teaching it a fourth fence character interacts with state-machine logic that is the most fragile area of the grammar. Not a blocker; just not the path for a launch milestone.

**Decision for v0.1.** Same post-processing path admonitions already use. A pre-parse lowering pass in `parse.go` (sibling of `lowerMarkdownPlusSource`):

1. Scan for `^:::` fences at column 0 outside code fences.
2. Pair opens to closes, tracking nesting and colon count.
3. Substitute the span with a sentinel fenced code block (info-string `__mdpp_container_<uuid>__`) before tree-sitter parsing.
4. After conversion, walk the AST and rewrite sentinels into `NodeContainerDirective` nodes, re-parsing the body via a recursive `Parse` call.

Byte ranges on the directive cover the outer fences; body-child ranges point back into the original source via offset tracking in the lowering pass.

**v0.2.** A grammargen patch adds `:::` as a fence token. The AST shape is identical in both implementations, so the switch is invisible to every downstream consumer.

### 4.4 HTML output mapping

Default mapping (overridable via `RenderOptions.ContainerRenderer`):

```html
<div class="mdpp-container mdpp-container-{name} {extra-classes}" id="{id}" data-mdpp-container="{name}" {key="value" ...}>
{body}
</div>
```

Notes:

- The `mdpp-container-{name}` class is what authors style against. The bare `mdpp-container` lets stylesheets target *all* containers.
- `data-mdpp-container="{name}"` is redundant with the class but is the stable hook for JS (avoids classname-collision concerns).
- If `{name}` matches a recognized admonition type (`note`/`warning`/`tip`/`caution`/`important` case-insensitive), the renderer emits the admonition shape (`<div class="admonition admonition-warning">…`) instead. This unifies `:::warning` and `> [!WARNING]`.
- The optional inline title becomes a `<p class="mdpp-container-title">…</p>` as the first child, parsed as inline markdown.
- Reserved names (in addition to the admonition set) get bespoke renderings: `columns` → flex container with `:::col` children mapped to `<div class="mdpp-col">`; `details` → HTML `<details>` element with the title becoming `<summary>`. Other names render as the generic `<div>`.

### 4.5 Nesting

Nesting is handled by colon-count matching, exactly like fenced code blocks:

```
::::columns
:::col
Left content.
:::

:::col
Right content with a nested callout:

:::warning
Nested!
:::
:::
::::
```

The outer `::::` (four colons) matches only an outer `::::` close. The inner `:::col` and `:::warning` open and close with three colons.

### 4.6 Examples

**1. Bare callout.** `:::warning\nDon't run this on prod.\n:::` → the admonition-shape HTML (unifies with `> [!WARNING]`).

**2. Custom-named container.** `:::aside\ntangent\n:::` → `<div class="mdpp-container mdpp-container-aside" data-mdpp-container="aside">…</div>`.

**3. Pandoc attributes.**

```
:::tip {#install-mac .platform-mac}
On macOS, `brew install mdpp`.
:::
```

Renders with `id="install-mac"` and an extra `platform-mac` class on the admonition div.

**4. Columns layout.**

```
::::columns
:::col
Left.
:::
:::col
Right.
:::
::::
```

Nested containers. Outer `::::` matches outer close, inner `:::col` pairs match inner closes.

**5. Inline title.** `:::note "Heads up"\nBody.\n:::` — the title string becomes `<p class="admonition-title">Heads up</p>`.

**6. Unclosed (recoverable).** `:::warning\nbody<EOF>` auto-closes at EOF, emits `MDPP-PARSE-002`.

**7. Inside code fence (no parse).** `:::` lines inside a ` ``` ` block stay as code body — the lowering pass tracks code-fence state.

---

## 4A. `[[name]]` inline directives (shipped)

The `[[name]]` form is a single-line, block-positioned directive that materializes a single AST node at the point of insertion. Distinct from `:::name` containers (§4): `[[name]]` is one line, takes no body, and slots into the document like a self-closing block; `:::name`...`:::` wraps arbitrary block content.

### 4A.1 Syntax

A directive occupies a line by itself:

```
[[name]]
[[name:argument]]
```

Rules:

1. **Position.** On a line by itself, no leading or trailing prose on that line. The line may have surrounding indentation up to three spaces (block-context rule); four-or-more-space indent makes it a code block, same as any other CommonMark block.
2. **Name.** `[A-Za-z][A-Za-z0-9_-]*`, **case-insensitive** (`[[TOC]]` and `[[toc]]` are equivalent).
3. **Argument.** Optional, separated from the name by a single colon. Free-form text up to the closing `]]`. Argument grammar is per-directive.
4. **Not a link reference definition.** Link reference definitions use *single* brackets at the start of a line (`[label]: target "title"`). `[[name]]` uses *double* brackets and never carries the `: target` form. The two cannot collide, but the double-bracket convention also matches Obsidian-style wikilink expectations and reads as visually distinct.
5. **Recognition pass.** Recognized in post-processing after CommonMark parsing, before final tree lowering — the same lifecycle stage admonitions and `[[toc]]` use today.

### 4A.2 Reserved name registry

`v0.1` reserves two names. Future names are added by extending the registry; unrecognized names fall back to literal text (`[[unknown]]` renders verbatim) so authors get a readable hint rather than a silent drop.

| Name | Argument | AST node | HTML mapping |
|---|---|---|---|
| `toc` | none | `NodeTableOfContents` | Nested `<ul>` (or `<ol>` when frontmatter requests ordering) of heading anchors, scoped to the document. Each entry is an `<a href="#slug">` to the heading's slugified ID. |
| `embed` | URL | `NodeAutoEmbed` | `<div class="mdpp-embed mdpp-embed-{provider}" data-src="{url}" data-provider="{provider}">` containing a fallback `<a href="{url}">{url}</a>`. Provider is detected from the URL host (`youtube`, `youtu.be` → `youtube`; `vimeo.com` → `vimeo`; otherwise `generic`). |

The registry is a Go map of name → handler in `extensions.go`. Adding a new directive in a future minor version requires (a) registering the name, (b) defining the `NodeType` (or reusing one), (c) wiring the renderer mapping. No changes to the recognition regex or the bracket convention.

### 4A.3 Examples

```
[[toc]]
```

→ `<ul class="mdpp-toc">…</ul>` at the insertion point.

```
[[TOC]]
```

→ Same as above; case-insensitive.

```
[[embed:https://www.youtube.com/watch?v=dQw4w9WgXcQ]]
```

→ `<div class="mdpp-embed mdpp-embed-youtube" data-src="..." data-provider="youtube"><a href="...">https://...</a></div>`.

```
[[embed:https://example.com/whatever.mp4]]
```

→ `<div class="mdpp-embed mdpp-embed-generic" data-src="..." data-provider="generic"><a href="...">...</a></div>`.

```
[[unknownname]]
```

→ Verbatim text `[[unknownname]]`, plus an info-severity diagnostic `MDPP-PARSE-007` (reserved code, not yet implemented) so linters can flag typos.

### 4A.4 Why double brackets

- Visually distinct from `[label]: target` link reference definitions (single brackets, with a colon and a target).
- Visually distinct from `[link text](url)` and `[ref][label]`.
- Aligns with Obsidian/wiki convention for "this is a structural marker, not a link," which lowers learning cost for incoming users.
- Survives plain-CommonMark renderers as readable plaintext (`[[toc]]` shows up as `[[toc]]` rather than as garbled markup).

---

## 5. PDF rendering pipeline

### 5.1 Approach

`RenderPDF` is a thin shim over `Render` plus chromedp, lifted directly from `m31labs.dev/app/blog_pdf.go` and `browser_allocator.go`. No new infrastructure.

### 5.2 Pipeline

1. Call `Render(doc, opts.RenderOptions)` for HTML.
2. Wrap in a minimal HTML5 shell with the built-in print stylesheet plus `opts.UserCSS`.
3. Serve via `data:text/html;base64,…` for small docs, or a one-shot localhost `net/http.Server` for > ~2 MB (embedded base64 images).
4. Acquire a browser: `BrowserURL` if set (remote chrome, used by long-running consumers), else local `chromedp.NewExecAllocator` with the m31labs.dev flag set (`headless=new`, `no-sandbox`, swiftshader).
5. `Navigate` → `WaitReady("body")` → sleep `SettleDelay` (default 250 ms) → `page.PrintToPDF` with `opts` geometry.

### 5.3 Page geometry defaults

Direct copies of the m31labs.dev defaults:

| Field | Default |
|---|---|
| `PaperSize` | `PaperLetter` (8.5 × 11 in) |
| `MarginInches` | 0.5 in on all sides |
| `Background` | `true` (`WithPrintBackground(true)`) |
| `Timeout` | 60 s |
| `SettleDelay` | 250 ms (lower than the blog PDF's 1500 ms because there is no remote-image hydration in markdown by default) |

`PaperA4` maps to 8.27 × 11.69 in. `PaperLegal` maps to 8.5 × 14 in. `PaperCustom` reads `PaperWidthInches` / `PaperHeightInches`.

### 5.4 Math rendering

Two paths, selected by `opts.RenderOptions.Math`:

- **`MathServer` (default).** Math is already rendered to semantic HTML by `mdpp/latex.go` (which uses gotreesitter's grammargen — no JS dependency). The print stylesheet ships fonts that look reasonable for the existing semantic-HTML output. This is the zero-dependency path and what we recommend.
- **`MathRaw` + a chromedp `JS` injection.** For authors who want full LaTeX coverage beyond mdpp's subset, the wrapper can inject MathJax via a `<script src="…mathjax.js">` and add a `chromedp.WaitVisible(".MathJax")` step before `PrintToPDF`. Documented as an optional escape hatch; not the default.

### 5.5 Diagram rendering

Mermaid by default, since that's what `NodeDiagram` carries. The print shell injects:

```html
<script src="https://cdn.jsdelivr.net/npm/mermaid/dist/mermaid.min.js"></script>
<script>mermaid.initialize({startOnLoad: true});</script>
```

…only when the document contains at least one `NodeDiagram` (cheap pre-walk). The `SettleDelay` step waits for the diagrams to render. For air-gapped use, `opts.UserCSS` and a future `opts.UserScripts` field (post-v0.1) let authors point at a vendored copy.

### 5.6 Headless Chrome lifecycle & errors

CLI default: launch a local browser per render (simple, isolates failures). Long-running consumers (LSP, batch CLI) set `BrowserURL` or `CHROME_WS_URL` and reuse a remote chrome — still one context per render. chromedp contexts are not safe to share; parallelism is allocator-per-goroutine. Errors wrap as `pdf: <op>: %w` (`op` in `{allocator, navigate, wait, print, write}`); `RenderPDF` never panics.

---

## 6. Parser hardening plan

### 6.1 The corpus

Target: 100+ real-world `.md` files, parsed with zero panics and zero unrecoverable errors. Sources, in priority order:

1. **Top-1000 GitHub repo READMEs** — scripted via the GitHub search API, filtered to > 1k-star repos, deduped by AST sketch. Target: 60.
2. **arXiv preprints exported as Markdown** via Pandoc (LaTeX→md) — heavy math, tables, footnotes. Target: 15.
3. **Obsidian community vault samples** — wikilinks, callouts, embedded queries. Target: 10.
4. **Popular long-form blog archives** (Dan Luu, Julia Evans, Simon Willison, Matt Rickard). Target: 10.
5. **CommonMark spec test suite** pulled in as `.md`. Counts as 5; full suite runs as a separate test.
6. **Legacy mdpp regressions** — existing `hardening_test.go`, `period_test.go`, `corpus_test.go` keep running.

Storage: `examples/corpus/` (separate from `examples/conformance/`). Each file ships verbatim with a one-line `LICENSE.txt` for attribution.

### 6.2 Recoverable diagnostics

Today the parser returns `*Document` and assumes anything weird either drops cleanly or panics in CI. v0.1 introduces `Document.Diagnostics()` and the `Diagnostic` type:

```go
type Diagnostic struct {
    Code     string   // stable, e.g. "MDPP-PARSE-001"
    Severity Severity // ParseError | ParseWarning | ParseInfo
    Message  string
    Range    Range    // byte range in Document.Source
}

type Severity int
const (
    SeverityInfo Severity = iota
    SeverityWarning
    SeverityError
)
```

Diagnostic codes are stable. v0.1 starts with:

| Code | Severity | Trigger |
|---|---|---|
| `MDPP-PARSE-001` | Error | Tree-sitter returned an `ERROR` node spanning N bytes; covered by best-effort fallback |
| `MDPP-PARSE-002` | Warning | Container directive auto-closed at EOF |
| `MDPP-PARSE-003` | Warning | Admonition body extended past blank line via lowering — original markup was ambiguous |
| `MDPP-PARSE-004` | Warning | Frontmatter parse failed; treated as document body |
| `MDPP-PARSE-005` | Warning | Math fence (`$$`) unclosed at EOF |
| `MDPP-PARSE-006` | Info | Diagram fence had unrecognized syntax; rendered as plain code block |

Hard panics get wrapped via a top-level `defer recover()` in `Parse`. A recovered panic produces a single `MDPP-PARSE-000` Error diagnostic and a degraded `Document` containing one `NodeText` with the source verbatim. CI runs the corpus and asserts the recover branch is never taken.

### 6.3 Test harness

A new `corpus_hardening_test.go` walks `examples/corpus/`, calls `Parse` on each file, and asserts no panic recovered and no `MDPP-PARSE-000` diagnostic. Records diagnostic counts per file and asserts `Render` succeeds. `go test -v` prints a summary table so corpus health is visible at a glance. A nightly `FuzzParse` target asserts no-panic on random input; gotreesitter is already fuzz-clean.

---

## 7. CLI design

### 7.1 Subcommands

`mdpp <subcommand> [flags] [args...]`. Built with the standard library `flag` package — no Cobra dependency. Each subcommand is a top-level function in `cmd/mdpp/`. Help text is hand-written per command, opinionated, short.

#### `mdpp render`

```
mdpp render [flags] <file>

Render a Markdown++ file to HTML (default) or PDF.

Flags:
  --pdf                Render to PDF instead of HTML.
  --out <path>         Write to <path> instead of stdout.
  --format <fmt>       Output format: "html" (default), "pdf", "slides" (reserved, errors).
  --no-heading-ids     Skip auto id="..." on headings.
  --hard-wraps         Treat single newlines as <br>.
  --highlight          Apply syntax highlighting to code blocks.
  --unsafe-html        Pass through raw HTML (default: escaped).
  --paper <size>       PDF paper size: letter (default), a4, legal.
  --margin <inches>    PDF margin in inches, all sides (default 0.5).
  --css <path>         Append a user CSS file (PDF only).
  --browser-url <url>  Remote chromedp URL for PDF (env: CHROME_WS_URL).
  --timeout <dur>      PDF timeout (default 60s).
```

If `<file>` is `-`, read stdin.

#### `mdpp parse`

```
mdpp parse [flags] <file>

Parse a Markdown++ file and emit its AST.

Flags:
  --json               Emit as JSON (default).
  --pretty             Pretty-print JSON.
  --diagnostics-only   Emit only the diagnostic list, suppress the AST.
```

The JSON shape mirrors `Node`:

```json
{"type":"Document","children":[
  {"type":"Heading","attrs":{"level":"1"},"range":{"start_byte":0,"end_byte":7,"start_line":1,"end_line":1},"children":[
    {"type":"Text","literal":"Hello"}
  ]}
]}
```

`type` is the `NodeType` String() form (without the `Node` prefix). Empty `children`, `attrs`, and `literal` are omitted to keep output compact.

#### `mdpp fmt`

Flags: `--write` (in place), `--diff` (unified diff), `--check` (exit non-zero if reformatting needed). Reads stdin when no files given. v0.1 of A ships this as a stub calling `mdpp/fmt.Format(src)`; until B lands, stub returns input unchanged and prints a one-line warning to stderr.

#### `mdpp lint`

Flags: `--fix`, `--format=human|json`, `--severity=error|warning|info`. Stub in v0.1 of A; forwards to `mdpp/lint.Lint(doc)` once C lands.

#### `mdpp version`

```
mdpp version

Print version, commit, build date, Go version.
```

Reads `runtime/debug.ReadBuildInfo()` and the `version` const from the package. Output:

```
mdpp 0.1.0 (rev abc1234, built 2026-04-19 with go1.23.0)
spec: markdown-plus-plus v0.1
gotreesitter: v0.15.1
```

### 7.2 Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic error (parse impossible to start, file not found, flag misuse) |
| 2 | Diagnostics found and `--check` / lint error severity threshold met |
| 3 | PDF rendering failed (browser unavailable, navigation timeout) |
| 64 | Usage error (matches `EX_USAGE` from `sysexits.h` for shell-friendly error handling) |

### 7.3 Output conventions

Payload (rendered HTML, JSON AST, formatted source) goes to stdout; warnings, progress, version go to stderr. Color respects `NO_COLOR` and defaults on for TTY stdout. Warnings: `mdpp: <subcmd>: <message>`. No timestamps or log levels in user-facing output.

---

## 8. Conformance corpus structure

### 8.1 Layout

```
examples/conformance/
├── README.md                    # how to add a case, how runner works
├── 001-headings/
│   ├── README.md                # what this case demonstrates (one paragraph)
│   ├── input.md
│   ├── expected.html
│   └── expected.pdf.png         # rasterized first page (optional)
├── 002-paragraphs/
│   ├── ...
└── ...
```

Numeric prefixes give a stable ordering for the docs site. Reordering is fine — the prefix is for humans, not tests.

### 8.2 Per-case files

- **`input.md`** — verbatim source. Always present.
- **`expected.html`** — output of `mdpp render input.md` with default flags. Always present.
- **`expected.pdf.png`** — first page rasterized at 96 DPI. Optional; required for cases whose PDF output is the point (math, diagrams, page breaks). Generated by a separate helper (`go test ./examples/conformance -update-pdf`).
- **`README.md`** — one-paragraph human description. Used by the docs site to label the case.

### 8.3 Golden-test runner

`examples/conformance/conformance_test.go`:

```go
func TestConformance(t *testing.T) {
    for _, dir := range listCaseDirs(t) {
        t.Run(filepath.Base(dir), func(t *testing.T) {
            src := mustRead(t, filepath.Join(dir, "input.md"))
            doc, _ := mdpp.Parse(src)
            got, _ := mdpp.Render(doc, mdpp.RenderOptions{})

            wantPath := filepath.Join(dir, "expected.html")
            if *update {
                mustWrite(t, wantPath, got)
                return
            }
            want := mustRead(t, wantPath)
            if !bytes.Equal(got, want) {
                t.Errorf("HTML mismatch:\n%s", diff(want, got))
            }

            pdfPath := filepath.Join(dir, "expected.pdf.png")
            if _, err := os.Stat(pdfPath); err == nil {
                comparePDFScreenshot(t, doc, pdfPath)
            }
        })
    }
}
```

Run with `go test ./examples/conformance` for verification, `go test ./examples/conformance -update` to regenerate goldens after intentional output changes.

### 8.4 PDF screenshot diffing

Library: `github.com/orisano/pixelmatch` (pure Go, no CGo, ~150 LOC of dep — small enough). Tolerance: 1.5% pixel difference per page, antialiasing-aware. Documents that hit higher diff get flagged with a per-pixel diff image written next to `expected.pdf.png` so the human review is fast.

### 8.5 Coverage targets at v0.1

A minimum of 30 cases per the roadmap, drawn from this matrix:

- Every node type in §3.1 and §3.2 — at least one case each (44 node types → 30 cases is achievable by overlapping multiple constructs per case).
- The full spec §3.5 construct list (math, footnotes, admonitions, super/sub, def lists, emoji, diagram, frontmatter, container directive).
- Edge cases: nested containers, mismatched fences, frontmatter without a doc body, math inside footnotes, images inside table cells, etc.

---

## 9. Migration / compatibility

**Breaks.** `Parse` signature shifts from `func Parse(src []byte) *Document` to `(*Document, error)` per roadmap §4.1 — existing callers need a one-line ignore. `Node.Range` is new; hand-constructed `Node` literals (none in m31labs.dev) pick up a zero-value field. **As of 2026-04-19 the `Node` struct in `ast.go` still has no `Range` / `StartByte` / `EndByte` field — this addition has not yet shipped (see §0).** When it lands, the only structural change to `Node` will be the appended `Range Range` field; nothing existing renames or moves.

**Stays.** All `NodeType` iota values (new types append only — `NodeTableOfContents` and `NodeAutoEmbed` are already in the iota block as of today). The `Renderer` builder API and every `WithX` option, in their current shape: `WithHighlightCode`, `WithHeadingIDs`, `WithUnsafeHTML`, `WithHardWraps`, `WithWrapEmoji`, `WithImageResolver`. Frontmatter, headings, TOC, word-count, reading-time accessors. HTML output of every existing construct, byte-for-byte — the conformance corpus locks this.

**Module path.** `github.com/odvcencio/mdpp` unchanged (roadmap §10).

**Version.** Add `const Version = "0.1.0"` in a new `version.go`. CLI reads it. Rolling back from the current `0.1.6` to `0.1.0` is acceptable since v0.1 is the first *named* Markdown++ release; pre-naming versions are noise. (Flagged in §12 in case it's contested.)

---

## 10. Performance considerations

The renderer was already optimized in a recent pass (`strings.Builder` per subtree, no per-node `fmt.Sprintf`). For v0.1:

- Render walk is tight; adding `Node.Range` is a 32-byte field — negligible.
- **Parser pool (`parser_pool.go`) is already in place (shipped 2026-04-19).** Wraps `gotreesitter.ParserPool` per language behind a `sync.Map`; resolves the concurrent-access overhead concern that originally motivated this bullet. No further work required for v0.1 on the concurrency front.
- New pre-parse linear scan for `:::` lowering — O(n), fast-exit if source has no `:::`.
- Range population costs two int assignments per `convertBlock`. Sub-ms on any realistic document.
- PDF is dominated by chromedp bring-up (hundreds of ms); HTML render is a rounding error. `BrowserURL` amortizes bring-up for long-running consumers.

Perf budget assertion: `Parse` on 100k chars under 50 ms. Met today; container lowering inherits the same budget.

---

## 11. Open questions

1. **Inline `:::` form.** Spec says block-only. If authors keep trying `::: warning Don't. :::` in corpus drafts, revisit — otherwise keep the block discipline.
2. **Custom container type registry.** `mdpp lint` wants to warn on unknown types; the per-project registration surface belongs in `.mdpprc` (deferred to post-v0.1).
3. **`mdpp render --watch`.** Live reload for CLI-only authors. v0.2.
4. **Range column units.** Byte columns in v0.1. LSP may want UTF-8/grapheme columns for some clients; D re-encodes at the boundary.
5. **Range on synthesized nodes.** Footnote defs get displaced to the tail but keep their original-source `Range`. Document this explicitly.
6. **PDF font embedding.** Relies on headless Chrome's fonts. Revisit on cross-platform inconsistency reports.

---

## 12. Issues with parent roadmap

Two minor flags, neither blocking:

1. **`Parse` signature.** Roadmap §4.1 sketches `(*Document, error)` but current impl is `*Document`-only. This sub-spec adopts the new signature (§9), but the roadmap should clarify that `error` is reserved and always `nil` in v0.1 — otherwise readers assume retry logic is expected.
2. **PDF in conformance suite.** Roadmap §3.6 prescribes `expected.pdf.png` per case. That requires headless Chrome at test time — fine locally, but a CI-surface cost (download Chrome, configure swiftshader) worth surfacing in §7.1. Already solved in m31labs.dev CI; just reuse.

**Resolved (2026-04-19):** Parent roadmap §3.5 previously listed only `:::name` as the directive form. It has since been updated to enumerate both `[[toc]]` and `[[embed:url]]` (shipped) alongside `:::name` (planned), reflecting that the `[[name]]` and `:::name` forms coexist in v0.1. The earlier flag noting that `[[name]]` directives were missing from the roadmap is resolved.

---

## 13. Next actions

1. Owner reviews and approves this sub-spec.
2. Implementation order within A:
   - First: `Node.Range` plumbed through `convertBlock`.
   - Then: `Diagnostic` and `Document.Diagnostics()`, plus the recover branch in `Parse`.
   - Then: `RenderOptions` + `PDFOptions` value-typed APIs.
   - Then: `:::name` container directive lowering + render mapping.
   - Then: `cmd/mdpp` with `render` and `parse`. `version` last (needs final version string).
   - Then: corpus collection + `examples/conformance/` runner.
   - Last: PDF wrapper (`render_pdf.go`) — the chromedp pattern is a copy-paste from m31labs.dev.
3. The formatter sub-spec (B) can begin in parallel with implementation; it consumes this AST surface.

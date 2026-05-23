# Markdown++ Roadmap — Design Spec

**Status.** Draft, updated with progress
**Date.** 2026-04-19 (initial), updated 2026-04-19
**Owner.** Oscar Villavicencio (m31labs)
**Scope.** Meta-roadmap: names the sub-projects that together constitute the Markdown++ authoring stack, defines their purpose, interfaces, and "done" criteria for v0.1 launch, and captures the cross-cutting decisions (format identity, repo shape, launch readiness, risks). This document does **not** implement any one piece — each sub-project gets its own spec → plan → implementation cycle, cross-referenced below.

---

## 0. Progress snapshot (as of 2026-04-19)

A burst of shipping happened in the same day this roadmap was written. Below is the actual state of the engine; the rest of the document still describes the target.

**Engine (A) — substantial progress:**
- Hardening: `corpus_test.go`, `hardening_test.go`, `security_test.go` shipped. XSS escaping verified at every render surface; "Hello World" full-document corpus exercises math, admonitions, mermaid, tables, task lists, footnotes, emoji, TOC. Roughly satisfies §4.1 "100+ real-world `.md` files" intent for v0.1, though more curated corpus expansion is welcome.
- Performance: `parser_pool.go` adds a concurrent `sync.Map`-backed pool over `gotreesitter.ParserPool`. Not in original §4.1 — pleasant addition.
- Shipped features the original §3.5 did not list, now part of v0.1 surface:
  - **`[[toc]]` directive** — auto-generated table of contents at insertion point. Case-insensitive. Emits `NodeTableOfContents` with a nested heading list.
  - **`[[embed:url]]` directive** — auto-embed with provider detection (YouTube, etc.). Emits `NodeAutoEmbed` with `data-src`, `data-provider`, fallback `<a>`.
- Wired previously-stub features: definition lists (`Term\n: Def` syntax), link reference definitions, autolinks, embeds.
- Table column alignment with responsive wrapper and ARIA accessibility.
- Many robustness fixes: list stitching, deep-nested lists, all-indented code blocks, bracket-quote heading text, numeric LaTeX command arguments, Go highlight spans for type conversions.

**Engine (A) — still pending:**
- `:::name` block container directive (this roadmap §3.5)
- PDF rendering via `chromedp` (this roadmap §1.3 / §4.1)
- **`Node.Range` (start/end byte fields on every AST node)** — A's sub-spec committed to this; still not done. Marked **blocking** for B, C, and D; should be implemented before any of those sub-projects start.
- `cmd/mdpp` CLI
- `examples/conformance/` directory (the embedded corpus in `corpus_test.go` is a step toward this but doesn't yet match the spec's per-case directory layout with `expected.html`, `expected.pdf.png`, `README.md`)
- `SPEC.md` document

**Sub-projects B (formatter), C (linter), D (LSP), F (editor integrations):** not started; sub-specs written but no code.

**Implication for §3.5 spec content:** the `[[name]]` directive form (TOC, embed) and the planned `:::name` container form coexist. They occupy different syntactic niches — `[[name]]` is a single-line inline-positioned directive, `:::name`...`:::` is a multi-line block container. Both stay; §3.5 has been updated to list both.

---

## 1. Vision / North Star

**Markdown++ is the authoring surface.** A file format and a complete writing environment that makes producing documents — research papers, design docs, internal wikis, books, slides, knowledge bases, public websites — feel as good as writing code in a top-tier IDE. Plain text in, beautiful artifact out, with tooling that actually understands what the author wrote.

### 1.1 Audiences, ranked

1. **Authors.** Anyone who writes. Researchers, engineers, technical writers, students, knowledge workers, novelists, bloggers, documentation teams. Currently scattered across Notion, Obsidian, Google Docs, Word, Typora, vanilla Markdown editors. *Primary.*
2. **Toolmakers.** People building documentation pipelines, static-site generators, internal CMSes, wikis — who want a real Markdown AST in Go instead of regex-and-prayer. *Secondary, free.*
3. **Casual `.md` users.** Anyone with a `.md` file. They get nicer rendering and zero forced migration. *Beneficiary.*

### 1.2 Differentiator

The single sentence that opens the README:

> *The only Markdown stack with a real grammar — so the LSP, formatter, and linter actually understand your document instead of pattern-matching at it, and the output (HTML, PDF, slides) is a faithful artifact rather than a best-effort guess.*

The grammar claim is load-bearing. It rests on `gotreesitter` and its grammargen tooling (m31labs' pure-Go tree-sitter runtime, 206 shipping grammars). No other Markdown library in the Go ecosystem has a real AST backed by a grammar generator; most are hand-rolled parsers with post-processing, which caps the fidelity of anything downstream. The tooling we build here — LSP, formatter, linter, semantic highlighter — is possible for us precisely because of that foundation.

### 1.3 Output targets (v0.1 ship)

- **HTML** — already the primary renderer target; hardened for v0.1.
- **PDF** — via `chromedp` headless Chromium, reusing the pattern already shipping in `m31labs.dev`. Ships as `mdpp render --pdf`.

Optionality preserved but not delivered in v0.1:

- Slides (`--target=slides`)
- ePub
- DOCX
- Terminal preview

The renderer architecture must not preclude these; implementing them is explicitly out of scope for v0.1.

### 1.4 Non-goals (firm)

These conflict with the vision, not merely deferred:

- **Scripting / executable code in documents.** Markdown stays declarative. We are not building a programming language, a template engine, or a macro system.
- **Multi-input format conversion.** Pandoc ingests LaTeX, DOCX, RST, etc. We do not. We are Markdown++-in, HTML/PDF-out. Authors who need format conversion keep using Pandoc.
- **WYSIWYG visual editor as the primary authoring surface.** The LSP + live preview combo is our answer (text in, rendered preview next to it). We are not Notion's editor and do not intend to become one.

### 1.5 Deferred but explicitly open (post-v0.1)

- Charts / data fences
- Themes and a stylesheet system
- Real-time collaborative editing
- Cloud sync
- Slides as a first-class output
- Citations / cross-references / figure numbering (academic-writing layer)
- Language server for non-English content detection / locale-aware rules

### 1.6 Parser-native powers — things Markdown++ can do because it is actually parsed

These are not just "more Markdown extensions." They are features that become practical because mdpp has a gotreesitter-backed syntax tree with byte ranges, node identity, and incremental reparsing. Regex-based Markdown tools can approximate some of them for happy paths, but they cannot make the same correctness claim across nested lists, code fences, blockquotes, HTML, math, footnotes, and future containers.

1. **Symbol-aware rename and refactor.** Rename a heading, footnote id, link reference label, container id, or future citation key and update only the real references to that symbol. A text search cannot reliably distinguish `[^id]` in prose from the same bytes inside a code fence, HTML attribute, or escaped literal.
2. **Definition / reference navigation.** Jump from `[text][ref]` to `[ref]: ...`, from `[^id]` to `[^id]: ...`, from `#anchor` to the exact heading, and later from `[[Page#Section]]` to another file. The AST already knows which spans are links, which are definitions, and which are plain text.
3. **Source-preserving formatter.** Format presentation while preserving format-stable interiors byte-for-byte: code blocks, math, raw HTML, YAML values, diagram bodies, and link targets. This depends on `Node.Range`; without a parsed tree the formatter either loses bytes or refuses to touch whole classes of documents.
4. **Structural lint fixes.** Emit fixes that are constrained by syntax, not text. Examples: remove an unused reference definition, normalize only list markers outside code blocks, flag duplicate heading ids with a related location, and suppress diagnostics by AST range rather than "nearest line that looked relevant."
5. **Semantic highlighting beyond TextMate.** Color resolved links differently from broken links, highlight footnote definitions vs references, distinguish `[[toc]]` from regular bracket text, identify container type names, and mark task states. TextMate grammars can color shapes; the parsed AST can color meaning.
6. **Incremental live preview and scroll sync.** Reparse only the edited region, render only the affected subtree later, and attach `data-source-range` to rendered HTML so source and preview can scroll together. This requires a stable mapping from rendered elements back to source nodes.
7. **Safe embedded-domain parsing.** A Markdown document can contain Go, Mermaid, TeX, YAML, HTML, shell, and future chart/data blocks. gotreesitter lets mdpp hand those regions to the correct parser or highlighter without confusing their syntax for surrounding Markdown.
8. **Document graph features.** Once wikilinks, includes, or citations land, the same parser-backed symbol table can power backlinks, orphan-page detection, broken asset detection, heading-aware search, dependency graphs, and "what changes if I rename this file?" analysis.
9. **Context-aware completions.** Completion can know whether the cursor is in frontmatter, a link target, a footnote reference, a blockquote admonition marker, a `[[...]]` directive, a container fence, or a code block. This keeps suggestions useful instead of spraying every Markdown-ish token everywhere.
10. **Trustworthy transformations between forms.** Convert an admonition blockquote to a `:::` container, inline links to reference links, reference links back to inline, setext headings to ATX, or a bare URL to a descriptive-link scaffold while preserving comments and unrelated whitespace. The tree gives each transformation a bounded edit surface.

These parser-native powers are the strongest product moat. Any extension we add should ask: does being parsed let us make this feature safer, smarter, or more composable than existing Markdown tools?

### 1.7 High-demand Markdown gaps worth considering

The ecosystem keeps re-inventing the same extensions across GFM, Python-Markdown, Pandoc/MultiMarkdown, Obsidian-style tools, GitLab, static-site generators, and note-taking plugins. Some are already shipped in mdpp; others belong in the post-v0.1 backlog. This list is not a commitment to implement every item. It is the demand map we should use when deciding what deserves syntax, AST nodes, LSP affordances, and conformance tests.

**Already in or planned for v0.1:**

- **Footnotes.** Long-form and academic writing need them; repeated community requests around footnote support show this is not niche.
- **Tables with alignment.** GFM made tables table-stakes for README and documentation workflows; mdpp should keep table editing and linting first-class.
- **Task lists.** Useful beyond GitHub issues: checklists, specs, meeting notes, and lightweight project docs.
- **Admonitions / callouts.** GitHub, GitLab, Obsidian, Trilium, MyST, and static-site stacks all converge on callout syntax because authors need semantic warnings, notes, tips, and cautions.
- **Math.** Technical authors expect inline and display math without switching to LaTeX or a notebook.
- **Diagrams.** Mermaid and graph-style fences are now normal in engineering docs; parsed diagram nodes let renderers and exporters handle them explicitly.
- **Generated TOC.** Authors routinely want a table of contents without hand-maintaining anchor links.
- **Auto-embeds.** Markdown is increasingly used as a publishing format; embeds for video, code demos, audio, and social links should degrade to safe links.
- **Container directives.** Generic `:::` regions unlock asides, columns, details blocks, warnings, and future layout targets without turning Markdown into a template language.

**Strong post-v0.1 candidates:**

- **Wikilinks and backlinks.** `[[Page]]`, `[[Page#Heading]]`, aliases, unresolved-link creation, backlinks, and orphan-note detection are the feature cluster that made Obsidian/Roam-style Markdown workflows feel alive. Because mdpp is parsed, wikilinks can coexist cleanly with normal links, code fences, and embeds.
- **Attributes on blocks and inline spans.** Pandoc-style `{#id .class key=value}` is the escape hatch authors keep reaching for when they need stable anchors, CSS hooks, language tags, or accessible labels. The parser can make this safe by restricting where attributes bind.
- **Captions and figures.** Images, tables, code blocks, and diagrams need captions, numbering, and cross-references. This is especially important for papers, books, reports, and design docs.
- **Citations and bibliography.** `[@key]`, `[-@key]`, bibliography frontmatter, CSL output, and unresolved-citation diagnostics are the academic-writing layer.
- **Cross-references and numbering.** `See Figure 2`, `§3.1`, equations, tables, examples, appendices, and headings should be referenceable without manual numbering.
- **Includes / transclusion.** Authors want reusable snippets: include another Markdown file, a code excerpt, a CSV table, or a partial. This needs strict cycle detection, source maps, and sandboxing; parser-backed dependency graphs make it tractable.
- **Asset management.** Detect missing images, unused assets, oversized images, broken relative links, and whether exported PDFs have embedded everything they need.
- **Table editing helpers.** Sort rows, normalize delimiter rows, align or intentionally not align pipes, convert CSV/TSV to tables, and keep table semantics stable under edits.
- **Definition lists and glossaries.** Already partially shipped; the next layer is glossary extraction, term references, and duplicate-term linting.
- **Details / disclosure blocks.** Authors often want collapsible sections. Native HTML exists, but Markdown-inside-`summary` behavior differs by renderer; a parsed container can define reliable semantics.
- **Spoilers and redactions.** Common in community docs and support knowledge bases. Needs careful accessibility and plaintext fallback semantics.
- **Frontmatter schemas.** Validate reserved keys (`title`, `lang`, `toc`, `date`, `mdpp`), offer completions, and let projects define schemas later without turning the format into a CMS.
- **Smart typography as an opt-in renderer layer.** Quotes, dashes, ellipses, fractions, and locale-aware punctuation are useful for prose authors but must be renderer-configurable so code-like docs stay byte-honest.
- **Multi-file workspaces.** Workspace symbol search, rename file and update links, broken backlink detection, publish graph, and "what pages include this snippet?" are all natural once Markdown is a parsed document graph.
- **Export-grade controls.** Page breaks, print-only / screen-only blocks, cover pages, headers/footers, figure lists, and PDF-safe asset checks are the things authors miss when Markdown becomes the source of record for serious documents.

Prioritization rule: prefer features that (1) degrade to readable CommonMark, (2) become meaningfully better because mdpp is parsed, and (3) unlock editor/LSP affordances, not just prettier HTML.

---

## 2. Brand identity

**Public name.** "Markdown++"
**Package / tool prefix.** `mdpp` (short, types fast, keeps as existing Go module name)
**Brand / publisher.** m31labs
**Primary domain.** `markdownpp.m31labs.dev` (subdomain of the existing m31labs.dev property, served by the existing gosx app)
**Go module path.** `github.com/odvcencio/mdpp` (unchanged; can move to an `m31labs` GitHub org later without breaking consumers via `go.mod` replace semantics or repo transfer)
**VS Code Marketplace publisher.** m31labs

The split between "Markdown++" (what humans say) and "mdpp" (what machines say) is deliberate. The long-form name is legible and searchable for positioning; the short form is ergonomic at the command line and in imports.

---

## 3. Format identity

### 3.1 File extension

`.md`. Always. No `.mdpp`, no `.md++`. Adoption friction kills formats; every editor, GitHub renderer, file manager preview, and grep-based tool already handles `.md`. We do not trade that for a fancier extension.

### 3.2 Language ID

The LSP registers as `markdown-plus-plus`. By default it is *also* registered for the standard `markdown` ID, so authors who install the VS Code extension get the better experience on every `.md` file automatically. Per-workspace opt-out is supported via editor config for authors who want the vanilla Markdown LSP on some files.

### 3.3 Version policy

Format version field in frontmatter: `mdpp: 0.1`. Absent field means "latest interpretation" at the reader's option.

Pre-1.0, the format can evolve in minor versions (`0.2`, `0.3`, ...) without breaking-change ceremony. v1.0 is a future commitment, cut when real-world feedback tells us the surface is stable. The conformance suite described in §3.6 documents "v0.1 behavior" rather than a frozen contract.

### 3.4 CommonMark superset commitment

Every valid CommonMark file is valid Markdown++. Every valid Markdown++ file renders *acceptably* in plain CommonMark renderers — extensions degrade to readable plaintext, not garbled output. This is a tested commitment, enforced by golden files that compare CommonMark-only output against a reference plain-MD renderer; failures break CI.

### 3.5 The `SPEC.md` document

Lives at the repo root. Also rendered to `markdownpp.m31labs.dev/spec`. Contents, in order:

1. **Base.** CommonMark, incorporated by reference.
2. **GFM extensions adopted.** Tables, task lists, autolinks, strikethrough. Reference GFM spec; note any divergences.
3. **Markdown++ additions.**
   - *Math.* `$inline$` and `$$display$$`. LaTeX subset enumerated in an appendix.
   - *Footnotes.* `[^id]` ref and `[^id]: text` definition. Block-body definitions supported (multi-line, indented continuation).
   - *Admonitions.* `> [!TYPE] Optional Title\n> body` for NOTE, TIP, WARNING, CAUTION, IMPORTANT. Type set is extensible in future minor versions.
   - *Super / subscript.* `^x^` and `~x~`.
   - *Definition lists.* Standard markdown-it-style syntax.
   - *Emoji shortcodes.* `:name:` resolved against a fixed table (our existing emoji table).
   - *Diagram fences.* ` ```mermaid ... ``` `, ` ```dot ... ``` `. Data only — mdpp emits a structured node; downstream consumers render.
   - ***`[[toc]]` directive (shipped).*** Single-line, case-insensitive, on its own line. Replaced at render time with an auto-generated nested heading list scoped to the document. AST node: `NodeTableOfContents`.
   - ***`[[embed:url]]` directive (shipped).*** Single-line auto-embed. Provider detected from URL host (YouTube, Vimeo, others); unknown providers fall back to a generic `<a>` link. AST node: `NodeAutoEmbed` with `data-src`, `data-provider` attributes.
   - ***`:::name` container directives (planned for v0.1).*** Generic styled block regions. Syntax: `:::type\nbody\n:::`. Multi-line. Nesting supported. Attribute syntax (`{.warning #intro}`) deferred to A's sub-spec. Use cases: callouts, columns, asides, styled regions, future slide targets. Distinct from the `[[name]]` directive form, which is single-line inline-positioned.
   - *Frontmatter.* YAML, keys reserved: `title`, `lang`, `toc`, `date`, `mdpp` (version). Arbitrary user keys pass through.
4. **Output semantics.** HTML mapping for each construct. ARIA roles where relevant. PDF differences (page breaks, link behavior, math rendering path).
5. **Format version.** The `mdpp:` frontmatter key, how future versions may gate behavior.
6. **Conformance suite.** Directory of `input.md` + `expected.html` + `expected.pdf.png` (screenshot) triples. Any compliant Markdown++ implementation must pass.

### 3.6 Conformance suite

Lives at `examples/conformance/`. Each case is a directory:

```
examples/conformance/footnotes/
  input.md
  expected.html
  expected.pdf.png     (rasterized first page, perceptual-diff tolerance)
  README.md            (what this case demonstrates)
```

The suite is used by:

- Golden tests in the main repo (every push runs it).
- The spec page on the docs site (renders each case side-by-side for human inspection).
- Future external implementations (if any exist) to claim Markdown++ conformance.

At v0.1, minimum 30 cases, covering every construct in §3.5 plus edge cases surfaced during engine hardening.

---

## 4. Sub-projects

Six pieces. Each gets its own spec → plan → implementation cycle, to be written after this roadmap is approved. Cross-references noted where decisions defer to individual sub-specs.

### 4.1 A. Engine (`mdpp` Go package + `cmd/mdpp` CLI)

**Purpose.** The parser, AST, and renderer. The foundation everything else sits on. Plus a CLI so people can try it without writing Go.

**Interface.**

```go
// Go API (stable within 0.x line)
func Parse(src []byte) (*Document, error)
func (d *Document) AST() *Node
func (d *Document) Frontmatter() map[string]any
func Render(d *Document, opts RenderOptions) ([]byte, error)
func RenderPDF(d *Document, opts PDFOptions) ([]byte, error)
```

AST node types are public, stable, documented. No `internal/` leakage.

CLI subcommands:

- `mdpp render <file>` — to stdout (HTML default), `--pdf` for PDF, `--out <path>` for file, `--format slides` reserved (errors in v0.1)
- `mdpp parse --json <file>` — dumps AST as JSON for toolchain use
- `mdpp fmt` — see §4.2
- `mdpp lint` — see §4.3
- `mdpp version` — prints version + Go build info

**Done for v0.1.**

- Existing parser/renderer hardened against a corpus of 100+ real-world `.md` files (README.md from popular repos, academic preprints, blog posts). Zero panics, zero unrecoverable errors; all parse failures become recoverable diagnostics with byte-range info.
- `:::name` container directives parse and render (grammar path preferred; post-processing fallback documented if grammar path proves infeasible).
- PDF output via `chromedp` (pattern reused from `m31labs.dev`; no net-new infrastructure work).
- `cmd/mdpp` CLI with `render`, `parse`, `version` wired up. `fmt` and `lint` stubbed (forwarded to B and C when those land).
- Stable public Go API. Godoc complete. No breaking changes after v0.1 within the v0.x line unless specifically flagged.
- `examples/` directory with at least 30 conformance cases (per §3.6) covering every spec feature.

**Sub-spec.** `docs/superpowers/specs/YYYY-MM-DD-a-engine-design.md` (to be written).

### 4.2 B. Formatter (`mdpp fmt`)

**Purpose.** Canonical reformatter. `gofmt` for Markdown++. Removes the entire class of "what's the right way to write this list?" debates, makes diffs meaningful, and produces a consistent input for downstream tools.

**Interface.** Reads Markdown++, produces Markdown++. Two modes: stdin/stdout (for editor integration) and in-place (`--write` for batch use). Rules are hardcoded canonical style in v0.1 — no `.mdpprc` knobs, no per-project overrides. (Opinionated > flexible at this stage; style config is deferred to a later minor version if demand is real.)

Go API:

```go
func Format(src []byte) ([]byte, error)
```

LSP dispatches `textDocument/formatting` to this.

**Invariants.**

- **Idempotency.** `Format(Format(x)) == Format(x)`. Property-tested against the example corpus.
- **Semantic stability.** `Parse(Format(x))` produces an AST semantically equal to `Parse(x)`. No constructs added, removed, or renamed; only presentation changes. Property-tested.
- **Frontmatter preservation.** YAML key order preserved; comments preserved if the YAML library supports it.

**Done for v0.1.**

- Idempotency property test passes across the example corpus.
- Semantic-stability property test passes.
- Existing mdpp tests still produce identical AST after formatting (round-trip test).
- LSP can call `Format` via `textDocument/formatting` (integration-tested via harness — see §4.4).

**Sub-spec.** `docs/superpowers/specs/YYYY-MM-DD-b-formatter-design.md`. Enumerates exact style choices: list marker (probably `-`), fence info-string canonicalization, emphasis style (`*` vs `_`), line wrap policy (proposal: no wrap; let the renderer handle it), spacing around headings, table column alignment behavior.

### 4.3 C. Linter (`mdpp lint`)

**Purpose.** Catch errors and stylistic issues that the formatter can't auto-fix. Becomes the LSP's diagnostic source.

**Interface.**

```go
type Diagnostic struct {
    Range    Range       // byte range in source
    Severity Severity    // Error, Warning, Info, Hint
    Code     string      // stable, e.g. "MDPP010"
    Message  string
    Fix      *TextEdit   // optional auto-fix
}

func Lint(d *Document) []Diagnostic
```

CLI:

- `mdpp lint <file>` — prints diagnostics, exit non-zero on Errors
- `mdpp lint --fix <file>` — applies auto-fixes in place
- `mdpp lint --format=json` — machine-readable output

**Rule categories and v0.1 target set (~15 rules):**

- *Semantic.* Undefined footnote references; broken intra-doc links (`[text](#anchor)` where anchor doesn't exist); duplicate heading IDs; undefined `:::` container types; reference-link definitions with no uses; footnote definitions with no references.
- *Accessibility.* Missing image alt text; heading-level skips (h1→h3 with no h2); bare URL autolinks where a descriptive link would help; empty link text.
- *Style.* Inconsistent list markers within a list; inconsistent emphasis style (`*foo*` vs `_foo_`); trailing whitespace; multiple consecutive blank lines.

Each rule has a stable `Code` (numeric namespace: `MD001`-`MD099` for rules that overlap with established markdownlint rules, `MDPP100+` for Markdown++-specific rules). Users suppress via inline comment (`<!-- mdpp-disable MDPP010 -->`) or config file (deferred to post-v0.1).

**Done for v0.1.**

- 15 rules implemented, each with unit tests covering positive and negative cases.
- Auto-fixes implemented for the style-category rules where mechanically safe.
- Performance budget: `Lint` on a 100k-character document completes in under 50ms on a modern laptop. Perf test enforces this.
- LSP consumes this directly (`publishDiagnostics` + `codeAction`).

**Sub-spec.** `docs/superpowers/specs/YYYY-MM-DD-c-linter-design.md`. Full rule list, rule codes, auto-fix specifications, suppression syntax, severity defaults.

### 4.4 D. LSP (`cmd/mdpp-lsp`)

**Purpose.** Editor-agnostic language server. The thing that makes writing Markdown++ feel like writing code in a real IDE.

**Interface.** Speaks LSP 3.17 over stdio. Editor-agnostic: no VS Code-specific assumptions; works in any LSP-conformant editor.

**Protocol surface for v0.1 (minimum viable LSP):**

- `initialize` / `initialized` / `shutdown` / `exit`
- `textDocument/didOpen` / `didChange` / `didSave` / `didClose` — incremental sync backed by tree-sitter `ParseIncremental` (already fast in gotreesitter)
- `textDocument/publishDiagnostics` — driven by C
- `textDocument/hover` — tooltips for footnote refs (show definition body), reference links (show target), math (show rendered preview via HTML-in-tooltip), emoji (show Unicode + name), admonition types (show description), container types (show description)
- `textDocument/definition` — jump from `[^1]` to `[^1]:`, from reference link `[text][ref]` to its `[ref]:` def, from heading link `[text](#anchor)` to the heading
- `textDocument/references` — find all uses of a footnote ID, link definition, heading anchor
- `textDocument/rename` — safe rename of footnote IDs, link definition IDs, heading anchors (updates all references atomically)
- `textDocument/foldingRange` — sections (by heading hierarchy), code fences, frontmatter, `:::` containers
- `textDocument/documentSymbol` — outline of headings + frontmatter keys + container types
- `textDocument/formatting` — calls B
- `textDocument/codeAction` — applies linter auto-fixes from C; also "Convert admonition to `:::` container", "Convert reference link to inline link", etc.
- `textDocument/completion` — footnote IDs (after `[^`), reference link IDs (after `][`), emoji shortcodes (after `:`), container types (after `:::`), frontmatter keys (in frontmatter context), admonition types (after `[!`)
- `textDocument/semanticTokens/full` and `range` — see §4.5

**Stretch (only if trivial to add):**

- `textDocument/signatureHelp` (probably not applicable to markdown)
- `textDocument/prepareRename`
- `textDocument/inlayHint` (e.g., show math rendered inline; a compelling demo)
- `workspace/symbol` (cross-file search by heading)

**MVP cut-line.** If D slips, ship: didOpen/didChange/didSave + publishDiagnostics + hover + formatting + definition + foldingRange + documentSymbol. Everything else layers in post-launch without breaking clients.

**Done for v0.1.**

- All required behaviors work against the example corpus, verified by an **LSP integration test harness** (mocks a minimal LSP client in-process, sends protocol messages, asserts responses). No editor required for CI.
- Manual smoke tests pass in VS Code, Neovim (nvim-lspconfig), and Helix. Zed and Emacs configs shipped as "community-supported."
- Performance: hover/diagnostics respond in under 50ms on documents up to 50k chars. Live preview updates within 100ms of keystroke via `ParseIncremental` (which is sub-microsecond on single-byte edits per gotreesitter benchmarks — this budget is basically all renderer cost).

**Sub-spec.** `docs/superpowers/specs/YYYY-MM-DD-d-lsp-design.md`. Full protocol method-by-method definition, state management, incremental sync strategy, error handling.

### 4.5 E. Semantic highlighter

**Purpose.** Per-token semantic info richer than what TextMate grammars can express. Distinguishes footnote *ref* from footnote *def*, undefined link from defined, math content from surrounding text, container type names from container body, frontmatter keys from values, broken reference links from working ones.

**Interface.** Delivered via LSP `textDocument/semanticTokens/full` and `range`. No separate binary, no separate grammar file, no TextMate export. The tree-sitter AST IS the grammar; D walks it and emits tokens. This is why the grammargen foundation matters: regex-based highlighters (which is what most editors ship for Markdown) cannot express any of this.

**Token types emitted (LSP standard types + custom):**

- `heading` (with modifier `level-1` through `level-6`)
- `link` (with modifiers: `resolved`, `broken`, `reference`, `inline`, `autolink`)
- `footnote` (with modifiers: `definition`, `reference`, `broken`)
- `math` (with modifiers: `inline`, `display`)
- `container.type` (custom — the type name after `:::`)
- `admonition.type` (custom — the `[!TYPE]` name)
- `emoji.shortcode` (custom — `:name:` form)
- `frontmatter.key` (custom)
- `frontmatter.value` (custom)
- `strikethrough`, `emphasis`, `strong` (with appropriate modifiers)
- Plus standard types for code spans, code blocks (with embedded-language info), blockquotes, lists

**Done for v0.1.** All token types above emitted correctly for the example corpus. Validated by an integration test that diff's expected semantic token streams against actual for each conformance case.

**Sub-spec.** Folded into D. No separate sub-spec document.

### 4.6 F. Editor integrations

**Purpose.** The launch surface. Distribute the LSP + semantic tokens to where authors actually live.

**Components shipped at v0.1:**

**1. VS Code extension** (`mdpp-vscode`, separate repo, TypeScript).

- Bundles the `mdpp-lsp` binary (downloaded from GitHub Releases on first activation; platform-specific binary selected automatically).
- Registers language IDs: `markdown-plus-plus` for files with `mdpp:` frontmatter or the extension explicitly opened them; also overrides the standard `markdown` ID by default (configurable).
- Registers commands: `Markdown++: Render to HTML`, `Markdown++: Export to PDF`, `Markdown++: Open Live Preview`.
- Ships a **side-by-side live preview webview**. Renders via the same `Render` function the engine exports (called through the LSP via a custom notification). Preview updates on content change, respects scroll sync with the editor.
- Configuration: override language ID behavior, set PDF page size / margins, disable live preview.

**2. Editor configs for other editors** (in main repo, `docs/editors/`):

- Neovim (`nvim-lspconfig` recipe)
- Helix (`languages.toml` snippet)
- Zed (`extensions.json` snippet)
- Emacs (`lsp-mode` and `eglot` snippets)

These are one-page install guides. The LSP works in all of them because it's protocol-conformant; we just publish the recipes.

**Done for v0.1.**

- VS Code extension on the Marketplace, working hover/diagnostics/format/preview demonstrated in launch GIFs.
- Editor configs verified working in Neovim and Helix at minimum (primary author uses/tests these). Zed and Emacs are community-validated.
- Live preview renders within 100ms of keystroke for documents up to 50k chars.

**Sub-spec.** `docs/superpowers/specs/YYYY-MM-DD-f-editor-integrations-design.md`. VS Code extension architecture, webview rendering strategy (proposal: native webview with engine HTML output; fallback: VS Code's built-in MD preview API), LSP binary distribution strategy, update flow.

---

## 5. Repo and module layout

**Pattern.** Single Go module for all Go code, separate repo for the TypeScript VS Code extension.

**`github.com/odvcencio/mdpp` (Go module — current repo).**

```
/
├── README.md                   # rewritten for launch
├── SPEC.md                     # format spec
├── LICENSE
├── go.mod
├── go.sum
├── ast.go
├── parse.go
├── render.go
├── render_pdf.go               # new; uses chromedp (pattern from m31labs.dev)
├── extensions.go
├── latex.go
├── diagram.go
├── emoji.go
├── highlight_*.go
├── fmt/                        # formatter package (B)
├── lint/                       # linter package (C)
├── lsp/                        # LSP server package (D + E)
├── cmd/
│   ├── mdpp/                   # CLI entry (A)
│   └── mdpp-lsp/               # LSP entry (D)
├── examples/
│   ├── conformance/            # §3.6 cases
│   └── showcase/               # curated README-linkable examples
└── docs/
    ├── superpowers/specs/      # this doc and sub-specs live here
    └── editors/                # per-editor setup recipes (F)
```

Rationale: single `go install` brings every binary; cross-cutting refactors (adding an AST node that fmt/lint/lsp all need) are atomic; Go tooling (godoc, staticcheck, etc.) sees a unified view.

**`github.com/odvcencio/mdpp-vscode` (new repo — TypeScript).**

```
/
├── README.md
├── package.json
├── tsconfig.json
├── src/
│   ├── extension.ts            # activation, binary download, commands
│   ├── preview.ts              # live-preview webview
│   └── lsp.ts                  # vscode-languageclient setup
├── syntaxes/                   # fallback TextMate grammar (for pre-LSP-boot highlighting)
└── images/                     # Marketplace icon, screenshots
```

Rationale: Marketplace tooling expects TypeScript + `package.json`. Separate repo means the TS build, extension versioning, and Marketplace release cadence don't entangle Go work.

**Module path.** `github.com/odvcencio/mdpp` stays unchanged. Migration to a hypothetical `github.com/m31labs/mdpp` can happen later via GitHub repo transfer + `replace` directive during transition; not blocking for v0.1.

---

## 6. Dependency graph and phasing

Phases are ordered, not timed. The user's pace is fast; calendar estimates are omitted deliberately.

```
Phase 1 — Foundation
  ┌─────────────────┐    ┌─────────────────┐
  │ A. Engine + CLI │    │ B. Formatter    │
  └─────────────────┘    └─────────────────┘
                   (parallel — both consume AST)

Phase 2 — Quality
         ┌──────────────────┐
         │ C. Linter        │
         └──────────────────┘

Phase 3 — Experience
         ┌──────────────────────┐
         │ D. LSP               │
         │   (E folds inside)   │
         └──────────────────────┘

Phase 4 — Distribution
         ┌──────────────────────┐
         │ F. Editor integr.    │
         └──────────────────────┘

Phase 5 — Launch readiness
         ┌──────────────────────┐
         │ Docs, GIFs, Mkt sub  │
         └──────────────────────┘
```

**Notes on the dependency model:**

- **A and B are parallel-safe.** Both consume the AST; neither produces input for the other.
- **B and C are parallel-safe too.** Some lint rules could defer auto-fixes to the formatter at runtime, but that's runtime coordination, not a build-time dependency. Phase 2 is drawn after Phase 1 for clarity, but C can overlap with B if engineering bandwidth permits.
- **D requires A's AST API to be frozen, not shipped.** D's protocol scaffolding can start as soon as A's public API is stable.
- **F requires D to be functional, not polished.** The VS Code extension can be built against an in-development LSP.
- **Phase 5 is not feature work.** It is README rewrite + GIF recording + Marketplace submission + example curation. Short by design.

---

## 7. Launch readiness

What the engineering work produces so that a launch is *possible* when the owner decides to pull the trigger. The launch itself (HN timing, cross-posting, announcement copy) gets its own doc written closer to ship.

### 7.1 Tier 1 — required (blocks launch)

1. Rewritten `README.md`. Differentiator first (§1.2), install second (VS Code Marketplace button, `go install`, Homebrew formula), feature table third, GIFs embedded inline.
2. `SPEC.md` complete.
3. VS Code Marketplace listing — submitted, approved, installable. (Marketplace has a review queue; submit at least a day before the launch moment.)
4. Pre-built `mdpp` and `mdpp-lsp` binaries on GitHub Releases for Mac (arm64 + x64), Linux (x64 + arm64), Windows (x64). `go install github.com/odvcencio/mdpp/cmd/...@latest` works.
5. `examples/` directory browseable on the docs site.
6. **Animated GIFs** — 4-5 short clips (screen-record + `gifski`, no narration, no editing required) embedded in the README, each showing a distinct LSP behavior: footnote rename, hover preview, code-action auto-fix, format-on-save, live preview with math/admonitions/containers.

### 7.2 Tier 2 — polish (can ship after launch)

7. Landing page at `markdownpp.m31labs.dev` (served by the existing gosx app on `m31labs.dev`). For v0.1, the GitHub README can serve this role; the dedicated landing page is an upgrade, not a blocker.
8. Demo video (only if the owner wants to make one; not required).
9. Blog post on `m31labs.dev/blog`.
10. Homebrew tap (`brew install m31labs/tap/mdpp`).

### 7.3 Tier 3 — the launch itself

Not in this roadmap. Separate doc when we are close to ship.

---

## 8. Risks and mitigations

1. **LSP scope creep.** Protocol surface is large; it's easy for D to balloon. **Mitigation:** D's sub-spec defines a "minimum viable LSP" cut-line explicitly (see §4.4). Ship the minimum; layer the rest post-launch.
2. **`:::` container grammar work.** Adding generic container directives to the existing tree-sitter Markdown grammar may be harder than it looks. **Mitigation:** Grammar path preferred (gives the LSP better ranges), post-processing fallback documented in A's sub-spec. Either works; pick based on implementation effort.
3. **Formatter round-trip stability.** Preserving comments, frontmatter key order, link definition block placement, table column alignment — each is a stability puzzle. **Mitigation:** B's sub-spec enumerates which constructs are "format-stable" (preserved verbatim) versus "format-canonicalized" (rewritten) up front, with examples. Property-tested idempotency + semantic stability on the conformance corpus.
4. **Multi-editor LSP validation time-sink.** Claiming "works in Neovim/Helix/Zed/Emacs" means actually testing in each. **Mitigation:** "Supported" for v0.1 = VS Code + Neovim + Helix (all three exercised by the primary author). Zed and Emacs get community-contributed configs, not promised support.
5. **Performance on large documents.** LSP must feel snappy on 50k+ char files. **Mitigation:** gotreesitter's `ParseIncremental` is already sub-microsecond on single-byte edits. The linter is the likely bottleneck; C has a stated perf budget (50ms on 100k chars) enforced by a perf test. Any rule that fails the budget gets optimized or cut.

**Explicitly not a risk:** PDF rendering. Already proven in m31labs.dev via chromedp. Reuse, don't rebuild.

---

## 9. Decisions deferred to sub-specs

These are real decisions but do not need to be made in this roadmap doc. They belong in the individual sub-specs where their context lives.

- **A.** Exact `:::name` container attribute syntax (Pandoc-style `{.warning #intro}` vs simpler). PDF pipeline concrete choice (chromedp page options, margins, page size defaults). Stable Go API surface list.
- **B.** List marker choice (`-`, `*`, `+`). Emphasis style (`*` vs `_`). Line wrap (no wrap preferred). Table alignment handling. Fence info-string canonicalization.
- **C.** Full 15-rule list with exact codes and messages. Auto-fix specifications per rule. Suppression comment syntax. Config file format (probably deferred to post-v0.1).
- **D.** Incremental sync edge cases. State management for unsaved buffers. Error recovery for malformed documents in the LSP state.
- **F.** Live-preview rendering strategy (native webview vs VS Code built-in MD preview API — proposal is native webview using our HTML output). LSP binary download strategy (GitHub Releases direct vs a Marketplace-friendly alternative). Update flow.

---

## 10. Decisions made (recorded here, do not relitigate in sub-specs)

- **Format name.** Markdown++
- **Go module.** `github.com/odvcencio/mdpp` stays
- **File extension.** `.md` only
- **Language ID.** `markdown-plus-plus`, also claims `markdown` by default
- **Format version.** `mdpp: 0.1` frontmatter key; pre-1.0 may evolve freely
- **Domain.** `markdownpp.m31labs.dev`
- **Marketplace publisher.** m31labs
- **Repo shape.** Single Go module + separate TS repo for VS Code extension
- **Launch shape.** One big bang (no staged v0.1/v0.2 drip)
- **PDF strategy.** `chromedp` headless Chromium (reusing m31labs.dev pattern)
- **LSP scope.** Editor-agnostic from day one; minimum viable cut-line defined in D's sub-spec
- **Charts in v0.1.** No. Deferred, not rejected.

---

## 11. Glossary

- **CommonMark.** The mainstream Markdown specification. We are a strict superset.
- **GFM.** GitHub Flavored Markdown. We adopt its extensions (tables, task lists, autolinks, strikethrough).
- **gotreesitter.** m31labs' pure-Go tree-sitter runtime. 206 grammars. No CGo.
- **grammargen.** gotreesitter's grammar-generation tooling used to produce the parse tables Markdown++ extensions ride on.
- **LSP.** Language Server Protocol, v3.17. Editor-agnostic IDE capabilities over a JSON-RPC connection.
- **AST.** Abstract Syntax Tree. The structured representation of a parsed document.
- **Tree-sitter.** An incremental parsing framework. gotreesitter is m31labs' pure-Go implementation of its runtime.
- **`:::` container.** A styled-region directive added in Markdown++ v0.1. Used for callouts, columns, custom-styled blocks.

---

## 12. References

- CommonMark Spec: https://spec.commonmark.org/
- GitHub Flavored Markdown Spec: https://github.github.com/gfm/
- Language Server Protocol 3.17: https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/
- gotreesitter: https://github.com/odvcencio/gotreesitter
- mdpp: https://github.com/odvcencio/mdpp
- m31labs.dev: https://m31labs.dev
- chromedp: https://github.com/chromedp/chromedp

---

## 13. Next actions

1. Owner reviews and approves this roadmap.
2. First sub-spec to write: **A. Engine** (it's the substrate; B's sub-spec is shaped by A's API decisions).
3. In parallel with A's sub-spec: create `examples/conformance/` directory and start populating the corpus with existing known cases.
4. Sub-specs B, C, D, F follow A. E is folded into D.

# Sub-project B — Formatter (`mdpp fmt`) — Design Spec

**Status.** Draft
**Date.** 2026-04-19
**Owner.** Oscar Villavicencio (m31labs)
**Parent.** [Markdown++ Roadmap](2026-04-19-markdown-plus-plus-roadmap-design.md), §4.2
**Scope.** Defines `mdpp fmt` end-to-end: invariants, the format-stable vs format-canonicalized partition, the v0.1 canonical style, the formatting algorithm, edge-case handling, public Go API, CLI surface, LSP integration, and the testing strategy that proves the invariants hold. Does **not** relitigate roadmap decisions (file extension, format name, language ID, format version policy — see roadmap §10).

---

## 0. Progress snapshot (as of 2026-04-19)

B has not started. The content of this sub-spec remains the design target; nothing below has been implemented or invalidated.

**Engine progress that affects B:**

- **Two new AST node types to format.** A shipped `NodeTableOfContents` (produced by the `[[toc]]` directive, single-line, case-insensitive) and `NodeAutoEmbed` (produced by `[[embed:url]]`, single-line, with provider detection). Both are **format-canonicalized** in B's terms: rewrite to canonical form (`[[toc]]` lowercase; `[[embed:url]]` with no whitespace inside the brackets), each on a line of its own, with a blank line above and below. Style rule recorded in §4 below; partition recorded in §3.
- **`Node.Range` field is still missing — BLOCKING.** The byte-exact preservation strategy this sub-spec depends on (§5.1, source-guided rewrite) requires every AST node to expose its source byte range. The current `Node` struct (`ast.go`) carries `Type`, `Children`, `Literal`, `Attrs` only — no `Range`, no start/end. **B cannot start implementation until A delivers `Node.Range` on every node type.** This is a hard precondition, not a soft preference. The roadmap §0 progress snapshot now flags the same gap as "blocking for B, C, D."
- **Interim approach trade-off.** If A slips on `Node.Range`, the AST-walk-and-re-emit fallback (§5.1, option 1) works for everything *except* code blocks and math interiors and HTML blocks — i.e., every format-stable construct in §3 — where it would lose original whitespace, comment, and indentation fidelity. Shipping B against the fallback would mean documenting that loss as a known limitation and weakening invariant §2.4 (no data loss). Strongly preferred: wait for `Node.Range`.

Other shipped engine work (concurrent parser pool, parser robustness fixes, hardening test suites) does not affect B's design.

---

## 1. Purpose and frame

`mdpp fmt` is the canonical reformatter for Markdown++. `gofmt` for the format: author writes whatever shape of Markdown they like; the formatter normalizes to a single canonical form. The win is the same as `gofmt`'s — diffs become meaningful, code review stops bikeshedding list markers and emphasis underscores, and downstream tools get a stable, predictable input.

Two forces shape every decision here:

1. **Idempotency and semantic stability are non-negotiable.** Property-tested, not aspirational.
2. **Opinionated > flexible at v0.1.** No `.mdpprc`, no per-project overrides, no rule toggles. If real demand for configuration appears post-launch, a later minor version can add a constrained surface.

The formatter is parallel-safe with the engine (roadmap §6). It underpins the linter's auto-fix layer and the LSP's `textDocument/formatting`.

---

## 2. Invariants

These hold for every input the formatter accepts. They are stated as testable propositions.

### 2.1 Idempotency

> `Format(Format(x)) == Format(x)` — exact byte-equality.

Property-tested against the conformance corpus (roadmap §3.6) and `examples/`. No "convergence after N passes" allowance — a single pass must reach the fixed point.

### 2.2 Semantic stability

> `Parse(Format(x))` produces an AST equal to `Parse(x)`, modulo whitespace-only nodes.

Property-tested. The comparison walks both ASTs in lockstep, ignoring purely-presentational deltas (leading/trailing whitespace, blank-line positioning). No construct may be added, removed, retyped, reparented, or have its content modified. Frontmatter values, code-block contents, math contents, and link targets are bit-exact.

### 2.3 Frontmatter preservation

YAML key order preserved exactly. YAML values preserved verbatim (no quoting/style normalization, no number reformatting). Comments preserved if the YAML library supports it; if it does not, the formatter declines to reformat the frontmatter block (preserves byte-for-byte) rather than drop comments silently. The closing `---` is normalized to a single line; exactly one blank line follows it.

### 2.4 No data loss

Every byte of meaningful content is present in the output. "Meaningful" excludes only: trailing whitespace, collapsed blank-line runs, indentation that doesn't affect parsing, and the specific syntactic alternates we explicitly canonicalize (e.g., `_x_` → `*x*`). It does **not** exclude any character of text, code, math, link target, image URL, HTML block, raw inline HTML, frontmatter value, or fence info-string content.

### 2.5 CommonMark superset preservation

A formatted document containing only CommonMark constructs remains valid CommonMark. The formatter does not rewrite a CommonMark construct into a Markdown++ extension form (e.g., does not turn a blockquote starting with "Note:" into an admonition).

---

## 3. Format-stable vs format-canonicalized

The single most important design decision in the formatter is what it touches and what it leaves alone. Two columns:

| Format-stable (preserved verbatim — byte-exact) | Format-canonicalized (rewritten to canonical form) |
|---|---|
| Code-block contents (fenced and indented) | Fence info-strings (lowercase language, canonical key=value casing) |
| Math content (`$inline$` and `$$display$$` interiors) | Surrounding whitespace around math display blocks |
| HTML blocks (entire block, exact bytes) | Blank-line counts between top-level blocks |
| Raw inline HTML spans | List markers (one chosen marker per list type) |
| Link target URLs and titles (no normalization, no percent-encoding fix-up) | Emphasis and strong delimiters (`*` / `**` only) |
| Image URLs and alt-text bytes | Heading style (ATX only; setext rewritten) |
| Reference link target URLs | Reference link definition placement and ordering |
| Frontmatter values (YAML scalars left as written) | Reference link definition formatting |
| Comments inside code or math blocks | Footnote definition placement and ordering |
| Diagram-fence contents (mermaid, dot, etc.) | Table column alignment (cell-padding canonicalization) |
| Footnote definition body content | Indentation of nested lists (canonical units) |
| Admonition title and body text | Trailing whitespace (stripped on every line) |
| Definition-list term and description text | Final newline (exactly one at EOF) |
| Container (`:::name`) body content | Container fence lines (`:::name` and `:::` on their own lines) |
| (none — directives have no preserved interior) | `[[toc]]` directive (lowercase, no inside-bracket whitespace, line of its own, blank line above and below) |
| (none — directives have no preserved interior) | `[[embed:url]]` directive (lowercase scheme, no inside-bracket whitespace, line of its own, blank line above and below; `url` payload preserved verbatim) |

The rule of thumb: if changing the bytes could change what a human or downstream tool perceives as content, the formatter does not touch the bytes. If the bytes are pure presentation — affecting parser disambiguation but not output meaning — the formatter is free to canonicalize.

This partition is the single biggest source of "round-trip stability" risk (see roadmap §8 risk #3). Enumerating it up front, with examples, is the mitigation.

---

## 4. Concrete style decisions for v0.1

For each canonicalized construct, the chosen canonical form and the reasoning behind it. Where a choice is contentious (line wrap, table alignment), the rejected alternative is named.

### 4.1 List markers

- **Unordered lists:** `-` (hyphen). Rejected alternates: `*` (visually collides with emphasis), `+` (rare; surprises readers).
- **Ordered lists:** `1.` form. The `1)` form is rewritten. The actual numbers used are renumbered to be sequentially correct (`1.`, `2.`, `3.`, ...) — this matches author intent more often than preserving accidental gaps and matches how every renderer numbers them anyway.
- **Consistency within a list:** enforced. A list whose siblings mix markers in the source becomes uniform in the output. Nested lists also use `-` (no alternation by depth).

### 4.2 Emphasis and strong

- **Italic:** `*text*`. The `_text_` form is rewritten.
- **Bold:** `**text**`. The `__text__` form is rewritten.
- **Bold-italic:** `***text***`.
- **Strikethrough:** `~~text~~` (GFM standard).

The `*` family wins because: (a) it does not interact with intra-word underscores in identifiers, file paths, and URLs; (b) it is the more common form in popular style guides; (c) consistency with the strong delimiter avoids mental mode-switching.

### 4.3 Headings

- **All levels:** ATX (`# H1`, `## H2`, ... `###### H6`).
- **Setext underline form** (`H1\n===` and `H2\n---`) is rewritten to ATX.
- **Closing hashes** (`# H1 #`) are stripped.
- **Exactly one space** between the leading `#` run and the heading text.
- **No trailing whitespace.**

ATX-only is universally readable, easier to scan in source, and trivially uniform across all six levels (setext only handles two).

### 4.4 Fence info-strings

- **Language name:** lowercase (`go`, not `Go`; `bash`, not `Bash`). The language registry in the engine is the source of truth for what's a recognized language; unrecognized strings are lowercased but otherwise left intact.
- **Single space** between the closing backticks of the fence opener and the info-string.
- **Key=value attributes** (e.g., ` ```go {linenos=true}`): canonical-cased keys (lowercased), canonical-cased values where the engine recognizes the key, otherwise verbatim.
- **Fence character:** triple backtick (` ``` `). Tilde fences (`~~~`) are rewritten unless the body contains a triple-backtick run, in which case the formatter preserves the tilde fence (to avoid escaping the body).
- **Fence length:** minimum needed to contain the body; defaults to 3.

### 4.5 Reference link definitions

- **Collected at end of document.** All `[label]: url "title"` definitions move to a single block at the end, after any trailing prose, before the footnote definitions block (§4.14).
- **Sorted by label** (case-insensitive ASCII sort). Stable for ties on case.
- **Blank line above the block.**
- **One definition per line.** Title, if present, on the same line as URL.
- **Label form:** `[label]:` with a single space before the URL.

This placement makes link targets easy to audit, easy to update when an external URL changes, and means inline reference-link usages stay clean.

### 4.6 Table column alignment

**Decision: not aligned.** Cells emit with a single space of padding on each side (` cell `); pipes placed without column-padding. Header separator uses minimum dashes required (`---`, `:---`, `---:`, `:---:`).

Rejected: pipe-aligned tables (cells padded so column pipes line up vertically). Aligned tables look better in source but generate noisy diffs every time the longest cell's width changes, and make incremental edits jumpy. Diff-cost wins. Visual width is the renderer's job.

Header alignment markers are canonicalized to the four forms above; non-standard variants (extra dashes, missing colons) normalize to the closest canonical form.

### 4.7 Line wrap

**Decision: no wrap.** Each paragraph is one logical line in the source.

Rejected: wrap at 80 / 100 / 120 columns. Wrapping has been a generation-long source of pain in Markdown formatters — every text edit can re-wrap a whole paragraph, and projects fight wars over the wrap column. Soft-wrap is the editor's job. Line breaks in source are semantically meaningful (`\n` is a soft break, `  \n` is a hard break); one-line-per-paragraph means diffs show only real changes.

This **will surprise** users who hand-wrap at 80 columns. Documented in `SPEC.md` and `mdpp fmt --help`: *one paragraph, one line.* Editor soft-wrap handles the visual.

### 4.8 Blank lines

- **Exactly one blank line between top-level blocks.** Multiple consecutive blank lines collapse to one.
- **No leading blank lines** at the top of the document (after frontmatter).
- **Within block constructs** (e.g., list items), blank-line policy follows the construct: a list with any blank-line-separated item becomes "loose" (all items separated by blank lines); a list with none stays "tight."

### 4.9 Indentation

- **Lists:** **two spaces per nesting level**, not four.
- Continuation lines inside a list item are indented to match the content column of the item (i.e., past the marker and the space after it — typically 2 columns for `- ` items, 3 columns for `1. ` items at single-digit depth).

Two-space indent chosen because: (a) matches Prettier/markdownlint default; (b) keeps deeply nested lists from running off-screen; (c) parser accepts both, so this is pure presentation. Four-space is the older CommonMark recommendation but visually heavy and inconsistent with the 2026 ecosystem.

### 4.10 Frontmatter

- **Delimiters** (`---`) on their own lines.
- **Exactly one blank line** between the closing `---` and the first body block.
- **YAML body** uses two-space indent (canonicalized via the round-trip YAML encoder if it preserves key order; otherwise body is left verbatim — see §2.3).
- **Key order preserved.**
- **No reordering, retyping, or re-quoting** of values.

### 4.11 Math

- **Inline math:** `$inline$`. No spaces immediately inside the dollar signs (`$x$`, never `$ x $`).
- **Display math:** `$$display$$` on its own line, with a blank line above and below it. Multi-line display math keeps internal whitespace as written.
- **Escaped dollar in prose:** `\$` left as written.
- **Dollar inside math content:** see §5.9.

### 4.12 Admonitions

- **Title on the same line as the type marker:** `> [!NOTE] Optional title`.
- **No blank `>` line** between the title line and the body.
- **Body lines** prefixed with `> ` (one space after the `>`), matching the surrounding blockquote convention.
- **Type names** uppercased canonical (`NOTE`, `TIP`, `WARNING`, `CAUTION`, `IMPORTANT`).

### 4.13 `:::` containers

- **Opening fence** (`:::name` or `:::name {.attrs}`) on its own line.
- **Closing fence** (`:::`) on its own line.
- **Body un-indented** — the container does not introduce indentation; nested constructs inside the container format with their own normal indentation rules.
- **Nested containers** open and close on their own lines, with the inner content un-indented relative to the outer container's content.
- **Attribute syntax** is canonicalized once A's sub-spec finalizes it (cross-reference). For now: whitespace inside the `{...}` is normalized to single spaces; attribute order preserved.

### 4.13a `[[name]]` directives (`[[toc]]`, `[[embed:url]]`)

Single-line, inline-positioned directive form (distinct from the multi-line `:::name` containers in §4.13). Two members today; the form is open-ended for future single-line directives.

- **Lowercase directive name.** `[[toc]]`, not `[[TOC]]` or `[[Toc]]`. For `[[embed:url]]`, the scheme `embed` is lowercased; the URL payload after the `:` is preserved verbatim (URL bytes are format-stable in the same sense as link targets, §3).
- **No whitespace inside the brackets.** `[[ toc ]]` rewrites to `[[toc]]`; `[[embed: https://... ]]` rewrites to `[[embed:https://...]]`.
- **On a line by itself.** The directive is the only non-whitespace content on its source line in the canonical form.
- **Blank line above and below.** Surrounding whitespace is canonicalized exactly like a top-level block per §4.8.
- **Nesting note.** A directive that the parser recovers inside a list item or blockquote (per §6.15 below) is left in place; only its on-line form is canonicalized.

### 4.14 Footnote definitions

- **Collected at end of document**, after the reference link definitions block (§4.5).
- **Sorted by ID** (case-insensitive ASCII sort).
- **Blank line above the block.**
- **Multi-line footnote bodies** keep their original line breaks; continuation lines are indented four spaces (per CommonMark footnote convention).

### 4.15 Trailing whitespace

Stripped on every line. No exceptions — including the hard-break form (`text  \n`), which is rewritten to the explicit `\` form (`text\\\n`) to avoid invisible-trailing-space diffs.

### 4.16 Final newline

Exactly one `\n` at end-of-file. No trailing blank lines, no missing final newline.

### 4.17 Emoji shortcodes, super/subscript, definition lists, task lists

- **Emoji:** `:name:` form preserved as written; case-sensitive lookup (the source-of-truth table is the engine's emoji registry).
- **Superscript/subscript:** `^x^` / `~x~` preserved verbatim. Whitespace inside the markers is preserved (this is a parser-level signal, not formatter business).
- **Definition lists:** term on one line, description on the next (prefixed with `: `). Multiple descriptions for one term are stacked, each on its own `: `-prefixed line, separated from the next term by a blank line.
- **Task lists:** `- [ ]` (unchecked) and `- [x]` (checked, lowercase x). The unicode-tick `[✓]` and uppercase `[X]` forms are rewritten.

---

## 5. Algorithm

### 5.1 The choice: AST re-emit vs source-guided rewrite

Two algorithm paths, one preferred and one fallback. Which path B actually ships against depends on whether A delivers `Node.Range` before B starts (see §0 and §12.2).

**Preferred path — source-guided rewrite (requires `Node.Range`).** Walk the source byte stream with the AST as a guide. For format-stable ranges (per §3), copy `Document.Source[start:end]` verbatim. For canonicalized ranges, emit bytes built from AST fields. Inter-block whitespace is canonicalized (§4.8) without consulting the source. Preserves byte-exactness trivially and is the only path that satisfies invariant §2.4 (no data loss) against code blocks, math interiors, HTML blocks, and frontmatter values.

This requires the AST to expose source byte ranges on every node. The engine contract: `Node.Range() (start, end int)` returns the byte range in `Document.Source` from which the node was parsed. For synthetic nodes (e.g., a fabricated frontmatter wrapper), `Range()` may return `(0, 0)` and the formatter falls back to per-node re-emit for that node. The engine-side work for this is in A's sub-spec — committed, not yet shipped as of 2026-04-19.

**Fallback path — pure AST re-emit (works without `Node.Range`).** Walk the AST; emit canonical bytes from scratch using only node fields. Clean and matches `render.go`'s `strings.Builder` shape. Ships today against the current AST. **Known limitation (must be documented as such if this path is taken):** any source byte not faithfully represented in the AST is lost — most consequentially, whitespace and blank-line positioning *inside* fenced code blocks (where `Literal` is typically the recovered text without a guarantee of every original trailing space), internal whitespace in math display blocks, exact byte content of HTML blocks, and comment placement inside frontmatter. This weakens invariant §2.4 by a stated amount. Use only if A slips on `Node.Range` and shipping B on schedule overrides the loss.

**Recommendation: wait for `Node.Range` and take the preferred path.** The fallback is a contingency, not a plan.

### 5.2 Walker structure

The formatter mirrors `render.go`'s shape: a single `strings.Builder`, a recursive `formatNodeInto(b *strings.Builder, n *Node)`, dispatched on `n.Type`. Per node type:

- **Format-stable**: `b.Write(d.Source[start:end])`.
- **Format-canonicalized**: emit a canonical prefix, recurse for children, emit a canonical suffix. Inter-child whitespace is governed by the parent's rules, not the source.

A small `formatContext` value (current indent prefix, current list-depth marker, are-we-inside-a-tight-list, etc.) threads through the walk.

### 5.3 Reference link and footnote collection

Reference link definitions and footnote definitions are not emitted in their source positions. The walker maintains two collected slices, populated on first encounter, and emits them in canonical sorted order at the end of the document body (§4.5, §4.14). The original source bytes for the definitions are skipped during the walk (the walker knows their byte ranges and elides them, replacing each with nothing).

### 5.4 Idempotency mechanics

Idempotency is achieved by canonical-form-everywhere, not by a fixed-point loop. Every code path that emits bytes emits the same bytes a second time given the same AST. The property test in §10 catches any path that doesn't.

One subtle case: formatting rewrites that change line count (e.g., setext → ATX) shift absolute byte positions of downstream nodes. This does not affect AST equality (same nodes, same order, same content) and the formatter's logic depends only on the relative shape of the AST.

### 5.5 Performance

`O(n)` in source size: one AST walk, one builder write per node, builder presized to source length. No second pass, no quadratic work. Target: 100k-character document in under 10ms. Not a stated v0.1 deliverable (perf budget lives with the linter, roadmap §4.3), but falls out of the architecture.

---

## 6. Edge cases

### 6.1 HTML blocks and inline HTML

Format-stable; copied byte-for-byte. Formatter does not touch tag casing, attribute quotes, or internal whitespace. Surrounding blank lines normalized per §4.8.

### 6.2 Mid-document YAML / TOML

Only the leading frontmatter block is treated as frontmatter. YAML/TOML later in the document is content (typically inside a fenced code block) and is therefore format-stable code-block content.

### 6.3 Hand-wrapped paragraphs

Joined to one line per §4.7. **Will surprise users** who hand-wrap at 80 columns. Expectation documented in `SPEC.md`, `--help`, and launch GIFs.

### 6.4 Tables with mixed alignment markers

Header separator rewritten per column to one of the four canonical forms. Extra dashes, internal whitespace, or non-standard alignment markers normalize to the closest canonical form. Ambiguous alignment (e.g., `:--`) resolves as left-aligned.

### 6.5 Empty cells in tables

Emitted as ` ` (single space between pipes). Cell count preserved from the AST.

### 6.6 Reference links with missing definitions

The inline form `[text][missing]` preserved as-is. Formatter does not invent definitions, strip brackets, or warn — that's the linter's job (`MDPP010` family).

### 6.7 Nested lists with mixed markers

Canonicalized to the chosen marker (§4.1) at every level. Loose-vs-tight property preserved per-list.

### 6.8 Code blocks with no language tag

Left untagged; no inference. Indentation-based code blocks (4-space) rewritten as fenced blocks with no info-string (fenced is canonical; indented form is confusable with list continuation).

### 6.9 Math with `$` inside

Inline: literal `$` must be escaped as `\$` per `SPEC.md`; formatter preserves the escape. Display: if body contains `$$` (rare parse error), formatter preserves source verbatim and lets the linter flag it.

### 6.10 Empty and whitespace-only documents

Empty input → empty output. Whitespace-only input → empty output (one trailing newline). Frontmatter-only document → frontmatter block plus trailing newline.

### 6.11 BOM and unusual line endings

Leading UTF-8 BOM stripped. CRLF and CR-only line endings normalized to LF. Output always LF-terminated.

### 6.12 Trailing whitespace in code blocks

Trailing whitespace inside a fenced code block is preserved (body is format-stable). Trailing whitespace on fence lines themselves is stripped.

### 6.13 Diagram fences (mermaid, dot, etc.)

Treated like code blocks: info-string lowercased, body byte-exact.

### 6.14 Frontmatter with comments or anchors

If the YAML library preserves them, they survive. If not, the formatter detects their presence and falls back to byte-verbatim preservation of the whole frontmatter block (silent; no warning).

### 6.15 `[[toc]]` and `[[embed:url]]` directives nested inside lists or blockquotes

The directive is left in place at its parsed position. The formatter canonicalizes the on-line form (lowercase, no inside-bracket whitespace) but does **not** lift the directive out of its container. The recommendation noted to downstream renderers: an embedded `[[toc]]` may be treated as document-scoped regardless of the surrounding list/blockquote context — semantics are the renderer's call, not the formatter's. The formatter's job ends at the canonical bytes.

---

## 7. Public Go API

```go
// Package fmt provides canonical formatting for Markdown++ source.
package fmt

// Format reformats src into canonical Markdown++ form.
// Idempotent: Format(Format(src)) == Format(src).
// Semantically stable: Parse(Format(src)) is AST-equal to Parse(src).
func Format(src []byte) ([]byte, error)
```

That is the entire v0.1 API surface. One function, two invariants.

**On `FormatRange`.** A range variant — `FormatRange(src []byte, start, end int) ([]byte, error)` — is **not** in v0.1 because: (a) partial formatting inside a list or table interacts badly with idempotency; (b) LSP `textDocument/rangeFormatting` is not in the MVP cut-line (roadmap §4.4) — modern LSP clients fall back to whole-document formatting; (c) keeping the API small makes the package easier to reason about. If post-launch demand is real, the addition is non-breaking: snap `start`/`end` outward to block boundaries, format as a sub-document, splice back.

**Errors.** `Format` returns an error only if `Parse` errors — source so malformed no usable AST is produced. A successfully-parsed document always formats; recoverable parse diagnostics do not block formatting.

---

## 8. CLI behavior

`mdpp fmt` is a subcommand of the `mdpp` CLI (per roadmap §4.1, the `fmt` subcommand is wired up by A and forwards to this package).

### 8.1 Flags and modes

| Flag | Effect |
|---|---|
| (none) | Read from stdin, write canonical form to stdout. |
| `--write` (`-w`) | In-place reformat each path argument. Overwrites the file only if the formatted bytes differ. Mutually exclusive with `--diff` and `--check`. |
| `--diff` (`-d`) | Print a unified diff (input vs formatted) to stdout for each path. Exit nonzero if any file would change. |
| `--check` (`-c`) | Print nothing on stdout. Exit nonzero if any file would change. Useful for CI. |
| `--stdin-filepath <path>` | When reading from stdin, treat the input as if it came from `<path>` (used for diagnostics; format behavior does not depend on path). |

### 8.2 Path arguments

Zero or more file paths. With zero paths, reads stdin and writes stdout. With one or more paths, each is formatted independently. Directories are not recursed in v0.1 — authors use shell globs (`mdpp fmt --write **/*.md`) or `find ... -exec`. Recursive traversal can be added later if friction is real.

### 8.3 Exit codes

| Code | Meaning |
|---|---|
| 0 | Everything succeeded; in `--check` and `--diff` modes, no file would change. |
| 1 | At least one file would change (in `--check`/`--diff`) or write failed (in `--write`). |
| 2 | A file failed to parse. |
| 3 | Invalid CLI usage (mutually exclusive flags, missing argument). |

### 8.4 Output conventions

- `mdpp fmt` (default mode): formatted Markdown++ on stdout, nothing on stderr unless an error occurs.
- `mdpp fmt --write`: nothing on stdout for files that did not change; for changed files, prints the path on stdout (one per line). Errors on stderr.
- `mdpp fmt --diff`: unified diff on stdout, with `--- <path>` / `+++ <path>` headers; exit 1 if any diff is non-empty.
- `mdpp fmt --check`: nothing on stdout; exit 1 if any file would change. Errors on stderr.

This mirrors `gofmt`'s conventions, which `mdpp fmt` users will already know.

---

## 9. LSP integration

### 9.1 `textDocument/formatting`

The LSP (sub-project D) handles `textDocument/formatting` by calling `fmt.Format(doc.Source)`, computing a single `TextEdit` that replaces the entire document with the formatted bytes. No diff-minimization, no per-line edits — clients render the result identically and the simplicity wins.

The LSP only invokes formatting if the document parses (recoverably or fully). If parse fails entirely, `formatting` returns an empty edit list and a log message; the linter's diagnostics already surface the parse failure.

### 9.2 `textDocument/rangeFormatting`

**Omitted from v0.1**, per §7. The LSP advertises `documentFormattingProvider: true` but not `documentRangeFormattingProvider`. Clients that ask for range formatting fall back gracefully.

Adding it post-launch is a `FormatRange` Go API addition plus a new LSP method handler — no breaking change to the v0.1 protocol surface.

### 9.3 `textDocument/onTypeFormatting`

Not implemented. Markdown++ has no constructs that benefit meaningfully from on-type formatting (closing brackets are not auto-balanced; lists are not auto-continued by the formatter — the editor handles list continuation via its own snippet logic).

### 9.4 Format-on-save

Driven entirely by the editor's own setting (`editor.formatOnSave` in VS Code, equivalent in other editors). The LSP exposes formatting; the editor decides when to invoke it. No `mdpp.fmt.onSave` config is needed.

---

## 10. Testing strategy

### 10.1 Property tests (the load-bearing ones)

Two property tests run on every push against: the conformance corpus (≥30 cases per roadmap §3.6), `examples/showcase/`, and a small generated corpus (random combinations of construct fragments from a fixture pool).

- **`TestFormatIdempotent`**: assert `Format(Format(x)) == Format(x)` (byte-equal).
- **`TestFormatSemanticallyStable`**: assert `astEqualModuloWhitespace(Parse(x), Parse(Format(x)))` per §2.2.

Both fail loudly with the offending input pinned and differing bytes/nodes printed in context.

### 10.2 Golden tests for style decisions

For each style decision in §4, a paired `input.md` / `expected.md` fixture in `fmt/testdata/golden/`. One directory per construct (`golden/list-markers/`, `golden/emphasis/`, ...). Fixtures intentionally minimal — one decision per fixture — so failures point precisely at the rule that broke.

### 10.3 Round-trip tests for the engine

The existing engine test suite is re-run with each input piped through `Format` first, asserting HTML output is unchanged. Catches cases where formatting accidentally changes parser behavior in a way the AST-equality test misses.

### 10.4 LSP integration tests

Sub-project D's LSP harness (roadmap §4.4) includes a `textDocument/formatting` test: open a document, request formatting, assert the returned `TextEdit` produces canonical output.

### 10.5 Fuzz testing

`go test -fuzz=FuzzFormat` runs `Format` on random byte sequences, asserting it never panics and satisfies idempotency on any input it accepts. Backstop, not primary guarantee.

---

## 11. Done for v0.1

(Restates roadmap §4.2's "Done" list with formatter-specific detail.)

- `fmt.Format` implemented per the algorithm in §5.
- All canonical-form decisions in §4 implemented and golden-tested.
- Idempotency and semantic-stability property tests passing on the conformance corpus and `examples/`.
- Round-trip test passing: existing engine tests produce identical HTML before and after formatting their inputs.
- `mdpp fmt` CLI subcommand wired up with the flag set in §8.
- LSP `textDocument/formatting` handler implemented (in D).
- Godoc complete on the `fmt` package; `Format` documents the two invariants.
- Performance falls out of the architecture; no explicit perf-test requirement in v0.1 (linter owns the perf budget).

---

## 12. Open questions

The decisions in this spec are made; these are downstream details that warrant a second pass during implementation.

1. **YAML library choice.** Pick `gopkg.in/yaml.v3` (good key-order, partial comment support) vs `goccy/go-yaml` after exercising both on representative frontmatter. Fallback: byte-verbatim preservation (§2.3).

2. **Source byte ranges on AST nodes — STILL UNMET, BLOCKING.** Required by the formatter (§5.1); the engine's existing AST does not expose them on any node type as of 2026-04-19 (`ast.go`'s `Node` struct has `Type`, `Children`, `Literal`, `Attrs` only). A's sub-spec committed to this; the work has not landed. **B cannot start implementation until A delivers `Node.Range` (start/end byte offsets into `Document.Source`) on every node type.** The roadmap §0 progress snapshot now also flags this gap as blocking for B, C, and D. See §0 of this sub-spec for the fallback trade-off (AST-walk re-emit) if A slips and waiting is not an option — strongly preferred: wait.

3. **`:::` container attribute syntax canonicalization.** Deferred to A's sub-spec resolution. Formatter normalizes whatever A picks; the placeholder rule is: single-space inside `{...}`, attribute order preserved. Roadmap §3.5 has been updated to acknowledge that both `[[name]]` (single-line, inline-positioned) and `:::name` (multi-line, block container) directive forms coexist in the v0.1 surface; this sub-spec canonicalizes both (see §4.13 and §4.13a).

4. **Hard break rewrite policy.** §4.15 rewrites `text  \n` to `text\\\n`. Verify the backslash form round-trips through every construct that supports hard breaks (paragraph, table cell, definition list description, admonition body). Adjust if any construct rejects it.

5. **Tilde-fence preservation.** §4.4 keeps `~~~` only when the body contains a backtick run. If the body contains both: prefer backticks and pad fence length; refine if a real input breaks the heuristic.

6. **Future config surface.** `--profile=strict/loose`, project-level `.mdpprc`. Not for v0.1. Revisit only if post-launch demand is real.

7. **Range formatting.** Omitted from v0.1 per §7 and §9.2. Add only after editor users ask; design sketch in §7.

---

## 13. References

- Parent roadmap: `2026-04-19-markdown-plus-plus-roadmap-design.md`
- CommonMark Spec: https://spec.commonmark.org/
- GitHub Flavored Markdown Spec: https://github.github.com/gfm/
- gofmt design notes (informal precedent for canonical-style invariants)
- Prettier Markdown formatter (precedent for two-space list indent and `-` marker default)

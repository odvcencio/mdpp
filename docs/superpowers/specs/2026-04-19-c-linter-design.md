# Sub-project C: Linter (`mdpp lint`) — Design Spec

**Status.** Draft
**Date.** 2026-04-19
**Owner.** Oscar Villavicencio (m31labs)
**Parent.** `docs/superpowers/specs/2026-04-19-markdown-plus-plus-roadmap-design.md` §4.3
**Scope.** Defines the linter's data model, v0.1 rule catalog, rule-code namespace, suppression syntax, Go API, CLI surface, performance budget, LSP integration pattern, and testing strategy. Does not implement rules; does not pick every message string; does not freeze a config-file format.

---

## 0. Progress snapshot (as of 2026-04-19)

C has not started; this sub-spec remains the design target. Engine progress shipped today affects the C surface as follows.

- **Two new directive node types to lint.** `NodeTableOfContents` (from `[[toc]]`) and `NodeAutoEmbed` (from `[[embed:url]]`, with provider detection in `extensions.go`) are now in the AST. New rules MDPP108–MDPP111 below cover them; total v0.1 rule count rises from 17 to 21.
- **`NodeContainer` (for `:::`) still not in engine.** Rule MDPP104 (undefined `:::` container type) remains future-pending until A delivers the node. The rule definition stays in §4.1 as the design target so it ships as soon as the AST node arrives.
- **`Node.Range` field still missing.** Hard precondition for any meaningful diagnostic output: every `Diagnostic` requires a `Range`, and §2.2 requires it to be non-empty. C cannot start implementation until A delivers byte/line ranges on AST nodes.
- **Heading-id slug algorithm now lives in the engine.** The `[[toc]]` integration in `corpus_test.go` confirms the engine `Heading` collection drives TOC contents — meaning the slug function is centralized in A and rules MDPP102 / MDPP103 can call it directly. Resolves §12 flag #2.

---

## 1. Purpose

The linter is the document-quality surface. It catches the class of problems the formatter cannot fix mechanically — problems that depend on meaning rather than shape: a footnote reference with no definition, a link to a heading anchor that does not exist, an image without alt text, a heading level that skips from `h1` to `h3`. It is also the LSP's diagnostic source, so every rule ships with the metadata the editor needs to render the diagnostic usefully (stable code, severity, range, optional fix).

The formatter (`mdpp fmt`) normalizes *presentation*. The linter (`mdpp lint`) reports *issues*, some of which are style issues the formatter would also fix. The split is deliberate: the formatter always rewrites without asking; the linter surfaces problems the author may want to see before anything changes. Where the two overlap — trailing whitespace, list-marker consistency — the linter reports the issue and defers the auto-fix to the formatter so both tools agree on the canonical form.

---

## 2. Diagnostic data model

### 2.1 Core types

```go
// Severity classifies a diagnostic.
type Severity int

const (
    SeverityError Severity = iota
    SeverityWarning
    SeverityInfo
    SeverityHint
)

// Diagnostic is a single lint finding.
type Diagnostic struct {
    Range    Range          // source location (byte + line/col)
    Severity Severity
    Code     string         // stable, e.g. "MDPP100"
    Message  string         // human-readable, one line
    Fix      *TextEdit      // optional auto-fix; nil if none
    Related  []RelatedInfo  // optional "see also" pointers
}
```

### 2.2 `Range`

`Range` is the same type used across the engine (parser, formatter, LSP). It carries both byte offsets — the natural unit for `[]byte` slicing inside Go — and line/column pairs, which the LSP needs for `publishDiagnostics`. The line/col pair is UTF-16 code-unit based per LSP 3.17 semantics; converting from byte offsets happens at the LSP boundary, not inside the linter.

```go
type Position struct {
    Byte   int // byte offset into Document.Source
    Line   int // zero-based line number
    Column int // zero-based column (in UTF-8 bytes at this layer)
}

type Range struct {
    Start Position
    End   Position
}
```

Every diagnostic MUST carry a non-empty `Range`. Rules that genuinely apply to the whole document (e.g., frontmatter version mismatch) point at the first byte of the relevant construct (the frontmatter block, the first heading) rather than `{0,0}–{0,0}`.

### 2.3 `TextEdit`

`TextEdit` is a single replacement. Most auto-fixes are a single edit; a few (e.g., heading-level skip) may prefer to skip the auto-fix entirely rather than surface a multi-edit fix that requires the author to review.

```go
type TextEdit struct {
    Range   Range  // region to replace
    NewText string // replacement contents
}
```

Rules whose fix is semantically non-trivial (rename a heading ID that is referenced elsewhere; convert a reference link to inline) set `Fix = nil` in v0.1. Such fixes surface via LSP `codeAction` handlers written directly against the AST — not through the linter's `Fix` field — because they need cross-document edit coordination the linter is not responsible for.

### 2.4 `RelatedInfo`

`RelatedInfo` lets a diagnostic point at a second location. Used by rules where the fix context lives elsewhere in the document: MDPP103 (duplicate heading ID) points the secondary info at the other heading that collides; MDPP100 (undefined footnote) points at the nearest correctly-defined footnote as a "did you mean?" hint when edit distance is small.

```go
type RelatedInfo struct {
    Range   Range
    Message string
}
```

Related info is always advisory. Consumers that do not support the concept (plain-text CLI output) MAY omit it. LSP clients render it as a linked secondary location.

---

## 3. Rule code namespace

### 3.1 Namespace layout

| Range          | Purpose |
|----------------|---------|
| `MD001`–`MD099` | **Compatibility codes.** Match markdownlint where the rule exists in both tools. Same number, same intent, same default severity. |
| `MDPP100`–`MDPP199` | Markdown++-specific **semantic** rules (footnotes, links, containers, frontmatter, `[[toc]]` and `[[embed:...]]` directives — constructs markdownlint does not know about). |
| `MDPP200`–`MDPP299` | **Accessibility** rules. |
| `MDPP300`–`MDPP399` | **Style** rules not covered by an established markdownlint equivalent. |
| `MDPP400+` | Reserved for future categories (content, localization, academic-writing). |

### 3.2 Why coexist with markdownlint codes

markdownlint is the de-facto standard Markdown linter in the JavaScript ecosystem, and many authors have muscle memory for its rule codes — `MD045` for missing image alt text, `MD001` for heading-increment. Reusing those codes for the same-intent rules means a user who already knows "MD045 means add alt text" is not surprised when `mdpp lint` says the same thing. Where a rule is genuinely Markdown++-specific (footnotes, containers, admonitions, `mdpp:` frontmatter), we use the `MDPP` prefix to signal "this is ours, don't expect markdownlint to have it."

Rule codes are stable across `0.x`. A rule may be deprecated (moved to `disabled-by-default`), but its code is not reused for a different rule.

---

## 4. The v0.1 rule set

Twenty-one rules ship in v0.1, organized by category. (Original seventeen plus four directive-aware rules MDPP108–MDPP111 added when the engine landed `[[toc]]` and `[[embed:url]]`.)

### 4.1 Semantic (MDPP100+)

#### MDPP100 — Undefined footnote reference

- **Severity default.** Error.
- **Trigger.** A `[^id]` footnote reference appears in the document body, and no `[^id]:` definition exists anywhere in the document.
- **Auto-fix.** No. The correct fix depends on intent (add a definition, fix a typo, remove the reference).
- **Rationale.** An undefined footnote is a broken cross-reference. HTML output rendering of this case would produce a dangling superscript link; authors almost always mean this as an error, not a style issue.
- **Example violation.**

  ```markdown
  The paper argues for the claim[^fermi] in detail.
  ```

- **Example correction.**

  ```markdown
  The paper argues for the claim[^fermi] in detail.

  [^fermi]: See Fermi (1954), p. 42.
  ```

#### MDPP101 — Footnote definition with no reference

- **Severity default.** Warning.
- **Trigger.** A `[^id]: ...` footnote definition exists, and no `[^id]` reference to it appears anywhere in the document body.
- **Auto-fix.** No. Either the definition is stale (delete it) or the reference was accidentally removed (re-add it).
- **Rationale.** Orphan definitions usually indicate a reference the author meant to make and forgot, or a deletion that left the definition behind. Warning (not error) because the definition still produces no broken output — it is merely unused.
- **Example violation.**

  ```markdown
  Body with no references to the footnote.

  [^unused]: This definition is never pointed at.
  ```

- **Example correction.** Either delete the definition or add a `[^unused]` somewhere in the body.

#### MDPP102 — Broken intra-doc link

- **Severity default.** Error.
- **Trigger.** An inline link `[text](#anchor)` points at a fragment (`#anchor`) and no heading in the document produces that auto-generated id. The heading-id generation algorithm is the same one used by the renderer.
- **Auto-fix.** No. The correct fix depends on intent — either the anchor is a typo or the target heading has moved.
- **Rationale.** Broken anchors produce links that silently fail at render time. These are among the most common review-catch issues in real documents.
- **Example violation.**

  ```markdown
  See [the introduction](#introduciton) for context.

  # Introduction
  ```

- **Example correction.**

  ```markdown
  See [the introduction](#introduction) for context.

  # Introduction
  ```

#### MDPP103 — Duplicate heading ID

- **Severity default.** Warning.
- **Trigger.** Two or more headings produce the same auto-generated id after slugification. The renderer disambiguates by appending `-2`, `-3`, etc., but the unambiguous fix is for the author to disambiguate.
- **Auto-fix.** No (the fix would rename a heading the author wrote deliberately; always human-directed).
- **Rationale.** Duplicate ids break intra-doc links and table-of-contents entries silently. `RelatedInfo` points at the colliding heading.
- **Example violation.**

  ```markdown
  ## Results
  Some text.

  ## Results
  More text.
  ```

- **Example correction.**

  ```markdown
  ## Initial results
  Some text.

  ## Final results
  More text.
  ```

#### MDPP104 — Undefined `:::` container type

- **Severity default.** Warning.
- **Trigger.** A `:::name` container uses a `name` that is not in the allowed-type set. v0.1 ships a hardcoded allowed set: `note`, `tip`, `warning`, `caution`, `important`, `info`, `details`, `aside`, `columns`, `column`. Unknown names fire the rule.
- **Auto-fix.** No in v0.1. Future versions may offer a "did you mean?" fix when edit distance to an allowed name is small.
- **Rationale.** Typos in container names produce silently-unstyled blocks in the HTML. Warning (not error) because the renderer still emits readable output.
- **Example violation.**

  ```markdown
  :::waring
  Be careful.
  :::
  ```

- **Example correction.**

  ```markdown
  :::warning
  Be careful.
  :::
  ```

#### MDPP105 — Unused link reference definition

- **Severity default.** Warning.
- **Trigger.** A `[label]: href` link reference definition exists, and no `[text][label]` or shortcut `[label]` reference uses it anywhere in the document.
- **Auto-fix.** Yes — delete the unused definition line (including trailing newline). Safe because removing an unused definition cannot change rendered output.
- **Rationale.** Unused references are leftover cruft from edits. Removing them keeps the reference-definition block honest.
- **Example violation.**

  ```markdown
  See the paper for more.

  [stale]: https://example.com/old-paper
  ```

- **Example correction.**

  ```markdown
  See the paper for more.
  ```

#### MDPP106 — Reference link to undefined ref

- **Severity default.** Error.
- **Trigger.** A `[text][label]` or shortcut `[label]` reference appears with no corresponding `[label]: ...` definition in the document.
- **Auto-fix.** No. Same reasoning as MDPP100 — the fix depends on intent.
- **Rationale.** Same class as MDPP100 but for reference links instead of footnotes. The rendered output is a literal string with brackets, almost never what the author wanted.
- **Example violation.**

  ```markdown
  For details, see [the paper][primary].
  ```

- **Example correction.**

  ```markdown
  For details, see [the paper][primary].

  [primary]: https://example.com/paper
  ```

#### MDPP107 — Frontmatter `mdpp:` version mismatched with parser

- **Severity default.** Info.
- **Trigger.** The frontmatter contains `mdpp: X.Y` where `X.Y` is newer than the parser's known format version. The parser is forward-compatible (unknown constructs degrade gracefully), but the author should know they are using a version of the tool that predates the version they declared.
- **Auto-fix.** No.
- **Rationale.** Version skew is almost always a non-issue in practice — but surfacing it as info means an author who upgrades the parser and sees the info go away has positive confirmation their environment is current.
- **Example violation.**

  ```markdown
  ---
  mdpp: 0.9
  ---
  Body.
  ```

  ...when the running parser is v0.1.

- **Example correction.** Either upgrade the parser or change the declared version to match the parser.

#### MDPP108 — Multiple `[[toc]]` directives in one document

- **Severity default.** Warning.
- **Trigger.** More than one `NodeTableOfContents` is present in the document.
- **Auto-fix.** Yes — remove all but the first `[[toc]]` directive (and its surrounding blank line, if the result would otherwise leave a double blank).
- **Rationale.** A document has exactly one outline. Two TOCs render two identical lists; readers see the redundancy as a defect, and the second TOC adds no information the first did not. Auto-fix is safe because the trailing TOCs by definition produce the same content as the first.
- **Example violation.**

  ```markdown
  [[toc]]

  # Introduction
  ...

  [[toc]]
  ```

- **Example correction.**

  ```markdown
  [[toc]]

  # Introduction
  ...
  ```

#### MDPP109 — `[[toc]]` directive with no headings to populate

- **Severity default.** Info.
- **Trigger.** A `NodeTableOfContents` is present but the document contains no headings (or no headings at the levels the TOC includes).
- **Auto-fix.** No. The directive may be intentional in a document still being assembled; removing it would surprise the author.
- **Rationale.** The rendered TOC will be empty — visually a stub or a missing element depending on the renderer. Info-level so a work-in-progress draft is not noisy, but the author gets a heads-up.
- **Example violation.**

  ```markdown
  [[toc]]

  Just a paragraph, no headings yet.
  ```

- **Example correction.** Either add at least one heading or remove the `[[toc]]` until headings exist.

#### MDPP110 — Auto-embed with unrecognized provider

- **Severity default.** Info.
- **Trigger.** A `NodeAutoEmbed` whose `data-provider` attribute is `"generic"` — i.e., the URL host did not match any known embed provider in `extensions.go`. The renderer falls back to a plain `<a>` link rather than a rich embed.
- **Auto-fix.** No. The author may genuinely want the fallback link, or may want to switch to an inline link explicitly.
- **Rationale.** Authors writing `[[embed:...]]` typically expect a rich embed (player, thumbnail, etc.). Surfacing the generic fallback as an Info diagnostic prevents surprise when the rendered output is "just a link."
- **Example violation.**

  ```markdown
  [[embed:https://my.private.host/video.mp4]]
  ```

- **Example correction.** Use an inline link explicitly, or switch the URL to a recognized provider.

  ```markdown
  See the [video](https://my.private.host/video.mp4).
  ```

#### MDPP111 — Auto-embed with malformed URL

- **Severity default.** Error.
- **Trigger.** A `NodeAutoEmbed` whose URL fails Go's `net/url.Parse` (no scheme, invalid host, etc.).
- **Auto-fix.** No.
- **Rationale.** A malformed URL produces broken output regardless of provider. This is always a defect, never a style choice.
- **Example violation.**

  ```markdown
  [[embed:not a url]]
  ```

- **Example correction.**

  ```markdown
  [[embed:https://www.youtube.com/watch?v=dQw4w9WgXcQ]]
  ```

### 4.2 Accessibility (MD045, MDPP200+)

#### MD045 — Missing image alt text

- **Severity default.** Warning.
- **Trigger.** An image `![](url)` has empty or whitespace-only alt text. Matches the markdownlint rule of the same code.
- **Auto-fix.** No. Alt text is content; it must be authored.
- **Rationale.** Missing alt text is the single most common accessibility failure in Markdown documents. Screen readers announce the filename as a fallback, which is almost always useless.
- **Example violation.**

  ```markdown
  ![](architecture-diagram.png)
  ```

- **Example correction.**

  ```markdown
  ![System architecture: three tiers with a message queue between them](architecture-diagram.png)
  ```

#### MDPP200 — Heading-level skip

- **Severity default.** Warning.
- **Trigger.** A heading of level `n` appears with no preceding heading of level `n-1` in the same section. The check is hierarchical: after an `h2`, both `h3` and `h2` are allowed, but an `h4` without an intervening `h3` fires the rule.
- **Auto-fix.** No. The correct fix depends on intent (was the skip deliberate? is a heading missing?).
- **Rationale.** Screen readers use heading hierarchy to build document outlines. Skipped levels break the outline and make the document harder to navigate for assistive-technology users.
- **Example violation.**

  ```markdown
  # Paper title

  ### Methods
  ```

- **Example correction.**

  ```markdown
  # Paper title

  ## Introduction

  ### Methods
  ```

#### MDPP201 — Bare URL autolink should use descriptive link text

- **Severity default.** Info.
- **Trigger.** An autolink `<https://example.com/path>` appears where the surrounding context strongly suggests a descriptive link would be clearer (heuristic: the URL's path segment count exceeds 1 and the autolink is the only content of its paragraph). Configurable off per-rule in future versions.
- **Auto-fix.** No.
- **Rationale.** Bare URLs read poorly to screen readers, which announce the full URL. Descriptive link text ("the HTTP/2 RFC") produces a better reading experience. Info-level because the rule is stylistic and context-sensitive.
- **Example violation.**

  ```markdown
  Read <https://datatracker.ietf.org/doc/html/rfc7540>.
  ```

- **Example correction.**

  ```markdown
  Read [the HTTP/2 RFC](https://datatracker.ietf.org/doc/html/rfc7540).
  ```

#### MDPP202 — Empty link text

- **Severity default.** Error.
- **Trigger.** An inline link `[](url)` has empty or whitespace-only link text.
- **Auto-fix.** No.
- **Rationale.** Empty link text renders as a clickable gap; screen readers cannot announce it. Always a defect.
- **Example violation.**

  ```markdown
  See the [](https://example.com/paper).
  ```

- **Example correction.**

  ```markdown
  See the [paper](https://example.com/paper).
  ```

#### MDPP203 — Table without header row

- **Severity default.** Warning.
- **Trigger.** A GFM table is constructed with no header row — i.e., the first row is not separated from the body by a delimiter row. The parser accepts tables with empty headers; this rule catches the degenerate case where the header row is missing entirely (which the parser promotes to a single-row paragraph-like construct).
- **Auto-fix.** No.
- **Rationale.** Screen readers use header rows to announce cell context ("Column: Year, Value: 2026"). Tables without headers are announced as unlabelled grids, which is hostile to non-visual readers.
- **Example violation.**

  ```markdown
  | 2024 | $1.2M |
  | 2025 | $1.4M |
  ```

- **Example correction.**

  ```markdown
  | Year | Revenue |
  |------|---------|
  | 2024 | $1.2M   |
  | 2025 | $1.4M   |
  ```

### 4.3 Style (MD001+, MDPP300+)

#### MD004 — Inconsistent unordered list marker

- **Severity default.** Info.
- **Trigger.** Within a single unordered list, different top-level items use different markers (`-`, `*`, `+`).
- **Auto-fix.** Yes — defers to the formatter, which canonicalizes to `-` per B's sub-spec.
- **Rationale.** Mixed markers within a list are almost always unintentional, and the rendered HTML is identical either way — which is exactly the kind of noise formatting-level consistency eliminates.
- **Example violation.**

  ```markdown
  - one
  * two
  + three
  ```

- **Example correction.**

  ```markdown
  - one
  - two
  - three
  ```

#### MD009 — Trailing whitespace

- **Severity default.** Info.
- **Trigger.** A line ends with one or more space or tab characters before its newline. Exception: Markdown's hard-break convention of two trailing spaces is allowed in contexts where a hard break is semantically meaningful (inside a paragraph, not at end-of-block).
- **Auto-fix.** Yes — defers to the formatter.
- **Rationale.** Trailing whitespace produces noisy diffs and occasionally changes parse behavior (see hard-break exception). Stripping it is safe in all other contexts.
- **Example violation.** (`·` represents a trailing space)

  ```markdown
  First line.··
  Second line.···
  ```

- **Example correction.**

  ```markdown
  First line.
  Second line.
  ```

#### MD012 — Multiple consecutive blank lines

- **Severity default.** Info.
- **Trigger.** Three or more consecutive blank lines appear outside of fenced code blocks (where blank lines are content).
- **Auto-fix.** Yes — defers to the formatter, which collapses to a single blank line.
- **Rationale.** Multiple blank lines render identically to one in CommonMark. The duplication is noise.
- **Example violation.**

  ```markdown
  First paragraph.



  Second paragraph.
  ```

- **Example correction.**

  ```markdown
  First paragraph.

  Second paragraph.
  ```

#### MD034 — Bare URL not in autolink form

- **Severity default.** Info.
- **Trigger.** A raw URL (`https://...`) appears as plain text in a paragraph, not wrapped in `<...>` autolink syntax or an inline `[text](url)` link. The parser's GFM-autolink extension makes these clickable, but the source form signals the author forgot to mark them as links.
- **Auto-fix.** No in v0.1. Rewriting to `<url>` is mechanically safe but depends on formatter policy; defer until B's sub-spec settles on the canonical URL-style.
- **Rationale.** Bare URLs produce inconsistent rendering across CommonMark implementations without GFM-autolinks. The autolink form is explicit and portable.
- **Example violation.**

  ```markdown
  See https://example.com/paper for details.
  ```

- **Example correction.**

  ```markdown
  See <https://example.com/paper> for details.
  ```

#### MD049 — Inconsistent emphasis style

- **Severity default.** Info.
- **Trigger.** A single document mixes `*foo*` and `_foo_` for emphasis (and analogously `**foo**` vs `__foo__` for strong).
- **Auto-fix.** Yes — defers to the formatter, which canonicalizes per B's sub-spec.
- **Rationale.** CommonMark treats both forms equivalently. Picking one per document is a style choice; mixing them is noise.
- **Example violation.**

  ```markdown
  *important* and _also important_
  ```

- **Example correction.**

  ```markdown
  *important* and *also important*
  ```

#### MDPP300 — Inconsistent fence info-string style

- **Severity default.** Info.
- **Trigger.** Code fences in the same document use inconsistent info-string casing or aliases for the same language (`JavaScript` vs `javascript` vs `js`).
- **Auto-fix.** Yes — defers to the formatter, which canonicalizes each known language to a single preferred info-string (per B's sub-spec).
- **Rationale.** Renderers and syntax-highlighters are mostly case-insensitive, but the output HTML class attribute inherits the source casing — which means `class="language-JavaScript"` and `class="language-javascript"` both appear in the rendered document and defeat CSS targeting.
- **Example violation.**

  ~~~markdown
  ```JavaScript
  ...
  ```

  ```js
  ...
  ```
  ~~~

- **Example correction.**

  ~~~markdown
  ```javascript
  ...
  ```

  ```javascript
  ...
  ```
  ~~~

### 4.4 Style/formatter relationship

Most style rules above duplicate what `mdpp fmt` would auto-fix unconditionally. The relationship is intentional:

- **The linter reports.** Authors who have not yet run the formatter on a document see the issue; the Info severity matches the low urgency.
- **The formatter fixes.** `mdpp fmt --write` rewrites the file; when rerun, the linter goes quiet on that rule.
- **`mdpp lint --fix` defers to the formatter.** The linter does not implement the fix independently. It calls `fmt.Format` on the document and writes back the result. This guarantees the two tools agree on canonical form — there is exactly one formatter, and it is authoritative.

Rules flagged "Auto-fix: Yes — defers to the formatter" in §4.1–§4.3 follow this contract. `Diagnostic.Fix` is set to a `TextEdit` that represents the formatter's change for *that rule's region only* (extracted from a full formatter diff by location); LSP clients see a normal per-diagnostic code action, and the unified behavior is transparent.

---

## 5. Suppression syntax

Authors suppress diagnostics with Markdown comments. Three scopes:

### 5.1 Inline — next-line scope

```markdown
<!-- mdpp-disable-next-line MDPP100 -->
The paper argues for the claim[^unresolved].
```

Suppresses the listed codes for the immediately-following non-blank line. The comment itself is not counted as the target line. Once the target line passes, suppression ends.

### 5.2 Block scope

```markdown
<!-- mdpp-disable MDPP100 -->
Several paragraphs with lots of [^work-in-progress] footnotes.
More text.
<!-- mdpp-enable MDPP100 -->
```

Suppresses the listed codes from immediately after the `disable` comment to either the matching `enable` comment or end-of-document, whichever comes first.

### 5.3 File scope — frontmatter

```markdown
---
mdpp-disable: [MDPP100, MD012]
---
```

Suppresses the listed codes for the entire document.

### 5.4 Directive syntax rules

- **Multiple codes.** Comma-separated inside the directive: `<!-- mdpp-disable MDPP100, MDPP101, MD012 -->`. Whitespace tolerant.
- **Disable-all.** `<!-- mdpp-disable -->` with no code list suppresses every rule in the scope. Same semantics for `disable-next-line` and `mdpp-disable: []` in frontmatter.
- **Unbalanced directives.** An `enable` with no prior `disable` is a no-op and produces no diagnostic of its own (treating it as an error would be hostile to edits-in-flight). A `disable` with no matching `enable` extends to end-of-document. An internal lint rule (`MDPP112` — *Unmatched disable directive*, disabled by default, to be added if user demand emerges; bumped from MDPP110 because that code is now taken by the auto-embed rule in §4.1) could later report unbalanced forms; not in v0.1.
- **Unknown codes.** A suppression that names a code the linter does not recognize is ignored silently in v0.1. This keeps file suppression forward-compatible when the linter adds new rules.
- **Case sensitivity.** Codes are case-sensitive (`MDPP100`, not `mdpp100`). Directive keywords (`mdpp-disable`, `mdpp-enable`, `mdpp-disable-next-line`) are case-sensitive too, matching the `mdpp` tool prefix convention.

### 5.5 Precedence

When multiple scopes apply to the same line, the narrowest wins. Inline suppression overrides block, which overrides file. A code suppressed at the file level cannot be re-enabled mid-document in v0.1 (adding `mdpp-enable` at block scope when no block-scope `disable` is active is treated as no-op, per §5.4); this behavior may be revisited if a concrete use case surfaces.

---

## 6. Configuration file (deferred)

`mdpp.toml` or `.mdpprc` is planned but **not in v0.1**. v0.1 ships with hardcoded rule defaults as documented in §4. Suppression comments and frontmatter are the only customization surface.

The intent for a future minor version: a single `mdpp.toml` at the workspace root with sections for linter configuration (`[lint]`), formatter overrides (`[fmt]`, though B's sub-spec currently defers knobs), and per-rule tunables (`[lint.rules.MDPP104]` with an `allowed = ["warning", "note", ...]` key). No attempt is made in this spec to design that format fully; it will get a separate sub-spec once v0.1 ship experience tells us which knobs are actually requested.

---

## 7. Public Go API

### 7.1 Primary surface

```go
// Package lint provides diagnostics over Markdown++ documents.
package lint

// Lint runs all default-enabled rules over d and returns all diagnostics.
// Diagnostics are returned in source order.
func Lint(d *mdpp.Document) []Diagnostic
```

`Lint` is the entry point the LSP and CLI both consume. It runs every rule that is enabled by default at its default severity. Suppression directives are honored internally — a suppressed diagnostic is not returned.

Rule signature is uniform across node types. The directive-aware rules added when the engine landed `[[toc]]` and `[[embed:url]]` (MDPP108–MDPP111) iterate `NodeTableOfContents` and `NodeAutoEmbed` via the same AST walk pattern that MDPP100 uses for `NodeFootnoteRef`, MDPP103 for `NodeHeading`, and MD045 for `NodeImage`. No new dispatcher, no new entry point — the visitor map in §9.1 grows by two keys.

### 7.2 Rule-by-rule access

```go
// Rule is a single lint rule.
type Rule interface {
    Code() string
    DefaultSeverity() Severity
    Title() string
    Description() string
    Check(d *mdpp.Document, emit func(Diagnostic))
}

// Rules returns every built-in rule, in code order.
func Rules() []Rule

// RuleByCode returns the built-in rule with the given code, or nil.
func RuleByCode(code string) Rule
```

Callers that want selective rule execution (e.g., a CI that only checks semantic rules) construct a custom runner around `Rules()` or `RuleByCode`. `Lint` itself is a thin wrapper around this surface.

### 7.3 Custom rules (deferred)

A `Register(r Rule)` extension point is not exposed in v0.1. The rule surface is built-in-only. This is deliberate: custom rules would lock the AST shape earlier than we want, and no concrete external user has asked for them. The `Rule` interface stays exported so an external package can implement it and run its own rules against the public AST — they just do not appear in `Lint(d)`. Future minor versions may add registration if demand emerges.

---

## 8. CLI behavior

### 8.1 Invocation

```
mdpp lint [flags] <file> [<file>...]
```

If no files are provided, reads from stdin.

### 8.2 Flags

| Flag | Purpose |
|------|---------|
| `--fix` | Apply auto-fixes in place. Safe fixes defer to `mdpp fmt` (see §4.4). Files without a path (stdin) write the fixed content to stdout. |
| `--format=<fmt>` | Output format. `text` (default, human-readable), `json` (machine-readable, one object per diagnostic), `github` (GitHub Actions annotation format using `::error file={path},line={line},col={col}::` directives). |
| `--severity=<level>` | Minimum severity to report. One of `error`, `warning`, `info`, `hint`. Default: `info`. Hints are never shown by default (authors usually don't want them in CI). |
| `--rules=<codes>` | Run only the listed rules. Comma-separated. Disables all other rules for this invocation. |
| `--no-rules=<codes>` | Run every rule *except* the listed ones. Cannot be combined with `--rules`. |
| `--quiet` | Suppress non-diagnostic output (summary line). |
| `--no-color` | Disable ANSI color in `text` output. Auto-detected off for non-TTY stdout. |

### 8.3 Exit codes

| Code | Meaning |
|------|---------|
| `0` | No diagnostics at the reported severity or above. |
| `1` | One or more Error-severity diagnostics were reported. |
| `2` | One or more Warning-severity diagnostics were reported (only when `--severity=warning` or below was requested with `--error-on-warning`; otherwise warnings exit `0`). |
| `3` | Usage error (unknown flag, unparseable file, etc.). |

v0.1 ships with `0` and `1` only; the `2` / `--error-on-warning` pair is reserved for future versions but mentioned here so the contract is explicit.

### 8.4 Output — `text` format

```
path/to/file.md:12:4: error  MDPP100  undefined footnote reference [^fermi]
path/to/file.md:42:1: info   MD012    multiple consecutive blank lines

2 issues (1 error, 1 info)
```

Severity is colored when the output is a TTY. Code is always eight characters wide (padded) for column alignment.

### 8.5 Output — `json` format

One JSON object per line (JSONL), with the diagnostic fields plus `file`. Stable across the `0.x` line.

```json
{"file":"paper.md","range":{"start":{"byte":312,"line":12,"column":4},"end":{"byte":319,"line":12,"column":11}},"severity":"error","code":"MDPP100","message":"undefined footnote reference [^fermi]"}
```

### 8.6 Output — `github` format

GitHub Actions annotation directives, one per line:

```
::error file=paper.md,line=12,col=4,endLine=12,endColumn=11::MDPP100 undefined footnote reference [^fermi]
```

Severity maps to `::error::`, `::warning::`, `::notice::` (GitHub has no `info`/`hint`; both map to `notice`).

---

## 9. Performance budget

**Target.** `Lint` on a 100,000-character document completes in under 50ms on a modern laptop (Apple Silicon M-series or equivalent x86-64). The budget covers the full pass: parsing is already done (the linter operates on an already-parsed `*Document`), so "lint time" is rule evaluation plus diagnostic construction only.

### 9.1 Architecture: single-pass traversal

Rules are implemented as visitor callbacks against a shared AST walk. The linter does **one** depth-first traversal of the document tree; each node dispatches to every rule that has registered interest in its `NodeType`. This avoids the naive N-rules × M-nodes cost of per-rule traversal.

```go
// internal sketch
type nodeVisitor func(*mdpp.Node, *ruleContext)

var visitorsByType = map[mdpp.NodeType][]nodeVisitor{
    mdpp.NodeFootnoteRef: {mdpp100Visit, ...},
    mdpp.NodeHeading:     {mdpp103Visit, mdpp200Visit, ...},
    mdpp.NodeImage:       {md045Visit},
    // ...
}
```

Rules that need cross-node state (MDPP100 collects all footnote refs, then cross-checks against the set of definitions; MDPP103 collects heading ids and flags duplicates) maintain that state inside a per-document `ruleContext` struct. Two-phase rules run a "collect" visitor during the main walk, then a "finalize" pass over accumulated state — both are O(N) over the AST.

### 9.2 Rules that need the rendered HTML

MDPP102 (broken intra-doc link) requires the heading-id slug. The slug algorithm runs against heading text, not rendered HTML, so the linter computes ids from the AST directly — no render pass needed.

### 9.3 Escape hatch

If an individual rule fails the 50ms budget in practice, the remediation order is: (1) optimize its implementation, (2) mark it opt-in rather than default, (3) cut it from v0.1. Every rule in §4 is believed to be trivially within budget; the guard is for rules added later.

### 9.4 Regression test

A performance test (see §11.3) generates a synthetic 100k-char document and asserts the 50ms budget under `go test -race`. The race build is used intentionally: if `Lint` is fast under `-race` (which adds substantial overhead), it is fast in production. The CI runs this test on every push.

---

## 10. LSP integration

The LSP server (D) consumes the linter through two protocol methods.

### 10.1 `textDocument/publishDiagnostics`

On `didChange` (debounced) and `didSave` (immediate), the LSP server:

1. Parses the current buffer contents (incremental via `ParseIncremental`).
2. Calls `lint.Lint(doc)`.
3. Maps each `Diagnostic` to an LSP `Diagnostic` object:
   - `Range` → LSP `Range` with UTF-16 columns (converted from UTF-8 bytes at this boundary).
   - `Severity` → LSP `DiagnosticSeverity` (Error=1, Warning=2, Information=3, Hint=4).
   - `Code` → LSP `code` string.
   - `Message` → LSP `message`.
   - `Related` → LSP `relatedInformation`.
4. Publishes the array for the document's URI.

### 10.2 `textDocument/codeAction`

When the editor requests code actions at a position, the LSP server:

1. Finds the diagnostics whose `Range` contains that position.
2. For each diagnostic with a non-nil `Fix`, emits an LSP `CodeAction` of kind `quickfix` with a `WorkspaceEdit` containing the `TextEdit`.
3. For diagnostics whose fix is deferred to the formatter (style rules), emits a code action of kind `source.fixAll.mdpp` that runs the full formatter. The editor's "fix all" UX groups these under a single action.

### 10.3 Debounce strategy

Run `Lint` on **debounced** change events (~250ms after the last keystroke), not on every keystroke. Rationale: `ParseIncremental` is microsecond-scale and can run per-keystroke, but rule finalization (cross-node state) is whole-document; 250ms of idle time is imperceptible and keeps the CPU quiet during rapid typing. On `didSave`, run immediately (the save is itself an explicit user request for feedback).

The 250ms figure is a starting point; D's sub-spec may tune it based on editor behavior.

---

## 11. Testing strategy

### 11.1 Per-rule unit tests

Each rule ships with at minimum one positive case (a fragment that should fire) and one negative case (a nearly-identical fragment that should not). Tests live in `lint/rule_<code>_test.go`, one file per rule. Table-driven where each row is `{input, wantCodes, wantRanges}`.

For rules with auto-fixes, each row additionally carries `wantFix` — the expected `TextEdit` or `nil`. Applying the fix and re-linting MUST produce zero diagnostics for the same rule.

### 11.2 Golden tests for full-document runs

`lint/testdata/golden/<case>/input.md` + `<case>/expected.json` pairs. The JSON file is the full serialized output of `Lint` against the input. The test runs `Lint`, serializes, and diffs against golden. Cases cover:

- Every rule in isolation (the same fragments used for unit tests, collected into a full document).
- Multiple rules interacting on the same construct.
- Full suppression behavior (inline, block, file-scope).
- The conformance corpus from A's §3.6 — every conformance input is lint-clean by construction; if a conformance case starts producing diagnostics, we broke something.

### 11.3 Performance regression

`lint/perf_test.go` generates a synthetic 100,000-character document (mix of headings, paragraphs, code blocks, footnotes, tables, links) once and times `Lint` over it. `go test -bench=. -benchtime=5s` reports ns/op; a `TestLintMeets50msBudget` asserts mean time under 50ms. Test runs in CI on every push.

### 11.4 LSP integration

Tested through D's LSP harness (§4.4 of the roadmap). Not repeated in C's own test suite.

### 11.5 Property tests

Two properties:

1. **Suppression is sound.** For any document and any suppression comment added at a position, the diagnostics at that position for the suppressed code disappear, and no other diagnostic changes. Randomized generation of suppression positions.
2. **Fix-then-lint is a fixed point.** For any document, applying every `Fix` from `Lint(d)` and re-linting produces a strict subset of the original diagnostic codes. (Fixing should never introduce new issues; re-running should not re-introduce fixed ones.)

Property tests use `go test -race` with a bounded random seed for reproducibility.

---

## 12. Open questions

Real decisions the implementer can make inside this sub-spec's frame without escalating — listed here so they get resolved consciously during implementation rather than by accident.

0. **`Node.Range` precondition (blocking).** Every `Diagnostic` requires a non-empty `Range` (§2.2). The engine has not yet added a byte/line-range field to `Node`. C cannot start implementation until A delivers it. This supersedes every other open question — none of them are decidable without positions to attach to. Tracked in the roadmap §0 progress snapshot as well.
1. **MDPP104's allowed-container-type list.** Is the v0.1 hardcoded set (`note`, `tip`, `warning`, `caution`, `important`, `info`, `details`, `aside`, `columns`, `column`) correct, or does it need additions? A's sub-spec will settle the canonical set; C follows. **Status:** still pending — the engine has not yet added `NodeContainer` for `:::`, so MDPP104 itself is future-pending until A delivers the node.
2. **Heading-id slug algorithm sharing.** ~~MDPP102 and MDPP103 both depend on the slug function. The function should live in the engine (A) as an exported utility, not duplicated in C.~~ **Resolved (2026-04-19):** the engine landed `[[toc]]` integration (`corpus_test.go`), and the TOC implementation drives its entries from the engine's `Heading` collection — meaning the slug algorithm is now centralized in A. MDPP102 and MDPP103 call it directly; no duplication.
3. **MDPP201's heuristic for "deserves descriptive text."** The proposed heuristic (path-segment count > 1, sole content of paragraph) is a first cut. If it produces too many false positives on real-world corpora, tune it or cut the rule to Hint severity.
4. **MD034 auto-fix.** Whether the formatter canonicalizes bare URLs to autolinks at all (B's sub-spec decision). If yes, C can mark MD034 as auto-fixable; if not, stays manual.
5. **Exit-code `2` for warnings.** Ship with `--error-on-warning` in v0.1 or not? Not required for launch; can land in a later patch release. Default off either way.
6. **Conformance corpus guarantee.** Every conformance case (§3.6) should be lint-clean. Confirm during §4.1 implementation that no conformance case is currently producing a diagnostic; if it does, either fix the case or mark the rule as opt-in.
7. **Range precision for inline constructs.** An undefined footnote reference spans `[^id]` — is the reported range the opening `[` through the closing `]` (5+ chars) or just the id (2-5 chars)? v0.1 proposal: the full `[^id]` span, because editors highlight the full diagnostic range and underlining just the id would look broken. Revisit if authors complain.
8. **Rule set additions before v0.1 ships.** Concrete candidates surfacing during implementation will be added to the rule catalog inline; the 21-rule count is a floor, not a cap. Caps come later if the rule surface becomes unwieldy.

---

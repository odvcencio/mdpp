# D. LSP (`cmd/mdpp-lsp`) — Design Spec

**Status.** Draft
**Date.** 2026-04-19
**Owner.** Oscar Villavicencio (m31labs)
**Scope.** Expands roadmap §4.4 (LSP) and §4.5 (Semantic highlighter, folded inside D). Defines the editor-agnostic language server that makes writing Markdown++ feel like writing code in a real IDE. Covers architecture, protocol surface, incremental sync, state management, semantic highlighter token model, performance budgets, and test harness. Assumes the decisions recorded in the roadmap §10; does not relitigate them.

---

## 0. Progress snapshot (as of 2026-04-19)

D has **not started**. No code under `lsp/` or `cmd/mdpp-lsp/` yet. This sub-spec remains the design target; nothing below has been implemented.

Engine progress that affects D:

- **Two new node types the LSP must recognize.** Engine A shipped `NodeTableOfContents` (from `[[toc]]`) and `NodeAutoEmbed` (from `[[embed:url]]`). Hover, completion, semanticTokens, foldingRange, and documentSymbol behaviors for both are specified inline below (see §4.4, §4.8, §4.9, §4.12, §5.2). The `:::name` block container directive remains pending in A; the `[[name]]` single-line directive form is what already ships.
- **Concurrent parser pool.** `parser_pool.go` exposes a `sync.Map`-backed pool over `gotreesitter.ParserPool`, keyed by `*Language`. The LSP's `DocumentStore` (§7) does not need to manage parser lifecycle — every `Parse`/`ParseIncremental` invocation routes through the engine's pool transparently. See §6 and §7.1 for the consequence.
- **Substantial parser hardening.** `corpus_test.go`, `hardening_test.go`, `security_test.go`, plus list/code-block/heading-text robustness fixes. Good news for LSP stability: real-world corpus pressure has already shaken out a class of panics and recovery edge cases that would otherwise have surfaced first under the LSP's per-keystroke `ParseIncremental` load.

Engine progress that did **not** land:

- **`Node.Range` (start/end byte positions on every AST node) — HARD BLOCKER for D.** Restated prominently because every LSP method that returns or accepts a position depends on byte→`{line, UTF-16 code unit}` translation rooted in node ranges. Without `Node.Range`:
  - `textDocument/hover` cannot resolve the node under a cursor position.
  - `textDocument/definition` cannot locate target ranges.
  - `textDocument/references` cannot enumerate use sites with ranges.
  - `textDocument/rename` cannot construct a `WorkspaceEdit`.
  - `textDocument/foldingRange` has no spans to fold.
  - `textDocument/documentSymbol` has no `range` or `selectionRange` to emit.
  - `textDocument/semanticTokens/full` and `range` have no positions to encode.
  - `textDocument/formatting` can still emit a single whole-document edit (the only LSP method that survives), but `codeAction` quickfixes from C cannot target ranges.
  - `textDocument/completion` cannot determine context-by-position.
  - `textDocument/publishDiagnostics` cannot translate lint diagnostic ranges.

  In short: **the LSP cannot start implementation until `Node.Range` lands on every AST node.** Roadmap §0 lists this as the engine's top priority gating B, C, and D; D depends on it before any handler beyond `initialize`/`shutdown` becomes meaningful.

- The `:::name` block container directive (a documentSymbol child kind, a foldingRange source, and a completion trigger context per this spec) remains pending in A. References to `:::` containers below assume A delivers the AST node before D ships.

D's implementation order, when it begins, is unchanged from §2: MVP column first, gated on `Node.Range` arrival from A.

---

## 1. Purpose and framing

The LSP is the piece of the Markdown++ stack that translates the grammar-backed AST into IDE affordances. Everything cool in the roadmap — hover-on-footnote-and-see-the-definition, rename-anchor-and-watch-all-links-update, semantic tokens that know whether a reference link is broken, live preview on every keystroke — is plumbed through this server. It is what the VS Code extension, Neovim, Helix, Zed, and Emacs configurations all talk to.

The LSP rides two pieces of substrate:

1. **gotreesitter's incremental engine.** `ParseIncremental` is 1.49μs on single-byte edits and 2.18ns on no-edit reparses with zero allocations (per gotreesitter's README). This is what makes every per-keystroke operation cheap enough to do in a handler goroutine without debouncing. The engine does the heavy lifting; the LSP is mostly a translator and a state keeper.
2. **The `mdpp` engine package.** The LSP imports `mdpp`, `mdpp/lint`, and `mdpp/fmt` directly as Go packages. No IPC to a separate parser binary, no subprocess boundaries inside the server, no serialization cost on the hot path. The LSP process holds live `*mdpp.Document` values in memory and operates on them directly.

The stance is: do the minimum that makes the demo GIFs compelling; layer the rest after v0.1 lands. The cut-line in §2 is written to be enforceable — if D runs long, there is a specific order in which features get dropped.

---

## 2. MVP cut-line

Two columns. The MVP column is what must ship for v0.1; the Stretch column ships only if it lands cleanly before the launch window.

| MVP (required for v0.1) | Stretch (ship if trivial) |
|---|---|
| `initialize` / `initialized` | `textDocument/rename` |
| `shutdown` / `exit` | `textDocument/references` |
| `textDocument/didOpen` | `textDocument/codeAction` |
| `textDocument/didChange` (incremental) | `textDocument/completion` |
| `textDocument/didSave` | `textDocument/prepareRename` |
| `textDocument/didClose` | `textDocument/inlayHint` |
| `textDocument/publishDiagnostics` (pushed) | `workspace/symbol` |
| `textDocument/hover` | custom `mdpp/renderPreview` notification |
| `textDocument/definition` | |
| `textDocument/foldingRange` | |
| `textDocument/documentSymbol` | |
| `textDocument/formatting` | |
| `textDocument/semanticTokens/full` | |
| `textDocument/semanticTokens/range` | |

**Drop order if D slips:**

1. Drop everything in Stretch first. The MVP column remains viable without any of it.
2. If still slipping, drop `textDocument/semanticTokens/range`. Clients that want range-scoped highlighting fall back to the `full` response.
3. If still slipping, drop `textDocument/foldingRange`. Clients fall back to their own heuristics.
4. **Never drop:** `didOpen`/`didChange`/`didSave`/`didClose`, `publishDiagnostics`, `hover`, `definition`, `documentSymbol`, `formatting`, `semanticTokens/full`. These are the operations that carry the "Markdown++ understands your document" story and are required for the launch GIFs.

Everything below is specified against the MVP column unless explicitly marked as Stretch.

---

## 3. Architecture

### 3.1 Process shape

A single Go binary at `cmd/mdpp-lsp`, speaking JSON-RPC 2.0 over stdio per LSP 3.17 §3.0 framing (`Content-Length` header, `\r\n\r\n` separator, UTF-8 body). The binary is editor-launched: VS Code's `vscode-languageclient` starts it, Neovim's `nvim-lspconfig` starts it, Helix reads `languages.toml` and starts it, etc. One server process per workspace. No TCP mode in v0.1 (stdio is universally supported; add TCP later if a use case emerges).

```
┌─────────────────────────────────────────────────────────────┐
│                       cmd/mdpp-lsp                          │
│                                                             │
│  ┌──────────────┐   ┌──────────────────┐   ┌─────────────┐  │
│  │ JSON-RPC I/O │──▶│ Router + handler │──▶│ Dispatcher  │  │
│  │  (stdio)     │◀──│   goroutines      │◀──│ (per-method)│  │
│  └──────────────┘   └──────────────────┘   └──────┬──────┘  │
│                                                   │         │
│              ┌────────────────────────────────────┼──────┐  │
│              │           DocumentStore             │      │  │
│              │  map[DocumentURI]*OpenDocument      │      │  │
│              │  sync.RWMutex per OpenDocument      │      │  │
│              └────────────┬────────────┬──────┬────┘      │  │
│                           │            │      │           │  │
│                      ┌────▼───┐   ┌────▼──┐ ┌─▼──────┐    │  │
│                      │ mdpp   │   │ mdpp/ │ │ mdpp/  │    │  │
│                      │ engine │   │ lint  │ │ fmt    │    │  │
│                      └────┬───┘   └───────┘ └────────┘    │  │
│                           │                               │  │
│                      ┌────▼───────────┐                   │  │
│                      │ gotreesitter   │                   │  │
│                      │ ParseIncremental│                  │  │
│                      └────────────────┘                   │  │
└───────────────────────────────────────────────────────────┘
```

### 3.2 Imports and package boundaries

The server package lives at `github.com/odvcencio/mdpp/lsp`. The binary entry point lives at `github.com/odvcencio/mdpp/cmd/mdpp-lsp`. The server package imports:

- `github.com/odvcencio/mdpp` — parser, AST, renderer.
- `github.com/odvcencio/mdpp/lint` — diagnostics source.
- `github.com/odvcencio/mdpp/fmt` — formatter.
- `github.com/odvcencio/gotreesitter` — `ParseIncremental`, `TreeCursor`, `InputEdit`, `Rewriter`.

No reverse dependencies: `mdpp`, `mdpp/lint`, `mdpp/fmt` do not import `mdpp/lsp`. The LSP is a consumer of the engine, not a peer.

A third-party JSON-RPC framing helper is acceptable (e.g., `go.lsp.dev/jsonrpc2` or a minimal hand-rolled one — the choice is deferred to implementation time; either works because the protocol is small). No LSP type-definitions library is mandated; the server ships its own Go types for the messages it uses. This avoids pinning to someone else's LSP version cadence.

### 3.3 Concurrency model

- One goroutine reads framed messages from stdin and pushes them onto an unbuffered work channel.
- The router pulls from the channel and dispatches one goroutine per request. This keeps the I/O reader responsive to `$/cancelRequest` while long handlers are in flight.
- Per-document locking is `sync.RWMutex` on the `OpenDocument`. Read handlers (`hover`, `definition`, `documentSymbol`, `foldingRange`, `semanticTokens/*`) take `RLock`; write handlers (`didChange`, `didSave`) take `Lock`. Readonly handlers run concurrently on the same document.
- One goroutine owns stdout; handler goroutines send responses through a response channel to avoid interleaved writes. Writes are newline-safe because the JSON-RPC framing is length-prefixed, but a single writer simplifies the mental model.
- The lint debouncer is a per-document timer reset on every `didChange`. When it fires, it grabs `RLock`, runs lint, releases, and pushes a `publishDiagnostics` notification. See §4.3.
- Cancellation: the router maintains `map[requestID]context.CancelFunc`. `$/cancelRequest` cancels the context; handlers check the context at natural yield points (after parse, before rendering hover markdown, between lint rules if lint turns into a slow path).

### 3.4 Startup and shutdown lifecycle

On startup, the server waits for `initialize` before doing anything substantive. Per LSP spec, only `initialize`, `initialized`, `shutdown`, and `exit` are legal before `initialize` completes. After `initialize` returns, clients may send any request.

On `shutdown`, the server stops accepting new requests, drains in-flight handlers (with a bounded timeout — 5s), and returns. On `exit`, the process terminates with status 0 if `shutdown` was received, 1 otherwise (per spec).

Signals: `SIGINT`/`SIGTERM` trigger the same drain-and-exit path. This matters for editors that kill their language server processes directly.

---

## 4. Per-method specifications

### 4.1 `initialize`

**Request.** Client sends `InitializeParams` including `capabilities`, `rootUri`, `workspaceFolders`, and `initializationOptions` (see §10).

**Response.** `InitializeResult` with `capabilities`:

```json
{
  "capabilities": {
    "textDocumentSync": {
      "openClose": true,
      "change": 2,
      "save": { "includeText": false }
    },
    "hoverProvider": true,
    "definitionProvider": true,
    "foldingRangeProvider": true,
    "documentSymbolProvider": true,
    "documentFormattingProvider": true,
    "semanticTokensProvider": {
      "legend": {
        "tokenTypes":    [<see §5.2>],
        "tokenModifiers":[<see §5.3>]
      },
      "range": true,
      "full": true
    },
    "referencesProvider": true,
    "renameProvider": { "prepareProvider": true },
    "codeActionProvider": {
      "codeActionKinds": [
        "quickfix",
        "refactor.rewrite",
        "source.fixAll"
      ]
    },
    "completionProvider": {
      "triggerCharacters": ["[", "]", ":", "^", "!", "#"],
      "resolveProvider": false
    }
  },
  "serverInfo": { "name": "mdpp-lsp", "version": "<build version>" }
}
```

`change: 2` is `TextDocumentSyncKind.Incremental`. We require this; §6 depends on it.

Stretch capabilities (`inlayHintProvider`, `workspaceSymbolProvider`) are advertised only if they are actually implemented by ship time. Advertising a capability we don't honor breaks clients.

**Error cases.** If `clientInfo` is present and indicates an editor the server has special handling for (none in v0.1), record it. Never fail `initialize`; even if initialization options are malformed, we fall back to defaults and log a warning.

**Performance.** Synchronous, returns in under 5ms. No parsing happens here.

### 4.2 `textDocument/didOpen`, `didChange`, `didSave`, `didClose`

This is the hottest path in the server and the one most likely to break under adversarial inputs. §6 covers incremental sync in depth; this section covers the surface protocol mapping.

**`didOpen`.** Notification. Params include the full document text. Server:

1. Allocates an `OpenDocument` with a fresh `sync.RWMutex`.
2. Runs `mdpp.Parse(src)` to produce the initial `*Document` (full parse, no old tree).
3. Stores the result under the URI in the `DocumentStore`.
4. Schedules a lint on the document (§4.3).
5. Publishes an initial empty `publishDiagnostics` if the client expects one (harmless and keeps older clients from showing stale results).

Parse errors never reject `didOpen`; they are translated into diagnostics via the linter or a surface-level "recoverable parse diagnostic" path.

**`didChange`.** Notification. Params include a `contentChanges` array. Because we advertised `Incremental` sync, each entry has a `range` and a `text` replacement (or a single entry with no range, meaning full replace). Server:

1. Acquires `Lock` on the document.
2. For each change in order: translates the LSP `TextDocumentContentChangeEvent` to a `gotreesitter.InputEdit` (see §6.3), applies it to the current source bytes, calls `tree.Edit(edit)`, then `parser.ParseIncremental(newSrc, oldTree)`.
3. Updates `OpenDocument.Source` and `OpenDocument.Document`.
4. Releases the lock.
5. Resets the lint debounce timer (§4.3).
6. If any semantic-tokens-aware client has requested proactive refresh, schedules nothing — semantic tokens are always pulled, not pushed.

**`didSave`.** Notification. In v0.1 we treat save as an opportunity to run a full lint pass immediately (bypass debounce) and publish diagnostics. We do not re-parse on save — the incremental state is already authoritative. If `save.includeText` were true we would reconcile, but we declared it false in §4.1 to keep the protocol lean.

**`didClose`.** Notification. Server:

1. Takes `Lock`.
2. Deletes the entry from `DocumentStore` (map delete; Go GC reclaims the AST).
3. Sends an empty `publishDiagnostics` to clear any lingering diagnostics on the client.

**Error cases.** If `didChange` arrives for a URI not in the store, log and drop — this indicates a client/server desync. If `ParseIncremental` fails (it shouldn't, but defensively), fall back to a full parse. If a full parse fails, keep the previous AST, mark the document as `DegradedParse`, and return — we still serve hover/definition from the last-good AST, and diagnostics reflect the parse failure at the offending byte range.

**Performance.** Per-keystroke `didChange` handler completes in under 2ms on documents up to 100k chars, dominated by `ParseIncremental` (a few μs) and the translation bookkeeping. This is the budget for "live preview feels instant."

### 4.3 `textDocument/publishDiagnostics`

Push-only notification from server to client. Triggered:

- After every `didOpen` (initial lint).
- After a debounced pause following `didChange` (250ms of idle since the last change).
- After every `didSave` (immediate, bypassing debounce).
- After `didClose` (empty array, to clear).

**Pipeline.**

1. Acquire `RLock`.
2. Call `lint.Lint(doc.Document)` — returns `[]lint.Diagnostic`.
3. For each `lint.Diagnostic`:
   - Translate byte `Range` to LSP `Range` with UTF-16 code-unit columns (see §6.4).
   - Map severity: `Error` → `1`, `Warning` → `2`, `Info` → `3`, `Hint` → `4` (LSP enum).
   - Copy `Code` (e.g., `"MDPP010"`) to LSP `diagnostic.code`.
   - Set `source: "mdpp"`.
   - Copy `Message`.
   - If `Fix` is present, attach a `codeDescription` reference — the actual fix surfaces through `codeAction`, not directly on the diagnostic.
4. Release lock.
5. Send `publishDiagnostics` with the full array (LSP requires the full list on every push; it's a complete replacement).

**Debounce details.** Timer is per-document. On every `didChange`, reset the timer. When the timer fires, take `RLock`, run lint, push diagnostics. If a new `didChange` arrives mid-lint, the fresh diagnostics are for a stale state — that's acceptable; the next debounce cycle will publish current ones. We do not cancel in-flight lints; they finish and the result is either dropped (if the doc changed) or published (if not).

**Performance.** Lint-on-100k-char-doc budget: 50ms (§4.3 of the roadmap, §7 here). Debounce: 250ms. `publishDiagnostics` arrives at the client within ~300ms of the user pausing typing.

### 4.4 `textDocument/hover`

**Request.** `HoverParams` = `{ textDocument, position }`.

**Response.** `Hover | null` where `Hover = { contents: MarkupContent, range?: Range }`. `MarkupContent = { kind: "markdown", value: string }`.

**Behavior matrix** — given the AST node at `position` (resolved by a position-to-node lookup using `TreeCursor.GotoFirstChildForPoint`):

| Node | Hover body (rendered markdown) |
|---|---|
| `FootnoteRef` | `### Footnote [^id]\n\n<rendered body of matching FootnoteDef>`. If undefined, "*Undefined footnote `[^id]`*." |
| `Link` (reference-style) | `**[text][ref]**\n\n<href>` + title if present; "*Undefined reference `[ref]`*" if unresolved. |
| `Link` (inline, URL) | `<url>` in code style; target-page title if derivable (we don't fetch remote titles in v0.1). |
| `Link` (internal `#anchor`) | `## <heading text>\n\nLine <N> · id: <anchor>`. |
| `Image` | `![alt](url)` rendered, plus dimensions from `width`/`height` attributes if present. |
| `MathInline` / `MathBlock` | An HTML-rendered preview wrapped in fenced `html` for clients that honor it, plus the raw LaTeX in a `tex` fence so clients that do not render HTML tooltips still show something useful. |
| `Emoji` | `<unicode> — :name: — <category>` (e.g., `🎉 — :tada: — Celebration`). Uses the engine's emoji table. |
| `Admonition` | `**[!TYPE]**\n\n<description of type>`. Descriptions are baked in (NOTE, TIP, WARNING, CAUTION, IMPORTANT). Custom types: "*Custom admonition type.*". |
| Container `:::name` | `**:::name**\n\n<description>`. Descriptions from the engine's container registry. |
| `CodeBlock` (fenced, with info-string) | `code fence: <language>`; if `mermaid` or `dot`, note "diagram — will render on output". |
| `Heading` | `# <text>\n\nid: <auto-id> · Level <n>`. |
| `NodeTableOfContents` (cursor on `[[toc]]`) | `Table of contents — auto-generated from headings in this document. Currently lists N headings (h2-h6).` `N` is computed from a quick AST scan at hover time (cheap; bounded by document heading count). |
| `NodeAutoEmbed` (cursor on `[[embed:url]]`) | `Auto-embed: provider={provider}. URL: {url}. Will render as: rich embed | generic link fallback (depending on provider).` Provider and URL come from the node's `data-provider` and `data-src` attributes; the "rich embed | generic link fallback" line picks the appropriate phrase for the resolved provider. |

**Position-to-node resolution.** Use `TreeCursor.GotoFirstChildForPoint(point)` recursively from the root until the deepest node containing `point` is reached. This is O(depth), not O(nodes).

**Error cases.** If position falls in a text-only region with no hoverable node, return `null`. Never return an error — clients treat `null` as "no hover info" gracefully.

**Performance.** Budget: p99 <50ms on 100k-char documents. Typical: <5ms. The cost is dominated by hover-content rendering for math (which calls the engine's HTML path on a small subtree) and for footnote bodies.

### 4.5 `textDocument/definition`

**Request.** `DefinitionParams`. **Response.** `Location | Location[] | null`.

**Cases:**

- `[^id]` ref at position → `Location` of the matching `[^id]:` def.
- `[text][ref]` → `Location` of `[ref]:` definition block.
- `[text](#anchor)` → `Location` of the heading whose auto-id matches `anchor`.
- Image `![alt][ref]` → reference-style image def.
- Link/image whose target is a `file://` URI in the same workspace → `Location` of that file (range = whole file, byte 0). v0.1 does not resolve across files for internal anchors; cross-file jumps ship if workspace indexing lands before launch.

Undefined references return `null` (not an error). The linter flags the brokenness; hover explains it.

**Performance.** <10ms. Uses a per-document index built once per parse:

```go
type DefinitionIndex struct {
    Footnotes     map[string]Range // id → range of definition
    LinkRefs      map[string]Range
    HeadingByID   map[string]Range
}
```

Built lazily on first `definition` or `references` call after a parse; invalidated on `didChange`.

### 4.6 `textDocument/references` (Stretch)

**Request.** `ReferenceParams` including `context.includeDeclaration`. **Response.** `Location[] | null`.

**Behavior.** Given the symbol at position:

- Footnote ID `id` → all `[^id]` refs (and def if `includeDeclaration`).
- Link reference `ref` → all `[text][ref]` and `![alt][ref]` uses.
- Heading anchor → all `[text](#anchor)` references.

Uses the same index as `definition`, plus a reverse map built during the AST walk.

**Error cases.** If position is not on a symbol, return `null`.

**Performance.** <20ms.

### 4.7 `textDocument/rename` (Stretch)

**Request.** `RenameParams`. **Response.** `WorkspaceEdit | null`.

**Behavior.** Atomic rename across one document (v0.1). Cross-file rename not shipped.

- Footnote ID rename: find all `[^id]` + the `[^id]:` def, rewrite to `[^newId]`. Collision check: if `newId` already exists as a footnote, return an `InvalidParams` error with message "footnote `newId` already defined".
- Link reference ID rename: same pattern for `[text][ref]` and `[ref]:`.
- Heading anchor rename: this one is different — renaming the anchor means renaming the heading text (because anchors are auto-derived from heading text). We present the heading's text as the rename target; on rename, we regenerate the auto-id and update every `[text](#oldId)` in the document.

All edits are bundled into a `WorkspaceEdit.documentChanges` (per-URI, ordered). LSP `prepareRename` (Stretch) reports the exact range the client should present for renaming.

**Error cases.** New name is invalid (empty, whitespace, collides) → `ResponseError` with code `InvalidParams`. Position is not on a renameable symbol → `null`.

**Performance.** <50ms.

### 4.8 `textDocument/foldingRange`

**Request.** `FoldingRangeParams` = `{ textDocument }`. **Response.** `FoldingRange[]`.

**Regions emitted:**

- Section folds by heading level: each `Heading` starts a fold that ends at the next heading of equal or lower level (or document end).
- Fenced `CodeBlock` spans.
- Frontmatter block.
- `:::name` containers (including nested).
- `Admonition` blocks.
- Multi-line `FootnoteDef` blocks.

Explicitly **not** folded: `NodeTableOfContents` and `NodeAutoEmbed`. Both are single-line directives by construction (`[[toc]]` and `[[embed:url]]` occupy exactly one line); a fold spanning a single line is meaningless and noisy in the editor's gutter. The walker skips these node types when emitting fold ranges.

Each `FoldingRange` carries `kind: "region" | "comment" | "imports"` (we use `"region"` for everything in v0.1).

**Performance.** <20ms. Single `TreeCursor` pass.

### 4.9 `textDocument/documentSymbol`

**Request.** `DocumentSymbolParams`. **Response.** `DocumentSymbol[]` (hierarchical, preferred over the flat `SymbolInformation` form).

**Hierarchy:**

- Top level: if frontmatter exists, a `File` symbol named "frontmatter" with children = one symbol per key (kind `Key`).
- Top level: headings organized by level. An h2 after an h1 is a child of that h1; an h3 after an h2 is a child of that h2; etc. Kind `Namespace` for all headings.
- Under each heading's range: any `:::name` containers in that range appear as nested symbols (kind `Class` — clients display it with a distinctive icon).
- Footnote definitions surface as a flat group at document end under a synthetic "footnotes" symbol (kind `Interface`), each def a child (kind `Field`).

Symbol `name` = heading text / container type / frontmatter key. `range` covers the full symbol span; `selectionRange` covers the identifier (heading text, container `:::name` line, key name).

**Explicitly excluded from the symbol tree.** `NodeTableOfContents` (`[[toc]]`) and `NodeAutoEmbed` (`[[embed:url]]`) do **not** appear as symbols in the outline. They are decorative content nodes — directives that influence rendering but do not belong in the document's structural overview. A reader navigating an outline wants headings, frontmatter, footnote definitions, and container regions; cluttering the tree with one entry per `[[toc]]` or `[[embed:url]]` would dilute the outline's signal. This is a deliberate UX choice; revisit only if user feedback identifies a navigation use case.

**Performance.** <30ms.

### 4.10 `textDocument/formatting`

**Request.** `DocumentFormattingParams` including `options` (tab size, insert spaces — ignored in v0.1; formatter has no knobs). **Response.** `TextEdit[] | null`.

**Behavior.** Call `fmt.Format(doc.Source)`. If the output equals the input, return `[]` (no edits). Otherwise return a single `TextEdit` covering the entire document range and replacing it with the formatted output. This is the conventional LSP pattern for whole-document formatters; granular diffs would be nicer but every mainstream client accepts the single-edit form.

**Error cases.** `fmt.Format` returns an error (e.g., engine panic during re-render) → return `ResponseError` with code `InternalError`. The document is not modified.

**Performance.** <200ms on 100k-char docs per the roadmap's §4.2 budget. Formatter is the expensive part; the LSP glue is cheap.

### 4.11 `textDocument/codeAction` (Stretch)

**Request.** `CodeActionParams` = `{ textDocument, range, context }`. `context.diagnostics` carries the client-side diagnostic list that intersects `range`.

**Response.** `(Command | CodeAction)[]`.

**Actions surfaced:**

- **Linter auto-fixes.** For each diagnostic in `context.diagnostics` with a matching auto-fix from `mdpp/lint`, emit a `quickfix` `CodeAction` with the fix as a `WorkspaceEdit`.
- **Refactorings** at the cursor:
  - "Convert reference link to inline" — when cursor is on `[text][ref]` and `[ref]:` is defined, rewrite to `[text](href "title")`, delete the `[ref]:` if it has no other users.
  - "Convert inline link to reference" — rewrite `[text](url)` to `[text][auto-ref]` and insert `[auto-ref]: url` at the document's reference-definition block (creating the block if absent).
  - "Convert admonition to `:::` container" — rewrite GFM `> [!TYPE] ...` to `:::type ... :::`, and vice versa.
  - "Add heading id explicitly" — replace auto-id with an explicit `{#custom-id}` suffix on the heading.
- **Source actions:**
  - `source.fixAll` — apply every available auto-fix in the document. Useful as an on-save action.

Each action returns an edit list as `WorkspaceEdit`. We do not use the `Command` form; everything is resolved eagerly.

**Performance.** <50ms for refactor enumeration; fix application is essentially free (byte-range text substitution).

### 4.12 `textDocument/completion` (Stretch)

**Request.** `CompletionParams` with `context.triggerCharacter` and `position`.

**Response.** `CompletionList` (preferred over raw `CompletionItem[]` so we can set `isIncomplete`).

**Trigger behavior.**

| Trigger | Context | Items |
|---|---|---|
| `[^` | Inline, not in code | All defined footnote IDs in the document. |
| `][` | After `[text]` inline | All defined link reference IDs. |
| `:` | Inline, not in code, not in URL | Emoji shortcodes (from the engine's emoji table). Filter as user types. |
| `:::` | Start of line | Container types from the allow-list (§A's sub-spec). |
| `[!` | Inside a blockquote line | Admonition types: `NOTE`, `TIP`, `WARNING`, `CAUTION`, `IMPORTANT`. |
| Frontmatter position | Inside the frontmatter block | Reserved keys (`title`, `lang`, `toc`, `date`, `mdpp`) + commonly used keys mined from the corpus (`author`, `tags`, `description`, `slug`). |
| `#` | After `](` in a link | Heading anchors in the current document. |
| `[[` | At line start (or only whitespace before) | `toc` (description: "Insert auto-generated table of contents at this position") and `embed:` (description: "Auto-embed a URL; provider detected from host"). Both completions insert with a snippet that closes the `]]` automatically. |
| `[[embed:` | After the literal `[[embed:` token on a line | No completions emitted. The URL portion is freeform and we do not maintain a URL registry. Documented as the contract: clients should not expect suggestions here. The `:` trigger character still fires; the handler returns an empty `CompletionList` with `isIncomplete: false`. |

Each item carries `kind` (Keyword, Value, Reference), `detail` (short description), `documentation` (markdown, e.g., for emoji a preview). `insertText` uses snippet syntax when useful (e.g., `[^${1:id}]${0}`).

**Position-context detection.** Walk the AST to the innermost node at position; check ancestor types to determine context (blockquote, inline code, code fence, frontmatter). The trigger character alone is insufficient — `:` inside a code fence should not offer emojis.

**Error cases.** No matches → empty list. Context cannot be determined → empty list.

**Performance.** <30ms. Footnote/link-ref lists are O(N) over document symbols; emoji list is a cached slice.

### 4.13 `textDocument/semanticTokens/full` and `range`

Full treatment in §5.

---

## 5. Semantic highlighter (sub-project E, folded)

### 5.1 Why this lives inside D

Per the roadmap, E is not a separate binary, separate grammar file, or separate deliverable. It is the `textDocument/semanticTokens/*` handler of D. This is the direct payoff of the grammargen-backed AST: the handler walks the real parse tree and emits tokens that express distinctions a TextMate grammar cannot (resolved vs broken link, inline vs reference footnote, math inline vs display, frontmatter key vs value, container-type name vs container body).

### 5.2 Token types

Declared in `initialize.capabilities.semanticTokensProvider.legend.tokenTypes`. Order is the wire index — `tokenType: N` in an encoded token refers to `tokenTypes[N]`.

Standard LSP types we use (they have well-known client themeings):

0. `comment` — HTML comments, `<!-- mdpp-disable ... -->` directives.
1. `string` — link URLs in the source position, image URLs.
2. `keyword` — frontmatter reserved keys (`title`, `lang`, `mdpp`, ...).
3. `operator` — math operators inside inline/display math if we do a cheap tex tokenization; skipped in v0.1 (the math block gets a single `math` token).
4. `number` — frontmatter numeric values; ordered-list markers.

Custom types:

5. `heading` — heading text.
6. `link` — link text.
7. `footnote` — footnote ID text, in both definition and reference positions.
8. `math` — math content (inline or display).
9. `containerType` — the name after `:::`.
10. `admonitionType` — the name inside `[!NAME]`.
11. `emojiShortcode` — `:tada:`.
12. `frontmatterKey` — key names (overlap-free with `keyword` because reserved keys get both; we emit `keyword` and the client modifier handles the distinction).
13. `frontmatterValue` — value bodies.
14. `strikethrough` — `~~text~~` content.
15. `emphasis` — `*x*`, `_x_`, `**x**`, `***x***`.
16. `taskMarker` — the `[ ]` / `[x]` of a task list item.
17. `definitionTerm` — definition-list term.
18. `tableHeader` — first row of a table.
19. `tableSeparator` — the `---|---|---` alignment row.
20. `imageAlt` — image alt text.
21. `imageUrl` — image URL (distinct from `string` so clients can theme it differently).
22. `directive` — the `[[name]]` form. Used for the `[[toc]]` and `[[embed:url]]` directive heads so themes can color them distinctively from regular text. Modifiers `toc` and `embed` (defined in §5.3) disambiguate which directive variant is in play.
23. `directive.argument` — the URL or argument body inside a parameterized directive (currently only `[[embed:<url>]]`). Lets clients theme the URL portion separately from the directive head, the same way `imageUrl` is themed separately from `imageAlt`.

Exact numeric indices are stable within the 0.x line; reordering is a breaking change for any client that caches them.

### 5.3 Token modifiers

Declared in `legend.tokenModifiers`. Each token's `modifiers` field is a bitmask over this list.

0. `level1` — heading level.
1. `level2`.
2. `level3`.
3. `level4`.
4. `level5`.
5. `level6`.
6. `inline` — link/math form.
7. `reference` — link form (reference-style vs inline).
8. `autolink` — link form.
9. `resolved` — link/footnote is resolved.
10. `broken` — link/footnote is unresolved (dangling reference).
11. `definition` — footnote in definition position (vs reference position).
12. `display` — math form (vs inline).
13. `italic` — emphasis variant.
14. `bold` — emphasis variant.
15. `bolditalic` — emphasis variant.
16. `checked` — task marker state.
17. `unchecked` — task marker state.
18. `toc` — directive variant. Set on `directive` tokens for `[[toc]]`.
19. `embed` — directive variant. Set on `directive` tokens for `[[embed:url]]`. Also set on the companion `directive.argument` token covering the URL.

Theming tip for the VS Code extension: use modifier selectors like `link.broken` to color broken links red, or `directive.toc` to color the TOC directive distinctly from `directive.embed`. The LSP only exposes data; the theme does the coloring.

### 5.4 Encoding

LSP's semantic tokens wire format is a flat `uint32[]` where every five integers describe one token: `[deltaLine, deltaStart, length, tokenType, tokenModifiers]`.

- `deltaLine`: lines since the previous token's start line (0-based, 0 for tokens on the same line).
- `deltaStart`: UTF-16 code units since the previous token's start column, reset to absolute when `deltaLine > 0`.
- `length`: token length in UTF-16 code units.
- `tokenType`: index into `legend.tokenTypes`.
- `tokenModifiers`: bitmask over `legend.tokenModifiers`.

Tokens must be sorted by start position (line then column). Overlapping tokens are illegal; the client behavior is undefined. We guarantee non-overlap by construction.

UTF-16 code units, not bytes, not runes: `"é"` is 1 code unit, `"😀"` is 2 code units (surrogate pair). §6.4 describes the byte→UTF-16 translator we share with diagnostics.

### 5.5 Walker algorithm

Single `TreeCursor`-based pass over the tree, depth-first pre-order. For each node, emit the appropriate token(s), then descend. The walker stays iterative — a `TreeCursor` moves without allocation, and the token list is preallocated with a capacity estimate proportional to document size.

```
tokens := make([]Token, 0, len(src)/64)   // heuristic

cursor := NewTreeCursorFromTree(tree)
for {
    emit(cursor.CurrentNode(), &tokens)
    if cursor.GotoFirstChild() { continue }
    for !cursor.GotoNextSibling() {
        if !cursor.GotoParent() { goto done }
    }
}
done:
sort-nothing (already in order by construction)
encode(tokens) -> uint32 slice
```

`emit` is a switch over `NodeType`. For each emitted token it computes:

- The node's byte range.
- Start line + UTF-16 column via the position-translator cache (§6.4).
- Length in UTF-16 code units.
- Type + modifier bitmask based on node attributes (e.g., `link.resolved` via the definition index; `footnote.broken` via the same index).

Container and admonition name tokens are sub-spans of the parent node — we look at the node's text to locate the name's byte offset.

For `semanticTokens/range`, the same walker runs, but `cursor.GotoFirstChildForPoint(startPoint)` to enter the range, and tokens outside the range are skipped. The encoded stream is delta-encoded from `(0, 0)` as if it were a standalone document — clients expect a self-contained stream per request.

### 5.6 Correctness guarantees

- No overlapping tokens (enforced by construction: emitted in AST pre-order, ranges are non-overlapping for non-leaf-vs-leaf only when we stop emitting at the leaf level).
- Non-leaf nodes do not emit tokens when their leaves already cover the span. Example: a `Link` node does not emit a `link` token; its `Text` child emits a `link` token, and its URL child emits a `string`/`imageUrl` token.
- Every emitted token is a contiguous byte range in source (no synthetic tokens).
- The token stream is byte-stable across runs on the same input (needed for client caches to work with `semanticTokens/full/delta` — a Stretch item, not declared in v0.1).

---

## 6. Incremental sync deep dive

This is the single most performance-critical subsystem in the LSP. Get it wrong and per-keystroke latency multiplies; get it right and typing in a 100k-char document stays invisible.

**Parser provisioning.** The engine ships `parser_pool.go` — a `sync.Map`-backed pool keyed by `*gotreesitter.Language`, wrapping `gotreesitter.ParserPool`. Every `Parse` and `ParseIncremental` call from the LSP must route through the pool so multi-document workloads (50 open buffers, all reparsing concurrently in handler goroutines) share parser instances rather than allocating per call. Concretely: the LSP does **not** instantiate `gotreesitter.Parser` directly, does **not** carry a parser handle on `OpenDocument`, and does **not** synchronize parser access itself. The engine package's pool handles all of that — either transparently (if A's eventual stable `Parse` API takes a pool option or auto-pools) or via a thin `mdpp.ParseIncremental(src, oldTree)` wrapper the engine exposes that calls into the pool. D's contract is: if it ever finds itself constructing a parser, that's a bug; route through the engine instead.

### 6.1 The translation problem

LSP sends `TextDocumentContentChangeEvent`:

```typescript
{
  range: { start: {line, character}, end: {line, character} },
  rangeLength?: number,   // deprecated; ignore
  text: string            // replacement
}
```

gotreesitter wants `InputEdit`:

```go
type InputEdit struct {
    StartByte   uint32
    OldEndByte  uint32
    NewEndByte  uint32
    StartPoint  Point    // {Row, Column} where Column is a byte offset from line start
    OldEndPoint Point
    NewEndPoint Point
}
```

Two translation problems:

- LSP coordinates are `{line, UTF-16 code unit}`. Tree-sitter coordinates are `{row, byte column}`. These agree only on ASCII.
- LSP gives us a replacement string; tree-sitter wants the before/after byte positions.

### 6.2 Line index cache

The `OpenDocument` carries a `LineIndex`: the starting byte offset of every line. On full parse (after `didOpen`), we build it with a single pass. On `didChange`, we update it incrementally:

1. For each change, locate the line range affected (start line → end line).
2. Compute the new bytes inserted vs deleted.
3. Shift subsequent line offsets by the byte delta.
4. Rebuild the line list in the affected region from the new source bytes.

This is O(lines affected + lines after change), not O(doc). Typical edits touch <3 lines; the cost is negligible.

For a byte offset → `{line, byte column}` lookup, binary-search the line index.

### 6.3 Edit application

Given an LSP `TextDocumentContentChangeEvent`:

```
1. startByte := lineIndex[range.start.line] +
                utf16ToBytes(line=range.start.line, utf16Col=range.start.character)
2. oldEndByte := lineIndex[range.end.line] +
                 utf16ToBytes(line=range.end.line, utf16Col=range.end.character)
3. newBytes := []byte(change.text)
4. newSrc := src[:startByte] + newBytes + src[oldEndByte:]
5. newEndByte := startByte + len(newBytes)
6. startPoint := Point{Row: range.start.line, Column: startByte - lineIndex[range.start.line]}
7. oldEndPoint := Point{Row: range.end.line, Column: oldEndByte - lineIndex[range.end.line]}
8. For newEndPoint, locate the row/column of newEndByte in newSrc (after applying the edit to the line index).
9. tree.Edit(InputEdit{startByte, oldEndByte, newEndByte, startPoint, oldEndPoint, newEndPoint})
10. newTree := parser.ParseIncremental(newSrc, tree)
11. src = newSrc; tree = newTree
12. Rebuild affected line index entries.
13. Invalidate the definition index (lazy; rebuilt on next demand).
```

Steps 1, 2, 6, 7, 8 all touch the UTF-16↔byte converter (§6.4).

Multiple changes in a single `didChange`: apply in order per the LSP spec, rebuilding the line index between them. Each change's `range` is expressed against the state *after* all earlier changes in the array have been applied. This is unintuitive; getting it wrong is a common LSP server bug. Test for it explicitly (§8.2).

### 6.4 UTF-16 ↔ byte conversion

Given a line's bytes and a UTF-16 column, return the byte offset within the line. Iterate codepoints with `utf8.DecodeRune`, count UTF-16 code units (1 for BMP, 2 for supplementary), accumulate byte offset, stop when the cumulative UTF-16 count reaches the target.

Given a line's bytes and a byte offset within the line, return the UTF-16 column — the inverse.

Both operations are O(line length). Lines are short in practice; this does not appear on any perf profile.

Cache nothing per-line by default; if the profile ever shows it, cache per-line column/offset tables lazily.

### 6.5 Edge cases

- **Whole-document replace.** LSP allows a single `contentChange` with no `range`, meaning full text replace. We handle this by running a full `mdpp.Parse` instead of `ParseIncremental` — `ParseIncremental` with a completely new source and no edit info provides no reuse benefit.
- **Edits crossing line boundaries.** The general formulation in §6.3 handles this already; no special case.
- **Empty-text edits (deletions).** `newBytes = []`, `newEndByte = startByte`. Fine.
- **Edits at document end (appending).** `endByte = len(src)`. Fine.
- **CRLF vs LF line endings.** LSP does not mandate one; we detect on `didOpen` and preserve. The line index counts logical lines regardless.
- **Client sends stale changes.** If a `didChange` carries a `textDocument.version` older than the one we've applied, log and drop. (Requires tracking version numbers per document; we do.)
- **Unicode combining marks, ZWJ, emoji sequences.** No special handling needed at the sync layer — we pass bytes to the grammar. The grammar treats every byte-valid UTF-8 sequence as content. Cursor placement concerns are the client's problem.
- **BOM on `didOpen`.** Strip before handing to the parser; preserve for round-trip if the client sent the original with a BOM.

### 6.6 Failure and degradation

If `ParseIncremental` ever produces a tree whose `HasError()` is true on a region that was previously error-free, we log the event with a reproducer (before-source hash, edit, after-source hash) and continue. The AST is still usable; error recovery in tree-sitter means ranges around the error are still structured. If the tree is catastrophically broken, we fall back to a full parse once; if that also fails, we mark the document as `DegradedParse` and serve stale-but-last-good AST for read methods while diagnostics reflect the failure.

---

## 7. State management

### 7.1 `DocumentStore`

```go
type DocumentStore struct {
    mu   sync.RWMutex                     // guards the map only
    docs map[lsp.DocumentURI]*OpenDocument
}

type OpenDocument struct {
    mu         sync.RWMutex               // guards the fields below
    URI        lsp.DocumentURI
    Version    int32                      // monotonic, from client

    Source     []byte
    Tree       *gotreesitter.Tree
    Document   *mdpp.Document             // includes Root, Frontmatter, link defs

    LineIndex  []uint32                   // byte offsets of line starts
    DefIndex   *DefinitionIndex           // lazy; nil after invalidation

    LintResult []lint.Diagnostic
    LintVersion int32                     // Version at which LintResult was computed
    LintTimer  *time.Timer                // debounce timer; may be nil

    Degraded   bool                       // true if last parse failed to produce a clean AST
}
```

Lock ordering: always acquire `DocumentStore.mu` first, then `OpenDocument.mu`. Never the other way. Readonly handlers briefly take `DocumentStore.mu.RLock` to look up the document, release, then take the per-document lock — this prevents map reshaping from blocking per-document operations.

**Parser lifecycle is not the LSP's responsibility.** The engine's `parser_pool.go` provides per-language parser pooling shared across all `OpenDocument`s. The `DocumentStore` does not allocate, retain, or release parsers; `OpenDocument` carries `Tree` (the parse tree, which the LSP does retain across keystrokes for `ParseIncremental` reuse) but no `Parser` field. When `didChange` fires, the handler hands `(newSrc, oldTree)` to the engine, and the engine's pool dispenses a parser, runs `ParseIncremental`, and returns it to the pool. This keeps `OpenDocument` purely state — bytes, tree, derived indices — and lets the engine optimize parser reuse across the entire LSP process without coordination from this layer.

### 7.2 Lifecycle

- `didOpen`: create `OpenDocument`, insert into map under `DocumentStore.mu.Lock`.
- `didChange`, `didSave`, hover, definition, etc.: `DocumentStore.mu.RLock` → look up → release → per-doc lock.
- `didClose`: `DocumentStore.mu.Lock` → delete from map → release. Any in-flight handler already holding the per-document lock finishes and drops its reference naturally.

### 7.3 Memory budget

A 100k-char document with full AST + line index + lint result is ~2–5 MB. 50 open documents ~100–250 MB. No paging, no LRU in v0.1; a user with 50 markdown files open in one workspace is already unusual. If it becomes a concern, add an LRU eviction for documents the editor has implicitly closed (common with VS Code's preview-mode tabs).

### 7.4 Cleanup and leak paths

Every `didClose` must delete. If the client crashes without sending `didClose`, the server eventually exits on EOF from stdin and all memory is freed. No long-running leak path.

Timers: the lint debounce timer must be stopped on `didClose`; otherwise it fires against a freed document. Guard with a nil check in the fire callback (the document lookup returns nil and the callback returns early).

---

## 8. Performance budgets and harness

### 8.1 Budgets

The numbers below remain achievable as designed. gotreesitter's `ParseIncremental` already meets the per-keystroke budget by an order of magnitude (1.49μs on single-byte edits per the engine README), and the engine's new `parser_pool.go` removes parser-allocation overhead from the hot path entirely — pool checkout is a `sync.Pool`-style amortized constant. No budget changes are needed in light of the engine progress; the budgets are reaffirmed.


| Operation | Document size | Budget |
|---|---|---|
| `didChange` handler (parse + state update) | 100k chars | <2ms p99 |
| `publishDiagnostics` debounce | — | 250ms after last change |
| `lint.Lint` (inside diagnostics) | 100k chars | <50ms |
| `textDocument/hover` | 100k chars | <50ms p99 |
| `textDocument/definition` | 100k chars | <10ms |
| `textDocument/foldingRange` | 100k chars | <20ms |
| `textDocument/documentSymbol` | 100k chars | <30ms |
| `textDocument/formatting` | 100k chars | <200ms |
| `textDocument/semanticTokens/full` | 100k chars | <100ms |
| `textDocument/semanticTokens/range` | 100k chars | <30ms |
| Live preview (parse + render) | 100k chars | <100ms |
| `didChange` per-keystroke (cold line index update) | 100k chars | <5ms p99 |

### 8.2 Perf test harness

`lsp/perf_test.go` contains:

- A synthetic corpus: 1k-char, 10k-char, and 100k-char Markdown++ documents covering every node type (generated from the conformance suite).
- Per-operation benchmarks using `testing.B`. Each benchmark runs 10,000 iterations after warmup.
- A regression threshold per op: CI fails if the benchmark exceeds 120% of the last green run's median. The threshold lives in `lsp/perf_baseline.json`, updated intentionally when an op's cost legitimately changes.

The harness is reused by F (editor integrations) for the live-preview latency test, since preview = parse incremental + render.

---

## 9. Integration test harness

Editor-free, in-process. The goal: exercise every protocol method with real LSP messages as if a conformant editor were driving, without any editor binary. Manual editor testing is reserved for smoke validation; every behavior has an automated test in this harness.

### 9.1 Shape

`lsp/harness/client.go` implements a minimal JSON-RPC client that:

- Runs the server in-process via `io.Pipe` pairs (no subprocess).
- Sends framed JSON-RPC messages on one pipe; reads on the other.
- Exposes a `Client` type with methods `Initialize(...)`, `DidOpen(uri, text)`, `DidChange(uri, edits)`, `Hover(uri, pos)`, etc.
- Collects pushed notifications (`publishDiagnostics`) into a channel for assertions.
- Supports synthesis of multi-edit `didChange` sequences, deliberate UTF-16 boundary cases, and parse-error scenarios.

### 9.2 Test categories

- **Protocol correctness.** `initialize` returns the expected capabilities; `shutdown` drains; `exit` returns the right status.
- **Sync correctness.** A reference interpreter (apply edits as plain-text replaces) is compared against the server's stored `Source` after every change. Property test with random edit sequences.
- **Semantic correctness.** For each conformance example, expected hover output / definition target / document symbol shape is recorded as a golden; the harness asserts equality.
- **Semantic tokens golden.** For each conformance example, the expected encoded token stream (decoded to a human-readable form) is recorded as a golden; the harness asserts equality.
- **Performance regression.** As described in §8.2.

### 9.3 Why this matters

VS Code, Neovim, Helix, Zed, Emacs — validating in each is slow and manual. The harness lets us iterate on the server confidently. Manual editor checks in v0.1 are reduced to "does it actually activate and light up" smoke tests for VS Code + Neovim + Helix (per the roadmap's §7.1).

---

## 10. Error handling and internal diagnostics

LSP distinguishes two failure classes: errors in the user's document (which become `publishDiagnostics`) and errors in the server itself (which become JSON-RPC error responses or, for notifications, log lines).

### 10.1 JSON-RPC errors

Return `ResponseError` only when the protocol requires it:

- `InvalidRequest` (-32600): malformed request shape.
- `MethodNotFound` (-32601): unsupported method (stretch methods return this if not compiled in).
- `InvalidParams` (-32602): required fields missing, rename target invalid, etc.
- `InternalError` (-32603): server panic (recovered), file I/O failure.
- `ServerCancelled` (-32802): request cancelled by `$/cancelRequest`.
- `ContentModified` (-32801): document changed while we were computing; returned from `hover`, `definition`, etc. if the document's `Version` changed during the handler.

For unimplemented optional methods in the MVP column, always return `MethodNotFound` rather than silently succeeding — this helps clients fall back.

### 10.2 Panics

Every top-level handler goroutine defers a `recover()`. On panic:

1. Log the stack with full context (method, URI, position, document length).
2. Return `ResponseError.InternalError` to the requester.
3. Continue serving — do not let a single handler crash the process.

The server does not auto-restart on panic; the editor does that. But single-panic isolation keeps long sessions healthy through buggy edits.

### 10.3 `window/logMessage` and `window/showMessage`

Internal errors surface to the user sparingly — they're noisy. We use:

- `window/logMessage` (level: `Log` or `Info`) for routine events: parse duration, edit count, cache rebuild. Off by default; enabled via `initializationOptions.trace`.
- `window/logMessage` (level: `Error`) for panics and recovered internal errors. Always on.
- `window/showMessage` (level: `Error`) for user-visible failures that block work (e.g., format failed). Rare.

### 10.4 Stale document state

Two recovery paths:

1. **`ContentModified` on read handlers.** If the document version changes mid-handler, return that error. The client retries.
2. **`DegradedParse` mode.** If a parse fails, serve from last-good AST with a note in diagnostics. Editors tolerate this gracefully — they already show the last-good outline when the user is mid-edit.

---

## 11. Configuration

`InitializeParams.initializationOptions` is free-form JSON. We accept the following shape:

```json
{
  "markdownPlusPlus": {
    "preview": {
      "enabled": true,
      "autoRefresh": true
    },
    "lint": {
      "disabledRules": ["MDPP010", "MD013"],
      "severityOverrides": {
        "MDPP011": "warning"
      }
    },
    "format": {
      "onSave": false
    },
    "semanticTokens": {
      "disabled": false
    },
    "trace": "off"
  }
}
```

- `preview.enabled` — whether the server should send `mdpp/renderPreview` notifications with pre-rendered HTML. Default false; VS Code extension sets it true.
- `preview.autoRefresh` — whether to auto-render on change. Default true. Debounce matches diagnostics (250ms).
- `lint.disabledRules` — array of rule codes to skip.
- `lint.severityOverrides` — per-rule severity override.
- `format.onSave` — whether the server should apply formatter on save (most clients handle this themselves; included for completeness).
- `semanticTokens.disabled` — kill switch for the highlighter in case a client's theme conflicts.
- `trace` — `"off"` | `"messages"` | `"verbose"`. Standard LSP trace control.

Per-workspace overrides also accepted via `workspace/didChangeConfiguration` if the client supports it (Stretch — we register the notification but handle only the subset above).

---

## 12. Custom protocol extensions

LSP permits servers to define custom request/notification methods under a server-specific prefix. We reserve `mdpp/` for ours.

- **`markdownpp/renderPreview` (request, client→server) — D owns this.** Originally noted by F's sub-spec as an open issue ("how does the webview get rendered HTML without an HTTP round-trip?"); D now owns registering and serving it. Used by F's VS Code webview for live preview without going through HTTP or a second engine instance.
  - **Request:** `{ "uri": "<DocumentURI>" }`. The client identifies the open document; the server already holds the parsed `*mdpp.Document` and renders from it directly.
  - **Response:** `{ "html": "<string>" }`. The fully rendered HTML body (no surrounding `<html>`/`<head>` chrome — the webview wraps the body in its own document shell with theme CSS).
  - **Behavior:** synchronous. Server takes `RLock` on the document, calls `mdpp.Render(doc.Document, RenderOptions{})`, releases, returns. No caching at this layer; the document store already serves a parsed AST and rendering is sub-100ms per the budgets in §8.1.
  - **Errors:** `InvalidParams` if `uri` is missing or refers to a document not in the store. `InternalError` if `Render` returns an error.
  - **Trigger cadence:** the F webview calls this on every meaningful document change (debounced at the F layer, typically 100–250ms). The server does not push updates; F polls.
- **`mdpp/renderPreview` (notification, server→client, Stretch but designed now).** Complementary push variant of the above for clients that prefer subscribe semantics over polling. Sent when preview is enabled and the document has re-parsed. Payload: `{ uri, html, frontmatter, tocEntries, version }`. The VS Code extension's webview will likely use the request form (`markdownpp/renderPreview`) for explicit fetches and ignore this push notification; other clients can pick whichever shape suits them. Other editors can ignore both.
- **`mdpp/outline` (request, client→server, deferred).** Synonym for `documentSymbol` in the `mdpp` namespace, reserved in case we want a richer outline shape later without breaking the LSP symbol contract.

No other custom methods in v0.1. Keep the surface minimal; custom methods are a maintenance burden because we own compatibility without LSP's help.

---

## 13. Open questions

These are real decisions but recoverable after v0.1 ships; listing here so they surface in implementation or first-use feedback.

**Precondition (not an open question — restated for the implementer who reads this section first).** `Node.Range` on every AST node is a hard precondition for D. See §0. No method below `initialize` can be implemented until A delivers it. This is not a recoverable post-v0.1 issue; it is a blocker on the start of D.

1. **Workspace-wide symbol index.** Cross-file `definition` / `references` / `rename` require it. Scope: should we index every `.md` under `rootUri` on startup, or lazily as files are opened? Lazy is cheaper, eager is more responsive. Deferred to Stretch; default is single-file in v0.1.
2. **`semanticTokens/full/delta` support.** The spec permits incremental delta encodings. Adding this would halve the token-stream bandwidth on large docs with frequent edits. Not required for any performance target we've committed to; ship only if VS Code's telemetry shows semantic-token bandwidth as a regression.
3. **Preview rendering in the server vs. the client.** Server-side rendering (via `mdpp/renderPreview`) keeps the render path canonical but doubles server work. Client-side (let the VS Code webview call the engine via wasm or a binary path) is more work in F. Proposal: server-side for v0.1; revisit post-launch based on editor-specific performance.
4. **Frontmatter YAML deep editing affordances.** Hover-on-key could list allowed values; completion-in-value could suggest reserved key values (`toc: true|false`, `mdpp: 0.1`). Attractive but bleeds into YAML LSP territory. Deferred.
5. **Embedded-language injection behavior.** Fenced code blocks with language tags already route to gotreesitter injection. Should the LSP forward `hover` / `definition` for positions inside a code fence to a child language server (e.g., `gopls` for a `go` fence)? Answer for v0.1: no. Markdown++ LSP handles markdown-level semantics; code fences are opaque content. A later minor version could add forwarding for specific languages.
6. **`textDocument/linkedEditingRange`.** Would be nice for paired edits (e.g., editing both halves of `[text][ref]` — `[ref]:` simultaneously). Not in MVP; revisit if users ask.
7. **Multi-workspace root support.** LSP 3.17 supports `workspaceFolders`. v0.1 uses only `rootUri`. Upgrade path is straightforward but untested.
8. **Cancellation granularity.** We check cancellation at natural yield points. If a specific slow handler is reported (e.g., formatting on a pathologically large doc), insert finer checks.

---

## 14. Summary

The LSP is the protocol glue between the grammar-backed engine and every editor. The architecture is straightforward: in-process engine import, per-document state, incremental sync powered by gotreesitter's sub-microsecond `ParseIncremental`, a single cursor pass for semantic tokens. The protocol surface is deliberately paced — MVP covers the operations that carry the differentiator story, Stretch extends into refactoring territory, and a specific drop order protects the launch if D runs long.

The two hardest implementation areas are incremental sync (§6) and the semantic-tokens walker (§5.5). Both are testable in isolation against the conformance corpus using the in-process harness in §9. Every performance budget has a perf-test owner in §8.

Ship the MVP column. Layer Stretch post-v0.1 as organic feedback dictates. Never ship capabilities we don't implement.

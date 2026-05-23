# Sub-project F: Editor Integrations — Design Spec

**Status.** Draft
**Date.** 2026-04-19
**Owner.** Oscar Villavicencio (m31labs)
**Parent.** [Markdown++ Roadmap](./2026-04-19-markdown-plus-plus-roadmap-design.md) §4.6
**Scope.** The distribution surface for the Markdown++ authoring stack: a VS Code extension (primary launch vehicle), one-page configs for other major LSP-capable editors (Neovim, Helix, Zed, Emacs), the mechanism for getting the `mdpp-lsp` binary onto users' machines, and the live-preview architecture that lets authors see their rendered document beside the source. Does **not** cover the LSP protocol itself (that is D) or the engine (that is A). Treats D's protocol surface as frozen for the purposes of this spec.

---

## 0. Progress snapshot (as of 2026-04-19)

F has not started. No code exists in `mdpp-vscode` (the repo itself has not been created yet); this sub-spec remains the design target. Awaiting VS Code repo creation as the first concrete next action.

**Engine progress that affects F.** A burst of engine shipping landed today; two changes alter F's webview surface, and two F-blocking dependencies are still outstanding.

*Shipped (preview must render):*
- **`[[toc]]` directive** → emits `NodeTableOfContents`, rendered as nested heading lists with anchor links (`<a href="#heading-id">`). The webview will receive these inside the rendered HTML payload and must style them. `preview.css` checklist updated below (§5).
- **`[[embed:url]]` directive** → emits `NodeAutoEmbed`, rendered with provider-specific CSS classes (`mdpp-embed`, `mdpp-embed-youtube`, `mdpp-embed-vimeo`, `mdpp-embed-generic`, etc.) plus `data-src`, `data-provider` attributes and a fallback `<a>` link inside the wrapper. The webview must style these wrappers and may optionally upgrade them to live embeds via `preview.js` (§5).

*NOT shipped (F-blocking upstream):*
- **PDF rendering via `chromedp`** is not yet in the engine. F's `markdownpp.exportPdf` command depends on A's PDF pipeline (the command shells out to `mdpp render --pdf`). Until A lands the chromedp path, this command cannot function. Restated as an upstream blocker; §7.2 updated to either hide the command via `enablement` or surface a "PDF rendering coming soon" notification.
- **`Node.Range` (start/end byte offsets on every AST node)** is still not on the AST. F's editor↔preview scroll sync (§5.4) depends on the rendered HTML carrying `data-source-range` (or `data-line`) attributes that map elements back to source positions. That data path requires the engine to attach byte ranges to nodes, the renderer to pass them through as attributes, and the LSP's `markdownpp/renderPreview` response to surface them. Restated as an upstream chain-of-blockers for scroll sync. The rest of preview (open, render, swap on change, theming, math, mermaid) can ship without it; only scroll sync waits.

---

## 1. Purpose and scope

Editor integrations are the *launch surface*. The engine, formatter, linter, and LSP are all invisible in isolation; authors meet Markdown++ through whatever editor they already use. F is the thin, per-editor layer that delivers the full stack into each of those environments.

F has two deliverables at v0.1:

1. **`mdpp-vscode`** — a TypeScript VS Code extension published to the VS Code Marketplace under the `m31labs` publisher account. This is the primary launch vehicle; the GIFs in the README and the "install" story on the landing page both point here.
2. **Editor recipes** — one-page install guides in the main `mdpp` repo at `docs/editors/{neovim,helix,zed,emacs}.md`. These are not code, just documentation: configuration snippets plus a "how to verify it works" section.

The asymmetry is deliberate. VS Code dominates editor market share; the Marketplace listing is a first-class marketing surface (install count, ratings, Q&A tab); and VS Code's webview API enables a live preview we cannot replicate elsewhere without per-editor custom work. Neovim, Helix, Zed, and Emacs users already configure LSP clients by hand — shipping them a recipe is the idiomatic delivery mechanism in those ecosystems, not a cop-out.

What F is **not**:

- Not a reimplementation of any engine behavior in TypeScript. All rendering, linting, and formatting stay in Go, reached via the LSP or by spawning the `mdpp` CLI.
- Not a custom WYSIWYG authoring surface. The editor is whatever the user already uses; our side-by-side live preview is the answer to the "see it rendered" need.
- Not a plugin ecosystem. We ship one official extension and a set of configuration recipes. Third-party extensions/forks are welcome but unsupported.

---

## 2. Repo shape: `mdpp-vscode`

### 2.1 Why a separate repo

The roadmap already locked this (§5). Restated in one paragraph so this spec stands alone: the VS Code Marketplace toolchain expects a TypeScript project with a `package.json` at the repo root, its own `CHANGELOG.md` feeding the Marketplace "Changelog" tab, its own versioning cadence (extension versions and engine versions evolve differently), and its own publisher signature. Cramming those artifacts into the Go repo would force every `go build` onlooker to wade past JavaScript tooling. Keeping them separate gives each ecosystem idiomatic tooling — `go install` pulls the binaries; `vsce` publishes the extension; neither trips over the other.

Repo: `github.com/odvcencio/mdpp-vscode`. Marketplace publisher: `m31labs`. License: matches the main repo (whatever the main repo chooses; typically MIT).

### 2.2 Directory layout

```
mdpp-vscode/
├── README.md                           # Marketplace-friendly landing
├── CHANGELOG.md                        # required by Marketplace "Changelog" tab
├── LICENSE
├── package.json                        # extension manifest
├── package-lock.json
├── tsconfig.json
├── .vscodeignore                       # excludes dev files from packaged .vsix
├── .vscode/
│   └── launch.json                     # F5 to launch Extension Dev Host
├── src/
│   ├── extension.ts                    # activate() / deactivate() entrypoints
│   ├── lspClient.ts                    # vscode-languageclient setup
│   ├── preview.ts                      # live-preview WebviewPanel provider
│   ├── binaryDownload.ts               # fetch mdpp-lsp from GH Releases
│   ├── commands.ts                     # render-html, export-pdf, open-preview, restart-server
│   ├── config.ts                       # typed wrapper over vscode.workspace.getConfiguration
│   └── logger.ts                       # OutputChannel logging
├── syntaxes/
│   └── markdown-plus-plus.tmLanguage.json  # fallback TextMate grammar
├── images/
│   ├── icon.png                        # 128x128 Marketplace icon
│   ├── screenshot-hover.png
│   ├── screenshot-rename.png
│   ├── screenshot-codeaction.png
│   ├── screenshot-format.png
│   ├── screenshot-preview.png
│   └── screenshot-pdf.png
├── webview-assets/
│   ├── preview.css                     # live-preview styles (VS Code theme-aware)
│   ├── preview.js                      # scroll sync, mermaid hookup
│   └── mermaid.min.js                  # bundled locally (no CDN)
├── test/
│   ├── suite/
│   │   ├── extension.test.ts           # activation, command registration
│   │   ├── lsp.test.ts                 # integration against real mdpp-lsp
│   │   └── preview.test.ts             # webview lifecycle
│   └── runTest.ts                      # @vscode/test-electron harness
└── scripts/
    ├── prepare-release.mjs             # updates manifest SHAs, bumps versions
    └── build-binaries.mjs              # CI helper for bundling per-platform .vsix
```

The `syntaxes/` TextMate grammar is intentionally thin. It covers the subset of Markdown++ tokens that need to look right during the roughly 200ms between file open and first semantic-tokens response from the LSP. Once the LSP responds, its semantic tokens take over. We do not try to match LSP fidelity in the TextMate grammar; it exists to avoid a "flash of unstyled text" at open time.

### 2.3 `package.json` essentials

Full manifest is produced in implementation; the shape is fixed here:

```jsonc
{
  "name": "markdown-plus-plus",
  "displayName": "Markdown++",
  "description": "Markdown++: LSP, formatter, linter, and live preview for the Markdown++ authoring surface.",
  "version": "0.1.0",
  "publisher": "m31labs",
  "license": "MIT",
  "icon": "images/icon.png",
  "engines": { "vscode": "^1.85.0" },
  "categories": ["Programming Languages", "Linters", "Formatters", "Other"],
  "keywords": ["markdown", "lsp", "linter", "formatter", "preview", "pdf"],
  "repository": { "type": "git", "url": "https://github.com/odvcencio/mdpp-vscode" },
  "bugs": { "url": "https://github.com/odvcencio/mdpp-vscode/issues" },
  "homepage": "https://markdownpp.m31labs.dev",
  "qna": "marketplace",
  "pricing": "Free",
  "activationEvents": [
    "onLanguage:markdown",
    "onLanguage:markdown-plus-plus"
  ],
  "main": "./out/extension.js",
  "contributes": {
    "languages": [{
      "id": "markdown-plus-plus",
      "aliases": ["Markdown++", "markdown-plus-plus"],
      "extensions": [".md"],
      "configuration": "./language-configuration.json"
    }],
    "grammars": [{
      "language": "markdown-plus-plus",
      "scopeName": "text.html.markdown.mdpp",
      "path": "./syntaxes/markdown-plus-plus.tmLanguage.json"
    }],
    "commands": [
      { "command": "markdownpp.renderHtml", "title": "Markdown++: Render to HTML" },
      { "command": "markdownpp.exportPdf", "title": "Markdown++: Export to PDF" },
      { "command": "markdownpp.openPreview", "title": "Markdown++: Open Live Preview" },
      { "command": "markdownpp.openPreviewToSide", "title": "Markdown++: Open Preview to the Side" },
      { "command": "markdownpp.restartServer", "title": "Markdown++: Restart Language Server" }
    ],
    "menus": {
      "editor/title": [
        {
          "when": "resourceLangId == markdown-plus-plus",
          "command": "markdownpp.openPreviewToSide",
          "group": "navigation"
        }
      ]
    },
    "configuration": { /* see §6 */ }
  },
  "dependencies": {
    "vscode-languageclient": "^9.0.1"
  },
  "devDependencies": {
    "@types/vscode": "^1.85.0",
    "@types/node": "^20.0.0",
    "@vscode/test-electron": "^2.3.0",
    "@vscode/vsce": "^2.24.0",
    "typescript": "^5.3.0"
  }
}
```

`engines.vscode: ^1.85.0` is chosen because it is the first release whose semantic-tokens API is stable enough for the custom token types enumerated in E. Older VS Code versions will be refused by the Marketplace, which is acceptable — authors on 1.85+ cover the overwhelming majority of the install base by launch.

`activationEvents` lists both `markdown` (because of §3.2 "claim the markdown language ID by default") and `markdown-plus-plus`. On extension load, `extension.ts` inspects `config.takeOverMarkdownLanguage`; if false, it skips the takeover — but the activation event still fires, so the command palette entries work regardless.

---

## 3. LSP client wiring

The extension is a thin `vscode-languageclient` host. `lspClient.ts` owns the client lifecycle.

### 3.1 ServerOptions

The binary path is resolved in this order:

1. `markdownPlusPlus.languageServer.path` setting, if non-empty and the file exists and is executable. (Escape hatch for developers working against a local build.)
2. The extension's global-storage path (`context.globalStorageUri`), which is where `binaryDownload.ts` writes the downloaded binary (see §4).
3. Fall back to a PATH lookup for `mdpp-lsp`. If PATH does not contain it either, surface a download prompt.

```ts
const serverOptions: ServerOptions = {
  run:   { command: binaryPath, transport: TransportKind.stdio },
  debug: { command: binaryPath, transport: TransportKind.stdio,
           args: ["--log-level=debug"] }
};
```

### 3.2 ClientOptions

```ts
const clientOptions: LanguageClientOptions = {
  documentSelector: [
    { scheme: "file", language: "markdown-plus-plus" },
    { scheme: "file", language: "markdown" }   // only if takeOverMarkdownLanguage
  ],
  synchronize: {
    fileEvents: workspace.createFileSystemWatcher("**/*.md")
  },
  initializationOptions: readConfigForInit(),
  outputChannel: logger.outputChannel,
  traceOutputChannel: logger.traceChannel
};
```

`documentSelector` is built dynamically from `takeOverMarkdownLanguage`: if disabled, only the `markdown-plus-plus` entry is registered. This is the mechanism that lets users keep the stock Markdown extension on their `.md` files in some workspaces while adopting Markdown++ only where they've opted in (e.g., a workspace with a `.vscode/settings.json` override).

`initializationOptions` is the vehicle for forwarding configuration to the LSP (see §6). Settings that the LSP needs at boot (e.g., `lint.disabledRules`) are serialized here. Settings that change frequently (e.g., `format.onSave`) are re-synced via `workspace/didChangeConfiguration` notifications.

### 3.3 Restart behavior

`markdownpp.restartServer` invokes `client.stop()` then constructs a new client with a re-resolved binary path. Useful when the user edits `languageServer.path` or manually drops a new binary into the storage location. Also useful during development.

On `stop()` failure (server hung), we escalate to `client.stop(5000)` timeout then hard-kill the child process. The LSP must survive being killed without corrupting workspace state; D's sub-spec owns that guarantee.

---

## 4. LSP binary distribution

**This is the trickiest part of the extension.** The LSP is a Go binary; VS Code extensions are expected to be self-contained JavaScript packages. Bridging that requires either bundling the binary (which explodes package size) or downloading it post-install (which introduces a network dependency and failure modes). We choose download.

### 4.1 Approach: download on first activation

`binaryDownload.ts` is invoked from `activate()`. Its contract:

1. Read the expected version from a bundled `manifest.json` shipped inside the extension. This manifest also lists SHA256 checksums per `{platform, arch}` tuple.
2. Read the currently-installed binary's version (by running `mdpp-lsp --version` in the storage location, if the file exists).
3. If the version matches, return the path immediately.
4. If no binary is installed, or the version is outdated, download it.
5. Verify the SHA256.
6. Extract to storage path.
7. `chmod +x` on Unix. Windows needs no permission bit; the `.exe` suffix is in the archive.

### 4.2 Download source

GitHub Releases on the main `mdpp` repo:

```
https://github.com/odvcencio/mdpp/releases/download/v<X.Y.Z>/mdpp-lsp-<platform>-<arch>.tar.gz
```

Where `<platform>` is one of `darwin`, `linux`, `windows` (mapping from `process.platform` with `win32` → `windows`), and `<arch>` is one of `x64`, `arm64` (no 32-bit support at v0.1).

Windows is distributed as `.zip` rather than `.tar.gz` because tar is not universally present on pre-WSL Windows. The code handles both formats.

### 4.3 SHA256 verification

The extension bundles `manifest.json`:

```json
{
  "binaryVersion": "0.1.0",
  "binaries": {
    "darwin-x64":   { "sha256": "abc123…", "size": 12345678 },
    "darwin-arm64": { "sha256": "def456…", "size": 12345678 },
    "linux-x64":    { "sha256": "789abc…", "size": 12345678 },
    "linux-arm64":  { "sha256": "…",       "size": 12345678 },
    "windows-x64":  { "sha256": "…",       "size": 12345678 }
  }
}
```

`scripts/prepare-release.mjs` regenerates this file from the GitHub release assets during the extension's release process. Hand-editing is discouraged; CI enforces that `manifest.json` matches real release artifacts before publishing the extension.

The SHA check is non-negotiable. A failed SHA aborts activation and surfaces an error telling the user to open an issue — we never fall back to "use the downloaded-but-unverified binary."

### 4.4 Error UX

Download can fail for four canonical reasons. Each needs a clear error path:

1. **No network.** Show a notification with "Retry" and "Open Manual Install Docs" buttons. The manual install doc (`docs/install-mdpp-lsp-manually.md` on the main repo) walks through downloading the archive and placing it at `languageServer.path`.
2. **GitHub rate-limit (HTTP 403).** Same treatment; extra note in the notification that unauthenticated GH downloads are rate-limited per IP and the user can wait or authenticate.
3. **SHA256 mismatch.** Hard error: "Downloaded binary does not match expected checksum. Refusing to run. Please report at <issues link>." No retry button — something is wrong.
4. **Extraction failure (disk full, permissions).** Surface the system error plus manual install docs.

The first activation never blocks editor startup on the download; the extension activates, registers commands, shows the download progress in the status bar, and lets the LSP start once the binary is ready. Files opened before then get TextMate-grammar highlighting only. This is acceptable because download completes in seconds on any reasonable connection.

### 4.5 Alternative rejected: bundle the binary in the `.vsix`

Considered and rejected. Bundling all five `{platform, arch}` binaries directly in the extension would produce a `.vsix` north of 150MB. The Marketplace does not hard-cap size, but large extensions hurt install-time UX, review queue friendliness, and the "quick install" pitch. Per-platform extensions (`mdpp-vscode-darwin-arm64`, `mdpp-vscode-linux-x64`, etc.) are a real pattern — rust-analyzer does it — but proliferate publisher-side complexity for a v0.1 launch. Deferred to post-launch if the download flow proves fragile in the field.

### 4.6 Update flow

When the extension is updated (either automatically by VS Code or manually), `activate()` runs against the new bundled `manifest.json`. If `binaryVersion` changed, the version check in §4.1 fails, and a fresh download begins. The old binary is replaced atomically (download to a sibling path, `fs.rename` over the live binary after client shutdown). LSP sessions survive extension reloads because the client is re-constructed; no open documents are lost.

Backwards compatibility: an extension release may keep the same `binaryVersion` if only TS code changed; it may bump `binaryVersion` without bumping extension version semantically if only the binary changed. Both are legal. The LSP protocol itself is forward/backward compatible at minor versions (per D's protocol contract), so an older extension run against a newer binary keeps working — this matters if a user pins the extension version but `go install` picks up a newer `mdpp-lsp`.

---

## 5. Live preview architecture

### 5.1 Decision: native webview, not built-in MD preview

VS Code ships a built-in markdown preview (the `vscode-markdown` extension's `markdown-preview-enhanced`-ish surface). We do **not** extend it. Reasons:

1. The built-in preview uses the `markdown-it` engine. It does not understand `:::` containers, admonition syntax with custom titles, our emoji table, our math extensions, or our frontmatter-driven behaviors. Extending it would mean re-implementing pieces of our engine in JavaScript — exactly the duplication the roadmap rules out.
2. Our engine already produces the HTML we want. Piping that HTML straight into a webview eliminates a whole category of fidelity mismatch between "what the preview shows" and "what the published HTML looks like."
3. A dedicated webview gives us full control over scroll sync, CSP, theming, and diagnostics overlays in ways the built-in preview's extension points do not.

Downside: we do not inherit the built-in preview's existing features (print-to-PDF button, "open in browser" link). We replicate only those we need. `Export to PDF` is a command, not a preview button, because it goes through the engine's chromedp path rather than the webview.

### 5.2 WebviewPanel lifecycle

One webview panel per editor document, created on demand:

- `markdownpp.openPreview` opens the panel in the active editor group.
- `markdownpp.openPreviewToSide` opens it in a split to the right.

If a panel already exists for the document, the command reveals it rather than creating a duplicate. Closing the preview tab disposes the panel; reopening creates a fresh one.

One-global-panel was considered and rejected: users routinely have multiple documents open, and "preview follows the active editor" is a UX tax (what happens when I click away?).

### 5.3 Content pipeline

```
VS Code editor buffer
   │
   ▼ onDidChangeTextDocument (debounced 100ms)
Extension sends custom request 'markdownpp/renderPreview'
   │         { uri, withSourceMap: true }
   ▼
mdpp-lsp renders via engine Render()
   │         returns { html, sourceMap }
   ▼
Extension wraps html in <html> scaffold + preview.css + preview.js
   │
   ▼ webview.postMessage({ type: 'update', html, sourceMap })
preview.js swaps innerHTML; updates scroll-sync map
```

The 100ms debounce is tight enough to feel live (well inside perception-as-instant) and loose enough to avoid thrashing the LSP on rapid keystrokes. This matches the roadmap's §4.6 "under 100ms from keystroke" target.

`markdownpp/renderPreview` is a custom LSP request, not a standard method. D's sub-spec owns registering it on the server side. The server-side implementation is a one-liner over the engine's `Render` function.

Source maps: the engine emits rendered elements with `data-line="N"` attributes (or a range form for multi-line constructs). The webview uses these for scroll sync.

### 5.4 Scroll sync

Bidirectional, both directions optional (controlled by `preview.scrollSync.enabled`):

- **Editor → preview.** On `onDidChangeTextEditorVisibleRanges`, the extension computes the topmost visible source line and sends it to the webview. `preview.js` finds the first element with `data-line >= N` and scrolls it to the top.
- **Preview → editor.** `preview.js` listens for scroll events, debounces, posts `{ type: 'revealLine', line: N }` to the extension. The extension calls `editor.revealRange` to center that line.

Click-to-jump: clicking any rendered element in the preview jumps the editor cursor to that line. Implementation is trivial given the `data-line` attributes.

### 5.5 Math rendering

Server-rendered. The engine's LaTeX → HTML path produces MathML (or KaTeX-style spans, per A's decisions). The webview just displays the HTML; no client-side math engine ships. This keeps the webview payload tiny and guarantees preview/PDF/HTML all render identically.

### 5.6 Diagram rendering

Mermaid diagrams are the interesting case. The engine emits a structured node (just the source text, not rendered output — rendering Mermaid in Go would mean embedding a Go JavaScript engine, which we are not doing). The webview renders it using a locally-bundled `mermaid.min.js` from `webview-assets/`.

No CDN loads. The Marketplace explicitly discourages extensions that pull runtime resources from external origins; our CSP forbids it.

For HTML and PDF output, Mermaid rendering is out of scope for v0.1 — the static HTML artifact emits a `<pre class="language-mermaid">` containing the source. A post-processor (e.g., a build step in a static-site pipeline) can render those if needed. A full "Mermaid in HTML output" story is deferred to post-v0.1.

### 5.7 Theme

The webview's body inherits the VS Code theme via the extension setting `window.activeColorTheme` → `vscode-light` / `vscode-dark` / `vscode-high-contrast` CSS classes. `preview.css` defines rules per class. Matches the VS Code editor look at a glance; authors can visualize the final HTML in their preferred mode.

Post-v0.1: the `markdownPlusPlus.preview.theme` setting will eventually drive a custom theme system decoupled from VS Code's (see deferred items in roadmap §1.5). For now, the setting exists but only accepts `vscode-default`.

### 5.8 Security

Webview content security policy is tightly locked:

```
default-src 'none';
img-src ${webview.cspSource} data: https:;
style-src ${webview.cspSource};
script-src ${webview.cspSource} 'nonce-<random>';
font-src ${webview.cspSource};
```

- No `eval`, no inline scripts except via nonce, no `unsafe-inline` styles.
- Images allow `https:` because the rendered HTML may legitimately contain remote image URLs from the user's document. This is the same trust level the published HTML would have.
- All our assets (`preview.css`, `preview.js`, `mermaid.min.js`) are loaded through `webview.asWebviewUri` and thus satisfy `${webview.cspSource}`.

`enableScripts: true` on the panel; `retainContextWhenHidden: true` so switching tabs does not reset scroll position and re-trigger a full re-render.

### 5.9 Webview asset checklist

The engine's HTML output already contains constructs the webview is responsible for styling and (in some cases) animating. Enumerated explicitly so nothing falls through the cracks during implementation:

**`preview.css` must style:**

- Standard prose: headings (with anchor offset for `:target` so heading-link clicks don't hide under the editor's tab bar), paragraphs, lists (ordered and unordered), blockquotes, tables, code blocks (fence info-string driven syntax-highlight classes), code spans, links, images, horizontal rules.
- Admonitions: `.mdpp-admonition`, `.mdpp-admonition-note`, `.mdpp-admonition-tip`, `.mdpp-admonition-warning`, `.mdpp-admonition-caution`, `.mdpp-admonition-important`, plus the inline title classes the renderer emits.
- Math: server-rendered MathML / KaTeX-style spans inherit color tokens; ensure display math has block spacing and inline math does not break line height.
- Footnotes: definition list at document foot; back-reference arrows; ref → def hover affordance via CSS only.
- Definition lists, super/subscript, strikethrough, task lists.
- **TOC blocks (shipped today):** `.mdpp-toc` wrapper plus nested `<ul>`/`<ol>` from the engine's `NodeTableOfContents` rendering. Anchor links inside the TOC use the engine's heading IDs (slug algorithm); style them as a list of links with appropriate indentation per level.
- **Auto-embed wrappers (shipped today):** `.mdpp-embed` base class plus provider-specific classes — `.mdpp-embed-youtube`, `.mdpp-embed-vimeo`, `.mdpp-embed-generic` (and any future providers A adds). Default styling: aspect-ratio box, neutral background, fallback `<a>` link styled visibly when no live embed is loaded.
- VS Code theme classes (§5.7): `vscode-light`, `vscode-dark`, `vscode-high-contrast` variants for everything above.

**`preview.js` must handle:**

- Receiving `update` messages from the extension (`html`, `sourceMap`) and swapping `innerHTML` (§5.3).
- Mermaid rendering via locally-bundled `mermaid.min.js` (§5.6).
- Scroll sync: editor → preview and preview → editor message handlers (§5.4). Blocked on `Node.Range` / source-range attributes flowing through the renderer (see §0); ship the rest of preview first, wire scroll sync once the data is available.
- Click-to-jump on rendered elements (§5.4). Same upstream dependency as scroll sync.
- **Embed upgrade (new):** auto-embed wrappers carry `data-src` and `data-provider`. By default the wrapper just shows the fallback `<a>` link inside it — that is acceptable v0.1 behavior. Optionally, `preview.js` may upgrade known providers to live embeds (e.g., YouTube via the YouTube iframe API at `https://www.youtube.com/embed/<id>`, Vimeo via `https://player.vimeo.com/video/<id>`). **Security implication:** loading third-party content from arbitrary origins inside the webview violates the strict CSP defined in §5.8. Two options: (a) leave embeds as fallback links in v0.1 (simplest, no CSP relaxation), or (b) load each upgraded embed inside a sandboxed `<iframe sandbox="allow-scripts allow-same-origin">` whose `src` is an extension-controlled HTML file that itself loads the third-party iframe — the outer CSP only needs to permit the extension's own origin. Option (a) is the v0.1 default; option (b) is a post-launch enhancement.
- TOC anchor navigation: clicks on `<a href="#heading-id">` inside `.mdpp-toc` should scroll the heading into view within the webview. Native browser behavior should handle this (the IDs are present on rendered headings), but if VS Code's webview eats the navigation event, fall back to a `click` handler that calls `element.scrollIntoView()` manually. Verify during implementation; flagged in §12.

The `markdownpp/renderPreview` custom LSP method remains required (§5.3). Server-side registration is owned by D — D's sub-spec has been updated to register it.

---

## 6. Configuration surface

All settings live under the `markdownPlusPlus.*` namespace. VS Code renders these in the Settings UI with the descriptions provided here.

| Key | Type | Default | Description |
|---|---|---|---|
| `markdownPlusPlus.languageServer.path` | string | `""` | Override path to `mdpp-lsp` binary. Empty means auto-download. |
| `markdownPlusPlus.languageServer.trace.server` | enum `"off" \| "messages" \| "verbose"` | `"off"` | LSP trace level for debugging. |
| `markdownPlusPlus.preview.scrollSync.enabled` | boolean | `true` | Bidirectional scroll sync between editor and live preview. |
| `markdownPlusPlus.preview.theme` | string | `"vscode-default"` | Preview theme. Reserved for future expansion. |
| `markdownPlusPlus.export.pdf.pageSize` | enum `"A4" \| "Letter" \| "Legal"` | `"A4"` | PDF page size. |
| `markdownPlusPlus.export.pdf.margin` | object | `{top:"1in",bottom:"1in",left:"1in",right:"1in"}` | PDF margins. |
| `markdownPlusPlus.export.pdf.defaultPath` | string | `""` | Default PDF output location. Empty means alongside source file. |
| `markdownPlusPlus.lint.disabledRules` | array of string | `[]` | Rule codes to suppress (e.g., `["MDPP010"]`). |
| `markdownPlusPlus.format.onSave` | boolean | `false` | Run `mdpp fmt` automatically on save. |
| `markdownPlusPlus.takeOverMarkdownLanguage` | boolean | `true` | Claim the `markdown` language ID in addition to `markdown-plus-plus`. |

### 6.1 Propagation to the LSP

Settings split into two classes:

- **Boot-time:** forwarded via `initializationOptions` on `initialize`. Examples: `lint.disabledRules`, `format.onSave`, anything the server needs to know up front.
- **Runtime:** forwarded via `workspace/didChangeConfiguration` when changed. Examples: toggling `lint.disabledRules` at runtime without a restart.

The LSP itself treats all options as optional and falls back to built-in defaults if unset; this keeps the server usable when invoked from non-VS-Code editors (which may not forward any configuration).

### 6.2 Settings that stay client-side

Some settings never reach the LSP because they are purely about editor UX:

- `languageServer.path` (used only to spawn the binary)
- `preview.scrollSync.enabled` (webview behavior)
- `preview.theme` (CSS class selection)
- `takeOverMarkdownLanguage` (extension activation logic)

---

## 7. Commands implementation

All commands live in `src/commands.ts`. Most shell out to the `mdpp` CLI rather than reaching into the LSP, because the CLI has an ergonomic surface for "produce an artifact now" and we do not want to duplicate that logic in TS.

### 7.1 `markdownpp.renderHtml`

```
1. Save the active document (prompt if unsaved, with confirm/cancel).
2. Spawn: mdpp render <path> --out <path>.html
3. On success: open the generated HTML in a new editor tab (or in an external browser via vscode.env.openExternal, user choice remembered).
4. On error: surface stderr in an information modal; offer to view the OutputChannel log.
```

### 7.2 `markdownpp.exportPdf`

```
1. Save the active document (as above).
2. Prompt for output location (default from export.pdf.defaultPath or alongside source).
3. Spawn: mdpp render <path> --pdf --out <pdf-path>
          --page-size=<cfg.pageSize> --margin=<cfg.margin serialized>
4. On success: open in OS default PDF viewer via vscode.env.openExternal.
5. On error: as above.
```

PDF export specifically does NOT go through the LSP because chromedp ownership lives in the engine (§A of the roadmap). The LSP has no reason to open a headless Chromium.

**Upstream dependency (as of 2026-04-19).** Engine PDF rendering via `chromedp` has not yet shipped (see §0 and roadmap §0). Until A lands `mdpp render --pdf`, this command cannot function. Two acceptable fallback behaviors during the gap, in order of preference:

1. **Hide via `enablement`.** Add an `enablement` clause on the `markdownpp.exportPdf` command contribution gated on a context key (e.g., `markdownPlusPlus.engineSupportsPdf`) that the extension sets to `false` at activation until the bundled binary version supports PDF. The command palette entry and editor-title button disappear cleanly; users do not see a feature that does not work.
2. **Surface a "PDF rendering coming soon" notification.** If hiding the command is awkward (e.g., already documented in launch GIFs), keep the command visible but, on invocation, short-circuit before the spawn step with `vscode.window.showInformationMessage("Markdown++ PDF export is shipping in an upcoming release. Track progress at <repo issue link>.")`.

Once A ships, the version-detection logic in `binaryDownload.ts` flips the context key true and the command activates without further extension changes.

### 7.3 `markdownpp.openPreview` / `markdownpp.openPreviewToSide`

See §5. Implementation is ~50 lines: construct a `WebviewPanel`, wire up message handlers, request the first render.

### 7.4 `markdownpp.restartServer`

```
1. client.stop() (with 5s timeout)
2. Construct a fresh LanguageClient and start()
3. Reconnect all file-change subscriptions
4. Show "Markdown++ server restarted" in the status bar for 2 seconds
```

### 7.5 Format on save

`markdownPlusPlus.format.onSave: true` does not use a VS Code command. Instead, `extension.ts` registers `workspace.onWillSaveTextDocument` and requests a `textDocument/formatting` LSP operation synchronously. The LSP's formatter (dispatching to B) returns edits; we apply them before the save completes. This is the standard VS Code formatter pattern.

---

## 8. Marketplace listing essentials

The Marketplace listing is itself a marketing surface (linked from `markdownpp.m31labs.dev`, from the README install button, from the launch post). Required fields:

- **`displayName`** — "Markdown++"
- **`description`** — One to two compelling sentences. Draft: *"Markdown++ turns any `.md` file into a first-class authoring experience: hover previews, rename refactoring, a real formatter and linter, a live side-by-side preview, and PDF export — all powered by a real grammar-backed AST."* Final copy lives in the launch-prep doc.
- **`icon`** — 128x128 PNG. Source file in `images/icon.png`.
- **README.md** — rendered as the Marketplace landing page. Must include:
  - One-paragraph differentiator (roadmap §1.2, restated)
  - GIFs: hover, rename, code action, format-on-save, live preview, PDF export. Same GIFs as the main repo README; they live in the main repo's `docs/gifs/` and are referenced by raw GitHub URL.
  - Install instructions (Marketplace button + `go install` + optionally Homebrew)
  - Feature table
  - Link to `markdownpp.m31labs.dev/spec` for the format specification
  - Link to main repo for issues
- **`repository`, `bugs`, `homepage`** — all `odvcencio`/`markdownpp.m31labs.dev` URLs
- **`qna: marketplace`** — questions go to Marketplace Q&A rather than GitHub issues (marginal preference; either works, but Q&A gives Marketplace signal)
- **`pricing: Free`**
- **`license`** — matches main repo (MIT expected)

### 8.1 CHANGELOG

`CHANGELOG.md` becomes the Marketplace "Changelog" tab. Required format: each version is an `## [X.Y.Z] - YYYY-MM-DD` heading followed by Added/Changed/Fixed bullets. Automated by `scripts/prepare-release.mjs` from git commit messages on release.

### 8.2 Submission timing

Marketplace has a review queue. It is usually quick (hours, occasionally a day), but not instant. We submit at least 24 hours before the intended launch moment. If the launch is the HN-plus-cross-post moment, the extension must be *installable* by that moment, not *submitted* by it.

---

## 9. Editor configs for non-VS-Code editors

Per-editor recipes published in the main `mdpp` repo at `docs/editors/*.md`. Each is a one-page guide with: install the binary, configure the editor, verify it works. The LSP works in all of them because it is protocol-conformant; we just publish the recipes.

### 9.1 Neovim

`docs/editors/neovim.md`. Primary mechanism is `nvim-lspconfig`:

```lua
-- ~/.config/nvim/init.lua (or your lsp setup file)
local lspconfig = require('lspconfig')
local configs = require('lspconfig.configs')

if not configs.mdpp then
  configs.mdpp = {
    default_config = {
      cmd = { 'mdpp-lsp' },
      filetypes = { 'markdown' },
      root_dir = lspconfig.util.find_git_ancestor,
      settings = {},
    },
  }
end

lspconfig.mdpp.setup {
  on_attach = function(client, bufnr)
    -- standard keymaps: K for hover, gd for definition, etc.
  end,
}
```

Alternative, one-liner for non-lspconfig setups, via `vim.lsp.start`:

```lua
vim.api.nvim_create_autocmd('FileType', {
  pattern = 'markdown',
  callback = function()
    vim.lsp.start({
      name = 'mdpp',
      cmd = { 'mdpp-lsp' },
      root_dir = vim.fs.dirname(vim.fs.find({'.git'}, { upward = true })[1]),
    })
  end,
})
```

Verification: open a `.md` file with an undefined footnote (`See [^nope].`); a diagnostic appears in the signs column.

### 9.2 Helix

`docs/editors/helix.md`. Edit `~/.config/helix/languages.toml`:

```toml
[[language]]
name = "markdown"
language-servers = ["mdpp-lsp"]

[language-server.mdpp-lsp]
command = "mdpp-lsp"
```

Verification: same footnote test; Helix shows diagnostics inline.

### 9.3 Zed

`docs/editors/zed.md`. Zed uses extension manifests (`extension.toml`). A minimal Zed extension pointing at our LSP:

```toml
id = "mdpp"
name = "Markdown++"
description = "Markdown++ LSP integration"
version = "0.1.0"
authors = ["m31labs"]
repository = "https://github.com/odvcencio/mdpp"

[language_servers.mdpp-lsp]
name = "mdpp-lsp"
languages = ["Markdown"]
binary.path_lookup = true
```

Users place this in `~/.config/zed/extensions/mdpp/` or install via a future Zed extension listing. Zed's extension ecosystem is still maturing; community-validated rather than officially supported at v0.1 per the roadmap.

### 9.4 Emacs

`docs/editors/emacs.md` covers both popular LSP clients.

`lsp-mode`:

```elisp
(with-eval-after-load 'lsp-mode
  (add-to-list 'lsp-language-id-configuration '(markdown-mode . "markdown"))
  (lsp-register-client
    (make-lsp-client
      :new-connection (lsp-stdio-connection "mdpp-lsp")
      :activation-fn (lsp-activate-on "markdown")
      :server-id 'mdpp-lsp)))
```

`eglot`:

```elisp
(with-eval-after-load 'eglot
  (add-to-list 'eglot-server-programs
               '(markdown-mode . ("mdpp-lsp"))))
```

Verification: open a `.md` file with a broken link anchor; `M-x flymake-show-buffer-diagnostics` lists it.

### 9.5 Shared sections across recipes

Each recipe ends with identical "Install the binary" and "Troubleshooting" sections:

- Install: `go install github.com/odvcencio/mdpp/cmd/mdpp-lsp@latest`, or download from Releases.
- Troubleshooting: "LSP does not attach" → check `:LspLog` / `helix --health` / `eglot-events-buffer`; "Diagnostics missing" → check the LSP launched (process should be visible); "Hover empty" → ensure the file is recognized as markdown.

---

## 10. Update flow

### 10.1 Extension updates

Standard Marketplace mechanism: VS Code auto-updates installed extensions by default. Users who opt out get a badge in the Extensions view and can update manually. Nothing custom here.

### 10.2 LSP binary updates

Tied to extension version via `manifest.json` (§4). On extension upgrade, `activate()` notices a `binaryVersion` mismatch and triggers a re-download. The old binary is overwritten atomically. No user interaction required.

Exception: if the user has `languageServer.path` set to an absolute location (i.e., developer mode or manual install), the extension does NOT touch that file. It assumes the user owns update cadence.

### 10.3 Version compatibility matrix

The LSP protocol is forward/backward compatible across minor versions by design. In practice:

- **Newer extension, older binary (user has `languageServer.path` pinned):** should still work; extension only uses capabilities announced by the server during `initialize`.
- **Older extension, newer binary (e.g., user ran `go install ...@latest` manually):** should still work; server degrades gracefully for clients that do not support newer capabilities.

The compatibility surface is tested by the integration suite (§11.2) against a matrix of `{extension, binary}` version pairs for the most recent two minor releases.

---

## 11. Testing strategy

Three layers, in ascending fidelity.

### 11.1 Unit tests (TypeScript)

`test/suite/extension.test.ts` using `@vscode/test-electron`. Covers:

- Activation fires on opening a `.md` file.
- All commands register (smoke check via `vscode.commands.getCommands`).
- Configuration schema parses without errors.
- `config.ts` produces correct LSP `initializationOptions` for various setting combinations.
- TextMate grammar loads and produces tokens for a basic fixture.

These run in CI on every PR. Fast (< 30s).

### 11.2 Integration tests (TypeScript + real LSP binary)

`test/suite/lsp.test.ts`. Spawns a real `mdpp-lsp` binary (built from the main repo checked out as a submodule, or downloaded as a test fixture), attaches the extension's LSP client, and verifies:

- Hover on a footnote reference returns the definition body.
- Diagnostics appear for documents with known lint violations.
- Formatter changes a misformatted document.
- Rename updates all references.
- The custom `markdownpp/renderPreview` request returns HTML.

Run in CI on every PR with the current `main` branch binary, and in a matrix test weekly against the last two released binaries.

### 11.3 Live-preview tests

`test/suite/preview.test.ts`. Opens a preview, triggers a document change, asserts the webview receives an `update` message within 200ms (slightly relaxed from the 100ms product target to avoid CI flake).

### 11.4 Manual test checklist

Pre-Marketplace-submission manual pass (single-page checklist in `docs/release-checklist.md` on the `mdpp-vscode` repo):

- Fresh install from a packaged `.vsix` on Mac, Linux, Windows.
- First-activation binary download completes and writes to the expected storage path.
- Each of the five commands runs end-to-end with visible effect.
- Each configuration key, flipped from default, produces the documented behavior.
- Live preview: open, scroll, edit, click-to-jump, close, reopen.
- Error paths: disconnect network, activate extension — download fails with actionable message.
- Uninstall leaves no stranded processes.

---

## 12. Open questions

Real unknowns that do not block drafting this spec but will be decided during implementation:

1. **Single-document vs multi-document LSP session.** When the user opens many `.md` files, do we want one LSP process with many open documents, or per-workspace server instances? VS Code's client defaults to one-per-workspace; we probably inherit that without customization, but worth validating at ≥50 open documents.
2. **Preview-on-type vs preview-on-save.** `preview.scrollSync.enabled` covers sync behavior but not render frequency. Current plan is debounced-100ms on every edit. If that proves expensive on very large documents, we may introduce `preview.updateMode: live | onSave` as a setting.
3. **Which language ID is authoritative for a `.md` file?** When `takeOverMarkdownLanguage` is true, VS Code may still report `resourceLangId` as `markdown` rather than `markdown-plus-plus` depending on activation order. The `menus.editor/title` entry's `when` clause may need to OR both IDs.
4. **Marketplace review friction.** First submission of an extension that ships a binary download may attract extra scrutiny from the Marketplace reviewer. Manifest + SHA verification + clear error UX mitigate, but we may need to pre-emptively add documentation linking to the verification flow from the README.
5. **Keybinding defaults.** None proposed at v0.1. Users who want them bind manually. Risk: no friction for existing muscle memory (`Ctrl+Shift+V` for stock Markdown preview is taken). Post-v0.1 may introduce opt-in keybindings.
6. **Remote development.** The VS Code Remote-SSH / WSL / Dev Containers flow needs the binary on the remote. Our download flow as specified works there because `context.globalStorageUri` resolves to remote storage. Needs explicit manual verification before launch; flagged as a test-matrix entry.
7. **`markdownpp/renderPreview` ownership (resolved).** Server-side registration of the custom LSP method sits with D. D's sub-spec was updated to own this; F treats it as a fixed dependency. No remaining open question on ownership; flagged as a roadmap-issues note for sequencing.
8. **Scroll-sync dependency chain (upstream-blocked).** Bidirectional editor↔preview scroll sync (§5.4) requires source positions to flow as attributes (e.g., `data-source-range` or `data-line`) on rendered HTML elements. That requires three things in sequence: (1) **Engine (A)** attaches `Node.Range` (start/end byte offsets) to every AST node — not yet done; (2) **Engine renderer** passes the range through as an HTML attribute on the corresponding rendered element; (3) **LSP (D)** ensures the `markdownpp/renderPreview` response carries this attribute through unchanged (no rewriting that strips data-attributes). Flag for sequencing: this chain blocks the scroll-sync portion of §5.4 only — F can ship the rest of the preview surface without it. When the chain completes, F's webview wiring is straightforward (~30 lines added to `preview.js`).
9. **TOC anchor navigation in the webview.** TOC anchor links (`<a href="#heading-id">`) generated by the engine's `[[toc]]` directive use the engine's slug algorithm for heading IDs. Native HTML anchor navigation should scroll the heading into view inside the webview, but VS Code webviews historically intercept some navigation events. Verify during implementation: if the anchor click does not scroll, hook clicks via `preview.js` and call `element.scrollIntoView()` manually. The same heading-ID slug algorithm is used by the LSP for `textDocument/definition` on heading links, so any divergence between the two consumers is a bug to fix in the engine, not in F.

---

## 13. Decisions made (do not relitigate in implementation)

- **Separate repo for the extension** (`mdpp-vscode`).
- **Native webview for live preview**, rendering engine-produced HTML; not extending VS Code's built-in Markdown preview.
- **Binary download from GitHub Releases** on first activation, with bundled SHA256 manifest. Not bundled in the `.vsix`. Not per-platform `.vsix` variants.
- **Mermaid rendered in the webview**, locally bundled, not from a CDN.
- **Math rendered server-side** in the engine; no client-side math library in the webview.
- **Both `markdown` and `markdown-plus-plus` language IDs are claimed by default**, with opt-out via `takeOverMarkdownLanguage`.
- **Marketplace publisher is `m31labs`.** All URLs point to `markdownpp.m31labs.dev` and `github.com/odvcencio/mdpp(-vscode)`.
- **Scroll sync is bidirectional** and enabled by default.
- **Format-on-save is opt-in**, disabled by default (standard VS Code formatter posture).
- **Commands shell out to the `mdpp` CLI** for render/export; they do not reimplement rendering logic in TypeScript.

---

## 14. References

- VS Code Extension API: https://code.visualstudio.com/api
- `vscode-languageclient`: https://github.com/microsoft/vscode-languageserver-node
- Language Server Protocol 3.17: https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/
- VS Code Webview API: https://code.visualstudio.com/api/extension-guides/webview
- VS Code Marketplace publishing: https://code.visualstudio.com/api/working-with-extensions/publishing-extension
- `@vscode/vsce`: https://github.com/microsoft/vscode-vsce
- nvim-lspconfig: https://github.com/neovim/nvim-lspconfig
- Helix language configuration: https://docs.helix-editor.com/languages.html
- Zed extensions: https://zed.dev/docs/extensions
- `lsp-mode` (Emacs): https://emacs-lsp.github.io/lsp-mode/
- `eglot` (Emacs): https://github.com/joaotavora/eglot

---

## 15. Next actions

1. Owner approves this sub-spec.
2. Create `github.com/odvcencio/mdpp-vscode` repo with the directory layout from §2.2.
3. Land a minimal skeleton: `package.json`, `extension.ts` with an activation log, empty `lspClient.ts`.
4. Wire up `binaryDownload.ts` against a real `mdpp-lsp` release (F depends on D reaching "functional, not polished" per roadmap §6).
5. Land the live-preview webview against a hardcoded HTML fixture, then switch to the real `markdownpp/renderPreview` custom request once D exposes it.
6. Complete the editor recipes in `docs/editors/{neovim,helix,zed,emacs}.md` in the main repo in parallel — these are documentation-only and do not block the VS Code work.
7. Record launch GIFs and write the Marketplace README last; they depend on the whole stack working end-to-end.

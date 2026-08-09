---
mdpp: "0.1"
id: spore.2026-07-11.formatter-idempotence
type: spore
space: hypha://m31labs/mdpp
scope: project
status: unreviewed
created: 2026-07-11T07:50:52Z

agent:
  id: agent://codex/mdpp/formatter
  kind: persistent
  model: gpt-5
  task_id: mdpp-formatter-idempotence

confidence: high

format_contract:
  preserves: "frontmatter, prose, list continuations, and terminal newlines"
  source_filter: "successful stdout formatting always emits one complete document"
  check_exit: "zero when canonical and one when canonicalization is required"
  error_exit: "two for invalid usage, input failures, or output failures"

source_refs:
  - hypha://m31labs/mdpp/initiative.formatter
  - hypha://m31labs/mdpp/object/trace.formatter
  - hypha://m31labs/mdpp/spec.authoring.v1

proposed_edges:
  - kind: supports
    src: spore.2026-07-11.formatter-idempotence
    dst: hypha://m31labs/mdpp/initiative.formatter
    confidence: 0.96

proposed_writes:
  - kind: append_section
    target: hypha://m31labs/mdpp/initiative.formatter
    heading: "Formatter stability and CLI source filters"
---

# Formatter idempotence

## Summary

Keep grammar-aware formatting as the primary lane for Markdown++ documents. Preserve frontmatter while joining safe wrapped paragraphs into one line. Repeated formatting must retain each continuation exactly once in the result. Normal stdout mode must emit a complete document on every successful pass.

## Verified current-state evidence

- `Format` receives a parsed syntax tree and the original source lines.
  The formatter must reconcile both representations without dropping text.
- Parent paragraph ranges can report different line-end conventions while
  child text and soft-break nodes still describe the physical paragraph.
- `fmt --check` is a CI predicate and should not rewrite the input file.
  A clean check returns zero and an unformatted document returns one.
- `fmt --write` updates files in place while retaining their permission bits.
  A second write pass must be a byte-for-byte no-op.
- `lint` reports document findings independently from formatting status.
  A document can be lint-clean while still requiring canonical formatting.
- CLI usage failures return a distinct operational error status.
  Scripts can distinguish tool errors from formatting or lint findings.

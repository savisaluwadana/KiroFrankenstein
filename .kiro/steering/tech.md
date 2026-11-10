---
inclusion: always
---

# Technical steering (proposed)

This repository currently contains **planning documents only**, not a working app. Treat choices below as proposals until the MVP spec is approved.

## Proposed stack

- TypeScript in strict mode for API, domain logic, and UI.
- Next.js App Router for a single deployable app with accessible React UI and server endpoints.
- Zod at all external data boundaries.
- A small relational persistence layer for imported evidence and investigations. Prefer SQLite in local development; choose a deployment-compatible store only when deployment requirements are confirmed.
- Vitest for pure domain logic; Playwright for at least one end-to-end demo path.
- Optional LLM integration only after deterministic evidence paths work; no paid API key should be required to try the demo.

## Engineering rules

- Keep source-specific parsers behind adapters; domain logic must not import GitHub/Kubernetes/OpenTelemetry SDK types.
- Normalize timestamps to UTC ISO-8601 and preserve original timestamps for auditing.
- Preserve evidence IDs and provenance. Never fabricate source links.
- Correlation results are hypotheses with a transparent rule and evidence IDs.
- Validate inputs, limit file sizes and record counts, and reject malformed imports safely.
- Never log secrets, access tokens, or raw sensitive payload fields.
- Never put credentials in source, demo fixtures, or client-side bundles.
- Prefer small, composable modules. Add tests for parsing, normalization, matching, and negative cases.
- Use Kiro specs as the implementation source of truth; update specs if behavior changes.
- Do not add `.kiro/` to `.gitignore`.

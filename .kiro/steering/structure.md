---
inclusion: always
---

# Repository structure (proposed)

The app directories described below **do not exist yet** and should only be created by the implementation tasks.

```text
.kiro/
  steering/                         # Persistent project instructions
  specs/incident-stitcher-mvp/      # Requirements, design, tasks
docs/
  kiro-build-log.md                 # Evidence of actual Kiro usage
fixtures/                            # Synthetic, non-sensitive incident exports
src/
  app/                               # Next.js routes and UI
  components/                        # Accessible presentation components
  server/adapters/                   # Source-specific JSON ingestion
  server/cases/                      # Incident persistence / query services
  domain/                            # Shared event types and deterministic correlation
  lib/                               # Shared utilities
tests/
  unit/                              # Parser, normalization, correlation tests
  e2e/                               # One complete import-to-investigation test
```

## Boundaries

- `src/domain` owns canonical types and pure correlation functions. No Next.js imports.
- `src/server/adapters` translates GitHub/Kubernetes/OpenTelemetry formats into canonical events.
- `src/server/cases` orchestrates imports and persistence.
- `src/app` handles request/response and page composition; keep business logic out of route handlers and components.
- `fixtures` contains only generated/synthetic, reviewable data.

## Conventions

- kebab-case filenames except React component names where existing conventions favor PascalCase.
- Explicit schema parsing at every adapter entrypoint.
- Typed error codes for import failures; clear user-facing messages.
- Prefer named exports and avoid circular dependencies.
- Test behavior, not implementation details.
- Avoid claiming Kiro usage until a human has actually used Kiro and recorded it.

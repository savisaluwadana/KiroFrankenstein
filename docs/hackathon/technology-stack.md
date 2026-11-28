# Technology stack

Versions are from `backend/go.mod` and `web/package.json` at the time of writing. Roles and locations are from the repository.

| Technology | Version | Role | Where |
|---|---|---|---|
| Go | 1.25.0 (module floor) | Engine, API, CLI | `backend/` |
| Standard library only | n/a | The pure simulation core | `backend/internal/sim` (allowlist enforced by `internal/archtest`) |
| Chi | v5.3.0 | HTTP router | `backend/internal/httpapi` |
| PostgreSQL | 16 (compose and CI service image) | Append-only input log, snapshots, decision records | `docker-compose.yml`, `backend/migrations/` |
| pgx | v5.9.2 | PostgreSQL driver, parameterized queries | `backend/internal/store/postgres` |
| Goose | v3.24.3 | SQL migrations (7 files, up and down) | `backend/migrations/`, `cmd/parallel-api/migrate.go` |
| rapid | v1.3.0 | Property-based tests (`TestProp*`) | `backend/internal/**/**_prop_test.go` |
| OpenAPI | 3.1 | API contract, source of truth | `backend/api/openapi.yaml` |
| SSE (Server-Sent Events) | n/a | One-way tick progress; polling fallback | `httpapi/sse.go`, `web/lib/api/progress.ts` |
| OpenAI-compatible chat completions | n/a | Optional LLM manager adapter (OpenRouter is the documented provider; no model name built in) | `backend/internal/policy/llm` |
| Next.js (App Router) | 16.4.6 | Web app | `web/app`, `web/features` |
| React | 19.3.1 | UI | `web/` |
| TypeScript | 5.9.3 (strict) | Web code | `web/` |
| PixiJS | 8.23.0 | Isometric warehouse scene | `web/features/world-view/scene` |
| Tailwind CSS | 4.3.5 | Styling | `web/app/globals.css` |
| shadcn/ui-style setup | `components.json` (new-york) plus `class-variance-authority` 0.7.1, `clsx`, `tailwind-merge`, `lucide-react` | Utility styling and icons. `web/components/ui` does not exist, so stock shadcn components are not in the repo | `web/components.json` |
| Zustand | 5.1.0 | View-state stores (not simulation truth) | `web/stores` |
| React Flow (`@xyflow/react`) | 13.0.3 | Branch graph | `web/features/branches` |
| `openapi-typescript` | 7.13.0 | Generates `web/lib/api/schema.d.ts` from the OpenAPI file | `web/package.json` script `gen:api` |
| Vitest | 5.1.2 | Web unit tests | `web/**/*.test.ts(x)` |
| Playwright | 1.65.1 | End-to-end tests, demo path timing, screenshots | `web/e2e` |
| Docker | n/a | API image, local PostgreSQL | `backend/Dockerfile`, `docker-compose.yml` |
| GitHub Actions | n/a | CI: Go (amd64 and arm64), OpenAPI lint, web, Docker build, e2e | `.github/workflows/ci.yml` |
| Kiro | n/a | Steering, specs, delegated task execution (no hooks, no MCP) | `.kiro/` |

Tools run in CI or locally, as pinned in `docs/ci.md`: staticcheck v0.8.2, govulncheck v1.10.0, `@redocly/cli` 2.84.0 (OpenAPI lint), `npm audit`.

Mentioned in the steering but **not** in `web/package.json`: GSAP (the steering lists it as optional). We do not list it as used.

Not used (explicit scope exclusions in `product.md`): Kubernetes, Kafka, microservices, vector databases, WebSockets.

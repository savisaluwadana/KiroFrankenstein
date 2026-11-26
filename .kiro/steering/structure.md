---
inclusion: always
---

# Repository structure

Planned layout. Directories are created only when the tasks that need them are executed.

```
/
├── .kiro/                     # steering, specs, hooks (tracked; required for the hackathon)
├── backend/                   # Go module: github.com/savisaluwadana/KiroFrankenstein/backend
│   ├── cmd/
│   │   ├── parallel-api/      # HTTP server entry point (wiring only)
│   │   └── parallel-sim/      # headless CLI: run, replay, verify-hash
│   ├── api/openapi.yaml       # API contract, source of truth
│   ├── internal/
│   │   ├── sim/               # PURE domain: engine, clock, rng, money, ledger, model, commands, disruptions, scenario, hash
│   │   ├── policy/            # AI managers: rulebased (pure) and llm adapter (I/O)
│   │   ├── app/               # use cases: worlds, branches, advance, fork, duel, comparison
│   │   ├── store/postgres/    # pgx repositories implementing app ports
│   │   └── httpapi/           # Chi routes, handlers, SSE, problem+json
│   ├── migrations/            # Goose SQL migrations
│   └── scenarios/             # versioned scenario files (black-friday.json) and recorded demo inputs
├── web/                       # Next.js app
│   ├── app/                   # routes
│   ├── features/
│   │   ├── world-view/        # PixiJS diorama
│   │   ├── timeline/          # scrubber and event markers
│   │   ├── branches/          # React Flow branch tree
│   │   ├── decisions/         # command builder, decision window
│   │   ├── duel/              # split comparison
│   │   └── ledger/            # P&L and ledger drill-down
│   ├── lib/api/               # generated types and fetch client
│   └── stores/                # Zustand view-state stores
├── docs/                      # build log, demo script, architecture notes, calibration.md (flagship calibration report), benchmarks.md (measured performance)
├── docker-compose.yml         # PostgreSQL only
├── LICENSE                    # MIT
└── README.md
```

## Module boundaries (dependency direction points inward)

- `internal/sim` depends on nothing outside the Go standard library allowlist. It exposes `Step(state, inputs) -> (state, outcomes)` style pure functions.
- `internal/policy/rulebased` depends only on `internal/sim` types (Observation, Command).
- `internal/policy/llm` depends on `internal/sim` types and an HTTP client. It returns proposals; it never mutates state.
- `internal/app` depends on `internal/sim`, policy interfaces and repository ports (interfaces it defines).
- `internal/store/postgres` and `internal/httpapi` depend on `internal/app`. Neither is imported by `internal/sim`.
- `web/` talks to the backend only through `/api/v1`. It never computes simulation results; it may interpolate positions between two authoritative ticks for display.

## Conventions

- Go: table-driven tests next to code (`*_test.go`); property tests named `TestProp*`.
- Go errors: wrap with `%w`; domain errors are typed values in `internal/sim` and mapped to problem+json in `httpapi`.
- SQL: snake_case; every migration has an up and a down section.
- TypeScript: strict mode, no `any`; components in PascalCase files; one feature per folder.
- Commits: Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`).
- Log each real Kiro session in `docs/kiro-build-log.md`.

# Contributing

Thank you for looking at Parallel. This project is under a **feature freeze** for the Kiroween 2026 submission, so changes are limited to fixes and documentation until the freeze is lifted. The rules below apply to any change.

## Setup

Prerequisites: Go 1.25+, Node.js 22 (the version CI uses), Docker for PostgreSQL (optional). Full instructions are in the [README quick start](README.md#quick-start).

```sh
git clone https://github.com/savisaluwadana/KiroFrankenstein.git
cd KiroFrankenstein
cd backend && go build ./cmd/...
cd ../web && npm ci
```

## Tests

```sh
# Backend
cd backend
go vet ./...
go test ./... -count=1
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 ./...

# PostgreSQL tests (skipped unless these are set); see the README for the compose setup
export PARALLEL_TEST_DATABASE_URL='postgres://parallel_app:app_dev_password@127.0.0.1:55432/parallel_test?sslmode=disable'
export PARALLEL_TEST_MIGRATE_DATABASE_URL='postgres://parallel_migrator:migrator_dev_password@127.0.0.1:55432/parallel_test?sslmode=disable'

# Web
cd web
npx tsc --noEmit && npx eslint . && npx vitest --run
npm run e2e          # Playwright; needs `npx playwright install chromium` once

# Documentation links and images
scripts/check-docs.sh
```

`scripts/ci-local.sh` runs the CI steps locally (see [docs/ci.md](docs/ci.md)). Do not run it while another process uses the `parallel_test` database.

If you change `backend/api/openapi.yaml`, regenerate the TypeScript types with `cd web && npm run gen:api` and commit both files. CI fails on drift.

## Invariants you must not break

These come from [`.kiro/steering/simulation-invariants.md`](.kiro/steering/simulation-invariants.md). Each is covered by an automated test.

- **Determinism.** Same scenario, seed and inputs give the same state hash at every tick, on any process and on amd64 and arm64.
- **No floating point in `backend/internal/sim` state transitions.** Money is `int64` cents; rates are integer basis points. Rounding goes through the shared half-to-even helper.
- **`internal/sim` stays pure.** No `time`, `math/rand`, `os`, `net`, `sync` or goroutines. The architecture test (`internal/archtest`) enforces an import allowlist.
- **No map iteration where order can affect output.** Use sorted slices.
- **Random streams.** Each named stream draws a fixed number of values per tick regardless of commands, so a decision never shifts exogenous randomness.
- **Immutability.** Recorded inputs are never updated or deleted. Changing the past is a fork. Do not add UPDATE or DELETE paths to `input_log`, `decision_records` or `duels`.
- **Every command passes the deterministic validator**, for humans and AI managers alike.
- **The LLM is optional.** The engine, replay, the rule-based AI and every test must work with no network and no LLM variables.
- **Money figures shown by the API are sums of ledger entries or a documented formula over them.**
- **Results carry the synthetic-results disclaimer.** Never present output as a forecast.

### Golden hashes and the engine version

The goldens pin results: `backend/scenarios/golden/*` (flagship no-command hash, demo fork hash and ticks, demo duel hashes, the 20-seed ensemble report) and `backend/scenarios/demo-fork-inputs.json`.

- A change to event offsets, priorities, draw counts or the state encoding **requires incrementing `EngineVersion`** in `backend/internal/sim/engine.go` and regenerating goldens in the same commit.
- A change to the flagship scenario data changes the hashes without an engine version bump. Record every calibration change in [docs/calibration.md](docs/calibration.md). Calibration must never be tuned so that a human or an AI wins.
- A golden may change only deliberately. If a golden test fails and you did not intend a behaviour change, you introduced a bug. Do not regenerate to make it pass.

Regenerating (from `backend/`, only after you have decided the change is intended):

```sh
UPDATE_GOLDEN=1 go test ./internal/app/demoplan -run TestDemoForkInputsFile
UPDATE_GOLDEN=1 go test ./internal/sim -run TestGoldenFlagshipHash
UPDATE_GOLDEN=1 go test ./internal/app -run TestGoldenDuelHashes -count=1
UPDATE_GOLDEN=1 go test ./internal/app/ensemble -run TestGoldenEnsembleReport
```

Then re-run those tests without `UPDATE_GOLDEN`, update the hash quoted in `README.md` (a test checks that it appears there) and in `docs/demo-script.md`, and note the reason in the commit message. Check amd64 as well as arm64 (see "Cross-architecture" in [docs/ci.md](docs/ci.md)).

## Commits and pull requests

Use [Conventional Commits](https://www.conventionalcommits.org): `feat:`, `fix:`, `docs:`, `test:`, `chore:`, with an optional scope, for example `fix(web): ...`.

Pull request checklist:

- [ ] The change is the smallest that does the job, and matches nearby code.
- [ ] Tests added or updated. New logic with money, ordering, parsing or security has a test.
- [ ] `go vet`, `go test ./...`, staticcheck, govulncheck, `tsc`, ESLint and vitest pass.
- [ ] No invariant above is weakened; goldens are unchanged, or changed deliberately with the reason stated.
- [ ] If the API changed: `openapi.yaml` and generated types updated.
- [ ] No secrets, keys, model identifiers or `.env` content committed. Provider settings come from environment variables only.
- [ ] Documentation updated where behaviour changed, and `scripts/check-docs.sh` passes.
- [ ] Claims in docs are true. State what was verified and what was not.

## Steering and specs (working with Kiro)

The project was built with Kiro's spec workflow, and the files are part of the repository:

- `.kiro/steering/`: always-on project rules (product, tech, structure, simulation invariants, security, UI design system, hackathon constraints).
- `.kiro/specs/parallel-simulation-mvp/`: `requirements.md`, `design.md` and `tasks.md`.

Workflow: change requirements first, then design, then tasks, then code. Keep steering consistent with the code when a decision changes. Log each real Kiro session in [docs/kiro-build-log.md](docs/kiro-build-log.md): what Kiro did, what you reviewed or corrected, commands and results. Do not describe pre-Kiro planning as Kiro output.

## Security

Report vulnerabilities privately; see [SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the [MIT License](LICENSE).

# KiroFrankenstein

A new project repository for the **Kiroween 2026** hackathon, **Frankenstein** category.

## Current status

**Concept approved: Parallel.** *Reality has no undo button. We built one.* A playable, forkable simulation of an e-commerce business in a Black Friday crisis, where a human and an AI manager compete under identical conditions.

**M0 and M1 (the vertical slice) are implemented and tested.** M2 and later milestones are not started. Steering lives in `.kiro/steering/` and the spec (requirements, design, tasks) in `.kiro/specs/parallel-simulation-mvp/`.

## Run locally

Prerequisites: Go 1.23+ and Node 20+.

1. API (port 8080, in-memory store, worlds are lost on restart):
   `cd backend && WEB_ORIGIN=http://localhost:3100 go run ./cmd/parallel-api`
2. Web app (port 3100; `NEXT_PUBLIC_API_BASE_URL` defaults to `http://localhost:8080/api/v1`):
   `cd web && npm install && npm run dev`
3. Open http://localhost:3100 and click "Start Black Friday demo".

Headless run:

```
cd backend && go run ./cmd/parallel-sim run --scenario scenarios/black-friday.json --seed 2026
```

Output at the time of writing: engine version 2, tick 120, state hash `4c6d0dd08cf7c1c8c27991355f734705356aced57fe060556e0d038ad03ab99e`, net profit -4342800 cents, service level 2214 bps. (Engine version 1 produced hash `96e7d8d94bc1b3d0e81598272df984b0e0afe86c8e848bc34275781b513bce08` and service level 2206 bps; version 2 adds the per-tick severity draw and the disruption role field.)

Tests:

- `cd backend && go test ./...`
- `cd web && npx tsc --noEmit && npx eslint . && npx vitest --run`
- `cd web && npm run e2e` runs the real-browser suite (Playwright, chromium, desktop 1440x900 and phone 390x844) against the real Go API and a production build of the web app. It starts both servers itself on ports 18080 and 3199 and writes screenshots to `docs/screenshots/`. One-time setup: `npx playwright install chromium`.

PostgreSQL via Docker Compose is optional and unused until M3.

### Known limitations

- No authentication; worlds are protected only by unguessable IDs.
- Assumptions are synthetic and uncalibrated.
- Results are not predictions or guarantees.
- No LLM manager yet.
- The studio is browser-tested with Playwright in headless Chromium only (see `docs/ux-redesign.md`, Verification results); other browsers and real touch devices are untested.

## First steps

1. Review the official [Kiroween 2026 rules](https://kiroween.devpost.com/rules).
2. Research, compare, and select an original, feasible product concept.
3. **Get explicit approval of the chosen concept before generating code, product-specific steering, architecture, or specs.**
4. Clone this repository and open it in Kiro IDE or Kiro CLI.
5. After approval, use **Kiro** to create and refine steering, specs, design, tasks, and implementation. Record genuine usage in [the Kiro build log](docs/kiro-build-log.md).
6. Run tests, deploy the functional demo, record a video under three minutes, and submit on Devpost.

## Repo setup

- Public GitHub repository
- [MIT License](LICENSE)
- [Hackathon-specific Kiro steering](.kiro/steering/hackathon.md)
- [Kiro build log](docs/kiro-build-log.md)

This is **planning scaffolding** created before the first Kiro build session. It is not evidence that the application has already been developed with Kiro.

## Hackathon checklist

- [x] Public GitHub repository
- [x] Open-source LICENSE
- [x] Root `.kiro/` directory
- [x] Select and approve a product concept
- [ ] Develop working software using Kiro during the eligible submission period
- [ ] Demonstrate and document actual Kiro workflows
- [ ] Deploy and test a public application
- [ ] Publish a public demo video and complete Devpost submission

## License

MIT, Copyright (c) 2026 Savi Saluwadana.
